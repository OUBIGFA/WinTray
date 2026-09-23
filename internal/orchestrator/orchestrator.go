package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wintray/internal/config"
	"wintray/internal/stringutil"
)

// startOptions describes how a managed entry should be brought up.
// hideProcessWindow creates the process without a console window;
// hideConsoleWindow creates a console program with its console hidden so the
// window can later be shown from a tray icon; manageWindow closes/hides the
// top-level window it opens afterwards.
// windowOptional treats window handling as best effort: a program that never
// opens a window (background scripts) still counts as a successful launch.
type startOptions struct {
	hideProcessWindow bool
	hideConsoleWindow bool
	manageWindow      bool
	windowOptional    bool
}

func (o startOptions) launchMode() launchMode {
	switch {
	case o.hideProcessWindow:
		return launchNoWindow
	case o.hideConsoleWindow:
		return launchHiddenConsole
	default:
		return launchVisible
	}
}

func (s *Service) StartAndManage(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	return s.start(ctx, entry, retrySeconds, startOptions{
		hideProcessWindow: entry.LaunchHiddenInBackground,
		manageWindow:      !entry.LaunchHiddenInBackground && entry.TrayBehavior.AutoMinimizeAndHideOnLaunch,
	})
}

// StartNow launches the entry on demand using the very same behavior configured
// on it (normal, hidden in background, or close the window after launch). Window
// handling is best effort here: the launch itself already succeeded, so it must
// not be reported as a failure.
func (s *Service) StartNow(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	return s.start(ctx, entry, retrySeconds, startOptions{
		hideProcessWindow: entry.LaunchHiddenInBackground,
		manageWindow:      !entry.LaunchHiddenInBackground && entry.TrayBehavior.AutoMinimizeAndHideOnLaunch,
		windowOptional:    true,
	})
}

func (s *Service) start(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int, opts startOptions) Result {
	if entry.ExePath == "" {
		return Result{AppName: entry.Name, Managed: false, Code: ResultEmptyExePath, Message: "empty exe path"}
	}
	if _, err := os.Stat(entry.ExePath); err != nil {
		s.logger.Warn(fmt.Sprintf("skip invalid exe path: %s", entry.ExePath))
		return Result{AppName: entry.Name, Managed: false, Code: ResultInvalidExePath, Message: "invalid exe path"}
	}

	expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
	expectedPath := normalizePath(entry.ExePath)
	// A console-subsystem program (syncthing.exe, frpc.exe, ...) owns no GUI
	// window: its only window is the console host, and closing that terminates
	// the process. "Close window after launch" therefore means "keep the
	// window hidden and reachable from a tray icon" for these programs.
	consoleProgram := opts.manageWindow && consoleExecutableCheck(entry.ExePath)

	if s.hasExistingManagedProcess(expectedPath, expectedName) || s.hasExistingManagedWindow(expectedPath, expectedName) {
		s.logger.Info(fmt.Sprintf("skip start: already running %s", entry.Name))
		if consoleProgram {
			if hidden, ok := s.adoptRunningConsole(ctx, entry, expectedPath, expectedName); ok {
				return hiddenToTrayResult(entry, hidden)
			}
		}
		if opts.manageWindow {
			// The program may have started itself (its own autorun entry) moments
			// ago, so its close delay is honoured here as well.
			if !s.waitQuietPeriod(ctx, entry, nil, expectedPath, expectedName) {
				return Result{AppName: entry.Name, Managed: true, Code: ResultAlreadyRunningSkipped, Message: "already running skipped"}
			}
			managed, ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
				return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
			}, expectedPath, expectedName, nil, nil, retrySeconds, "close", closeDelay(entry))
			if ok {
				return managedResult(entry, managed, ResultAlreadyRunningManaged, "already running managed existing")
			}
		}
		return Result{AppName: entry.Name, Managed: true, Code: ResultAlreadyRunningSkipped, Message: "already running skipped"}
	}

	if consoleProgram {
		s.logger.Info(fmt.Sprintf("console executable detected, launching with hidden console for tray hosting: %s", entry.Name))
		opts.hideConsoleWindow = true
		opts.manageWindow = false
	}

	baseline := s.captureBaseline(func(w ManagedWindowInfo) bool {
		return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
	})

	cmd, err := startProcess(entry.ExePath, entry.Args, opts.launchMode())
	if err != nil {
		s.logger.Error(fmt.Sprintf("start failed: %s err=%v", entry.Name, err))
		return Result{AppName: entry.Name, Managed: false, Code: ResultProcessStartFailed, Message: "process start failed"}
	}
	pid := uint32(cmd.Process.Pid)
	defer cmd.Process.Release()
	s.logger.Info(fmt.Sprintf("started: %s pid=%d mode=%d", entry.Name, pid, opts.launchMode()))

	if opts.hideConsoleWindow {
		return s.hostLaunchedConsole(ctx, entry, pid)
	}

	if !opts.manageWindow {
		return startedResult(entry, opts)
	}

	if !s.waitQuietPeriod(ctx, entry, &pid, expectedPath, expectedName) {
		return windowNotManagedResult(entry, opts)
	}

	managed, ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return w.ProcessID == pid || matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
	}, expectedPath, expectedName, &pid, baseline, retrySeconds, "close", closeDelay(entry))
	if !ok {
		return windowNotManagedResult(entry, opts)
	}
	return managedResult(entry, managed, ResultManaged, "managed")
}

