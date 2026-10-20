//go:build windows

package tray

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	menuUser32              = windows.NewLazySystemDLL("user32.dll")
	procEndMenu             = menuUser32.NewProc("EndMenu")
	procSetWindowsHookEx    = menuUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = menuUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = menuUser32.NewProc("CallNextHookEx")
	procGetAsyncKeyState    = menuUser32.NewProc("GetAsyncKeyState")
	procGetMenuItemRect     = menuUser32.NewProc("GetMenuItemRect")
)

const (
	msgfMenu        = 2
	boxRefreshTimer = 0x5742
)

type boxMenuTracking struct {
	owner        win.HWND
	menu         win.HMENU
	refresh      func()
	rightClicked uint32
}

// Only the UI thread tracks menus. WH_MSGFILTER is scoped to that thread and
// to TrackPopupMenuEx's modal loop; it never hooks another process's input.
var activeBoxMenu *boxMenuTracking
var boxMenuFilterProc = syscall.NewCallback(func(code int32, wParam uintptr, m *win.MSG) uintptr {
	if code == msgfMenu && activeBoxMenu != nil {
		session := activeBoxMenu
		if m.HWnd == session.owner {
			if m.Message == win.WM_TIMER && m.WParam == boxRefreshTimer && session.refresh != nil {
				session.refresh()
				return 1
			}
			// WM_MENURBUTTONUP depends on Windows having highlighted a row.
			// A stationary pointer/reopened menu can have no highlighted row
			// even after mouse movement. Hit-test the native row rectangles
			// on the actual button release instead of relying on that state.
			if m.Message == win.WM_RBUTTONUP {
				if id := collectedMenuItemAtPoint(session.owner, session.menu, m.Pt); id != 0 {
					session.rightClicked = id
					procEndMenu.Call()
					return 1
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, uintptr(unsafe.Pointer(m)))
	return r
})

func collectedMenuItemAtPoint(owner win.HWND, menu win.HMENU, point win.POINT) uint32 {
	for position, count := int32(0), win.GetMenuItemCount(menu); position < count; position++ {
		var rect win.RECT
		ok, _, _ := procGetMenuItemRect.Call(uintptr(owner), uintptr(menu), uintptr(position), uintptr(unsafe.Pointer(&rect)))
		if ok != 0 && point.X >= rect.Left && point.X < rect.Right && point.Y >= rect.Top && point.Y < rect.Bottom {
			return collectedMenuCommand(menu, uint32(position))
		}
	}
	return 0
}

func collectedMenuCommand(menu win.HMENU, position uint32) uint32 {
	item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID | win.MIIM_STATE}
	if win.GetMenuItemInfo(menu, position, 1, &item) &&
		item.WID >= trayBoxedBase && item.WID < trayIDLimit && item.FState&win.MFS_DISABLED == 0 {
		return item.WID
	}
	return 0
}

// trackBoxRightClicks keeps left/keyboard selection native, but closes the
// menu on a collected icon's right-click so its own program can show a menu.
func trackBoxRightClicks(hwnd win.HWND, menu win.HMENU, refresh func(), track func()) (uint32, error) {
	if activeBoxMenu != nil {
		return 0, errors.New("tray menu is already active")
	}
	if hwnd == 0 || menu == 0 {
		return 0, errors.New("tray menu or owner is unavailable")
	}
	session := &boxMenuTracking{owner: hwnd, menu: menu, refresh: refresh}
	// WH_MSGFILTER = -1; a thread-local hook does not need a DLL module.
	hook, _, err := procSetWindowsHookEx.Call(^uintptr(0), boxMenuFilterProc, 0, uintptr(win.GetCurrentThreadId()))
	if hook == 0 {
		return 0, fmt.Errorf("install tray menu input handler: %w", err)
	}
	activeBoxMenu = session
	defer func() {
		activeBoxMenu = nil
		procUnhookWindowsHookEx.Call(hook)
	}()
	if refresh != nil {
		if win.SetTimer(hwnd, boxRefreshTimer, 50, 0) == 0 {
			return 0, errors.New("start tray menu refresh timer")
		}
		defer win.KillTimer(hwnd, boxRefreshTimer)
	}
	track()
	return session.rightClicked, nil
}
