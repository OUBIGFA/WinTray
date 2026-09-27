//go:build windows

package tray

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	comctl32                 = windows.NewLazySystemDLL("comctl32.dll")
	procSetWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass = comctl32.NewProc("RemoveWindowSubclass")
	procDefSubclassProc      = comctl32.NewProc("DefSubclassProc")
	menuUser32               = windows.NewLazySystemDLL("user32.dll")
	procEndMenu              = menuUser32.NewProc("EndMenu")
)

const (
	wmMenuRButtonUp = 0x0122
	boxSubclassID   = 0x57494E54 // "WINT"
	boxRefreshTimer = 0x5742
)

// menuRightClick is the collected icon right-clicked in the open menu. The
// menu runs modally on the UI thread, which is the only thread using it.
var (
	menuRightClick   uint32
	menuRefresh      func()
	menuSubclassProc = syscall.NewCallback(func(hwnd, msg, wParam, lParam, _, _ uintptr) uintptr {
		if msg == win.WM_TIMER && wParam == boxRefreshTimer && menuRefresh != nil {
			menuRefresh()
			return 0
		}
		if msg == wmMenuRButtonUp {
			item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID | win.MIIM_STATE}
			if win.GetMenuItemInfo(win.HMENU(lParam), uint32(wParam), win.BOOL(1), &item) &&
				item.WID >= trayBoxedBase && item.WID < trayIDLimit && item.FState&win.MFS_DISABLED == 0 {
				menuRightClick = item.WID
				procEndMenu.Call()
				return 0
			}
		}
		r, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
		return r
	})
)

// trackBoxRightClicks runs a popup menu owned by hwnd and reports which
// collected icon, if any, was right-clicked instead of selected: that opens
// the program's own tray menu rather than clicking its icon.
func trackBoxRightClicks(hwnd win.HWND, refresh func(), track func()) uint32 {
	menuRightClick = 0
	menuRefresh = refresh
	installed, _, _ := procSetWindowSubclass.Call(uintptr(hwnd), menuSubclassProc, boxSubclassID, 0)
	if installed != 0 {
		if refresh != nil {
			win.SetTimer(hwnd, boxRefreshTimer, 50, 0)
		}
		defer func() {
			win.KillTimer(hwnd, boxRefreshTimer)
			procRemoveWindowSubclass.Call(uintptr(hwnd), menuSubclassProc, boxSubclassID)
		}()
	}
	defer func() { menuRefresh = nil }()
	track()
	return menuRightClick
}
