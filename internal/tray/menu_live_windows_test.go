//go:build windows

package tray

import (
	"image"
	"image/color"
	"runtime"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/traybox"
)

func TestOpenMenuFollowsOriginalIconFramesAndTooltip(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("CreatePopupMenu")
	}
	defer win.DestroyMenu(menu)
	frame := func(c color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetNRGBA(x, y, c)
			}
		}
		return img
	}
	first := traybox.Icon{ExePath: `C:\test\original.exe`, Tooltip: "original", Image: frame(color.NRGBA{R: 255, A: 255})}
	icons := []traybox.Icon{first}
	bitmaps := appendBox(menu, icons)
	defer func() {
		for _, b := range bitmaps {
			if b != 0 {
				win.DeleteObject(win.HGDIOBJ(b))
			}
		}
	}()
	read := func() win.MENUITEMINFO {
		info := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_BITMAP | win.MIIM_STATE}
		if !win.GetMenuItemInfo(menu, trayBoxedBase, 0, &info) {
			t.Fatal("GetMenuItemInfo")
		}
		return info
	}
	before := read().HbmpItem
	current := first
	current.Image = frame(color.NRGBA{B: 255, A: 255})
	current.Tooltip = "program: unread message"
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) || read().HbmpItem == before || icons[0].Tooltip != current.Tooltip {
		t.Fatal("open menu froze the original icon/tooltip")
	}
	// Program-owned blank/nonblank frames are not replaced with an EXE icon.
	current.Image = nil
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{current}) || read().HbmpItem != 0 {
		t.Fatal("original blank frame was replaced or ignored")
	}
	if !refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{first}) || read().HbmpItem == 0 {
		t.Fatal("original image did not resume")
	}
	other := traybox.Icon{ExePath: `C:\test\different.exe`, Tooltip: "different", Image: current.Image}
	refreshBoxMenu(menu, icons, bitmaps, []traybox.Icon{other})
	if icons[0].ExePath != first.ExePath || read().FState&win.MFS_DISABLED == 0 {
		t.Fatal("removed icon was retargeted to another menu item")
	}
}
