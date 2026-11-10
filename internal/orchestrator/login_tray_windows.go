//go:build windows

package orchestrator

import (
	"context"
	"fmt"
	"golang.org/x/sys/windows"
	"time"
	"wintray/internal/config"
)

func (s *Service) finishLoginToTray(ctx context.Context, entry config.ManagedAppEntry, opts startOptions, main HiddenWindow, code ResultCode) Result {
	if opts.waitQQLogin {
		var err error
		main, err = waitQQLogin(ctx, entry.ExePath)
		if err != nil {
			if ctx.Err() == context.Canceled {
				return cancelledResult(entry)
			}
			return Result{AppName: entry.Name, Code: ResultQQLoginIncomplete, Message: err.Error()}
		}
	}
	managed, err := s.dismissLoginMain(ctx, entry.ExePath, main)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return cancelledResult(entry)
		}
		return Result{AppName: entry.Name, Code: ResultNoWindowManaged, Message: err.Error()}
	}
	return managedResult(entry, managed, code, "managed after login")
}

// Unlike the generic timed path, authenticated startup is an observation job:
// retry only recognized main windows until dismissed, cancelled or exited.
// It finishes once, so restoring the application later remains the user's action.
func (s *Service) dismissLoginMain(ctx context.Context, path string, main HiddenWindow) (managedWindow, error) {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, main.ProcessID)
	if err != nil {
		return managedWindow{}, err
	}
	defer windows.CloseHandle(process)
	alive := func() bool {
		state, err := windows.WaitForSingleObject(process, 0)
		return err == nil && state == uint32(windows.WAIT_TIMEOUT)
	}
	hiddenLastScan := false
	for ctx.Err() == nil {
		if !alive() {
			return managedWindow{}, errLoginProcessExited
		}
		var candidates []ManagedWindowInfo
		for _, w := range s.enumerator.EnumerateTopLevelWindows() {
			if w.ProcessID != main.ProcessID || w.OwnerHandle != 0 || w.IsToolWindow || !executablePathsMatch(w.ProcessPath, path) {
				continue
			}
			ok, err := s.loginMain(ctx, path, HiddenWindow{Handle: w.Handle, ProcessID: w.ProcessID})
			if err != nil {
				s.logger.Warn(fmt.Sprintf("login main observation failed: %v", err))
				continue
			}
			if ok {
				candidates = append(candidates, w)
			}
		}
		// The app can close its recognized main window to tray on its own.
		// Re-scan once before accepting that transition, so a replacement main
		// window is handled instead of leaving this observation job pending.
		hidden := len(candidates) == 0 && main.valid() && !isWindowVisible(main.Handle)
		if hidden && hiddenLastScan {
			return managedWindow{Handle: main.Handle, ProcessID: main.ProcessID}, nil
		}
		hiddenLastScan = hidden
		if len(candidates) == 1 {
			w := candidates[0]
			score := computeCandidateScore(w, path, "", &main.ProcessID, nil)
			if managed, ok := s.tryManageAndVerify(ctx, w, score, "close"); ok {
				// Allow posted close/exit messages to settle before claiming success.
				if !waitWithContext(ctx, 500*time.Millisecond) {
					break
				}
				if !alive() {
					return managedWindow{}, errLoginProcessExited
				}
				if !isWindowVisible(w.Handle) {
					return managed, nil
				}
			}
		}
		if !waitWithContext(ctx, 500*time.Millisecond) {
			break
		}
	}
	return managedWindow{}, ctx.Err()
}
