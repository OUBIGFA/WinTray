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

func (s *Service) StartAndManage(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	if entry.ExePath == "" {
		return Result{AppName: entry.Name, Managed: false, Message: "empty exe path"}
	}
	if _, err := os.Stat(entry.ExePath); err != nil {
		s.logger.Warn(fmt.Sprintf("skip invalid exe path: %s", entry.ExePath))
		return Result{AppName: entry.Name, Managed: false, Message: "invalid exe path"}
	}

	expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
	expectedPath := normalizePath(entry.ExePath)
	if s.hasExistingManagedWindow(expectedPath, expectedName, entry.WindowMatch.Strategy) {
		s.logger.Info(fmt.Sprintf("skip start: already running %s", entry.Name))
		if !entry.LaunchHiddenInBackground && entry.TrayBehavior.AutoMinimizeAndHideOnLaunch {
			ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
				return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName) && matchStrategy(w, entry.WindowMatch.Strategy)
			}, expectedPath, expectedName, nil, nil, retrySeconds, "hide")
			if ok {
				return Result{AppName: entry.Name, Managed: true, Action: "hide", Message: "already running managed existing"}
			}
		}
		return Result{AppName: entry.Name, Managed: true, Message: "already running skipped"}
	}

	baseline := s.captureBaseline(func(w ManagedWindowInfo) bool {
		return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName) && matchStrategy(w, entry.WindowMatch.Strategy)
	})

	cmd, err := startProcess(entry.ExePath, entry.Args, entry.LaunchHiddenInBackground)
	if err != nil {
		s.logger.Error(fmt.Sprintf("start failed: %s err=%v", entry.Name, err))
		return Result{AppName: entry.Name, Managed: false, Message: "process start failed"}
	}
	pid := uint32(cmd.Process.Pid)
	s.logger.Info(fmt.Sprintf("started: %s pid=%d hidden=%t", entry.Name, pid, entry.LaunchHiddenInBackground))

	if entry.LaunchHiddenInBackground {
		return Result{AppName: entry.Name, Managed: true, Message: "started hidden"}
	}

	if !entry.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		return Result{AppName: entry.Name, Managed: true, Message: "started only"}
	}

	ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return (w.ProcessID == pid || matchesExecutableWithIdentityFallback(w, expectedPath, expectedName)) && matchStrategy(w, entry.WindowMatch.Strategy)
	}, expectedPath, expectedName, &pid, baseline, retrySeconds, "close")
	if !ok {
		return Result{AppName: entry.Name, Managed: false, Message: "no window managed"}
	}
	return Result{AppName: entry.Name, Managed: true, Action: "close", Message: "managed"}
}

func (s *Service) hasExistingManagedWindow(expectedPath, expectedName string, strategy config.MatchStrategy) bool {
	for _, w := range s.enumerator.EnumerateTopLevelWindows() {
		if isUnmanageableWindow(w) {
			continue
		}
		if !matchesExecutableWithIdentityFallback(w, expectedPath, expectedName) {
			continue
		}
		if !matchStrategy(w, strategy) {
			continue
		}
		return true
	}
	return false
}

func (s *Service) HideExisting(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
	if expectedName == "" {
		return Result{AppName: entry.Name, Managed: false, Message: "invalid process name"}
	}
	expectedPath := normalizePath(entry.ExePath)
	ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return matchesExecutableWithIdentityFallback(w, expectedPath, expectedName) && matchStrategy(w, entry.WindowMatch.Strategy)
	}, expectedPath, expectedName, nil, nil, retrySeconds, "hide")
	if !ok {
		return Result{AppName: entry.Name, Managed: false, Message: "no existing window managed"}
	}
	return Result{AppName: entry.Name, Managed: true, Action: "hide", Message: "managed existing"}
}

func (s *Service) manageFirstMatchingWindow(ctx context.Context, predicate func(ManagedWindowInfo) bool, expectedPath, expectedName string, launchedPID *uint32, baseline map[uintptr]struct{}, retrySeconds int, actionType string) bool {
	attempts := max(1, max(0, retrySeconds)*2+1)
	const delay = 500 * time.Millisecond

	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
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
			s.logger.Info(fmt.Sprintf("match round %d/%d candidates=%d top=%s", i+1, attempts, len(candidates), summarizeCandidates(candidates, 3)))
		}

		for _, c := range candidates {
			if s.tryManageAndVerify(ctx, c.Window, c.Score, actionType) {
				return true
			}
		}

		if i < attempts-1 {
			if !waitWithContext(ctx, delay) {
				return false
			}
		}
	}
	return false
}

func (s *Service) tryManageAndVerify(ctx context.Context, window ManagedWindowInfo, score int, actionType string) bool {
	if score < closeAllowedScoreThreshold {
		s.logger.Warn(fmt.Sprintf("skip low confidence candidate score=%d threshold=%d %s", score, closeAllowedScoreThreshold, describeWindow(window)))
		return false
	}

	// "hide": WM_CLOSE first — many tray-oriented apps intercept close and hide
	//         themselves to tray while preserving their own tray-click restore logic.
	//         Fallback to SW_HIDE for apps that don't support close-to-tray.
	// "close": WM_CLOSE first — asks the app to close itself (some apps hide to tray
	//          on WM_CLOSE). Fallback to SW_HIDE for apps that ignore WM_CLOSE.
	//
	// Verification for "hide" accepts IsWindowVisible==0 as success; it does NOT
	// require the window to vanish from EnumWindows (the window handle stays valid
	// for hidden tray apps). Verification for "close" requires the handle to
	// disappear from EnumWindows.
	//
	// A single primary action is attempted per candidate per round; no in-round
	// fallback to avoid spamming a single window with redundant messages.
	if actionType == "hide" {
		// Prefer app-native close-to-tray behavior first. Many apps (Tauri/Electron)
		// intercept close and move to tray, preserving tray-click restore semantics.
		if s.applyAndVerify(ctx, window, score, "hide", s.manager.CloseWindow) {
			return true
		}
		return s.applyAndVerify(ctx, window, score, "hide", s.manager.HideWindow)
	}
	return s.applyAndVerify(ctx, window, score, "close", s.manager.CloseWindow)
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
		// For "close": the window should be fully destroyed; require it to vanish
		// from EnumWindows (IsWindowVisible check alone is insufficient since the
		// process might briefly hide before destroying).
		if action == "hide" {
			if !isWindowVisible(hwnd) {
				return true
			}
		} else {
			// For close we must verify destruction, not just invisibility.
			if !isWindow(hwnd) {
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
