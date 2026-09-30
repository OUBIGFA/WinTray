//go:build windows

package traybox

import (
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
)

// These are private windows, not Shell_TrayWnd. Tests exercise real z-order
// changes without moving the user's mouse or touching Explorer's taskbar.
func pointerListener(t *testing.T) (*listener, win.HWND) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	instance := win.GetModuleHandle(nil)
	class, err := registerListenerClass("WinTrayTestPointerOrder", instance)
	if err != nil {
		t.Fatal(err)
	}
	target := win.CreateWindowEx(win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW, class, nil, win.WS_POPUP, -200, -100, 400, 40, 0, 0, instance, nil)
	hwnd := win.CreateWindowEx(win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, instance, nil)
	t.Cleanup(func() { win.DestroyWindow(hwnd); win.DestroyWindow(target) })
	if target == 0 || hwnd == 0 {
		t.Fatal("create private taskbar windows")
	}
	l := &listener{hwnd: hwnd, logger: quietLogger{}, taskbarCreated: fakeTaskbar,
		cfg: listenerConfig{className: "WinTrayTestPointerOrder", raise: true, target: func(win.HWND) win.HWND { return target }}}
	l.keepOnTop()
	return l, target
}

func firstPointerWindow() win.HWND {
	return win.FindWindow(syscall.StringToUTF16Ptr("WinTrayTestPointerOrder"), nil)
}

func TestListenerYieldsDuringTaskbarPointerGesture(t *testing.T) {
	for _, button := range []struct {
		name     string
		down, up uint32
	}{
		{"left", win.WM_LBUTTONDOWN, win.WM_LBUTTONUP},
		{"right", win.WM_RBUTTONDOWN, win.WM_RBUTTONUP}, // swapped primary mouse button
	} {
		t.Run(button.name, func(t *testing.T) {
			l, target := pointerListener(t)
			style := win.GetWindowLong(target, win.GWL_EXSTYLE)
			var bounds win.RECT
			win.GetWindowRect(target, &bounds)
			foreground := win.GetForegroundWindow()
			l.onPointer(button.down, win.POINT{X: -150, Y: -80})
			if firstPointerWindow() != target {
				t.Fatal("pointer-down did not give the native taskbar priority")
			}
			// The timer and an Explorer restart must not interrupt the gesture.
			l.pointerResumeAt = time.Now().Add(time.Hour)
			l.wndProc(win.WM_TIMER, timerRaise, 0)
			l.wndProc(l.taskbarCreated, 0, 0)
			if firstPointerWindow() != target {
				t.Fatal("maintenance stole priority during a native gesture")
			}
			// Release may occur outside the taskbar. Do not raise from the hook
			// before Explorer has received and processed the release event.
			l.onPointer(button.up, win.POINT{X: 900, Y: 900})
			l.keepOnTop()
			if firstPointerWindow() != target {
				t.Fatal("listener raised before native drop processing")
			}
			deadline := l.pointerResumeAt
			if !l.pointerBusy(deadline.Add(time.Second), true) {
				t.Fatal("a still-held button did not extend the gesture")
			}
			if l.pointerBusy(l.pointerResumeAt.Add(time.Second), false) {
				t.Fatal("completed gesture did not release the listener")
			}
			l.keepOnTop()
			if firstPointerWindow() != l.hwnd {
				t.Fatal("collection priority did not recover after the gesture")
			}
			var after win.RECT
			win.GetWindowRect(target, &after)
			if after != bounds || win.GetWindowLong(target, win.GWL_EXSTYLE) != style || win.GetForegroundWindow() != foreground {
				t.Fatal("listener changed the native taskbar geometry, style or foreground window")
			}
		})
	}
}

func TestListenerIgnoresPointerOutsideTaskbar(t *testing.T) {
	l, _ := pointerListener(t)
	for _, point := range []win.POINT{{X: -201, Y: -80}, {X: 200, Y: -80}, {X: 0, Y: -101}, {X: 0, Y: -60}} {
		l.onPointer(win.WM_LBUTTONDOWN, point)
		if firstPointerWindow() != l.hwnd || l.pointerYielded {
			t.Fatalf("outside point %+v paused collection", point)
		}
	}
	l.onPointer(win.WM_MOUSEMOVE, win.POINT{X: 0, Y: -80})
	l.onPointer(win.WM_LBUTTONUP, win.POINT{X: 0, Y: -80})
	if l.pointerYielded {
		t.Fatal("movement or an unrelated release paused collection")
	}
}

func TestListenerReleasesPointerHookOnStop(t *testing.T) {
	f := startFakeShell(t)
	l, err := startListener(listenerConfig{className: "WinTrayTestHookLifecycle", raise: true,
		target: func(win.HWND) win.HWND { return f.target }}, "", nil, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	if l.pointerHook == 0 {
		t.Error("native pointer guard was not installed")
	}
	if err := l.stop(); err != nil {
		t.Fatal(err)
	}
	if l.pointerHook != 0 {
		t.Fatal("pointer guard survived listener shutdown")
	}
}
