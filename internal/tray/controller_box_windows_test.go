//go:build windows

package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/i18n"
	"wintray/internal/traybox"
)

func TestCollectedIconsAreDirectMenuCommandsWithOwnBitmaps(t *testing.T) {
	snapshot := func(c color.NRGBA) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	view := traybox.View{Boxed: []traybox.Located{
		{Icon: traybox.Icon{ExePath: `C:\Apps\One.exe`, Tooltip: "First", Snapshot: snapshot(color.NRGBA{R: 255, A: 255})}},
		{Icon: traybox.Icon{ExePath: `C:\Apps\Two.exe`, Tooltip: "Second", Snapshot: snapshot(color.NRGBA{G: 255, A: 255})}},
	}}
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("CreatePopupMenu failed")
	}
	defer win.DestroyMenu(menu)
	bitmaps := new(Controller).appendBox(menu, view, i18n.For("en-US"))
	defer func() {
		for _, bitmap := range bitmaps {
			win.DeleteObject(win.HGDIOBJ(bitmap))
		}
	}()
	if len(bitmaps) != 2 || bitmaps[0] == 0 || bitmaps[1] == 0 || bitmaps[0] == bitmaps[1] {
		t.Fatalf("program-specific icon bitmaps = %v", bitmaps)
	}
	if got := win.GetMenuItemCount(menu); got != 3 {
		t.Fatalf("menu items = %d, want two direct commands and a separator", got)
	}
	for i, bitmap := range bitmaps {
		info := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID | win.MIIM_SUBMENU | win.MIIM_BITMAP}
		if !win.GetMenuItemInfo(menu, uint32(i), win.BOOL(1), &info) || info.WID != uint32(trayBoxedBase+i) || info.HSubMenu != 0 || info.HbmpItem != bitmap {
			t.Errorf("entry %d is not a direct command with its own icon: %+v", i, info)
		}
	}
}
