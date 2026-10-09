//go:build windows

package traybox

import (
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

const testCallback = win.WM_APP + 7

var procPostThreadMessageW = user32.NewProc("PostThreadMessageW")

// fakeShell plays Explorer's taskbar (target) and a program owning an icon
// (program) on its own thread. The listener under test uses a private window
// class, so the real notification area is never involved.
type fakeShell struct {
	target, program win.HWND
	result          atomic.Uintptr

	mu        sync.Mutex
	received  []trayData
	raw       [][]byte
	events    []callbackEvent
	onEvent   func(win.HWND, uintptr, uintptr)
	refreshed int
	onRefresh func()
	onRequest func(trayData)
	reply     func(trayData) uintptr
}

type callbackEvent struct {
	wParam, lParam uintptr
}

var (
	activeFake   atomic.Pointer[fakeShell]
	fakeTaskbar  = win.RegisterWindowMessage(syscall.StringToUTF16Ptr("TaskbarCreated"))
	fakeShellWnd = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		f := activeFake.Load()
		if f == nil {
			return win.DefWindowProc(hwnd, msg, wParam, lParam)
		}
		if hwnd == f.target && msg == win.WM_COPYDATA {
			result := f.result.Load()
			cds := (*copyDataStruct)(foreignPointer(lParam))
			if cds.DwData == copyDataNotifyIcon {
				f.mu.Lock()
				f.raw = append(f.raw, append([]byte(nil), unsafe.Slice((*byte)(foreignPointer(cds.LpData)), cds.CbData)...))
				f.mu.Unlock()
				if d, ok := parseTrayData(unsafe.Slice((*byte)(foreignPointer(cds.LpData)), cds.CbData)); ok {
					f.mu.Lock()
					f.received = append(f.received, d)
					hook, reply := f.onRequest, f.reply
					f.mu.Unlock()
					if reply != nil {
						result = reply(d)
					}
					// A target may send a message back while the listener is
					// forwarding synchronously. Never hold the test lock here.
					if hook != nil {
						hook(d)
					}
				}
			}
			return result
		}
		if hwnd == f.program && msg == testCallback {
			f.mu.Lock()
			f.events = append(f.events, callbackEvent{wParam, lParam})
			hook := f.onEvent
			f.mu.Unlock()
			if hook != nil {
				hook(hwnd, wParam, lParam)
			}
			return 0
		}
		switch {
		case hwnd == f.program && msg == fakeTaskbar:
			f.mu.Lock()
			f.refreshed++
			hook := f.onRefresh
			f.mu.Unlock()
			if hook != nil {
				hook()
			}
			return 0
		}
		// DefWindowProc may reenter during ShowWindow/SetWindowPos.
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	})
)

func startFakeShell(t *testing.T) *fakeShell {
	t.Helper()
	f := &fakeShell{}
	f.result.Store(1)
	if !activeFake.CompareAndSwap(nil, f) {
		t.Fatal("another fake shell is running")
	}
	ready := make(chan error, 1)
	var thread uint32
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(done)
		class := syscall.StringToUTF16Ptr("WinTrayTestShell")
		wc := win.WNDCLASSEX{LpfnWndProc: fakeShellWnd, HInstance: win.GetModuleHandle(nil), LpszClassName: class}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		win.RegisterClassEx(&wc)
		defer win.UnregisterClass(class)
		f.target = win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, wc.HInstance, nil)
		f.program = win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, wc.HInstance, nil)
		thread = win.GetCurrentThreadId()
		if f.target == 0 || f.program == 0 {
			ready <- syscall.EINVAL
			return
		}
		ready <- nil
		var m win.MSG
		var messages runtime.Pinner
		messages.Pin(&m)
		defer messages.Unpin()
		for win.GetMessage(&m, 0, 0, 0) > 0 {
			win.TranslateMessage(&m)
			win.DispatchMessage(&m)
		}
		win.DestroyWindow(f.program)
		win.DestroyWindow(f.target)
	}()
	if err := <-ready; err != nil {
		t.Fatal("create fake shell windows failed")
	}
	t.Cleanup(func() {
		procPostThreadMessageW.Call(uintptr(thread), win.WM_QUIT, 0, 0)
		<-done
		activeFake.Store(nil)
	})
	return f
}

func (f *fakeShell) snapshot() ([]trayData, []callbackEvent, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.received), slices.Clone(f.events), f.refreshed
}

