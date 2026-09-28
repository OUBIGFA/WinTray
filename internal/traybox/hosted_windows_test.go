//go:build windows

package traybox

import (
	"os"
	"testing"

	"github.com/lxn/win"
)

// The native owner is this process, just like a WinTray-hosted console icon.
// A private shell verifies registration, visibility and callbacks without
// altering Explorer or requiring a real console service.
func TestHostedIconCollectionFollowsProgramAcrossBoxRestarts(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := startFakeShell(t)
	b := NewBox(self, quietLogger{})
	b.config = listenerConfig{className: "WinTrayTestHostedTray", target: func(win.HWND) win.HWND { return f.target }}
	defer func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}()
	path := `C:\WinTray-test-only\syncthing.exe`
	release := b.RegisterHostedIcon(uintptr(f.program), path, func() {
		win.PostMessage(f.program, fakeTaskbar, 0, 0)
	})
	defer release()
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 1, flags: nifMessage | nifTip, callback: testCallback, tip: "Syncthing"}
	for cycle := 0; cycle < 2; cycle++ {
		f.clear()
		_, _, before := f.snapshot()
		if err := b.SetPaths([]string{path, self}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "hosted owner refresh", func() bool { _, _, n := f.snapshot(); return n > before })
		if r := send(b.listener, add); r != 1 {
			t.Fatalf("hosted NIM_ADD returned %d", r)
		}
		icons := b.Icons()
		if len(icons) != 1 || icons[0].ExePath != canonicalIconPath(path) || !icons[0].Clickable() {
			t.Fatalf("hosted icon not associated with selected program: %+v", icons)
		}
		if requests, _, _ := f.snapshot(); len(requests) != 1 || !isHidden(requests[0]) {
			t.Fatalf("hosted icon was not hidden in Explorer: %+v", requests)
		}
		// The main WinTray window has no hosted association and must pass
		// through unchanged, even when its executable is selected.
		f.clear()
		main := add
		main.hwnd = uint32(f.target)
		send(b.listener, main)
		if requests, _, _ := f.snapshot(); len(requests) != 1 || isHidden(requests[0]) || len(b.Icons()) != 1 {
			t.Fatalf("main icon must remain separate: %+v", requests)
		}
		f.clear()
		if err := Activate(icons[0], ActionRightClick); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "hosted context menu callback", func() bool { _, events, _ := f.snapshot(); return len(events) == 2 })
		_, events, _ := f.snapshot()
		if events[0].lParam != win.WM_RBUTTONDOWN || events[1].lParam != win.WM_RBUTTONUP {
			t.Fatalf("hosted right click events: %+v", events)
		}
		if err := b.SetPaths(nil); err != nil {
			t.Fatal(err)
		}
		if requests, _, _ := f.snapshot(); len(requests) != 1 || !isShown(requests[0]) || len(b.Icons()) != 0 {
			t.Fatalf("deselected hosted icon was not restored: %+v", requests)
		}
	}
	// A released owner must not transfer its old identity to a later window.
	release()
	if err := b.SetPaths([]string{path}); err != nil {
		t.Fatal(err)
	}
	f.clear()
	send(b.listener, add)
	if requests, _, _ := f.snapshot(); len(requests) != 1 || isHidden(requests[0]) || len(b.Icons()) != 0 {
		t.Fatalf("released owner kept hosted identity: %+v", requests)
	}
}