// closeDelay is how long the program must have been running before its
// window is looked up and acted on.
func closeDelay(entry config.ManagedAppEntry) time.Duration {
	return time.Duration(config.ClampCloseDelaySeconds(entry.TrayBehavior.CloseDelaySeconds)) * time.Second
}

// quietPeriodRemaining reports how much of the entry's close delay is still
// ahead. Some programs (the NT-based QQ) show a login window first and quit
// when it is closed, so the delay counts from the creation of the program's
// process whoever started it: WinTray (launchedPID) or the program's own
// autorun entry, in which case the oldest running process of the program is
// the anchor (helpers spawned from the same image are younger). A program
// that has been running longer than the delay is handled right away.
func quietPeriodRemaining(entry config.ManagedAppEntry, launchedPID *uint32, expectedPath, expectedName string, now time.Time) time.Duration {
	delay := closeDelay(entry)
	if delay <= 0 {
		return 0
	}
	var started time.Time
	var known bool
	if launchedPID != nil && *launchedPID != 0 {
		started, known = processStartLookup(*launchedPID)
		if !known {
			// A launch that cannot be inspected any more is treated as brand new.
			return delay
		}
	} else {
		started, known = runningProcessStartLookup(expectedPath, expectedName)
		if !known {
			return 0
		}
	}
	if remaining := delay - now.Sub(started); remaining > 0 {
		return remaining
	}
	return 0
}

// waitQuietPeriod holds window handling until the program has been running
// for its close delay. It reports false when the context ends first.
func (s *Service) waitQuietPeriod(ctx context.Context, entry config.ManagedAppEntry, launchedPID *uint32, expectedPath, expectedName string) bool {
	remaining := quietPeriodRemaining(entry, launchedPID, expectedPath, expectedName, time.Now())
	if remaining <= 0 {
		return true
	}
	s.logger.Info(fmt.Sprintf("window handling delayed: %s wait=%s (close delay %s from process start)", entry.Name, remaining.Round(time.Millisecond), closeDelay(entry)))
	if !waitWithContext(ctx, remaining) {
		s.logger.Info(fmt.Sprintf("window handling cancelled during delay: %s", entry.Name))
		return false
	}
	return true
}

// windowNotManagedResult reports a launch whose window was not handled. When
// window handling is best effort the launch itself still counts as a success.
func windowNotManagedResult(entry config.ManagedAppEntry, opts startOptions) Result {
	if opts.windowOptional {
		return startedResult(entry, opts)
	}
	return Result{AppName: entry.Name, Managed: false, Code: ResultNoWindowManaged, Message: "no window managed"}
}