func (f *fakeShell) setRequestHook(hook func(trayData)) {
	f.mu.Lock()
	f.onRequest = hook
	f.mu.Unlock()
}

func (f *fakeShell) setReply(reply func(trayData) uintptr) {
	f.mu.Lock()
	f.reply = reply
	f.mu.Unlock()
}

func (f *fakeShell) clear() {
	f.mu.Lock()
	f.received, f.raw, f.events = nil, nil, nil
	f.mu.Unlock()
}

type quietLogger struct{}

func (quietLogger) Info(string) {}
func (quietLogger) Warn(string) {}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !ok(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// send delivers a request to the listener as Shell_NotifyIcon would.
func send(l *listener, req trayRequest) uintptr {
	data := req.bytes()
	cds := copyDataStruct{DwData: copyDataNotifyIcon, CbData: uint32(len(data)), LpData: uintptr(unsafe.Pointer(&data[0]))}
	r := win.SendMessage(l.hwnd, win.WM_COPYDATA, uintptr(req.hwnd), uintptr(unsafe.Pointer(&cds)))
	runtime.KeepAlive(data)
	return r
}

func isHidden(d trayData) bool {
	return d.Flags&nifState != 0 && d.StateMask&nisHidden != 0 && d.State&nisHidden != 0
}

func isShown(d trayData) bool {
	return d.Message == nimModify && d.Flags&nifState != 0 && d.StateMask&nisHidden != 0 && d.State&nisHidden == 0
}

func TestListenerCollectsSelectedProgramsIcons(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f := startFakeShell(t)
	cfg := listenerConfig{className: "WinTrayTestTray", target: func(win.HWND) win.HWND { return f.target }}
	l, err := startListener(cfg, "", []string{self}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			l.stop()
		}
	}()

	// Programs already running are asked to register their icons again.
	waitFor(t, "TaskbarCreated for the selected program", func() bool { _, _, n := f.snapshot(); return n >= 1 })

	program := uint32(uintptr(f.program))
	appIcon := win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))
	add := trayRequest{message: nimAdd, hwnd: program, uid: 7, flags: nifMessage | nifIcon | nifTip,
		callback: testCallback, icon: uint32(uintptr(appIcon)), tip: "Fixture #1"}
	if r := send(l, add); r != 1 {
		t.Fatalf("NIM_ADD result = %d, want Explorer's result", r)
	}
	received, _, _ := f.snapshot()
	if len(received) != 1 || received[0].Message != nimAdd || !isHidden(received[0]) || received[0].Tip != "Fixture #1" {
		t.Fatalf("Explorer received %+v, want one hidden NIM_ADD", received)
	}
	icons := l.snapshot()
	if len(icons) != 1 || icons[0].Tooltip != "Fixture #1" || icons[0].Image == nil || !icons[0].Clickable() || icons[0].ExePath == "" {
		t.Fatalf("collected icons = %+v", icons)
	}
	firstImage := icons[0].Image

	// Updates keep flowing to Explorer (still hidden) and into the menu.
	f.clear()
	send(l, trayRequest{message: nimModify, hwnd: program, uid: 7, flags: nifTip, tip: "Fixture #2 online"})
	if received, _, _ = f.snapshot(); len(received) != 1 || received[0].Flags&nifState != 0 {
		t.Fatalf("tooltip update reached Explorer as %+v", received)
	}
	if icons = l.snapshot(); len(icons) != 1 || icons[0].Tooltip != "Fixture #2 online" || icons[0].Image != firstImage {
		t.Fatalf("after tooltip update = %+v", icons)
	}
	send(l, trayRequest{message: nimModify, hwnd: program, uid: 7, flags: nifIcon,
		icon: uint32(uintptr(win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_WARNING))))})
	if icons = l.snapshot(); icons[0].Image == nil || icons[0].Image == firstImage {
		t.Fatal("icon update did not replace the image")
	}

	// A click reaches the program through its callback message.
	f.clear()
	if err := Activate(icons[0], ActionClick); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "left click events", func() bool { _, e, _ := f.snapshot(); return len(e) == 2 })
	_, events, _ := f.snapshot()
	if events[0] != (callbackEvent{7, win.WM_LBUTTONDOWN}) || events[1] != (callbackEvent{7, win.WM_LBUTTONUP}) {
		t.Fatalf("version 0 click events = %+v", events)
	}
	send(l, trayRequest{message: nimSetVersion, hwnd: program, uid: 7, version: 4})
	icons = l.snapshot()
	f.clear()
	if err := Activate(icons[0], ActionRightClick); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "right click events", func() bool { _, e, _ := f.snapshot(); return len(e) == 3 })
	_, events, _ = f.snapshot()
	for i, want := range []uint32{win.WM_RBUTTONDOWN, win.WM_RBUTTONUP, win.WM_CONTEXTMENU} {
		if uint32(events[i].lParam&0xFFFF) != want || uint32(events[i].lParam>>16) != 7 {
			t.Errorf("version 4 event %d = %#x, want %#x with uID 7", i, events[i].lParam, want)
		}
	}

	// Adding an icon Explorer already has fails; it is hidden explicitly.
	f.clear()
	f.result.Store(0)
	send(l, add)
	f.result.Store(1)
	if received, _, _ = f.snapshot(); len(received) != 2 || received[1].Message != nimModify || !isHidden(received[1]) {
		t.Fatalf("failed re-add was followed by %+v, want a hiding NIM_MODIFY", received)
	}

	// An icon the program hides itself leaves the menu until shown again.
	send(l, trayRequest{message: nimModify, hwnd: program, uid: 7, flags: nifState, state: nisHidden, mask: nisHidden})
	if icons = l.snapshot(); len(icons) != 0 {
		t.Fatalf("icon hidden by its program is still listed: %+v", icons)
	}
	f.clear()
	send(l, trayRequest{message: nimModify, hwnd: program, uid: 7, flags: nifState, mask: nisHidden})
	if received, _, _ = f.snapshot(); len(received) != 1 || !isHidden(received[0]) || len(l.snapshot()) != 1 {
		t.Fatalf("program showing its icon: Explorer got %+v, menu %d", received, len(l.snapshot()))
	}

	// Deselecting hands the icon back to Explorer and stops collecting it.
	f.clear()
	l.setPaths([]string{`C:\WinTray-test-only\other.exe`})
	waitFor(t, "restore after deselection", func() bool { r, _, _ := f.snapshot(); return len(r) == 1 })
	if received, _, _ = f.snapshot(); !isShown(received[0]) || received[0].HWnd != program || received[0].UID != 7 {
		t.Fatalf("deselection sent %+v, want the icon shown again", received)
	}
	if icons = l.snapshot(); len(icons) != 0 {
		t.Fatalf("deselected icons still listed: %+v", icons)
	}
	f.clear()
	send(l, add)
	if received, _, _ = f.snapshot(); len(received) != 1 || received[0].Flags&nifState != 0 {
		t.Fatalf("unselected program's request was changed: %+v", received)
	}

	// Selecting again asks the program to re-register, and stopping shows
	// its icon in the notification area again.
	_, _, before := f.snapshot()
	l.setPaths([]string{self})
	waitFor(t, "TaskbarCreated after reselection", func() bool { _, _, n := f.snapshot(); return n > before })
	send(l, trayRequest{message: nimDelete, hwnd: program, uid: 7})
	send(l, add)
	f.clear()
	l.stop()
	stopped = true
	if received, _, _ = f.snapshot(); len(received) != 1 || !isShown(received[0]) {
		t.Fatalf("stopping sent %+v, want the icon shown again", received)
	}
	if activeListener.Load() != nil {
		t.Fatal("listener still registered after stop")
	}
}

func TestListenerForwardsOtherMessagesUnchanged(t *testing.T) {
	f := startFakeShell(t)
	cfg := listenerConfig{className: "WinTrayTestTray", target: func(win.HWND) win.HWND { return f.target }}
	l, err := startListener(cfg, "", []string{`C:\WinTray-test-only\nothing.exe`}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.stop()
	if _, err := startListener(cfg, "", nil, quietLogger{}); err == nil {
		t.Fatal("a second listener started")
	}
	// Icon-position queries and AppBar messages pass through untouched.
	payload := make([]byte, 40)
	cds := copyDataStruct{DwData: copyDataIconRect, CbData: uint32(len(payload)), LpData: uintptr(unsafe.Pointer(&payload[0]))}
	f.result.Store(0x300020)
	if r := win.SendMessage(l.hwnd, win.WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds))); r != 0x300020 {
		t.Fatalf("icon rect query returned %#x", r)
	}
	if received, _, _ := f.snapshot(); len(received) != 0 {
		t.Fatalf("rect query was parsed as an icon request: %+v", received)
	}
}
