//go:build windows

package traybox

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Explicit opt-in only: unlike fixture tests this briefly collects existing
// user-selected running programs. It never restarts them or saves settings.
func TestExistingOwnersLiveCollection(t *testing.T) {
	paths := strings.Split(os.Getenv("WINTRAY_EXISTING_OWNER_PATHS"), "|")
	if len(paths) == 1 && paths[0] == "" {
		t.Skip("set WINTRAY_EXISTING_OWNER_PATHS explicitly")
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("must verify ordinary user privilege")
	}
	for _, path := range paths {
		if len(processesByPath(map[string]bool{canonicalIconPath(path): true})) == 0 {
			t.Fatalf("requested existing owner is not running: %s", path)
		}
	}
	var collected []Icon
	b := NewBox(nativeTestSelfPath(t), testLogger{t})
	defer func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := b.SetPaths(paths); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		canonical := canonicalIconPath(path)
		var icon Icon
		waitFor(t, "existing owner collected: "+path, func() bool {
			for _, item := range b.Icons() {
				if item.ExePath == canonical {
					icon = item
					return true
				}
			}
			return false
		})
		if icon.Image == nil || !icon.Clickable() {
			t.Fatalf("missing native image/callback: %+v", icon)
		}
		collected = append(collected, icon)
		b.listener.mu.Lock()
		record := b.listener.icons[icon.key]
		original := append([]byte(nil), record.template...)
		b.listener.mu.Unlock()
		waitFor(t, "original icon hidden: "+path, func() bool { return !b.listener.shownByExplorer(original) })
		var pid uint32
		win.GetWindowThreadProcessId(win.HWND(icon.owner), &pid)
		t.Logf("collected %s pid=%d owner=%#x uid=%d callback=%d", icon.DisplayName(), pid, icon.owner, icon.uid, icon.callback)
		if os.Getenv("WINTRAY_EXISTING_OWNER_CLICK") == "1" {
			before := map[win.HWND]bool{}
			for _, hwnd := range allWindows() {
				if win.IsWindowVisible(hwnd) {
					before[hwnd] = true
				}
			}
			if err := Activate(icon, ActionRightClick); err != nil {
				t.Fatal(err)
			}
			var menu win.HWND
			waitFor(t, "actual native menu: "+path, func() bool {
				for _, hwnd := range allWindows() {
					var owner uint32
					win.GetWindowThreadProcessId(hwnd, &owner)
					if owner == pid && win.IsWindowVisible(hwnd) && !before[hwnd] {
						var cls [128]uint16
						win.GetClassName(hwnd, &cls[0], len(cls))
						t.Logf("new native menu window hwnd=%#x class=%s", hwnd, syscall.UTF16ToString(cls[:]))
						menu = hwnd
						return true
					}
				}
				return false
			})
			win.PostMessage(win.HWND(icon.owner), win.WM_CANCELMODE, 0, 0)
			win.PostMessage(menu, win.WM_KEYDOWN, win.VK_ESCAPE, 0)
			win.PostMessage(menu, win.WM_KEYUP, win.VK_ESCAPE, 0)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	for _, icon := range collected {
		request := trayRequest{hwnd: icon.owner, uid: icon.uid}
		if icon.key.guid != [16]byte{} {
			request.flags = nifGUID
			request.guid = icon.key.guid
		}
		waitFor(t, "original icon restored: "+icon.DisplayName(), func() bool { return (&listener{}).shownByExplorer(request.bytes()) })
	}
}

type testLogger struct{ t *testing.T }

func (l testLogger) Info(s string) { l.t.Log(s) }
func (l testLogger) Warn(s string) { l.t.Log(s) }
