//go:build windows

package traybox

import (
	"fmt"
	"syscall"
	"time"

	"github.com/lxn/win"
)

const (
	whMouseLL           = 14
	pointerReleaseDelay = 300 * time.Millisecond
)

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	pointerHookProc         = syscall.NewCallback(func(code int32, message, data uintptr) uintptr {
		if code >= 0 {
			if l := activeListener.Load(); l != nil && !l.closing {
				switch uint32(message) {
				case win.WM_LBUTTONDOWN, win.WM_LBUTTONUP, win.WM_RBUTTONDOWN, win.WM_RBUTTONUP:
					// MSLLHOOKSTRUCT begins with a screen-coordinate POINT. Copy
					// it while Windows owns the callback buffer; never retain it.
					l.onPointer(uint32(message), *(*win.POINT)(foreignPointer(data)))
				}
			}
		}
		// Observe only: never consume or synthesize the user's mouse input.
		r, _, _ := procCallNextHookEx.Call(0, uintptr(code), message, data)
		return r
	})
)

// startPointerHook runs on the listener's message-loop thread. A timer alone
// cannot yield before Explorer processes a fast press-and-drag gesture.
func (l *listener) startPointerHook() error {
	hook, _, err := procSetWindowsHookExW.Call(whMouseLL, pointerHookProc, uintptr(win.GetModuleHandle(nil)), 0)
	if hook == 0 {
		return fmt.Errorf("install taskbar pointer guard: %w", err)
	}
	l.pointerHook = hook
	return nil
}

func (l *listener) stopPointerHook() {
	if l.pointerHook != 0 {
		if ok, _, err := procUnhookWindowsHookEx.Call(l.pointerHook); ok == 0 {
			l.logger.Warn(fmt.Sprintf("tray box: remove taskbar pointer guard: %v", err))
		}
		l.pointerHook = 0
	}
}

// onPointer yields only our invisible window, before the native taskbar sees
// the button-down. Leaving the proxy first makes Windows 11's in-place icon
// reorder target the wrong Shell_TrayWnd; merely skipping timer raises is not
// enough. No Explorer styles, window procedures or z-order are changed here.
func (l *listener) onPointer(message uint32, point win.POINT) {
	switch message {
	case win.WM_LBUTTONDOWN, win.WM_RBUTTONDOWN:
		var bounds win.RECT
		tray := l.cfg.target(l.hwnd)
		if tray == 0 || !win.GetWindowRect(tray, &bounds) ||
			point.X < bounds.Left || point.X >= bounds.Right || point.Y < bounds.Top || point.Y >= bounds.Bottom {
			return
		}
		l.pointerYielded = true
		l.pointerResumeAt = time.Now().Add(pointerReleaseDelay)
		if !win.SetWindowPos(l.hwnd, win.HWND_BOTTOM, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE) {
			l.logger.Warn("tray box: could not yield to the native taskbar for a pointer gesture")
		}
	case win.WM_LBUTTONUP, win.WM_RBUTTONUP:
		if l.pointerYielded {
			// The hook runs before Explorer gets button-up. Allow its drop to
			// complete before restoring interception, even for a quick click.
			l.pointerResumeAt = time.Now().Add(pointerReleaseDelay)
		}
	}
}

func pointerButtonDown() bool {
	for _, button := range []uintptr{win.VK_LBUTTON, win.VK_RBUTTON} {
		state, _, _ := procGetAsyncKeyState.Call(button)
		if state&0x8000 != 0 {
			return true
		}
	}
	return false
}

// pointerBusy also recovers if a release happened on another input desktop:
// the physical button state, not an indefinitely latched hook event, decides.
func (l *listener) pointerBusy(now time.Time, buttonsDown bool) bool {
	if !l.pointerYielded {
		return false
	}
	if buttonsDown {
		l.pointerResumeAt = now.Add(pointerReleaseDelay)
		return true
	}
	if now.Before(l.pointerResumeAt) {
		return true
	}
	l.pointerYielded = false
	// New icons could have registered directly with Explorer while yielding.
	// Reuse the existing refresh path to collect them once priority is back.
	win.SetTimer(l.hwnd, timerRefresh, uint32(refreshDelay.Milliseconds()), 0)
	return false
}
