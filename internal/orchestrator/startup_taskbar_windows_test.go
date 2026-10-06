//go:build windows

package orchestrator

import (
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
)

// Explorer consumes shell creation/destruction notifications, not just
// IsWindowVisible. Closing an already registered window behind the startup
// shield must still notify the shell so it can remove the taskbar button.
func TestStartupVisibilityPreservesShellCloseNotification(t *testing.T) {
	for _, beforeLaunch := range []bool{false, true} {
		name := "already registered window"
		if beforeLaunch {
			name = "shield before process launch"
		}
		t.Run(name, func(t *testing.T) { checkStartupShellClose(t, beforeLaunch) })
	}
}

func checkStartupShellClose(t *testing.T, beforeLaunch bool) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	observer := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, 0, 0, 0, 1, 1, 0, 0, 0, nil)
	if observer == 0 {
		t.Fatal("create shell observer")
	}
	defer win.DestroyWindow(observer)
	register := user32.NewProc("RegisterShellHookWindow")
	if ok, _, err := register.Call(uintptr(observer)); ok == 0 {
		t.Fatalf("register shell observer: %v", err)
	}
	defer user32.NewProc("DeregisterShellHookWindow").Call(uintptr(observer))
	shellMessage := win.RegisterWindowMessage(syscall.StringToUTF16Ptr("SHELLHOOK"))
	events := make(map[uintptr][]uintptr)
	awaitEvent := func(hwnd, event uintptr) bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var msg win.MSG
			for win.PeekMessage(&msg, observer, 0, 0, win.PM_REMOVE) {
				if msg.Message == shellMessage {
					events[msg.LParam] = append(events[msg.LParam], msg.WParam)
				}
				win.DispatchMessage(&msg)
			}
			for _, got := range events[hwnd] {
				if got == event {
					return true
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	entry, file := startupFixtureEntry(t, "taskbar")
	var shield *startupVisibility
	if beforeLaunch {
		var err error
		shield, err = beginStartupVisibility(entry.ExePath, &testLogger{})
		if err != nil {
			t.Fatal(err)
		}
		defer shield.close()
	}
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	state := awaitStartupFixture(t, file)
	for _, form := range state.Forms {
		if !awaitEvent(form, 1) {
			t.Fatalf("shell did not register window %x: %v", form, events[form])
		}
	}
	if shield == nil {
		shield, err = beginStartupVisibility(entry.ExePath, &testLogger{})
		if err != nil {
			t.Fatal(err)
		}
		defer shield.close()
	}
	if len(shield.candidates()) != 2 {
		t.Fatal("both taskbar windows must be shielded before closing")
	}
	for _, form := range state.Forms {
		if ok, err := NewWin32WindowManager().CloseWindow(form); !ok {
			t.Fatal(err)
		}
		if !waitVisible(form, false) {
			t.Fatal("fixture did not close to tray")
		}
	}
	if err := shield.close(); err != nil {
		t.Fatal(err)
	}
	for _, form := range state.Forms {
		if !awaitEvent(form, 2) {
			t.Errorf("closed window %x never notified shell to remove taskbar button: %v", form, events[form])
		}
	}
	// The application's own tray callback must recreate the taskbar entry,
	// and a subsequent ordinary close must remove it again.
	clear(events)
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	for _, form := range state.Forms {
		if !awaitEvent(form, 1) {
			t.Errorf("restored window %x did not return to taskbar: %v", form, events[form])
		}
		if ok, err := NewWin32WindowManager().CloseWindow(form); !ok {
			t.Fatal(err)
		}
		if !awaitEvent(form, 2) {
			t.Errorf("second close left taskbar window %x: %v", form, events[form])
		}
	}
}
