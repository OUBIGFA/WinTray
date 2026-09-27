//go:build windows

package traybox

import (
	"errors"
	"fmt"

	"github.com/lxn/win"
)

var (
	// ErrIconGone means the program closed its icon or exited.
	ErrIconGone = errors.New("the program's tray icon is gone")
	// ErrNotClickable means the program does not accept clicks on its icon.
	ErrNotClickable = errors.New("the program's tray icon does not accept clicks")
)

// Activate delivers a click to the program as Explorer would: through the
// callback message the program registered with its icon. It returns at once.
func Activate(icon Icon, action Action) error {
	owner := win.HWND(uintptr(icon.owner))
	if owner == 0 || !isWindow(owner) {
		return ErrIconGone
	}
	if !icon.Clickable() {
		return ErrNotClickable
	}
	// The program usually brings a window or its menu to the front; WinTray
	// just closed its own menu and may hand the foreground on.
	var pid uint32
	win.GetWindowThreadProcessId(owner, &pid)
	procAllowSetForegroundWindow.Call(uintptr(pid))
	var cursor win.POINT
	win.GetCursorPos(&cursor)
	for _, event := range clickEvents(action, icon.version) {
		wParam, lParam := callbackParams(icon, event, cursor)
		if r, _, err := procSendNotifyMessageW.Call(uintptr(owner), uintptr(icon.callback), wParam, lParam); r == 0 {
			return fmt.Errorf("notify %s: %w", icon.DisplayName(), err)
		}
	}
	return nil
}

// clickEvents is what Explorer reports for a click. Since version 3, a click
// is followed by NIN_SELECT and a right click by WM_CONTEXTMENU.
func clickEvents(action Action, version uint32) []uint32 {
	var events []uint32
	switch action {
	case ActionRightClick:
		events = []uint32{win.WM_RBUTTONDOWN, win.WM_RBUTTONUP}
		if version >= 3 {
			events = append(events, win.WM_CONTEXTMENU)
		}
	case ActionDoubleClick:
		// Like Explorer's second click (and Zebar/Seelen): do not prepend
		// another full single click, which can toggle a program twice.
		events = []uint32{win.WM_LBUTTONDBLCLK, win.WM_LBUTTONUP}
		if version >= 3 {
			events = append(events, win.NIN_SELECT)
		}
	default:
		events = []uint32{win.WM_LBUTTONDOWN, win.WM_LBUTTONUP}
		if version >= 3 {
			events = append(events, win.NIN_SELECT)
		}
	}
	return events
}

// callbackParams follows the notification format of the icon's version:
// version 4 passes the anchor point and the uID alongside the event.
func callbackParams(icon Icon, event uint32, at win.POINT) (wParam, lParam uintptr) {
	if icon.version >= 4 {
		return makeLong(uint32(at.X), uint32(at.Y)), makeLong(event, icon.uid)
	}
	return uintptr(icon.uid), uintptr(event)
}

func makeLong(lo, hi uint32) uintptr { return uintptr(lo&0xFFFF | (hi&0xFFFF)<<16) }
