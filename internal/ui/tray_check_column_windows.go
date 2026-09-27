//go:build windows

package ui

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const lvmGetImageList = win.LVM_FIRST + 2

var (
	commonControls          = windows.NewLazySystemDLL("comctl32.dll")
	setWindowSubclass       = commonControls.NewProc("SetWindowSubclass")
	removeWindowSubclass    = commonControls.NewProc("RemoveWindowSubclass")
	defSubclassProc         = commonControls.NewProc("DefSubclassProc")
	imageListGetIconSize    = commonControls.NewProc("ImageList_GetIconSize")
	trayCheckColumnCallback = syscall.NewCallback(trayCheckColumnWndProc)
	trayCheckColumns        = map[win.HWND]*trayCheckColumn{}
)

type trayCheckColumn struct {
	model       *managedListTableModel
	view        win.HWND
	stateImages win.HIMAGELIST
	width       int32
	height      int32
}

// Reuse the first column's native checkbox images after the list has painted
// each subitem. No cell background is drawn here, so hover and selection stay native.
func installTrayCheckColumn(list *walk.TableView, model *managedListTableModel) error {
	for child := win.GetWindow(list.Handle(), win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		images := win.HIMAGELIST(win.SendMessage(child, lvmGetImageList, win.LVSIL_STATE, 0))
		if images == 0 {
			continue
		}
		column := &trayCheckColumn{model: model, view: child, stateImages: images}
		if ok, _, _ := imageListGetIconSize.Call(uintptr(images), uintptr(unsafe.Pointer(&column.width)), uintptr(unsafe.Pointer(&column.height))); ok == 0 || column.width <= 0 || column.height <= 0 {
			return fmt.Errorf("get native checkbox size")
		}
		runtime.KeepAlive(column)
		viewHandle := list.Handle()
		if ok, _, err := setWindowSubclass.Call(uintptr(viewHandle), trayCheckColumnCallback, 1, 0); ok == 0 {
			return fmt.Errorf("watch tray checkbox column: %w", err)
		}
		trayCheckColumns[viewHandle] = column
		list.Disposing().Attach(func() {
			removeWindowSubclass.Call(uintptr(viewHandle), trayCheckColumnCallback, 1)
			delete(trayCheckColumns, viewHandle)
		})
		return nil
	}
	return fmt.Errorf("native checkbox image list is unavailable")
}

func trayCheckColumnWndProc(hwnd, msg, wp uintptr, lp unsafe.Pointer, id, ref uintptr) uintptr {
	result, _, _ := defSubclassProc.Call(hwnd, msg, wp, uintptr(lp))
	column := trayCheckColumns[win.HWND(hwnd)]
	if column == nil || uint32(msg) != win.WM_NOTIFY || lp == nil {
		return result
	}
	draw := (*win.NMLVCUSTOMDRAW)(lp)
	if draw.Nmcd.Hdr.HwndFrom != column.view || draw.Nmcd.Hdr.Code != win.NM_CUSTOMDRAW || draw.ISubItem != 1 {
		return result
	}
	switch draw.Nmcd.DwDrawStage {
	case win.CDDS_ITEMPREPAINT | win.CDDS_SUBITEM:
		return result&^win.CDRF_SKIPPOSTPAINT | win.CDRF_NOTIFYPOSTPAINT
	case win.CDDS_ITEMPOSTPAINT | win.CDDS_SUBITEM:
		row := int(draw.Nmcd.DwItemSpec)
		if row < 0 || row >= len(column.model.rows) || !column.model.rows[row].CanCollect {
			return result
		}
		rc := draw.Nmcd.Rc
		x := rc.Left + (rc.Right-rc.Left-column.width)/2
		y := rc.Top + (rc.Bottom-rc.Top-column.height)/2
		index := int32(0)
		if column.model.rows[row].Collected {
			index = 1
		}
		win.ImageList_DrawEx(column.stateImages, index, draw.Nmcd.Hdc, x, y, column.width, column.height, win.CLR_DEFAULT, win.CLR_DEFAULT, win.ILD_TRANSPARENT)
	}
	return result
}