// hostLaunchedConsole locates the hidden console window of a console program
// WinTray just started so the caller can host it behind a tray icon. The
// window is created hidden, so nothing needs to be closed; when the lookup
// fails the program still runs and is hosted by process id only.
func (s *Service) hostLaunchedConsole(ctx context.Context, entry config.ManagedAppEntry, pid uint32) Result {
	// Once a hidden process exists, finish the bounded lookup even during
	// shutdown so its caller can restore the window instead of orphaning it.
	hwnd := consoleWindowLookup(context.WithoutCancel(ctx), pid, consoleWindowWait)
	if hwnd == 0 {
		s.logger.Warn(fmt.Sprintf("console window not found after launch: %s pid=%d (hosted by process only)", entry.Name, pid))
	} else {
		s.logger.Info(fmt.Sprintf("console window hidden for tray hosting: %s pid=%d hwnd=0x%X", entry.Name, pid, hwnd))
	}
	return hiddenToTrayResult(entry, HiddenWindow{Handle: hwnd, ProcessID: pid})
}

// adoptRunningConsole handles a console program that is already running: it
// finds the program's console window (visible or already hidden, e.g. after a
// WinTray restart), hides it when needed and reports it for tray hosting.
// ok is false when no matching process is running.
func (s *Service) adoptRunningConsole(ctx context.Context, entry config.ManagedAppEntry, expectedPath, expectedName string) (HiddenWindow, bool) {
	pid := runningProcessLookup(expectedPath, expectedName)
	if pid == 0 {
		return HiddenWindow{}, false
	}
	hwnd := consoleWindowLookup(ctx, pid, 0)
	if hwnd == 0 {
		s.logger.Warn(fmt.Sprintf("running console program has no console window: %s pid=%d (hosted by process only)", entry.Name, pid))
		return HiddenWindow{ProcessID: pid}, true
	}
	if windowVisibleCheck(hwnd) {
		if ok, err := s.manager.HideWindow(hwnd); !ok {
			s.logger.Warn(fmt.Sprintf("hide running console failed: %s pid=%d hwnd=0x%X err=%v", entry.Name, pid, hwnd, err))
			return HiddenWindow{}, false
		}
		s.logger.Info(fmt.Sprintf("running console window hidden for tray hosting: %s pid=%d hwnd=0x%X", entry.Name, pid, hwnd))
	} else {
		s.logger.Info(fmt.Sprintf("adopted already hidden console window: %s pid=%d hwnd=0x%X", entry.Name, pid, hwnd))
	}
	return HiddenWindow{Handle: hwnd, ProcessID: pid}, true
}

func hiddenToTrayResult(entry config.ManagedAppEntry, hidden HiddenWindow) Result {
	h := hidden
	return Result{AppName: entry.Name, Managed: true, Action: "hide", Code: ResultHiddenToTray, Message: "hidden to tray", Hidden: &h}
}

// managedResult reports a successful window action. When WinTray hid the
// window itself the program has no tray icon to get it back, so the result
// carries the hidden window for tray hosting.
func managedResult(entry config.ManagedAppEntry, managed managedWindow, code ResultCode, message string) Result {
	if managed.Hidden {
		return hiddenToTrayResult(entry, HiddenWindow{Handle: managed.Handle, ProcessID: managed.ProcessID})
	}
	return Result{AppName: entry.Name, Managed: true, Action: "close", Code: code, Message: message}
}

func startedResult(entry config.ManagedAppEntry, opts startOptions) Result {
	if opts.hideProcessWindow {
		return Result{AppName: entry.Name, Managed: true, Code: ResultStartedHidden, Message: "started hidden"}
	}
	return Result{AppName: entry.Name, Managed: true, Code: ResultStartedOnly, Message: "started only"}
}

func (s *Service) hasExistingManagedProcess(expectedPath, expectedName string) bool {
	return hasRunningProcessByIdentity(expectedPath, expectedName)
}

