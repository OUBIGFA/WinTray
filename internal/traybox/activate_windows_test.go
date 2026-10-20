//go:build windows

package traybox

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

// A busy framework can inspect its queue inside WM_PAINT. Sent notifications
// are dispatched by PeekMessage itself, before that paint finishes; posted
// gestures remain queued for the normal dispatch loop. No desktop input or
// artificial menu drawing is needed to exercise this ordering defect.
func TestTrayGestureDoesNotReenterProgramPaint(t *testing.T) {
	for _, version := range []uint32{0, 3, 4} {
		for _, action := range []Action{ActionClick, ActionDoubleClick, ActionRightClick} {
			t.Run(fmt.Sprintf("version_%d/action_%d", version, action), func(t *testing.T) {
				f := startFakeShell(t)
				ready, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var once sync.Once
				release := func() { once.Do(func() { close(resume) }) }
				defer release()
				var painting, reentered, sent atomic.Bool
				f.mu.Lock()
				f.onPaint = func() {
					painting.Store(true)
					close(ready)
					<-resume
					var message win.MSG
					// Inspect, but do not dispatch posted messages mid-paint.
					win.PeekMessage(&message, 0, 0, 0, win.PM_NOREMOVE)
					painting.Store(false)
					close(finished)
				}
				f.onEvent = func(win.HWND, uintptr, uintptr) {
					if painting.Load() {
						reentered.Store(true)
					}
					if flags, _, _ := user32.NewProc("InSendMessageEx").Call(0); flags != 0 {
						sent.Store(true)
					}
				}
				f.mu.Unlock()
				if win.PostMessage(f.program, win.WM_PAINT, 0, 0) == 0 {
					t.Fatal("start program paint")
				}
				select {
				case <-ready:
				case <-time.After(3 * time.Second):
					t.Fatal("program did not enter WM_PAINT")
				}
				icon := Icon{owner: uint32(f.program), uid: 71, callback: testCallback, version: version}
				if err := Activate(icon, action); err != nil {
					t.Fatal(err)
				}
				release()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Fatal("program paint did not return")
				}
				want := len(clickEvents(action, version))
				waitFor(t, "queued tray gesture", func() bool { _, events, _ := f.snapshot(); return len(events) == want })
				if reentered.Load() || sent.Load() {
					t.Fatalf("tray gesture interrupted the program: during-paint=%t sent-context=%t", reentered.Load(), sent.Load())
				}
			})
		}
	}
}

// A version 4 program receives the pointer position with every event. Older
// versions receive no coordinates and read the pointer themselves.
func TestReportedAnchorReachesOnlyVersionFour(t *testing.T) {
	anchor := win.POINT{X: 1200, Y: 700}
	for _, keyword := range []uint32{win.WM_RBUTTONDOWN, win.WM_CONTEXTMENU} {
		wParam, lParam := callbackParams(Icon{uid: 7, version: 4}, keyword, anchor)
		got := win.POINT{X: int32(int16(wParam)), Y: int32(int16(wParam >> 16))}
		if got != anchor || lParam>>16 != 7 || uint32(lParam&0xffff) != keyword {
			t.Fatalf("version 4 event %#x: anchor=%+v lParam=%#x", keyword, got, lParam)
		}
		wParam, lParam = callbackParams(Icon{uid: 7, version: 3}, keyword, anchor)
		if wParam != 7 || uint32(lParam) != keyword {
			t.Fatalf("version 3 event %#x carries anchor data: wParam=%#x lParam=%#x", keyword, wParam, lParam)
		}
	}
}

// Hosted tray icons can share WinTray's UI thread. They still need queued
// delivery, rather than opening a nested TrackPopupMenu inside Activate.
func TestTrayGestureOnSameThreadIsQueued(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var events []callbackEvent
	proc := syscall.NewCallback(func(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
		if msg == testCallback {
			events = append(events, callbackEvent{wp, lp})
			return 0
		}
		return win.DefWindowProc(hwnd, msg, wp, lp)
	})
	class := syscall.StringToUTF16Ptr("WinTray.QueuedIconFixture")
	wc := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: proc, HInstance: win.GetModuleHandle(nil), LpszClassName: class}
	if win.RegisterClassEx(&wc) == 0 {
		t.Fatal("register queued-icon fixture")
	}
	defer win.UnregisterClass(class)
	hwnd := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, wc.HInstance, nil)
	if hwnd == 0 {
		t.Fatal("create queued-icon fixture")
	}
	defer win.DestroyWindow(hwnd)
	icon := Icon{owner: uint32(hwnd), uid: 71, callback: testCallback, version: 4}
	if err := Activate(icon, ActionRightClick); err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatal("Activate called the same-thread receiver synchronously")
	}
	var message win.MSG
	for win.PeekMessage(&message, hwnd, testCallback, testCallback, win.PM_REMOVE) {
		win.DispatchMessage(&message)
	}
	if len(events) != 3 {
		t.Fatalf("queued right-click events = %d, want 3", len(events))
	}
}
