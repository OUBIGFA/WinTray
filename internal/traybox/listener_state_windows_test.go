//go:build windows

package traybox

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxn/win"
)

func selectedListener(t *testing.T) (*fakeShell, *listener) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := startFakeShell(t)
	l, err := startListener(listenerConfig{
		className: "WinTrayTestTray",
		target:    func(win.HWND) win.HWND { return f.target },
	}, "", []string{self}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.setRequestHook(nil)
		f.setReply(nil)
		f.result.Store(1)
		if err := l.stop(); err != nil {
			t.Errorf("stop test listener: %v", err)
		}
	})
	waitFor(t, "initial registration request", func() bool { _, _, n := f.snapshot(); return n > 0 })
	return f, l
}

func TestListenerGUIDOnlyUpdatesKeepOwnerAndHiddenState(t *testing.T) {
	f, l := selectedListener(t)
	guid := [16]byte{9, 1, 2, 3}
	owner := uint32(f.program)
	send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 17, flags: nifGUID | nifMessage | nifTip,
		guid: guid, callback: testCallback, tip: "before"})
	f.clear()
	send(l, trayRequest{message: nimModify, flags: nifGUID | nifTip | nifIcon | nifState, guid: guid,
		tip: "after", icon: uint32(win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_WARNING))), mask: nisHidden})
	icons := l.snapshot()
	if len(icons) != 1 || icons[0].Tooltip != "after" || icons[0].Image == nil || icons[0].owner != owner || icons[0].uid != 17 {
		t.Fatalf("GUID-only update lost current state or callback target: %+v", icons)
	}
	if got, _, _ := f.snapshot(); len(got) != 1 || !isHidden(got[0]) {
		t.Fatalf("GUID-only show request was not kept hidden: %+v", got)
	}
	send(l, trayRequest{message: nimSetVersion, flags: nifGUID, guid: guid, version: 4})
	if icons = l.snapshot(); len(icons) != 1 || icons[0].version != 4 || icons[0].owner != owner || icons[0].uid != 17 {
		t.Fatalf("GUID-only version update lost callback target: %+v", icons)
	}
	f.clear()
	if err := Activate(icons[0], ActionRightClick); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "GUID icon callback", func() bool { _, e, _ := f.snapshot(); return len(e) == 3 })
	_, events, _ := f.snapshot()
	if events[2].lParam != makeLong(win.WM_CONTEXTMENU, 17) {
		t.Fatalf("GUID callback = %#x, want original uID 17", events[2].lParam)
	}
	send(l, trayRequest{message: nimDelete, flags: nifGUID, guid: guid})
	if got := l.snapshot(); len(got) != 0 {
		t.Fatalf("GUID-only delete left icons: %+v", got)
	}
}

func TestListenerReentrantRequestsKeepLatestState(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		name := "update"
		if deletion {
			name = "delete"
		}
		t.Run(name, func(t *testing.T) {
			f, l := selectedListener(t)
			owner := uint32(f.program)
			send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 3, flags: nifTip, tip: "initial"})
			f.setRequestHook(func(trayData) {
				f.setRequestHook(nil)
				nested := trayRequest{message: nimModify, hwnd: owner, uid: 3, flags: nifTip, tip: "latest"}
				if deletion {
					nested.message = nimDelete
				}
				send(l, nested)
			})
			send(l, trayRequest{message: nimModify, hwnd: owner, uid: 3, flags: nifTip, tip: "outer"})
			icons := l.snapshot()
			if deletion {
				if len(icons) != 0 {
					t.Fatalf("outer update resurrected the deleted icon: %+v", icons)
				}
			} else if len(icons) != 1 || icons[0].Tooltip != "latest" {
				t.Fatalf("outer update overwrote the nested update: %+v", icons)
			}
		})
	}
}

func TestListenerReentrantVersionDoesNotChangeReplacementIcon(t *testing.T) {
	f, l := selectedListener(t)
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 8, flags: nifTip, tip: "initial"}
	send(l, add)
	f.setRequestHook(func(trayData) {
		f.setRequestHook(nil)
		send(l, trayRequest{message: nimDelete, hwnd: add.hwnd, uid: add.uid})
		add.tip = "replacement"
		send(l, add)
	})
	send(l, trayRequest{message: nimSetVersion, hwnd: add.hwnd, uid: add.uid, version: 4})
	if icons := l.snapshot(); len(icons) != 1 || icons[0].Tooltip != "replacement" || icons[0].version != 0 {
		t.Fatalf("old SETVERSION changed a replacement icon: %+v", icons)
	}
}

