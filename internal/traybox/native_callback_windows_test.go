//go:build windows

package traybox

import (
	"bytes"
	"os"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

func TestOriginalNotificationPayloadAndSoundFlagsAreForwardedIntact(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 7, flags: nifMessage, callback: testCallback})
	for _, flags := range []uint32{0x1, 0x11, 0x84} { // info, no-sound info, custom/quiet-time
		raw := trayRequest{message: nimModify, hwnd: uint32(f.program), uid: 7, flags: 0x10}.bytes() // NIF_INFO
		for i := 296; i < len(raw); i++ {
			raw[i] = byte(i)
		}
		put(raw, 940, flags) // dwInfoFlags: must not acquire NIIF_NOSOUND
		f.clear()
		cds := copyDataStruct{DwData: copyDataNotifyIcon, CbData: uint32(len(raw)), LpData: uintptr(unsafe.Pointer(&raw[0]))}
		win.SendMessage(l.hwnd, win.WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds)))
		f.mu.Lock()
		got := slices.Clone(f.raw)
		f.mu.Unlock()
		if len(got) != 1 || !bytes.Equal(raw, got[0]) {
			t.Fatalf("native notification flags/payload were rewritten: flags=%x", flags)
		}
	}
}

func TestNativeClickGesturesStayDistinct(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 37, flags: nifMessage, callback: testCallback})
	for _, version := range []uint32{0, 3, 4} {
		send(l, trayRequest{message: nimSetVersion, hwnd: uint32(f.program), uid: 37, version: version})
		for _, tc := range []struct {
			action Action
			events []uint32
		}{
			{ActionClick, []uint32{win.WM_LBUTTONDOWN, win.WM_LBUTTONUP}},
			{ActionDoubleClick, []uint32{win.WM_LBUTTONDBLCLK, win.WM_LBUTTONUP}},
			{ActionRightClick, []uint32{win.WM_RBUTTONDOWN, win.WM_RBUTTONUP}},
		} {
			expected := slices.Clone(tc.events)
			if version >= 3 {
				extra := uint32(win.NIN_SELECT)
				if tc.action == ActionRightClick {
					extra = win.WM_CONTEXTMENU
				}
				expected = append(expected, extra)
			}
			f.clear()
			if err := Activate(l.snapshot()[0], tc.action); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "native gesture", func() bool { _, events, _ := f.snapshot(); return len(events) >= len(expected) })
			_, events, _ := f.snapshot()
			if len(events) != len(expected) {
				t.Fatalf("extra synthetic clicks: %+v", events)
			}
			for i, event := range events {
				if uint32(event.lParam&0xffff) != expected[i] {
					t.Fatalf("gesture=%v version=%d: %+v", tc.action, version, events)
				}
				if version < 4 && event.wParam != 37 || version >= 4 && event.lParam>>16 != 37 {
					t.Fatal("wrong native icon identity")
				}
			}
		}
	}
}

// Opt-in: briefly hosts a real Shell_TrayWnd and one test icon. Run with
// WinTray closed so two collectors cannot race for the real taskbar class.
func TestCollectedIconLiveShell32Callback(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := startFakeShell(t)
	l, err := startListener(listenerConfig{className: trayWindowClass, target: explorerTray, raise: true}, "", []string{self}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.stop(); err != nil {
			t.Error(err)
		}
	}()
	nid := win.NOTIFYICONDATA{HWnd: f.program, UID: 71, UFlags: win.NIF_MESSAGE | win.NIF_ICON | win.NIF_TIP, UCallbackMessage: testCallback, HIcon: win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	if !win.Shell_NotifyIcon(win.NIM_ADD, &nid) {
		t.Fatal("Shell_NotifyIcon ADD")
	}
	defer win.Shell_NotifyIcon(win.NIM_DELETE, &nid)
	waitFor(t, "real hidden icon registration", func() bool { return len(l.snapshot()) == 1 })
	type result struct {
		hr   uintptr
		rect win.RECT
	}
	received := make(chan result, 16)
	f.mu.Lock()
	f.onEvent = func(owner win.HWND, _, event uintptr) {
		if event&0xffff != win.WM_LBUTTONUP {
			return
		}
		id := notifyIconIdentifier{HWnd: owner, UID: 71}
		id.CbSize = uint32(unsafe.Sizeof(id))
		var rect win.RECT
		hr, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&rect)))
		received <- result{hr, rect}
	}
	f.mu.Unlock()
	for _, version := range []uint32{0, 3, 4} {
		nid.UVersion = version
		if !win.Shell_NotifyIcon(win.NIM_SETVERSION, &nid) {
			t.Fatalf("set native version %d", version)
		}
		if err := Activate(l.snapshot()[0], ActionClick); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-received:
			if got.hr != 0 || got.rect.Right <= got.rect.Left || got.rect.Bottom <= got.rect.Top {
				t.Fatalf("native callback dropped its click: version=%d hr=%x rect=%+v", version, got.hr, got.rect)
			}
			t.Logf("version %d: actual Shell32 query inside callback succeeded, rect=%+v", version, got.rect)
		case <-time.After(5 * time.Second):
			t.Fatal("native click callback did not complete")
		}
	}
}