// hasExistingManagedWindow decides whether the program is already up, so it
// only trusts strong evidence: the window's owning process must match by
// executable path or process name. The loose title/class identity fallback is
// deliberately not used here — an unrelated window whose title merely contains
// the program name (common for short names) would otherwise suppress the launch.
func (s *Service) hasExistingManagedWindow(expectedPath, expectedName string) bool {
	for _, w := range s.enumerator.EnumerateTopLevelWindows() {
		if isUnmanageableWindow(w) {
			continue
		}
		if !matchesExecutable(w, expectedPath, expectedName) {
			continue
		}
		return true
	}
	return false
}

func (s *Service) HideExisting(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
	if expectedName == "" {
		return Result{AppName: entry.Name, Managed: false, Code: ResultInvalidProcessName, Message: "invalid process name"}
	}
	expectedPath := normalizePath(entry.ExePath)
	if consoleExecutableCheck(entry.ExePath) {
		if hidden, ok := s.adoptRunningConsole(ctx, entry, expectedPath, expectedName); ok {
			return hiddenToTrayResult(entry, hidden)
		}
		if !s.hasExistingManagedWindow(expectedPath, expectedName) {
			// Not running: let the caller launch it instead of waiting for a
			// window that will never appear.
			return Result{AppName: entry.Name, Managed: false, Code: ResultNoExistingWindowManaged, Message: "no existing window managed"}
		}
	}
	if !s.waitQuietPeriod(ctx, entry, nil, expectedPath, expectedName) {
		return Result{AppName: entry.Name, Managed: false, Code: ResultNoExistingWindowManaged, Message: "no existing window managed"}
	}
	managed, ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
	}, expectedPath, expectedName, nil, nil, retrySeconds, "close", closeDelay(entry))
	if !ok {
		return Result{AppName: entry.Name, Managed: false, Code: ResultNoExistingWindowManaged, Message: "no existing window managed"}
	}
	return managedResult(entry, managed, ResultManagedExisting, "managed existing")
}

