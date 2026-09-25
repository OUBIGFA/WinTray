//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// A click on an empty part of the window, the list below its rows included,
// ends program editing. Walk has no message filter, its labels swallow mouse
// input and the native list consumes the button release itself, so a message
// hook on the UI thread is the one place that sees all of these clicks.
const (
	whGetMessage = 3
	hcAction     = 0
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	blankClickHookCallback  = syscall.NewCallback(blankClickHook)

	// Hooked main windows by handle; only their UI thread reads or changes it.
	blankClickWindows = map[win.HWND]*MainWindow{}
)

func (w *MainWindow) installBlankClickReset() error {
	w.blankSurfaces = map[win.HWND]walk.Widget{}
	w.mw.ForEachDescendant(func(widget walk.Widget) bool {
		switch widget.(type) {
		case *walk.Composite, *walk.Label, *walk.Spacer:
			w.blankSurfaces[widget.Handle()] = widget
		}
		return true
	})
	hook, _, err := procSetWindowsHookExW.Call(whGetMessage, blankClickHookCallback, 0, uintptr(windows.GetCurrentThreadId()))
	if hook == 0 {
		return fmt.Errorf("install blank click hook: %w", err)
	}
	hwnd := w.mw.Handle()
	blankClickWindows[hwnd] = w
	w.mw.Disposing().Attach(func() {
		delete(blankClickWindows, hwnd)
		_, _, _ = procUnhookWindowsHookEx.Call(hook)
	})
	return nil
}

func blankClickHook(code, wParam, lParam uintptr) uintptr {
	if int32(code) == hcAction && wParam == win.PM_REMOVE {
		// lParam points to the MSG being retrieved; reading it through
		// &lParam keeps vet's unsafeptr check quiet for this system pointer.
		msg := *(**win.MSG)(unsafe.Pointer(&lParam))
		if msg.Message == win.WM_LBUTTONDOWN {
			w := blankClickWindows[win.GetAncestor(msg.HWnd, win.GA_ROOT)]
			if w != nil && w.isBlankClick(msg.HWnd, int(win.GET_X_LPARAM(msg.LParam)), int(win.GET_Y_LPARAM(msg.LParam))) {
				// Runs once the click itself has been dispatched.
				w.synchronize(w.leaveEditorOnBlankClick)
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
	return ret
}

// isBlankClick reports whether a left click at x, y in hwnd's client area
// landed on the window background, a label, a spacer or the list outside its
// rows, rather than on a control.
func (w *MainWindow) isBlankClick(hwnd win.HWND, x, y int) bool {
	surface, ok := w.blankSurfaces[hwnd]
	if !ok {
		parent := win.GetParent(hwnd)
		if parent == w.managedList.Handle() {
			// The list draws its rows in native listviews it owns.
			hit := win.LVHITTESTINFO{Pt: win.POINT{X: int32(x), Y: int32(y)}}
			win.SendMessage(hwnd, win.LVM_HITTEST, 0, uintptr(unsafe.Pointer(&hit)))
			return hit.IItem < 0
		}
		// A label draws its text in a native static control it owns.
		_, isLabel := w.blankSurfaces[parent].(*walk.Label)
		return isLabel
	}
	container, ok := surface.(walk.Container)
	if !ok {
		return true
	}
	// Disabled controls pass their clicks on to the container behind them.
	children := container.Children()
	for i := 0; i < children.Len(); i++ {
		child := children.At(i)
		if _, blank := w.blankSurfaces[child.Handle()]; blank || !child.Visible() {
			continue
		}
		if b := child.BoundsPixels(); x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
			return false
		}
	}
	return true
}

// leaveEditorOnBlankClick takes focus off the fields first, so text still being
// typed is committed to the program it belongs to, then clears the selection,
// which returns the program editor to its empty default state.
func (w *MainWindow) leaveEditorOnBlankClick() {
	w.focusDefaultControl()
	w.clearManagedSelection()
}
