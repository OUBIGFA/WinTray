//go:build windows

package orchestrator

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

// startupTaskbar temporarily removes ordinary window buttons through the shell
// API, not TOOLWINDOW/NOACTIVATE styles. Changing those styles can prevent the
// application's later close-to-tray from notifying Explorer of the removal.
// Every method, including close, belongs to the shield's locked OS thread.
type startupTaskbar struct {
	object *win.ITaskbarList3
}

func newStartupTaskbar() (*startupTaskbar, error) {
	if hr := win.CoInitializeEx(nil, win.COINIT_APARTMENTTHREADED); hr < 0 {
		return nil, fmt.Errorf("initialize taskbar COM: HRESULT 0x%08X", uint32(hr))
	}
	var object unsafe.Pointer
	var pin runtime.Pinner
	pin.Pin(&object)
	hr := win.CoCreateInstance(&win.CLSID_TaskbarList, nil, win.CLSCTX_INPROC_SERVER, &win.IID_ITaskbarList3, &object)
	pin.Unpin()
	if hr < 0 || object == nil {
		win.CoUninitialize()
		return nil, fmt.Errorf("create taskbar interface: HRESULT 0x%08X", uint32(hr))
	}
	t := &startupTaskbar{object: (*win.ITaskbarList3)(object)}
	result, _, _ := syscall.SyscallN(t.object.LpVtbl.HrInit, uintptr(object))
	if int32(result) < 0 {
		t.close()
		return nil, fmt.Errorf("initialize taskbar interface: HRESULT 0x%08X", uint32(result))
	}
	return t, nil
}

func (t *startupTaskbar) setVisible(hwnd uintptr, visible bool) error {
	method := t.object.LpVtbl.DeleteTab
	if visible {
		method = t.object.LpVtbl.AddTab
	}
	hr, _, _ := syscall.SyscallN(method, uintptr(unsafe.Pointer(t.object)), hwnd)
	if int32(hr) < 0 {
		return fmt.Errorf("set taskbar visibility=%t: HRESULT 0x%08X", visible, uint32(hr))
	}
	return nil
}

func (t *startupTaskbar) close() {
	syscall.SyscallN(t.object.LpVtbl.Release, uintptr(unsafe.Pointer(t.object)))
	win.CoUninitialize()
}

// Utility/owned windows must not gain a button when the temporary shield is
// released. APPWINDOW explicitly opts an owned window into the taskbar.
func taskbarEligibleWindow(hwnd uintptr) bool {
	style := win.GetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE)
	if style&exToolWindow != 0 || win.GetWindowLong(win.HWND(hwnd), win.GWL_STYLE)&win.WS_CHILD != 0 {
		return false
	}
	if style&win.WS_EX_APPWINDOW != 0 {
		return true
	}
	return style&exNoActivate == 0 && win.GetWindow(win.HWND(hwnd), win.GW_OWNER) == 0
}

func (g *startupVisibility) hideTaskbar(hwnd uintptr, v *veiledWindow) {
	if g.taskbar == nil || !isWindowVisible(hwnd) || !taskbarEligibleWindow(hwnd) {
		return
	}
	// SHOW events and scans repeat this: applications and Explorer can add
	// the button again while startup is still waiting for the native close.
	if err := g.taskbar.setVisible(hwnd, false); err != nil {
		g.warn(hwnd, err.Error())
		return
	}
	v.taskbarHidden = true
}
