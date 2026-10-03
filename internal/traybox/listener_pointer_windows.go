//go:build windows

package traybox

import (
	"fmt"
	"runtime"
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
	procPostPointerMessage  = user32.NewProc("PostThreadMessageW")
	pointerHookProc         = syscall.NewCallback(func(code int32, message, data uintptr) uintptr {
		if code >= 0 {
			if l := activeListener.Load(); l != nil && !l.pointerStopping.Load() {
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

// startPointerHook gives the hook a dedicated native message queue. Windows
// waits for this queue on every mouse event, including movement, so it cannot
// share the collector's filesystem, Shell calls or logging work.
func (l *listener) startPointerHook() error {
	l.pointerDone = make(chan struct{})
	l.pointerStopping.Store(false)
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(l.pointerDone)
		var msg win.MSG
		var pinned runtime.Pinner
		pinned.Pin(&msg)
		defer pinned.Unpin()
		// Create the queue before publishing its thread ID to the stopper.
		win.PeekMessage(&msg, 0, 0, 0, win.PM_NOREMOVE)
		l.pointerThreadID = win.GetCurrentThreadId()
		hook, _, err := procSetWindowsHookExW.Call(whMouseLL, pointerHookProc, uintptr(win.GetModuleHandle(nil)), 0)
		if hook == 0 {
			ready <- fmt.Errorf("install taskbar pointer guard: %w", err)
			return
		}
		l.pointerHook = hook
		defer func() {
			if ok, _, err := procUnhookWindowsHookEx.Call(hook); ok == 0 {
				l.logger.Warn(fmt.Sprintf("tray box: remove taskbar pointer guard: %v", err))
			}
		}()
		ready <- nil
		for {
			switch win.GetMessage(&msg, 0, 0, 0) {
			case -1:
				l.logger.Warn("tray box: taskbar pointer message loop failed")
				return
			case 0:
				return
			}
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
	}()
	if err := <-ready; err != nil {
		<-l.pointerDone
		l.pointerDone = nil
		l.pointerThreadID = 0
		return err
	}
	return nil
}

func (l *listener) stopPointerHook() error {
	if l.pointerDone == nil {
		return nil
	}
	l.pointerStopping.Store(true)
	if ok, _, err := procPostPointerMessage.Call(uintptr(l.pointerThreadID), win.WM_QUIT, 0, 0); ok == 0 {
		select {
		case <-l.pointerDone: // the native loop already ended
		default:
			l.pointerStopping.Store(false)
			return fmt.Errorf("stop taskbar pointer guard: %w", err)
		}
	}
	<-l.pointerDone
	l.pointerHook, l.pointerThreadID, l.pointerDone = 0, 0, nil
	return nil
}

// onPointer requests that only our invisible window yield on taskbar presses.
// Leaving the proxy first makes Windows 11's in-place icon reorder target the
// wrong Shell_TrayWnd; merely skipping timer raises is not enough. Explorer's
// styles, window procedures and z-order are never changed here.
func (l *listener) onPointer(message uint32, point win.POINT) {
	switch message {
	case win.WM_LBUTTONDOWN, win.WM_RBUTTONDOWN:
		var bounds win.RECT
		tray := win.HWND(l.pointerTarget.Load())
		if tray == 0 || !win.GetWindowRect(tray, &bounds) ||
			point.X < bounds.Left || point.X >= bounds.Right || point.Y < bounds.Top || point.Y >= bounds.Bottom {
			return
		}
		l.pointerMu.Lock()
		l.pointerYielded = true
		l.pointerResumeAt = time.Now().Add(pointerReleaseDelay)
		l.pointerMu.Unlock()
		// The proxy belongs to the busy collector thread. Queue its move rather
		// than synchronously sending it window-position messages from the hook.
		if !win.SetWindowPos(l.hwnd, win.HWND_BOTTOM, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE|win.SWP_ASYNCWINDOWPOS) {
			l.pointerFailed.Store(true) // report from maintenance, outside the hook
		}
	case win.WM_LBUTTONUP, win.WM_RBUTTONUP:
		l.pointerMu.Lock()
		if l.pointerYielded {
			// The hook runs before Explorer gets button-up. Allow its drop to
			// complete before restoring interception, even for a quick click.
			l.pointerResumeAt = time.Now().Add(pointerReleaseDelay)
		}
		l.pointerMu.Unlock()
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
	l.pointerMu.Lock()
	if !l.pointerYielded {
		l.pointerMu.Unlock()
		return false
	}
	if buttonsDown {
		l.pointerResumeAt = now.Add(pointerReleaseDelay)
		l.pointerMu.Unlock()
		return true
	}
	if now.Before(l.pointerResumeAt) {
		l.pointerMu.Unlock()
		return true
	}
	l.pointerYielded = false
	l.pointerMu.Unlock()
	// New icons could have registered directly with Explorer while yielding.
	// Reuse the existing refresh path to collect them once priority is back.
	win.SetTimer(l.hwnd, timerRefresh, uint32(refreshDelay.Milliseconds()), 0)
	return false
}
