//go:build windows

package tray

import (
	"image"
	"image/color"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/traybox"
)

func TestCollectedIconsAreDirectMenuCommandsWithOwnBitmaps(t *testing.T) {
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
	bitmaps := appendBox(menu, icons)
	defer func() {
		for _, bitmap := range bitmaps {
			win.DeleteObject(win.HGDIOBJ(bitmap))
		}
	}()
	if len(bitmaps) != 3 || bitmaps[2] != 0 || bitmaps[0] == 0 || bitmaps[1] == 0 || bitmaps[0] == bitmaps[1] {
		t.Fatalf("program-specific icon bitmaps = %v", bitmaps)
	}
	if got := win.GetMenuItemCount(menu); got != 4 {
		t.Fatalf("menu items = %d, want three direct commands and a separator", got)
	}
	want := []win.HBITMAP{bitmaps[0], bitmaps[1], 0}
	for i := range icons {
		info := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID | win.MIIM_SUBMENU | win.MIIM_BITMAP | win.MIIM_STATE}
		if !win.GetMenuItemInfo(menu, uint32(i), win.BOOL(1), &info) || info.WID != uint32(trayBoxedBase+i) || info.HSubMenu != 0 || info.HbmpItem != want[i] {
			t.Errorf("entry %d is not a direct command with its own icon: %+v", i, info)
		}
		// None of these test icons registered a callback message.
		if info.FState&win.MFS_DISABLED == 0 {
			t.Errorf("entry %d without a callback message is enabled", i)
		}
	}
}
