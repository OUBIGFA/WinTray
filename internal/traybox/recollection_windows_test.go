//go:build windows

package traybox

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lxn/win"
)

// A delayed refresh may expire during the next taskbar gesture. Sending it
// while the proxy has yielded makes the client's duplicate ADD reach Explorer
// directly, permanently disabling clients that remember a failed registration.
func TestRefreshPreservesRegistrationDuringTaskbarGesture(t *testing.T) {
	f := startFakeShell(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const className = "WinTrayTestGestureRefresh"
	class, err := registerListenerClass(className, win.GetModuleHandle(nil))
	if err != nil {
		t.Fatal(err)
	}
	// Both endpoints must share a class, exactly as Explorer and the real
	// proxy do. Subclass only our disposable target on its message thread.
	created := make(chan win.HWND, 1)
	f.mu.Lock()
	f.onEvent = func(win.HWND, uintptr, uintptr) {
		target := win.CreateWindowEx(0, class, nil, win.WS_POPUP, -200, -100, 400, 40, 0, 0, win.GetModuleHandle(nil), nil)
		if target != 0 {
			win.SetWindowLongPtr(target, win.GWLP_WNDPROC, fakeShellWnd)
			win.DestroyWindow(f.target)
			f.target = target
		}
		created <- target
	}
	f.mu.Unlock()
	win.SendMessage(f.program, testCallback, 0, 0)
	f.mu.Lock()
	f.onEvent = nil
	f.mu.Unlock()
	if <-created == 0 {
		t.Fatal("create private native taskbar")
	}
	f.clear()
	var installed atomic.Bool
	installed.Store(true)
	f.setReply(func(d trayData) uintptr {
		if d.Message == nimAdd {
			return 0 // Explorer already has the original icon.
		}
		return 1
	})
	firstTray := func() win.HWND {
		return win.FindWindow(class, nil)
	}
	add := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 31, flags: nifTip, tip: "gesture fixture"}
	f.mu.Lock()
	f.onRefresh = func() {
		if installed.Load() {
			// Resolve the current native z-order, as Shell_NotifyIcon does,
			// instead of routing directly to the listener under test.
			installed.Store(send(&listener{hwnd: firstTray()}, add) != 0)
		}
	}
	f.mu.Unlock()
	l, err := startListener(listenerConfig{className: className, raise: true,
		target: func(win.HWND) win.HWND { return f.target }}, "", []string{self}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.stop(); err != nil {
			t.Error(err)
		}
	})
	waitFor(t, "initial registration", func() bool { return len(l.snapshot()) == 1 })
	l.onPointer(win.WM_LBUTTONDOWN, win.POINT{X: -150, Y: -80})
	l.pointerMu.Lock()
	l.pointerResumeAt = time.Now().Add(time.Minute)
	l.pointerMu.Unlock()
	waitFor(t, "native taskbar gesture priority", func() bool { return firstTray() == f.target })
	win.SendMessage(l.hwnd, win.WM_TIMER, timerRefresh, 0)
	// The marker is queued after any refresh request, so the assertion does
	// not race the application's message queue.
	win.PostMessage(f.program, testCallback, 0, 0)
	waitFor(t, "refresh queue drained", func() bool { _, events, _ := f.snapshot(); return len(events) != 0 })
	if !installed.Load() {
		t.Fatal("refresh bypassed the yielded collector and disabled future native registrations")
	}
	l.onPointer(win.WM_LBUTTONUP, win.POINT{X: 900, Y: 900})
	waitFor(t, "collection priority restored", func() bool { return firstTray() == l.hwnd })
	_, _, before := f.snapshot()
	win.SendMessage(l.hwnd, win.WM_TIMER, timerRefresh, 0)
	waitFor(t, "refresh after gesture", func() bool { _, _, after := f.snapshot(); return after > before })
	if !installed.Load() || len(l.snapshot()) != 1 {
		t.Fatal("registration did not survive the completed taskbar gesture")
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
