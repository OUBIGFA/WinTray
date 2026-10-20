//go:build windows

package tray

import (
	"image"
	"image/color"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/i18n"
	"wintray/internal/traybox"
)

func TestContextMenuWithoutCollectedIconsContainsOnlyActions(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			menu := win.CreatePopupMenu()
			if menu == 0 {
				t.Fatal("CreatePopupMenu failed")
			}
			defer win.DestroyMenu(menu)
			msg := i18n.For(language)
			bitmaps, ok := appendContextMenu(menu, msg, nil)
			defer func() {
				for _, bitmap := range bitmaps {
					win.DeleteObject(win.HGDIOBJ(bitmap))
				}
			}()
			if !ok || len(bitmaps) != 0 {
				t.Fatalf("appendContextMenu: ok=%t bitmaps=%v", ok, bitmaps)
			}
			if got := win.GetMenuItemCount(menu); got != 2 {
				t.Fatalf("menu items = %d, want only run silently and exit", got)
			}
			assertTrayMenuActions(t, menu, msg)
		})
	}
}

func TestCollectedIconsHavePlainRowsAndOwnBitmaps(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	solid := func(c color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		return img
	}
	icons := []traybox.Icon{
		{ExePath: `C:\Apps\One.exe`, Tooltip: "First", Image: solid(color.NRGBA{R: 255, A: 255})},
		{ExePath: `C:\Apps\Two.exe`, Tooltip: "Second", Image: solid(color.NRGBA{G: 255, A: 255})},
		// A program that has not set an image yet still gets an entry.
		{ExePath: `C:\Apps\Three.exe`},
	}
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("CreatePopupMenu failed")
	}
	defer win.DestroyMenu(menu)
	msg := i18n.For("zh-CN")
	bitmaps, ok := appendContextMenu(menu, msg, icons)
	defer func() {
		for _, bitmap := range bitmaps {
			win.DeleteObject(win.HGDIOBJ(bitmap))
		}
	}()
	if !ok {
		t.Fatal("appendContextMenu failed")
	}
	if len(bitmaps) != 3 || bitmaps[2] != 0 || bitmaps[0] == 0 || bitmaps[1] == 0 || bitmaps[0] == bitmaps[1] {
		t.Fatalf("program-specific icon bitmaps = %v", bitmaps)
	}
	if got := win.GetMenuItemCount(menu); got != 6 {
		t.Fatalf("root menu items = %d, want two actions, a separator and three individual program rows", got)
	}
	assertTrayMenuActions(t, menu, msg)
	separator := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_FTYPE}
	if !win.GetMenuItemInfo(menu, 2, 1, &separator) || separator.FType&win.MFT_SEPARATOR == 0 {
		t.Fatal("missing separator between WinTray actions and collected icons")
	}
	want := []win.HBITMAP{bitmaps[0], bitmaps[1], 0}
	for i := range icons {
		info := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID | win.MIIM_SUBMENU | win.MIIM_BITMAP | win.MIIM_STATE}
		if !win.GetMenuItemInfo(menu, uint32(i+3), win.BOOL(1), &info) || info.WID != uint32(trayBoxedBase+i) || info.HSubMenu != 0 || info.HbmpItem != want[i] {
			t.Errorf("entry %d must be a plain command with its own icon: %+v", i, info)
		}
		// None of these test icons registered a callback message.
		if info.FState&win.MFS_DISABLED == 0 {
			t.Errorf("entry %d without a callback message is enabled", i)
		}
	}
}

// Read the native rows so labels, order and enabled state are checked together.
func assertTrayMenuActions(t *testing.T, menu win.HMENU, msg i18n.Messages) {
	t.Helper()
	for position, want := range []menuItem{
		{id: trayActionRunSilently, text: msg.RunSilently, bitmap: win.HBMMENU_POPUP_MINIMIZE},
		{id: trayActionExit, text: msg.TrayExit, bitmap: win.HBMMENU_POPUP_CLOSE},
	} {
		var label [128]uint16
		info := win.MENUITEMINFO{
			CbSize:     uint32(unsafe.Sizeof(win.MENUITEMINFO{})),
			FMask:      win.MIIM_ID | win.MIIM_STRING | win.MIIM_FTYPE | win.MIIM_STATE | win.MIIM_BITMAP,
			DwTypeData: &label[0],
			Cch:        uint32(len(label)),
		}
		if !win.GetMenuItemInfo(menu, uint32(position), 1, &info) {
			t.Fatalf("read action at position %d", position)
		}
		if info.HbmpItem != want.bitmap {
			t.Errorf("action %q has bitmap %d, want native glyph %d", want.text, info.HbmpItem, want.bitmap)
		}
		if info.WID != want.id || syscall.UTF16ToString(label[:]) != want.text ||
			info.FType&win.MFT_SEPARATOR != 0 || info.FState&win.MFS_DISABLED != 0 {
			t.Errorf("action at position %d = id %d, text %q, type %d, state %d; want enabled %q (id %d)",
				position, info.WID, syscall.UTF16ToString(label[:]), info.FType, info.FState, want.text, want.id)
		}
	}
}
