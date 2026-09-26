//go:build windows

package traybox

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

// Only this test's temporary windows/icons are created; no user icon is
// clicked or promoted and no real notification settings are edited.
func TestLocateKeepsDifferentOwnersWithSameUID(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const uid = 4242
	var owners []win.HWND
	for i := 0; i < 2; i++ {
		hwnd := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, 0, 0, 0, 1, 1, 0, 0, 0, nil)
		if hwnd == 0 {
			t.Fatal("create test window failed")
		}
		defer win.DestroyWindow(hwnd)
		nid := win.NOTIFYICONDATA{
			HWnd: hwnd, UID: uid, UFlags: win.NIF_ICON | win.NIF_TIP,
			HIcon: win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION)),
		}
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		copy(nid.SzTip[:], syscall.StringToUTF16("WinTray temporary locate test"))
		if !win.Shell_NotifyIcon(win.NIM_ADD, &nid) {
			t.Fatal("add test icon failed")
		}
		defer win.Shell_NotifyIcon(win.NIM_DELETE, &nid)
		owners = append(owners, hwnd)
	}
	for _, hwnd := range owners {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, ok := iconRect(hwnd, uid); ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("Explorer did not expose test icon")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// Historical registry entries can repeat the same path/UID. Neither
	// duplicate rows nor a shared UID may hide the second actual icon.
	icon := Icon{ExePath: self, UID: uid}
	got := Locate([]Icon{icon, icon}, "")
	if len(got) != 2 {
		t.Fatalf("located %d icons, want both owners once: %+v", len(got), got)
	}
	for _, owner := range owners {
		count := 0
		for _, located := range got {
			if located.Owner == owner {
				count++
			}
		}
		if count != 1 {
			t.Errorf("owner %x found %d times, want once", owner, count)
		}
	}
	if got := Locate([]Icon{icon}, self); len(got) != 0 {
		t.Errorf("self-exclusion returned %d icons", len(got))
	}
}
