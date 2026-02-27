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
)

func (s *Service) StartAndManage(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	if entry.ExePath == "" {
		return Result{AppName: entry.Name, Managed: false, Message: "empty exe path"}
	}
	if _, err := os.Stat(entry.ExePath); err != nil {
		s.logger.Warn(fmt.Sprintf("skip invalid exe path: %s", entry.ExePath))
		return Result{AppName: entry.Name, Managed: false, Message: "invalid exe path"}
	}

	expectedName := trimExt(filepath.Base(entry.ExePath))
	expectedPath := normalizePath(entry.ExePath)
	baseline := s.captureBaseline(func(w ManagedWindowInfo) bool {
		return matchesExecutable(w, expectedPath, expectedName) && matchStrategy(w, entry.WindowMatch.Strategy)
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
		return (w.ProcessID == pid || matchesExecutable(w, expectedPath, expectedName)) && matchStrategy(w, entry.WindowMatch.Strategy)
	}, expectedPath, expectedName, &pid, baseline, retrySeconds, "close")
	if !ok {
		return Result{AppName: entry.Name, Managed: false, Message: "no window managed"}
	}
	return Result{AppName: entry.Name, Managed: true, Action: "close", Message: "managed"}
}

func (s *Service) HideExisting(ctx context.Context, entry config.ManagedAppEntry, retrySeconds int) Result {
	expectedName := trimExt(filepath.Base(entry.ExePath))
	if expectedName == "" {
		return Result{AppName: entry.Name, Managed: false, Message: "invalid process name"}
	}
	expectedPath := normalizePath(entry.ExePath)
	ok := s.manageFirstMatchingWindow(ctx, func(w ManagedWindowInfo) bool {
		return matchesExecutable(w, expectedPath, expectedName) && matchStrategy(w, entry.WindowMatch.Strategy)
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
		candidates := make([]MatchCandidate, 0)
		for _, w := range windows {
			if !predicate(w) {
				continue
			}
			score := computeCandidateScore(w, expectedPath, expectedName, launchedPID, baseline)
			candidates = append(candidates, MatchCandidate{Window: w, Score: score})
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
			time.Sleep(delay)
		}
	}
	return false
}

func (s *Service) tryManageAndVerify(ctx context.Context, window ManagedWindowInfo, score int, actionType string) bool {
	if score < closeAllowedScoreThreshold {
		s.logger.Warn(fmt.Sprintf("skip low confidence candidate score=%d threshold=%d %s", score, closeAllowedScoreThreshold, describeWindow(window)))
		return false
	}

	// Determine primary and fallback actions based on actionType.
	// "hide" flow: SW_HIDE first (works for tray apps), then WM_CLOSE fallback.
	// "close" flow: WM_CLOSE first, then SW_HIDE fallback (for apps that ignore WM_CLOSE).
	type actionFn func(uintptr) (bool, error)
	var primary, fallback struct {
		name string
		fn   actionFn
	}
	if actionType == "hide" {
		primary = struct {
			name string
			fn   actionFn
		}{"hide", s.manager.HideWindow}
		fallback = struct {
			name string
			fn   actionFn
		}{"close", s.manager.CloseWindow}
	} else {
		primary = struct {
			name string
			fn   actionFn
		}{"close", s.manager.CloseWindow}
		fallback = struct {
			name string
			fn   actionFn
		}{"hide", s.manager.HideWindow}
	}

	if s.applyAndVerify(ctx, window, score, primary.name, primary.fn) {
		return true
	}
	return s.applyAndVerify(ctx, window, score, fallback.name, fallback.fn)
}

func (s *Service) applyAndVerify(ctx context.Context, window ManagedWindowInfo, score int, action string, fn func(uintptr) (bool, error)) bool {
	ok, err := fn(window.Handle)
	if !ok {
		if err != nil {
			s.logger.Warn(fmt.Sprintf("action request failed action=%s score=%d %s err=%v", action, score, describeWindow(window), err))
		} else {
			s.logger.Warn(fmt.Sprintf("action request failed action=%s score=%d %s", action, score, describeWindow(window)))
		}
		return false
	}

	s.logger.Info(fmt.Sprintf("action requested action=%s score=%d %s", action, score, describeWindow(window)))
	if s.verifyActionApplied(ctx, window.Handle, score, action) {
		s.logger.Info(fmt.Sprintf("action applied action=%s score=%d hwnd=0x%X", action, score, window.Handle))
		return true
	}

	s.logger.Warn(fmt.Sprintf("action not applied action=%s score=%d hwnd=0x%X", action, score, window.Handle))
	return false
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
	const attempts = 6
	const delay = 200 * time.Millisecond
	for i := 0; i < attempts; i++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		windows := s.enumerator.EnumerateTopLevelWindows()
		if _, found := findWindowByHandle(windows, hwnd); !found {
			return true
		}

		if i < attempts-1 {
			time.Sleep(delay)
		}
	}

	if score >= closeAllowedScoreThreshold {
		s.logger.Warn(fmt.Sprintf("verify timeout action=%s score=%d hwnd=0x%X", action, score, hwnd))
	}
	return false
}

func findWindowByHandle(windows []ManagedWindowInfo, hwnd uintptr) (ManagedWindowInfo, bool) {
	for _, w := range windows {
		if w.Handle == hwnd {
			return w, true
		}
	}
	return ManagedWindowInfo{}, false
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

func trimExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)]
}