// manageFirstMatchingWindow acts on the best matching window. Windows of a
// process that has been running for less than minProcessAge are skipped:
// they are inside the program's close delay.
func (s *Service) manageFirstMatchingWindow(ctx context.Context, predicate func(ManagedWindowInfo) bool, expectedPath, expectedName string, launchedPID *uint32, baseline map[uintptr]struct{}, retrySeconds int, actionType string, minProcessAge time.Duration) (managedWindow, bool) {
	const delay = 500 * time.Millisecond
	var last managedWindow
	managedAny := false
	processStarts := map[uint32]time.Time{}
	insideDelayLogged := map[uint32]struct{}{}
	singleRound := retrySeconds <= 0
	timeout := 2 * time.Second
	if actionType == "close" {
		timeout = 4 * time.Second
	}
	if retrySeconds > 0 {
		timeout = time.Duration(retrySeconds) * time.Second
	}
	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for round := 1; ; round++ {
		select {
		case <-actionCtx.Done():
			return last, false
		default:
		}

		windows := s.enumerator.EnumerateTopLevelWindows()
		bestByRoot := map[uintptr]MatchCandidate{}
		now := time.Now()
		for _, w := range windows {
			if !predicate(w) || !hasTrustedWindowIdentity(w, expectedPath, launchedPID) {
				continue
			}
			if isUnmanageableWindow(w) {
				continue
			}
			if actionType == "close" && w.IsToolWindow {
				continue
			}
			// A window of a process still inside its close delay is left alone: it
			// is likely a login or splash window whose close would end the program
			// (a program that started itself while this loop was already running).
			if minProcessAge > 0 {
				if age, known := processAge(w.ProcessID, processStarts, now); known && age < minProcessAge {
					if _, logged := insideDelayLogged[w.ProcessID]; !logged {
						insideDelayLogged[w.ProcessID] = struct{}{}
						s.logger.Info(fmt.Sprintf("skip window inside close delay: age=%s delay=%s %s", age.Round(time.Millisecond), minProcessAge, describeWindow(w)))
					}
					continue
				}
			}
			score := computeCandidateScore(w, expectedPath, expectedName, launchedPID, baseline)
			root := resolveActionTargetHandle(w)
			if prev, ok := bestByRoot[root]; !ok || score > prev.Score {
				bestByRoot[root] = MatchCandidate{Window: w, Score: score}
			}
		}
		candidates := make([]MatchCandidate, 0, len(bestByRoot))
		for _, c := range bestByRoot {
			candidates = append(candidates, c)
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
		if len(candidates) > 0 {
			s.logger.Info(fmt.Sprintf("match round %d candidates=%d top=%s", round, len(candidates), summarizeCandidates(candidates, 3)))
		}

		managedThisRound := false
		for _, c := range candidates {
			if managed, ok := s.tryManageAndVerify(actionCtx, c.Window, c.Score, actionType); ok {
				last = managed
				if actionType != "hide" {
					return last, true
				}
				managedAny = true
				managedThisRound = true
				continue
			}
		}

		if actionType == "hide" {
			if managedThisRound {
				if singleRound {
					return last, true
				}
				if !waitWithContext(actionCtx, 150*time.Millisecond) {
					return last, managedAny
				}
				continue
			}
			if managedAny && len(candidates) == 0 {
				return last, true
			}
		}

		if singleRound {
			return last, managedAny
		}
		if !waitWithContext(actionCtx, delay) {
			return last, false
		}
	}
}

func (s *Service) tryManageAndVerify(ctx context.Context, window ManagedWindowInfo, score int, actionType string) (managedWindow, bool) {
	if score < closeAllowedScoreThreshold {
		s.logger.Warn(fmt.Sprintf("skip low confidence candidate score=%d threshold=%d %s", score, closeAllowedScoreThreshold, describeWindow(window)))
		return managedWindow{}, false
	}
	target := resolveActionTargetHandle(window)
	closed := managedWindow{Handle: target, ProcessID: window.ProcessID}
	hidden := managedWindow{Handle: target, ProcessID: window.ProcessID, Hidden: true}

	// Console host windows (conhost / Windows Terminal) are never closed:
	// closing them terminates the hosted program, while the user asked for the
	// program to keep running out of sight. Hide the host window instead.
	if isConsoleHostWindow(window) {
		return hidden, s.applyAndVerify(ctx, window, score, "hide", s.manager.HideWindow)
	}

	// "hide" uses WM_CLOSE first and falls back to SW_HIDE for callers that
	// explicitly request a hide action. "close" sends WM_CLOSE only.
	// A close succeeds when the window is destroyed or becomes invisible, which
	// covers applications that intercept close and move themselves to the tray.
	if actionType == "hide" {
		// Prefer app-native close-to-tray behavior first. Many apps (Tauri/Electron)
		// intercept close and move to tray, preserving tray-click restore semantics.
		if s.applyAndVerify(ctx, window, score, "hide", s.manager.CloseWindow) {
			return closed, true
		}
		return hidden, s.applyAndVerify(ctx, window, score, "hide", s.manager.HideWindow)
	}
	if s.applyAndVerify(ctx, window, score, "close", s.manager.CloseWindow) {
		return closed, true
	}
	return managedWindow{}, false
}

func (s *Service) applyAndVerify(ctx context.Context, window ManagedWindowInfo, score int, action string, fn func(uintptr) (bool, error)) bool {
	targetHwnd := resolveActionTargetHandle(window)
	if targetHwnd != window.Handle {
		s.logger.Info(fmt.Sprintf("retarget action action=%s score=%d from=0x%X to=0x%X", action, score, window.Handle, targetHwnd))
	}

	ok, err := fn(targetHwnd)
	if !ok {
		if err != nil {
			s.logger.Warn(fmt.Sprintf("action request failed action=%s score=%d hwnd=0x%X %s err=%v", action, score, targetHwnd, describeWindow(window), err))
		} else {
			s.logger.Warn(fmt.Sprintf("action request failed action=%s score=%d hwnd=0x%X %s", action, score, targetHwnd, describeWindow(window)))
		}
		return false
	}

	s.logger.Info(fmt.Sprintf("action requested action=%s score=%d hwnd=0x%X %s", action, score, targetHwnd, describeWindow(window)))
	if s.verifyActionApplied(ctx, targetHwnd, score, action) {
		s.logger.Info(fmt.Sprintf("action applied action=%s score=%d hwnd=0x%X", action, score, targetHwnd))
		return true
	}

	s.logger.Warn(fmt.Sprintf("action not applied action=%s score=%d hwnd=0x%X", action, score, targetHwnd))
	return false
}

func resolveActionTargetHandle(window ManagedWindowInfo) uintptr {
	return resolveOwnerChain(window)
}

func (s *Service) captureBaseline(predicate func(ManagedWindowInfo) bool) map[uintptr]struct{} {
	m := map[uintptr]struct{}{}
	for _, w := range s.enumerator.EnumerateTopLevelWindows() {
		if predicate(w) {
			m[w.Handle] = struct{}{}
		}
	}
	return m
}

func (s *Service) verifyActionApplied(ctx context.Context, hwnd uintptr, score int, action string) bool {
	// Keep verification responsive for hide (avoids long per-candidate stalls)
	// while still allowing async framework event loops enough time.
	attempts := 10
	delay := 400 * time.Millisecond
	if action == "hide" {
		attempts = 4
		delay = 300 * time.Millisecond
	}

	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		// For "hide": the window handle stays valid (tray apps keep the HWND alive
		// but invisible). Accept IsWindowVisible==0 as success — do NOT require the
		// handle to disappear from EnumWindows.
		// For "close": both destruction and close-to-tray hiding are successful.
		if action == "hide" {
			if !isWindowVisible(hwnd) {
				return true
			}
		} else {
			if !isWindow(hwnd) || !isWindowVisible(hwnd) {
				return true
			}
		}

		if i < attempts-1 {
			if !waitWithContext(ctx, delay) {
				return false
			}
		}
	}

	if score >= closeAllowedScoreThreshold {
		s.logger.Warn(fmt.Sprintf("verify timeout action=%s score=%d hwnd=0x%X", action, score, hwnd))
	}
	return false
}

