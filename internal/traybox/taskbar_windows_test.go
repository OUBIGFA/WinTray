//go:build windows

package traybox

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Explorer can replace or temporarily remove its endpoint during a restart.
func TestTaskbarEndpointFollowsNativeReplacement(t *testing.T) {
	f, l := selectedListener(t)
	name := uintptr(unsafe.Pointer(taskbandProperty))
	defer procRemovePropW.Call(uintptr(f.target), name)
	for _, endpoint := range []win.HWND{f.program, f.target, 0} {
		if endpoint == 0 {
			procRemovePropW.Call(uintptr(f.target), name)
		} else if ok, _, err := procSetPropW.Call(uintptr(f.target), name, uintptr(endpoint)); ok == 0 {
			t.Fatal(err)
		}
		l.syncTaskband(f.target)
		got, _, _ := procGetPropW.Call(uintptr(l.hwnd), name)
		if got != uintptr(endpoint) {
			t.Fatalf("stale taskbar endpoint: got %#x want %#x", got, endpoint)
		}
	}
	procSetPropW.Call(uintptr(f.target), name, uintptr(f.program))
	l.syncTaskband(f.target)
	l.syncTaskband(0)
	if got, _, _ := procGetPropW.Call(uintptr(l.hwnd), name); got != 0 {
		t.Fatalf("missing Explorer retained endpoint %#x", got)
	}
	if !isWindow(f.program) {
		t.Fatal("collector destroyed the borrowed taskbar endpoint")
	}
}

// Menus implemented as ordinary windows use ITaskbarList to hide their taskbar
// button. This must work even when our collector is the first Shell_TrayWnd.
func TestCollectedTaskbarLiveCOMInitialization(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := windows.NewLazySystemDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		t.Fatalf("COM initialization: %#x", hr)
	}
	defer ole.NewProc("CoUninitialize").Call()
	check := func() {
		t.Helper()
		clsid := windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11d0, Data4: [8]byte{0x95, 0x8a, 0, 0x60, 0x97, 0xc9, 0xa0, 0x90}}
		iid := clsid
		iid.Data1 = 0x56FDF342 // ITaskbarList
		var object unsafe.Pointer
		hr, _, _ := ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsid)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&object)))
		if int32(hr) < 0 || object == nil {
			t.Fatalf("ITaskbarList creation: %#x", hr)
		}
		vtable := *(*[8]uintptr)(*(*unsafe.Pointer)(object))
		defer syscall.SyscallN(vtable[2], uintptr(object))
		hr, _, _ = syscall.SyscallN(vtable[3], uintptr(object)) // HrInit
		if int32(hr) < 0 {
			t.Fatalf("ITaskbarList::HrInit failed while collecting: %#x", hr)
		}
	}
	check()
	// No program is selected, so the test never re-registers a user's icon.
	l, err := startListener(listenerConfig{className: trayWindowClass, target: explorerTray, raise: true}, "", nil, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.stop(); err != nil {
			t.Error(err)
		}
	}()
	if win.FindWindow(syscall.StringToUTF16Ptr(trayWindowClass), nil) != l.hwnd {
		t.Fatal("collector is not the first taskbar window")
	}
	check()
}
