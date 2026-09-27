//go:build windows

package ui

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

const lvmGetItemCount = win.LVM_FIRST + 4

var (
	listReorderCallback = syscall.NewCallback(listReorderWndProc)
	listReorders        = map[win.HWND]*listReorder{}
)

// listReorder lets the user drag a row of a TableView to another position.
// The list view reports a drag it has recognized itself (LVN_BEGINDRAG), so a
// click, a check box toggle or the tray column click is never mistaken for
// one. While the row is dragged it moves along with the pointer; the new
// order is reported once, when the button is released.
type listReorder struct {
	table    win.HWND // the TableView: parent of the list view and capture owner
	view     win.HWND // the native list view that shows the rows
	dragging bool
	row      int  // the dragged row's current position
	moved    bool // whether the dragged row left its start position
	move     func(from, to int)
	finished func()
}

func installListReorder(list *walk.TableView, view win.HWND, move func(from, to int), finished func()) error {
	table := list.Handle()
	reorder := &listReorder{table: table, view: view, move: move, finished: finished}
	if ok, _, err := setWindowSubclass.Call(uintptr(table), listReorderCallback, 2, 0); ok == 0 {
		return fmt.Errorf("watch program list drags: %w", err)
	}
	listReorders[table] = reorder
	list.Disposing().Attach(func() {
		removeWindowSubclass.Call(uintptr(table), listReorderCallback, 2)
		delete(listReorders, table)
	})
	return nil
}

func listReorderWndProc(hwnd, msg, wp uintptr, lp unsafe.Pointer, id, ref uintptr) uintptr {
	reorder := listReorders[win.HWND(hwnd)]
	if reorder == nil {
		result, _, _ := defSubclassProc.Call(hwnd, msg, wp, uintptr(lp))
		return result
	}
	switch uint32(msg) {
	case win.WM_NOTIFY:
		header := (*win.NMHDR)(lp)
		if lp != nil && header.HwndFrom == reorder.view && header.Code == uint32(win.LVN_BEGINDRAG) {
			reorder.begin(int((*win.NMLISTVIEW)(lp).IItem))
			return 0
		}
	case win.WM_MOUSEMOVE:
		if reorder.dragging {
			reorder.follow(uintptr(unsafe.Pointer(lp)))
			return 0
		}
	case win.WM_LBUTTONUP:
		if reorder.dragging {
			win.ReleaseCapture() // ends the drag through WM_CAPTURECHANGED
			return 0
		}
	case win.WM_CAPTURECHANGED:
		if reorder.dragging {
			reorder.end()
		}
	}
	result, _, _ := defSubclassProc.Call(hwnd, msg, wp, uintptr(lp))
	return result
}

func (r *listReorder) begin(row int) {
	if row < 0 {
		return
	}
	r.dragging, r.row, r.moved = true, row, false
	win.SetCapture(r.table)
}

// follow moves the dragged row to the row at the pointer's height, so the
// pointer may leave the list sideways. Above the rows (over the header or the
// list) it steps up one row, scrolling; below them it goes last.
func (r *listReorder) follow(lParam uintptr) {
	point := win.POINT{X: win.GET_X_LPARAM(lParam), Y: win.GET_Y_LPARAM(lParam)}
	win.ClientToScreen(r.table, &point)
	win.ScreenToClient(r.view, &point)
	count := int(win.SendMessage(r.view, lvmGetItemCount, 0, 0))
	top := int(win.SendMessage(r.view, win.LVM_GETTOPINDEX, 0, 0))
	first := win.RECT{Left: win.LVIR_BOUNDS}
	win.SendMessage(r.view, win.LVM_GETITEMRECT, uintptr(top), uintptr(unsafe.Pointer(&first)))
	hit := win.LVHITTESTINFO{Pt: win.POINT{X: first.Left + 1, Y: point.Y}}
	win.SendMessage(r.view, win.LVM_HITTEST, 0, uintptr(unsafe.Pointer(&hit)))
	runtime.KeepAlive(&hit)
	runtime.KeepAlive(&first)
	target := int(hit.IItem)
	if target < 0 {
		target = count - 1
		if point.Y < first.Top {
			target = top - 1
		}
		target = max(target, 0)
	}
	if target == r.row || target >= count {
		return
	}
	r.move(r.row, target)
	r.row, r.moved = target, true
	win.SendMessage(r.view, win.LVM_ENSUREVISIBLE, uintptr(target), 0)
}

func (r *listReorder) end() {
	r.dragging = false
	if r.moved && r.finished != nil {
		r.finished()
	}
}
