//go:build windows

package traybox

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/lxn/win"
)

// Refresh uses the private endpoint, not a temporary topmost taskbar window.
// Repeated refreshes must preserve clients that remember their ADD result.
func TestPrivateBrokerRefreshPreservesRegistration(t *testing.T) {
	f, l := selectedListener(t)
	var installed atomic.Bool
	installed.Store(true)
	f.setReply(func(d trayData) uintptr {
		if d.Message == nimAdd {
			return 0 // Explorer already has the original icon.
		}
		return 1
	})
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 31, flags: nifTip, tip: "refresh fixture"}
	f.mu.Lock()
	f.onRefresh = func() {
		if installed.Load() {
			installed.Store(send(l, add) != 0)
		}
	}
	f.mu.Unlock()
	for i := 0; i < 3; i++ {
		_, _, before := f.snapshot()
		win.SendMessage(l.hwnd, win.WM_TIMER, timerRefresh, 0)
		waitFor(t, "private broker refresh", func() bool {
			_, _, after := f.snapshot()
			return after > before && len(l.snapshot()) == 1
		})
		if !installed.Load() {
			t.Fatal("refresh disabled future native registrations")
		}
	}
}

// Model an application that remembers the result of re-registering its icon,
// as Flutter system_tray does. Explorer keeps the original registration when
// WinTray exits; a duplicate ADD rejection must not disable the application's
// future TaskbarCreated handling or leave the next collector without an icon.
func TestRecollectionKeepsNativeRegistrationAcrossListenerRestarts(t *testing.T) {
	for _, guid := range []bool{false, true} {
		t.Run(fmt.Sprintf("guid=%t", guid), func(t *testing.T) {
			f := startFakeShell(t)
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 7,
				flags: nifMessage | nifIcon | nifTip, callback: testCallback,
				icon: uint32(win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))), tip: "original"}
			if guid {
				add.flags |= nifGUID
				add.guid = [16]byte{1, 7, 8, 9}
			}
			var installed, nativeHidden atomic.Bool
			installed.Store(true) // the application started before the collector
			f.setReply(func(d trayData) uintptr {
				if d.Message == nimAdd {
					return 0 // this registration already exists in Explorer
				}
				if d.Message == nimModify && d.Flags&nifState != 0 {
					nativeHidden.Store(isHidden(d))
				}
				return 1
			})
			var refreshes atomic.Int32
			f.mu.Lock()
			f.onRefresh = func() {
				if !installed.Load() {
					return
				}
				l := activeListener.Load()
				if l == nil {
					return
				}
				add.tip = fmt.Sprintf("native update %d", refreshes.Add(1))
				installed.Store(send(l, add) != 0)
				if installed.Load() {
					send(l, trayRequest{message: nimSetVersion, hwnd: add.hwnd, uid: add.uid, flags: add.flags & nifGUID, guid: add.guid, version: 4})
				}
			}
			f.mu.Unlock()
			cfg := listenerConfig{className: "WinTrayTestTray", target: func(win.HWND) win.HWND { return f.target }}
			for cycle := 0; cycle < 3; cycle++ {
				l, err := startListener(cfg, "", []string{self}, quietLogger{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = l.stop() })
				waitFor(t, "completed icon re-registration", func() bool {
					icons := l.snapshot()
					return !installed.Load() || len(icons) == 1 && icons[0].version == 4
				})
				if !installed.Load() {
					t.Fatalf("cycle %d: duplicate ADD disabled the application's future tray registrations", cycle)
				}
				icons := l.snapshot()
				if len(icons) != 1 || !nativeHidden.Load() || icons[0].Image == nil || !icons[0].Clickable() {
					t.Fatalf("cycle %d: native collection is incomplete: %+v hidden=%t", cycle, icons, nativeHidden.Load())
				}
				requests, events, _ := f.snapshot()
				found := false
				for _, request := range requests {
					if request.Message == nimModify && request.Tip == icons[0].Tooltip && request.Callback == testCallback && request.Icon == add.icon && isHidden(request) {
						found = true
					}
				}
				if !found {
					t.Fatal("duplicate registration did not update Explorer's native icon and callback together")
				}
				before := len(events)
				if err := Activate(icons[0], ActionRightClick); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "native menu callback", func() bool { _, events, _ := f.snapshot(); return len(events) == before+3 })
				if err := l.stop(); err != nil {
					t.Fatal(err)
				}
				if nativeHidden.Load() || !installed.Load() {
					t.Fatal("normal exit did not restore the application's usable native icon")
				}
			}
		})
	}
}

// A timeout/unavailable taskbar is not an explicit ADD rejection. Even if a
// subsequent state-only hide succeeds, never replay its fields/notification
// payload or claim that the original registration completed.
func TestUnconfirmedAddDoesNotReplayFieldsOrReportSuccess(t *testing.T) {
	f := startFakeShell(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var unavailable atomic.Bool
	cfg := listenerConfig{className: "WinTrayTestTray", target: func(win.HWND) win.HWND {
		if unavailable.Swap(false) {
			return 0
		}
		return f.target
	}}
	l, err := startListener(cfg, "", []string{self}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.stop()
	unavailable.Store(true)
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 21, flags: nifTip | 0x10, tip: "unknown result"}
	if result := send(l, add); result != 0 {
		t.Fatal("unconfirmed ADD was reported as successful")
	}
	requests, _, _ := f.snapshot()
	if len(requests) != 1 || requests[0].Message != nimModify || requests[0].Flags != nifState || !isHidden(requests[0]) {
		t.Fatalf("unconfirmed ADD replayed more than its hidden state: %+v", requests)
	}
}

func TestDuplicateRegistrationDoesNotReplayOverReentrantChanges(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		t.Run(fmt.Sprintf("delete=%t", deletion), func(t *testing.T) {
			f, l := selectedListener(t)
			add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 19, flags: nifTip, tip: "initial"}
			send(l, add)
			f.clear()
			f.setReply(func(d trayData) uintptr {
				if d.Message == nimAdd {
					return 0
				}
				return 1
			})
			f.setRequestHook(func(d trayData) {
				if d.Message != nimAdd {
					return
				}
				f.setRequestHook(nil)
				nested := trayRequest{message: nimModify, hwnd: add.hwnd, uid: add.uid, flags: nifTip, tip: "latest"}
				if deletion {
					nested.message = nimDelete
				}
				send(l, nested)
			})
			add.tip = "stale duplicate"
			send(l, add)
			requests, _, _ := f.snapshot()
			for _, d := range requests {
				if d.Message == nimModify && d.Tip == add.tip {
					t.Fatal("duplicate replay overwrote a newer native request")
				}
			}
			icons := l.snapshot()
			if deletion && len(icons) != 0 || !deletion && (len(icons) != 1 || icons[0].Tooltip != "latest") {
				t.Fatalf("reentrant state lost: %+v", icons)
			}
		})
	}
}
