//go:build windows

package ui

import (
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
	"wintray/internal/config"
	"wintray/internal/tray"
)

// Exercise the actual NotifyIcon popup route, not the settings window's exit
// button. Only this test's windows and messages are used; no global input is
// sent and no user program, settings file or autorun entry is touched.
func TestTrayMenuExitKeepsSettingsHidden(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deactivate := activateTestManifest(t)
	defer deactivate()
	w, err := NewMainWindow(config.DefaultSettings(), Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.mw.Dispose()
	var exits atomic.Int32
	controller, err := tray.New(w.Native(), w.ShowMainWindow, func() {
		t.Log("tray exit callback")
		exits.Add(1)
		w.mw.SetEnabled(false)
		go w.RequestExplicitClose()
	}, "en-US", true)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Dispose()
	shown := 0
	w.mw.VisibleChanged().Attach(func() {
		if w.mw.Visible() {
			shown++
		}
	})
	w.HideMainWindow()
	hwnd := w.mw.Handle()
	finished := make(chan struct{})
	defer close(finished)
	w.mw.Starting().Attach(func() {
		go func() {
			time.Sleep(100 * time.Millisecond)
			// WM_APP is the shell callback message of the pinned Walk version.
			win.PostMessage(hwnd, win.WM_APP, 0, win.WM_RBUTTONUP)
			time.Sleep(200 * time.Millisecond)
			menu := win.FindWindow(syscall.StringToUTF16Ptr("#32768"), nil)
			var menuPID uint32
			win.GetWindowThreadProcessId(menu, &menuPID)
			if menu != 0 && menuPID == uint32(os.Getpid()) {
				win.PostMessage(menu, 0x1E5 /* MN_SELECTITEM */, 1, 0)
				win.PostMessage(menu, win.WM_KEYDOWN, win.VK_RETURN, 0)
			}
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				win.PostMessage(hwnd, win.WM_CANCELMODE, 0, 0)
				w.RequestExplicitClose()
			}
		}()
	})
	w.Run()
	if exits.Load() != 1 {
		t.Errorf("tray exit callback count=%d, want 1", exits.Load())
	}
	if shown != 0 {
		t.Errorf("tray exit showed the hidden settings %d times", shown)
	}
}
