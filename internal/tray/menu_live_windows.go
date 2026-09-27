//go:build windows

package tray

import (
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/traybox"
)

// refreshBoxMenu updates native menu items from the application's latest
// NIF_ICON/NIF_TIP values. No synthetic attention icon, blinking timer or sound
// is generated: the menu simply shows the frames the program supplied.
// Keep the open menu's row identities stable so a deletion cannot retarget a
// user's click. New registrations appear the next time the menu is opened.
func refreshBoxMenu(menu win.HMENU, icons []traybox.Icon, bitmaps []win.HBITMAP, latest []traybox.Icon) bool {
	changed := false
	size := int(win.GetSystemMetrics(win.SM_CXSMICON))
	for i, before := range icons {
		if i >= len(bitmaps) {
			break
		}
		var current traybox.Icon
		found := false
		for _, candidate := range latest {
			if before.SameIcon(candidate) {
				current, found = candidate, true
				break
			}
		}
		item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STATE}
		if !found || !current.Clickable() {
			item.FState = win.MFS_DISABLED
		}
		if !found {
			if win.SetMenuItemInfo(menu, uint32(trayBoxedBase+i), false, &item) {
				changed = true
			}
			continue
		}
		if current.Image == before.Image && current.Tooltip == before.Tooltip && current.Clickable() == before.Clickable() {
			oldState := win.MENUITEMINFO{CbSize: item.CbSize, FMask: win.MIIM_STATE}
			if win.GetMenuItemInfo(menu, uint32(trayBoxedBase+i), 0, &oldState) && oldState.FState == item.FState {
				continue
			}
		}
		label, err := syscall.UTF16PtrFromString(current.DisplayName())
		if err != nil {
			continue
		}
		item.FMask |= win.MIIM_STRING | win.MIIM_BITMAP
		item.DwTypeData = label
		bitmap := bitmaps[i]
		if current.Image != before.Image {
			bitmap = 0 // a null icon is also an original animation frame
			if current.Image != nil {
				bitmap, err = traybox.MenuBitmap(current.Image, size)
				if err != nil {
					continue
				}
			}
		}
		item.HbmpItem = bitmap
		if !win.SetMenuItemInfo(menu, uint32(trayBoxedBase+i), false, &item) {
			if bitmap != bitmaps[i] && bitmap != 0 {
				win.DeleteObject(win.HGDIOBJ(bitmap))
			}
			continue
		}
		if bitmap != bitmaps[i] {
			if bitmaps[i] != 0 {
				win.DeleteObject(win.HGDIOBJ(bitmaps[i]))
			}
			bitmaps[i] = bitmap
		}
		icons[i] = current
		changed = true
	}
	return changed
}

func redrawOpenBoxMenu() {
	// #32768 is the native popup menu. Only invalidate this thread's menu;
	// Windows continues to own its layout and painting.
	hwnd := win.FindWindow(syscall.StringToUTF16Ptr("#32768"), nil)
	if hwnd != 0 && win.GetWindowThreadProcessId(hwnd, nil) == win.GetCurrentThreadId() {
		win.RedrawWindow(hwnd, nil, 0, win.RDW_INVALIDATE|win.RDW_UPDATENOW)
	}
}
