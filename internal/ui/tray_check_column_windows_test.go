//go:build windows

package ui

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

// Selection/hot-item post-paint notifications can carry an empty rc. Exercise the actual
// subclass on a memory DC: the native checkbox must stay in its tray cell,
// with no pixels appearing at the list/header origin.
func checkTrayCheckboxPostPaint(t *testing.T, w *MainWindow) {
	t.Helper()
	column := trayCheckColumns[w.managedList.Handle()]
	screen := win.GetDC(column.view)
	defer win.ReleaseDC(column.view, screen)
	dc := win.CreateCompatibleDC(screen)
	defer win.DeleteDC(dc)
	bitmap := win.CreateCompatibleBitmap(screen, 1024, 512)
	if dc == 0 || bitmap == 0 {
		t.Fatal("create checkbox paint surface")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(dc, win.HGDIOBJ(bitmap))
	defer win.SelectObject(dc, previous)
	win.BitBlt(dc, 0, 0, 1024, 512, 0, 0, 0, win.WHITENESS)
	draw := &win.NMLVCUSTOMDRAW{ISubItem: 1}
	draw.Nmcd.Hdr.HwndFrom = column.view
	draw.Nmcd.Hdr.Code = win.NM_CUSTOMDRAW
	draw.Nmcd.DwDrawStage = win.CDDS_ITEMPOSTPAINT | win.CDDS_SUBITEM
	draw.Nmcd.Hdc = dc
	draw.Nmcd.DwItemSpec = 0
	var pinned runtime.Pinner
	pinned.Pin(draw)
	defer pinned.Unpin()
	win.SendMessage(w.managedList.Handle(), win.WM_NOTIFY, 0, uintptr(unsafe.Pointer(draw)))
	white := win.COLORREF(0xFFFFFF)
	for y := int32(0); y < column.height; y++ {
		for x := int32(0); x < column.width; x++ {
			if win.GetPixel(dc, x, y) != white {
				t.Fatalf("post-paint drew a stray checkbox at the list origin (%d,%d)", x, y)
			}
		}
	}
	rc := win.RECT{Top: 1, Left: win.LVIR_BOUNDS}
	if win.SendMessage(column.view, win.LVM_GETSUBITEMRECT, 0, uintptr(unsafe.Pointer(&rc))) == 0 {
		t.Fatal("get tray checkbox cell")
	}
	for y := rc.Top; y < rc.Bottom; y++ {
		for x := rc.Left; x < rc.Right; x++ {
			if win.GetPixel(dc, x, y) != white {
				return
			}
		}
	}
	t.Fatal("post-paint failed to draw the checkbox in its actual cell")
}
