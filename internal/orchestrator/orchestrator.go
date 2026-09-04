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
// manageWindow closes/hides the top-level window it opens afterwards.
// windowOptional treats window handling as best effort: a program that never
// opens a window (background scripts) still counts as a successful launch.
type startOptions struct {
	hideProcessWindow bool
	manageWindow      bool
	windowOptional    bool
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
	if s.hasExistingManagedProcess(expectedPath, expectedName) || s.hasExistingManagedWindow(expectedPath, expectedName) {
		s.logger.Info(fmt.Sprintf("skip start: already running %s", entry.Name))
		if opts.manageWindow {
			ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
				return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
			}, expectedPath, expectedName, nil, nil, retrySeconds, "close")
			if ok {
				return Result{AppName: entry.Name, Managed: true, Action: "close", Code: ResultAlreadyRunningManaged, Message: "already running managed existing"}
			}
		}
		return Result{AppName: entry.Name, Managed: true, Code: ResultAlreadyRunningSkipped, Message: "already running skipped"}
	}

	baseline := s.captureBaseline(func(w ManagedWindowInfo) bool {
		return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
	})

	cmd, err := startProcess(entry.ExePath, entry.Args, opts.hideProcessWindow)
	if err != nil {
		s.logger.Error(fmt.Sprintf("start failed: %s err=%v", entry.Name, err))
		return Result{AppName: entry.Name, Managed: false, Code: ResultProcessStartFailed, Message: "process start failed"}
	}
	pid := uint32(cmd.Process.Pid)
	s.logger.Info(fmt.Sprintf("started: %s pid=%d hidden=%t", entry.Name, pid, opts.hideProcessWindow))

	if !opts.manageWindow {
		return startedResult(entry, opts)
	}

	ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return w.ProcessID == pid || matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
	}, expectedPath, expectedName, &pid, baseline, retrySeconds, "close")
	if !ok {
		if opts.windowOptional {
			return startedResult(entry, opts)
		}
		return Result{AppName: entry.Name, Managed: false, Code: ResultNoWindowManaged, Message: "no window managed"}
	}
	return Result{AppName: entry.Name, Managed: true, Action: "close", Code: ResultManaged, Message: "managed"}
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
	ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)
	}, expectedPath, expectedName, nil, nil, retrySeconds, "close")
	if !ok {
		return Result{AppName: entry.Name, Managed: false, Code: ResultNoExistingWindowManaged, Message: "no existing window managed"}
	}
	return Result{AppName: entry.Name, Managed: true, Action: "close", Code: ResultManagedExisting, Message: "managed existing"}
}

func (s *Service) manageFirstMatchingWindow(ctx context.Context, predicate func(ManagedWindowInfo) bool, expectedPath, expectedName string, launchedPID *uint32, baseline map[uintptr]struct{}, retrySeconds int, actionType string) bool {
	const delay = 500 * time.Millisecond
	managedAny := false
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
			return false
		default:
		}

		windows := s.enumerator.EnumerateTopLevelWindows()
		bestByRoot := map[uintptr]MatchCandidate{}
		for _, w := range windows {
			if !predicate(w) {
				continue
			}
			if isUnmanageableWindow(w) {
				continue
			}
			if actionType == "close" && w.IsToolWindow {
				continue
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
			if s.tryManageAndVerify(actionCtx, c.Window, c.Score, actionType) {
				if actionType != "hide" {
					return true
				}
				managedAny = true
				managedThisRound = true
				continue
			}
		}

		if actionType == "hide" {
			if managedThisRound {
				if singleRound {
					return true
				}
				if !waitWithContext(actionCtx, 150*time.Millisecond) {
					return managedAny
				}
				continue
			}
			if managedAny && len(candidates) == 0 {
				return true
			}
		}

		if singleRound {
			return managedAny
		}
		if !waitWithContext(actionCtx, delay) {
			return false
		}
	}
}

func (s *Service) tryManageAndVerify(ctx context.Context, window ManagedWindowInfo, score int, actionType string) bool {
	if score < closeAllowedScoreThreshold {
		s.logger.Warn(fmt.Sprintf("skip low confidence candidate score=%d threshold=%d %s", score, closeAllowedScoreThreshold, describeWindow(window)))
		return false
	}

	// "hide" uses WM_CLOSE first and falls back to SW_HIDE for callers that
	// explicitly request a hide action. "close" sends WM_CLOSE only.
	// A close succeeds when the window is destroyed or becomes invisible, which
	// covers applications that intercept close and move themselves to the tray.
	if actionType == "hide" {
		// Prefer app-native close-to-tray behavior first. Many apps (Tauri/Electron)
		// intercept close and move to tray, preserving tray-click restore semantics.
		if s.applyAndVerify(ctx, window, score, "hide", s.manager.CloseWindow) {
			return true
		}
		return s.applyAndVerify(ctx, window, score, "hide", s.manager.HideWindow)
	}
	if s.applyAndVerify(ctx, window, score, "close", s.manager.CloseWindow) {
		return true
	}
	return false
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