func TestListenerRetriesFailedDeselectionRestore(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 4, flags: nifTip, tip: "restore me"})
	f.clear()
	f.result.Store(0)
	defer f.result.Store(1)
	l.setPaths([]string{`C:\WinTray-test-only\other.exe`})
	waitFor(t, "first restore attempt", func() bool { r, _, _ := f.snapshot(); return len(r) > 0 })
	if got := l.snapshot(); len(got) != 0 {
		t.Fatalf("deselected icon still listed: %+v", got)
	}
	f.result.Store(1)
	waitFor(t, "retry after Explorer recovers", func() bool { r, _, _ := f.snapshot(); return len(r) >= 2 })
	if got, _, _ := f.snapshot(); !isShown(got[len(got)-1]) {
		t.Fatalf("retry did not restore visibility: %+v", got)
	}
}

func TestBoxFailedStopKeepsListenerAndSelectionForRetry(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 4, flags: nifTip, tip: "restore me"})
	icons := l.snapshot()
	b := &Box{listener: l, paths: []string{icons[0].ExePath}, logger: quietLogger{}, config: l.cfg}
	f.clear()
	f.result.Store(0)
	defer f.result.Store(1)
	if err := b.SetPaths(nil); err == nil {
		t.Fatal("stopping reported success even though Explorer rejected the restore")
	}
	if !b.HasSelection() || b.listener != l || len(b.Icons()) != 1 || activeListener.Load() != l {
		t.Fatal("failed stop lost the active selection or listener")
	}
	f.result.Store(1)
	if err := b.SetPaths(nil); err != nil {
		t.Fatal(err)
	}
	if b.HasSelection() || b.listener != nil || activeListener.Load() != nil {
		t.Fatal("successful retry did not stop the box")
	}
	got, _, _ := f.snapshot()
	if len(got) < 2 || !isShown(got[len(got)-1]) {
		t.Fatalf("retry did not restore the icon: %+v", got)
	}
}

func TestListenerPendingRestoreRespectsProgramHidingItsIcon(t *testing.T) {
	f, l := selectedListener(t)
	owner := uint32(f.program)
	send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 4})
	f.clear()
	f.result.Store(0)
	defer f.result.Store(1)
	l.setPaths([]string{`C:\WinTray-test-only\other.exe`})
	waitFor(t, "failed restore", func() bool { r, _, _ := f.snapshot(); return len(r) > 0 })
	f.result.Store(1)
	send(l, trayRequest{message: nimModify, hwnd: owner, uid: 4, flags: nifState, state: nisHidden, mask: nisHidden})
	f.clear()
	win.SendMessage(l.hwnd, msgSync, 0, 0)
	if got, _, _ := f.snapshot(); len(got) != 0 {
		t.Fatalf("retry overrode the program's own hidden state: %+v", got)
	}
}

func TestListenerShutdownDoesNotHideReentrantUpdates(t *testing.T) {
	f, l := selectedListener(t)
	owner := uint32(f.program)
	send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 4})
	f.clear()
	f.setRequestHook(func(d trayData) {
		if isShown(d) {
			f.setRequestHook(nil)
			send(l, trayRequest{message: nimModify, hwnd: owner, uid: 4, flags: nifState, mask: nisHidden})
		}
	})
	if err := l.stop(); err != nil {
		t.Fatal(err)
	}
	got, _, _ := f.snapshot()
	if len(got) != 2 || !isShown(got[0]) || !isShown(got[1]) {
		t.Fatalf("shutdown hid a reentrant update: %+v", got)
	}
}

func TestListenerShutdownKeepsProgramHiddenIconsHidden(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 4, flags: nifState, state: nisHidden, mask: nisHidden})
	f.clear()
	if err := l.stop(); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := f.snapshot(); len(got) != 0 {
		t.Fatalf("shutdown showed an icon hidden by its program: %+v", got)
	}
}

func TestListenerStopWaitsForAllSlowRestores(t *testing.T) {
	f, l := selectedListener(t)
	for _, uid := range []uint32{1, 2} {
		send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: uid})
	}
	f.clear()
	f.setRequestHook(func(d trayData) {
		if isShown(d) {
			// Each call is within forwardTimeout, but together exceed the
			// old five-second stop timeout that falsely reported success.
			time.Sleep(2700 * time.Millisecond)
		}
	})
	if err := l.stop(); err != nil {
		t.Fatal(err)
	}
	if activeListener.Load() != nil {
		t.Fatal("stop returned while the listener was still restoring icons")
	}
	if got, _, _ := f.snapshot(); len(got) != 2 || !isShown(got[0]) || !isShown(got[1]) {
		t.Fatalf("stop did not finish both restores: %+v", got)
	}
}

func TestBoxIgnoresEmptyAndOwnPaths(t *testing.T) {
	b := NewBox(`C:\Apps\WinTray.exe`, quietLogger{})
	if err := b.SetPaths([]string{"", `c:\apps\WINTRAY.EXE`}); err != nil {
		t.Fatal(err)
	}
	if b.HasSelection() || b.listener != nil {
		t.Fatal("empty or own paths started a listener")
	}
}

