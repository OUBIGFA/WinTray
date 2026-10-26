//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"wintray/internal/config"
)

const weChatLoginTimeout = 30 * time.Second

type weChatWindowKind uint8

const (
	weChatUnknown weChatWindowKind = iota
	weChatLoginWindow
	weChatMainWindow
)

type weChatObservation struct {
	Window HiddenWindow
	Kind   weChatWindowKind
	Ready  bool
}

func weChatKind(class string) weChatWindowKind {
	switch class {
	case "WeChatLoginWnd", "mmui::LoginWindow":
		return weChatLoginWindow
	case "WeChatMainWndForPC", "mmui::MainWindow":
		return weChatMainWindow
	default:
		return weChatUnknown
	}
}

// Qt's native class changes with its version; the UIA root supplies the
// semantic LoginWindow/MainWindow class. Utility HWNDs have no useful UIA tree.
func weChatCandidateClass(class string) bool {
	return weChatKind(class) != weChatUnknown || (strings.HasPrefix(class, "Qt") && strings.HasSuffix(class, "QWindowIcon"))
}

func weChatLoginButton(name string) bool {
	switch name {
	case "进入微信", "登录", "進入微信", "登入", "Log In", "Log in", "Enter WeChat", "Enter Weixin":
		return true
	}
	return false
}

// Login is serialized with launches of this executable. Simultaneous manual
// and automatic requests must not both click the same login button.
func (s *Service) completeWeChatLogin(ctx context.Context, entry config.ManagedAppEntry, opts startOptions) (HiddenWindow, error) {
	if !opts.autoLogin {
		return HiddenWindow{}, nil
	}
	unlock, ok := s.lockProcessLaunch(ctx, normalizePath(entry.ExePath))
	if !ok {
		return HiddenWindow{}, ctx.Err()
	}
	defer unlock()
	window, err := s.wechatLogin(ctx, entry.ExePath)
	if err != nil {
		s.logger.Warn(fmt.Sprintf("WeChat login not confirmed: %s: %v", entry.Name, err))
	}
	return window, err
}

func weChatLoginFailure(entry config.ManagedAppEntry, err error) Result {
	if errors.Is(err, context.Canceled) {
		return cancelledResult(entry)
	}
	return Result{AppName: entry.Name, Code: ResultWeChatLoginIncomplete, Message: err.Error()}
}

// Once login is confirmed, dismiss only that main window, never another
// account's login window or a dialog found by the generic executable matcher.
func weChatManagedWindowMatches(enabled bool, main HiddenWindow, window ManagedWindowInfo) bool {
	return !enabled || (main.Handle != 0 && window.Handle == main.Handle && window.ProcessID == main.ProcessID)
}

func loginWeChat(ctx context.Context, path string) (HiddenWindow, error) {
	if !config.IsWeChatExecutable(path) {
		return HiddenWindow{}, errors.New("not a WeChat executable")
	}
	ctx, cancel := context.WithTimeout(ctx, weChatLoginTimeout)
	defer cancel()
	// UIA objects stay on this MTA thread and are released before COM teardown.
	// Provider calls have their own timeout; cancellation cannot leak a worker.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a, err := newWeChatAutomation()
	if err != nil {
		return HiddenWindow{}, err
	}
	defer a.close()
	return waitWeChatLogin(ctx, 500*time.Millisecond, func() ([]weChatObservation, error) {
		var observations []weChatObservation
		for _, window := range enumerateTopLevelWindows(true) {
			// Never use a process-name fallback for an action that logs in an account.
			if !window.IsVisible || !executablePathsMatch(window.ProcessPath, path) || window.OwnerHandle != 0 || !weChatCandidateClass(window.ClassName) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			observation, err := a.observe(ctx, HiddenWindow{Handle: window.Handle, ProcessID: window.ProcessID})
			if err != nil {
				return nil, err
			}
			if observation.Kind != weChatUnknown {
				observations = append(observations, observation)
			}
		}
		return observations, nil
	}, func(window HiddenWindow) error {
		return a.clickLogin(ctx, window)
	})
}

// A successful input request is not proof of login. Wait for a recognized main
// window in the same process, click at most once, and leave QR/phone prompts
// alone. Multiple accounts are deliberately not chosen on the user's behalf.
func waitWeChatLogin(ctx context.Context, interval time.Duration, scan func() ([]weChatObservation, error), click func(HiddenWindow) error) (HiddenWindow, error) {
	var clicked HiddenWindow
	var lastErr error
	for ctx.Err() == nil {
		observations, err := scan()
		if err != nil {
			lastErr = err // a provider can disappear while login rebuilds the window
		} else {
			if len(observations) > 1 {
				return HiddenWindow{}, errors.New("multiple WeChat windows; choose an account manually")
			}
			if len(observations) == 1 {
				current := observations[0]
				if clicked.ProcessID != 0 && current.Window.ProcessID != clicked.ProcessID {
					return HiddenWindow{}, errors.New("WeChat process changed during login")
				}
				if current.Kind == weChatMainWindow {
					if err := ctx.Err(); err != nil {
						return HiddenWindow{}, err
					}
					return current.Window, nil
				}
				if current.Kind == weChatLoginWindow && current.Ready && clicked.ProcessID == 0 {
					if err := ctx.Err(); err != nil {
						return HiddenWindow{}, err
					}
					clicked = current.Window
					if err := click(clicked); err != nil {
						return HiddenWindow{}, err
					}
				}
			}
		}
		if !waitWithContext(ctx, interval) {
			break
		}
	}
	return HiddenWindow{}, errors.Join(ctx.Err(), lastErr)
}
