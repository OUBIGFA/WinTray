//go:build windows

package traybox

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func TestBrokerRejectsTaskbarClass(t *testing.T) {
	for _, name := range []string{trayWindowClass, "shell_traywnd"} {
		l, err := startListener(listenerConfig{className: name}, "", nil, quietLogger{})
		if l != nil || err == nil {
			t.Fatalf("allowed a fake taskbar: %s, %v", name, err)
		}
	}
}

type taskbarIdentity struct {
	hwnd, parent win.HWND
	pid          uint32
	bounds       win.RECT
	style, ex    int32
	endpoint     uintptr
	composition  win.HWND
}

func currentTaskbar(t *testing.T) taskbarIdentity {
	t.Helper()
	hwnd := win.FindWindow(syscall.StringToUTF16Ptr(trayWindowClass), nil)
	if hwnd == 0 {
		t.Skip("no Explorer taskbar on this desktop")
	}
	identity := taskbarIdentity{hwnd: hwnd, parent: win.GetParent(hwnd),
		style: win.GetWindowLong(hwnd, win.GWL_STYLE), ex: win.GetWindowLong(hwnd, win.GWL_EXSTYLE)}
	win.GetWindowThreadProcessId(hwnd, &identity.pid)
	win.GetWindowRect(hwnd, &identity.bounds)
	identity.endpoint, _, _ = user32.NewProc("GetPropW").Call(uintptr(hwnd), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("TaskbandHWND"))))
	// This is the same layout discriminator used by taskbar widgets. A fake
	// taskbar lacks the native child and selects the wrong embedding layout.
	child, _, _ := procFindWindowExW.Call(uintptr(hwnd), 0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Windows.UI.Composition.DesktopWindowContentBridge"))), 0)
	identity.composition = win.HWND(child)
	return identity
}

func TestBrokerPreservesNativeTaskbarIdentityAndGeometry(t *testing.T) {
	before := currentTaskbar(t)
	cfg := NewBox("", quietLogger{}).config
	cfg.newRecovery = nil // no icons are hidden in this invariant test
	l, err := startListener(cfg, "", nil, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.stop()
	if win.GetWindowLong(l.hwnd, win.GWL_EXSTYLE)&win.WS_EX_TOPMOST != 0 || win.IsWindowVisible(l.hwnd) {
		t.Fatal("broker is visible or competes in the topmost window band")
	}
	for _, message := range []uint32{l.taskbarCreated, win.WM_DISPLAYCHANGE, win.WM_SETTINGCHANGE, win.WM_TIMER} {
		win.SendMessage(l.hwnd, message, timerHeal, 0)
		if after := currentTaskbar(t); after != before {
			t.Fatalf("broker changed native taskbar lookup/layout: before=%+v after=%+v", before, after)
		}
	}
	if err := l.stop(); err != nil {
		t.Fatal(err)
	}
	if after := currentTaskbar(t); after != before {
		t.Fatal("broker shutdown changed the native taskbar")
	}
}

type stalledBrokerLogger struct {
	once             sync.Once
	entered, release chan struct{}
}

func (l *stalledBrokerLogger) Info(string) { l.once.Do(func() { close(l.entered); <-l.release }) }
func (*stalledBrokerLogger) Warn(string)   {}

func TestBusyBrokerDoesNotReplaceTaskbar(t *testing.T) {
	before := currentTaskbar(t)
	log := &stalledBrokerLogger{entered: make(chan struct{}), release: make(chan struct{})}
	l, err := startListener(listenerConfig{className: brokerWindowClass, target: explorerTray}, "",
		[]string{`C:\WinTray-test-only\absent.exe`}, log)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { close(log.release); l.stop() }()
	select {
	case <-log.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("collector did not reach the controlled stall")
	}
	started := time.Now()
	for i := 0; i < 10; i++ {
		if got := currentTaskbar(t); got != before {
			t.Fatal("a blocked collector replaced the taskbar seen by other programs")
		}
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("native taskbar lookup stalled with the broker: %s", elapsed)
	}
}

// Native tray menus use ITaskbarList to hide their own taskbar button. The
// private broker must leave COM initialization on Explorer's real endpoint.
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
		iid.Data1 = 0x56FDF342
		var object unsafe.Pointer
		hr, _, _ := ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&clsid)), 0, 1, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&object)))
		if int32(hr) < 0 || object == nil {
			t.Fatalf("ITaskbarList creation: %#x", hr)
		}
		vtable := *(*[8]uintptr)(*(*unsafe.Pointer)(object))
		defer syscall.SyscallN(vtable[2], uintptr(object))
		hr, _, _ = syscall.SyscallN(vtable[3], uintptr(object))
		if int32(hr) < 0 {
			t.Fatalf("ITaskbarList::HrInit failed while collecting: %#x", hr)
		}
	}
	check()
	before := currentTaskbar(t)
	l, err := startListener(listenerConfig{className: brokerWindowClass, target: explorerTray, native: true}, "", nil, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.stop()
	attachNativeTestBridge(t, l)
	if currentTaskbar(t) != before {
		t.Fatal("collector replaced the taskbar window")
	}
	check()
}
