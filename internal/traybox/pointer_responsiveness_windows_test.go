//go:build windows

package traybox

import (
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
)

type stalledPointerLogger struct {
	once             sync.Once
	entered, release chan struct{}
}

func (l *stalledPointerLogger) Info(string) {
	l.once.Do(func() { close(l.entered); <-l.release })
}
func (*stalledPointerLogger) Warn(string) {}

// Low-level hooks are delivered to the installing thread. Probe that native
// queue while real listener work is stalled, without injecting mouse input
// into the user's desktop. Button handling must not wait on the listener either.
func TestPointerQueueRespondsWhileCollectorIsBusy(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const className = "WinTrayTestPointerResponsive"
	class, err := registerListenerClass(className, win.GetModuleHandle(nil))
	if err != nil {
		t.Fatal(err)
	}
	target := win.CreateWindowEx(0, class, nil, win.WS_POPUP, -200, -100, 400, 40, 0, 0, win.GetModuleHandle(nil), nil)
	if target == 0 {
		t.Fatal("create private taskbar")
	}
	defer win.DestroyWindow(target)
	logger := &stalledPointerLogger{entered: make(chan struct{}), release: make(chan struct{})}
	l, err := startListener(listenerConfig{className: className, raise: true,
		target: func(win.HWND) win.HWND { return target }}, "", []string{`C:\WinTray-test-only\absent.exe`}, logger)
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	resume := func() { release.Do(func() { close(logger.release) }) }
	defer func() {
		resume()
		if err := l.stop(); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-logger.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("collector did not reach the controlled stall")
	}

	const probe = win.WM_APP + 91
	responded := make(chan struct{}, 1)
	callback := syscall.NewCallback(func(code int32, removed, data uintptr) uintptr {
		if code >= 0 && removed == win.PM_REMOVE {
			msg := (*win.MSG)(foreignPointer(data))
			if msg.Message == probe {
				l.onPointer(win.WM_LBUTTONDOWN, win.POINT{X: -150, Y: -80})
				l.onPointer(win.WM_LBUTTONUP, win.POINT{X: -150, Y: -80})
				responded <- struct{}{}
			}
		}
		r, _, _ := procCallNextHookEx.Call(0, uintptr(code), removed, data)
		return r
	})
	hook, _, err := procSetWindowsHookExW.Call(3, callback, 0, uintptr(l.pointerThreadID))
	if hook == 0 {
		t.Fatalf("install pointer queue probe: %v", err)
	}
	defer procUnhookWindowsHookEx.Call(hook)
	started := time.Now()
	if ok, _, err := procPostThreadMessageW.Call(uintptr(l.pointerThreadID), probe, 0, 0); ok == 0 {
		t.Fatalf("post pointer queue probe: %v", err)
	}
	select {
	case <-responded:
		t.Logf("pointer queue and taskbar gesture responded in %s while collector stayed blocked", time.Since(started))
	case <-time.After(200 * time.Millisecond):
		t.Error("mouse hook queue blocked by collector work for more than 200ms")
		resume()
		select {
		case <-responded:
		case <-time.After(3 * time.Second):
			t.Error("pointer probe did not recover")
		}
	}
	// The queued move must still yield to the taskbar and recover collection
	// after release; making the hook responsive must not discard the gesture.
	resume()
	waitFor(t, "taskbar priority after the collector resumes", func() bool { return win.FindWindow(class, nil) == target })
	waitFor(t, "collection priority after button release", func() bool { return win.FindWindow(class, nil) == l.hwnd })
}
