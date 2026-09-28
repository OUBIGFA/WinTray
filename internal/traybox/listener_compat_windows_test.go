//go:build windows

package traybox

import (
	"bytes"
	"runtime"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

func TestRejectedHiddenAddRollsBackOriginalNativeRegistration(t *testing.T) {
	f, l := selectedListener(t)
	f.clear()
	f.setReply(func(d trayData) uintptr {
		if isHidden(d) {
			return 0
		}
		return 1
	})
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 33, flags: nifMessage | nifTip, callback: testCallback, tip: "legacy icon"}
	if result := send(l, add); result != 1 {
		t.Fatalf("native registration was not recovered: %d", result)
	}
	f.mu.Lock()
	raw := append([][]byte(nil), f.raw...)
	f.mu.Unlock()
	if len(raw) != 3 || !bytes.Equal(raw[2], add.bytes()) {
		t.Fatalf("want rejected hidden ADD, rejected hide, unchanged original ADD; got %d requests", len(raw))
	}
	if icons := l.snapshot(); len(icons) != 0 {
		t.Fatal("failed collection was advertised as collected")
	}
	f.clear()
	update := trayRequest{message: nimModify, hwnd: add.hwnd, uid: add.uid, flags: nifTip, tip: "native update"}
	send(l, update)
	f.mu.Lock()
	raw = append([][]byte(nil), f.raw...)
	f.mu.Unlock()
	if len(raw) != 1 || !bytes.Equal(raw[0], update.bytes()) {
		t.Fatal("failed collection continued intercepting native updates")
	}
	f.clear()
	l.setPaths(nil)
	win.SendMessage(l.hwnd, msgSync, 0, 0)
	if got, _, _ := f.snapshot(); len(got) != 0 {
		t.Fatal("deselection changed the already restored native icon")
	}
}

func TestRejectedCollectionDoesNotResurrectReentrantDeletion(t *testing.T) {
	f, l := selectedListener(t)
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 34, flags: nifTip}
	f.result.Store(0)
	f.setRequestHook(func(d trayData) {
		if d.Message == nimModify {
			f.setRequestHook(nil)
			send(l, trayRequest{message: nimDelete, hwnd: add.hwnd, uid: add.uid})
		}
	})
	send(l, add)
	got, _, _ := f.snapshot()
	adds := 0
	for _, d := range got {
		if d.Message == nimAdd {
			adds++
		}
	}
	if adds != 1 || len(l.snapshot()) != 0 {
		t.Fatal("rollback resurrected an icon the app deleted")
	}
}

func TestUnselectedNativeRequestsAreForwardedUnchanged(t *testing.T) {
	f, l := selectedListener(t)
	l.setPaths([]string{`C:\WinTray-test-only\another-program.exe`})
	win.SendMessage(l.hwnd, msgSync, 0, 0)
	for _, payload := range [][]byte{
		trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 50, flags: nifMessage | nifTip, callback: testCallback, tip: "IDM-like native icon"}.bytes(),
		[]byte("legacy payload unknown to WinTray"),
	} {
		f.clear()
		f.result.Store(7)
		cds := copyDataStruct{DwData: copyDataNotifyIcon, CbData: uint32(len(payload)), LpData: uintptr(unsafe.Pointer(&payload[0]))}
		result := win.SendMessage(l.hwnd, win.WM_COPYDATA, uintptr(f.program), uintptr(unsafe.Pointer(&cds)))
		runtime.KeepAlive(payload)
		f.mu.Lock()
		raw := append([][]byte(nil), f.raw...)
		f.mu.Unlock()
		if result != 7 || len(raw) != 1 || !bytes.Equal(raw[0], payload) || len(l.snapshot()) != 0 {
			t.Fatalf("unselected request changed: result=%d calls=%d", result, len(raw))
		}
	}
}
