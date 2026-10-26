//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/stringutil"
)

// Window-action errors distinguish an absent process/window from a foreground
// refusal, where the original window is already visible and usable.
var (
	ErrProgramNotRunning    = errors.New("program is not running")
	ErrProgramStopFailed    = errors.New("program did not stop")
	ErrProgramNoWindow      = errors.New("program has no native window")
	ErrWindowActivation     = errors.New("window shown but Windows refused foreground activation")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// IsVisible is a cheap, live check for the action label. A cached handle never
// authorizes an action: SetProgramWindowVisible resolves the process anew.
func (w HiddenWindow) IsVisible() bool {
	if !w.valid() || !isWindowVisible(w.Handle) {
		return false
	}
	minimized, _, _ := procIsIconic.Call(w.Handle)
	return minimized == 0
}

func (w HiddenWindow) valid() bool {
	if w.Handle == 0 || w.ProcessID == 0 {
		return false
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcess.Call(w.Handle, uintptr(unsafe.Pointer(&pid)))
	return pid == w.ProcessID
}

// IsProgramRunning performs a live identity check for the UI action label.
// Process identity takes precedence over window visibility because collected
// and background programs can be running without a visible window.
func (s *Service) IsProgramRunning(entry config.ManagedAppEntry) bool {
	expectedPath := normalizePath(entry.ExePath)
	expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
	if expectedName == "" {
		return false
	}
	return hasRunningProcessByIdentity(expectedPath, expectedName) || s.hasExistingManagedWindow(expectedPath, expectedName)
}

// StopNow explicitly exits the program, unlike startup window dismissal.
// Give enabled native close handlers their cleanup path, then terminate any
// remaining matching process trees. Esc can merely hide a window, so it is
// not part of this exit operation.
func (s *Service) StopNow(ctx context.Context, entry config.ManagedAppEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	expectedPath := normalizePath(entry.ExePath)
	expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
	if expectedName == "" {
		return ErrProgramNotRunning
	}

	pids := runningProgramPIDs(expectedPath, expectedName)
	if len(pids) == 0 {
		return ErrProgramNotRunning
	}
	pidSet := make(map[uint32]bool, len(pids))
	for _, pid := range pids {
		pidSet[pid] = true
	}
	closed := make(map[uintptr]bool)
	for _, window := range s.enumerator.EnumerateTopLevelWindows() {
		if !pidSet[window.ProcessID] || window.Handle == 0 || window.IsToolWindow || isUnmanageableWindow(window) {
			continue
		}
		target := resolveActionTargetHandle(window)
		if target == 0 || closed[target] {
			continue
		}
		closed[target] = true
		if ok, err := s.manager.CloseWindow(target); !ok {
			if err != nil {
				s.logger.Warn(fmt.Sprintf("stop program close failed: %s pid=%d hwnd=0x%X err=%v", entry.Name, window.ProcessID, target, err))
			}
		}
	}
	if len(closed) > 0 && !waitWithContext(ctx, 2*time.Second) {
		return ctx.Err()
	}

	remaining := runningProgramPIDs(expectedPath, expectedName)
	if len(remaining) > 0 {
		if s.terminateProcesses == nil {
			return ErrProgramStopFailed
		}
		s.terminateProcesses(remaining)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(runningProgramPIDs(expectedPath, expectedName)) == 0 {
			return nil
		}
		if time.Now().After(deadline) || !waitWithContext(ctx, 50*time.Millisecond) {
			if err := ctx.Err(); err != nil {
				return err
			}
			return ErrProgramStopFailed
		}
	}
}

func runningProgramPIDs(expectedPath, expectedName string) []uint32 {
	var pids []uint32
	forEachRunningProcessByIdentity(expectedPath, expectedName, func(pid uint32) bool {
		pids = append(pids, pid)
		return true
	})
	return pids
}

// SetProgramWindowVisible changes the existing native window, never the launch
// settings or the running process. In particular, no new console is allocated
// for an old CREATE_NO_WINDOW process: its discarded output cannot be restored.
func (s *Service) SetProgramWindowVisible(ctx context.Context, entry config.ManagedAppEntry, visible bool) (HiddenWindow, error) {
	if err := ctx.Err(); err != nil {
		return HiddenWindow{}, err
	}
	path := normalizePath(entry.ExePath)
	name := stringutil.TrimExt(filepath.Base(entry.ExePath))
	pids := make(map[uint32]bool)
	forEachRunningProcessByIdentity(path, name, func(pid uint32) bool { pids[pid] = true; return true })
	if len(pids) == 0 {
		return HiddenWindow{}, ErrProgramNotRunning
	}
	window := selectProgramWindow(enumerateTopLevelWindows(true), pids)
	if window.Handle == 0 {
		return HiddenWindow{}, ErrProgramNoWindow
	}
	// Hold a process handle through the operation and recheck identity after
	// enumeration, so a dying/reused PID cannot lend another program its window.
	process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, window.ProcessID)
	if err != nil {
		return window, err
	}
	defer windows.CloseHandle(process)
	if !processIdentityMatches(window.ProcessID, filepath.Base(processExecutablePath(window.ProcessID)), path, normalizeIdentity(name)) || !window.valid() {
		return HiddenWindow{}, ErrProgramNotRunning
	}
	if err := ctx.Err(); err != nil {
		return window, err
	}
	command := uintptr(windows.SW_HIDE)
	if visible {
		command = windows.SW_RESTORE
	}
	if ok, _, err := procShowWindowAsync.Call(window.Handle, command); ok == 0 {
		return window, errors.Join(errors.New("native window request failed"), err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, err := windows.WaitForSingleObject(process, 0)
		if err != nil {
			return window, err
		}
		if status == windows.WAIT_OBJECT_0 || !window.valid() {
			return HiddenWindow{}, ErrProgramNotRunning
		}
		if (visible && window.IsVisible()) || (!visible && !isWindowVisible(window.Handle)) {
			break
		}
		if time.Now().After(deadline) {
			return window, errors.New("native window request timed out")
		}
		if !waitWithContext(ctx, 25*time.Millisecond) {
			return window, ctx.Err()
		}
	}
	if visible {
		if ok, _, _ := procSetForegroundWindow.Call(window.Handle); ok == 0 {
			foreground, _, _ := procGetForegroundWindow.Call()
			if foreground != window.Handle {
				return window, ErrWindowActivation
			}
		}
	}
	return window, nil
}

func selectProgramWindow(windows []ManagedWindowInfo, pids map[uint32]bool) HiddenWindow {
	var selected HiddenWindow
	best := -1
	for _, window := range windows {
		if !pids[window.ProcessID] || window.Handle == 0 || window.IsToolWindow || isUnmanageableWindow(window) {
			continue
		}
		console := strings.EqualFold(window.ClassName, consoleWindowClass)
		if window.Title == "" && !console {
			continue
		}
		score := 0
		if console {
			score += 1000 // the original console, not an auxiliary application dialog
		}
		if window.OwnerHandle == 0 {
			score += 100
		}
		if window.IsForeground {
			score += 20
		}
		if window.IsVisible {
			score += 10
		}
		if score > best {
			selected = HiddenWindow{Handle: window.Handle, ProcessID: window.ProcessID}
			best = score
		}
	}
	return selected
}
