//go:build windows

package traybox

import (
	"github.com/lxn/win"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"
)

// Mirrors an independently launched taskbar widget: discovers the taskbar and
// its native layout before/during/after collection, without being selected.
func TestShellBridgeLiveKeepsUnmanagedTaskbarClientUnchanged(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("interactive live opt-in")
	}
	before := currentTaskbar(t)
	observer := nativeObserver(t)
	check := func() {
		t.Helper()
		if currentTaskbar(t) != before {
			t.Fatal("real taskbar identity/geometry/style/parent changed")
		}
		if win.HWND(observer.request(t, clientTaskbar, 0, 0)) != before.hwnd || win.HWND(observer.request(t, clientLayout, 0, 0)) != before.composition {
			t.Fatal("unmanaged client found a different taskbar/layout")
		}
	}
	check()
	b := NewBox(nativeTestSelfPath(t), quietLogger{})
	defer b.Close()
	for cycle := 0; cycle < 3; cycle++ {
		if err := b.SetPaths([]string{`C:\WinTray-test-only\absent.exe`}); err != nil {
			t.Fatal(err)
		}
		check()
		for _, msg := range []uint32{b.listener.taskbarCreated, win.WM_DISPLAYCHANGE, win.WM_SETTINGCHANGE} {
			win.PostMessage(b.listener.hwnd, msg, 0, 0)
			check()
		}
		// Real Explorer must keep processing non-tray messages while the broker
		// is unavailable; query it with a bounded native SendMessageTimeout.
		var result uintptr
		var pin runtime.Pinner
		pin.Pin(&result)
		ok, _, err := procSendMessageTimeoutW.Call(uintptr(before.hwnd), win.WM_NULL, 0, 0, smtoAbortIfHung, 200, uintptr(unsafe.Pointer(&result)))
		pin.Unpin()
		if ok == 0 {
			t.Fatalf("taskbar unresponsive: %v", err)
		}
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		check()
	}
}