func TestListenerRejectedRegistrationDoesNotBlockRestoration(t *testing.T) {
	f, l := selectedListener(t)
	var allowAdd atomic.Bool
	existing := make(map[iconID]bool) // accessed only by the fake-shell thread
	f.setReply(func(d trayData) uintptr {
		id := idOf(d)
		switch d.Message {
		case nimAdd:
			if !allowAdd.Load() || existing[id] {
				return 0
			}
			existing[id] = true
		case nimModify:
			if !existing[id] {
				return 0
			}
		case nimDelete:
			delete(existing, id)
		}
		return 1
	})
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 6, flags: nifTip, tip: "fixture"}
	if result := send(l, add); result != 0 {
		t.Fatal("fake shell did not reject the initial registration")
	}
	if icons := l.snapshot(); len(icons) != 0 {
		t.Fatalf("a rejected registration left a phantom icon requiring restoration: %+v", icons)
	}
	allowAdd.Store(true)
	send(l, add)
	// A rejected duplicate ADD must still keep the real icon, since its
	// explicit hiding MODIFY succeeds against an existing registration.
	if result := send(l, add); result != 0 {
		t.Fatal("fake shell accepted a duplicate registration")
	}
	if icons := l.snapshot(); len(icons) != 1 {
		t.Fatalf("a real icon was lost after duplicate registration: %+v", icons)
	}
	if err := l.stop(); err != nil {
		t.Fatal(err)
	}
}

func TestListenerFailedStopKeepsIconsAddedDuringRestoration(t *testing.T) {
	f, l := selectedListener(t)
	owner := uint32(f.program)
	send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 1, flags: nifTip, tip: "first"})
	f.result.Store(0)
	defer f.result.Store(1)
	f.setRequestHook(func(d trayData) {
		if isShown(d) {
			f.setRequestHook(nil)
			f.result.Store(1)
			send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 2, flags: nifTip, tip: "during stop"})
			f.result.Store(0)
		}
	})
	if err := l.stop(); err == nil {
		t.Fatal("failed restoration reported success")
	}
	if icons := l.snapshot(); len(icons) != 2 || icons[1].Tooltip != "during stop" {
		t.Fatalf("aborted shutdown lost the new icon: %+v", icons)
	}
}

func TestListenerReentrantVersionUsesLatestSuccessfulRequest(t *testing.T) {
	for _, nestedSucceeds := range []bool{false, true} {
		name := "nested rejection"
		if nestedSucceeds {
			name = "nested success"
		}
		t.Run(name, func(t *testing.T) {
			f, l := selectedListener(t)
			owner := uint32(f.program)
			send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 6})
			f.setRequestHook(func(trayData) {
				f.setRequestHook(nil)
				if !nestedSucceeds {
					f.result.Store(0)
				}
				send(l, trayRequest{message: nimSetVersion, hwnd: owner, uid: 6, version: 3})
				f.result.Store(1)
			})
			send(l, trayRequest{message: nimSetVersion, hwnd: owner, uid: 6, version: 4})
			want := uint32(4)
			if nestedSucceeds {
				want = 3
			}
			if got := l.snapshot()[0].version; got != want {
				t.Fatalf("callback version = %d, want latest successful request %d", got, want)
			}
		})
	}
}

func TestListenerControlMessagesDoNotReplaceIconFields(t *testing.T) {
	f, l := selectedListener(t)
	old := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 5,
		flags: nifMessage | nifTip | nifIcon, callback: testCallback, tip: "old",
		icon: uint32(win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION)))}
	send(l, old)
	send(l, trayRequest{message: nimModify, hwnd: old.hwnd, uid: old.uid,
		flags: nifTip | nifIcon, tip: "current", icon: uint32(win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_WARNING)))})
	current := l.snapshot()[0]
	for _, message := range []uint32{3, nimSetVersion} { // NIM_SETFOCUS, NIM_SETVERSION
		old.message, old.version = message, 4
		send(l, old)
		icons := l.snapshot()
		if len(icons) != 1 || icons[0].Tooltip != current.Tooltip || icons[0].Image != current.Image || icons[0].callback != current.callback {
			t.Errorf("control message %d replaced icon fields: %+v", message, icons)
		}
	}
	if got := l.snapshot()[0].version; got != 4 {
		t.Errorf("successful SETVERSION = %d, want 4", got)
	}
	f.result.Store(0)
	old.version = 99
	send(l, old)
	f.result.Store(1)
	if got := l.snapshot()[0].version; got != 4 {
		t.Errorf("rejected SETVERSION changed version to %d", got)
	}
	for _, message := range []uint32{3, nimSetVersion, 99} {
		send(l, trayRequest{message: message, hwnd: old.hwnd, uid: 77, version: 4})
	}
	if icons := l.snapshot(); len(icons) != 1 {
		t.Errorf("control messages created phantom icons: %+v", icons)
	}
}