func summarizeCandidates(candidates []MatchCandidate, top int) string {
	if len(candidates) == 0 {
		return "none"
	}
	if top <= 0 {
		top = 1
	}
	if len(candidates) < top {
		top = len(candidates)
	}
	parts := make([]string, 0, top)
	for i := 0; i < top; i++ {
		c := candidates[i]
		parts = append(parts, fmt.Sprintf("score=%d %s", c.Score, describeWindow(c.Window)))
	}
	return strings.Join(parts, "; ")
}

func describeWindow(window ManagedWindowInfo) string {
	title := window.Title
	if title == "" {
		title = "<empty>"
	}
	className := window.ClassName
	if className == "" {
		className = "<empty>"
	}
	process := window.ProcessName
	if process == "" {
		process = "<empty>"
	}
	return fmt.Sprintf("hwnd=0x%X pid=%d process=%s title=%q class=%q min=%t fg=%t owner=0x%X tool=%t", window.Handle, window.ProcessID, process, title, className, window.IsMinimized, window.IsForeground, window.OwnerHandle, window.IsToolWindow)
}

// processAge reports how long the process has been running. Unknown creation
// times report false so they never block an action; cache keeps one lookup
// per process for a whole matching loop.
func processAge(pid uint32, cache map[uint32]time.Time, now time.Time) (time.Duration, bool) {
	started, ok := cache[pid]
	if !ok {
		var known bool
		if started, known = processStartLookup(pid); !known {
			return 0, false
		}
		cache[pid] = started
	}
	return now.Sub(started), true
}

func waitWithContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
