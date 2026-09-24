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

// Tray menu order: open settings, run silently, exit.
const (
	trayMenuOpenSettings = 0
	trayMenuRunSilently  = 1
	trayMenuExit         = 2
	mnSelectItem         = 0x1E5
)

// Exercise all NotifyIcon popup actions through one real message loop. The
// single loop matters because Walk's application owns the thread quit state.
// Only this test's windows and messages are used; no global input is sent and
// no user program, settings file or autorun entry is touched.
func TestTrayMenuActionsDoNotReopenMenu(t *testing.T) {
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

	var opened, silent, exited atomic.Int32
	var shown, popupCount atomic.Int32
	controller, err := tray.New(w.Native(), func() {
		opened.Add(1)
		w.ShowMainWindow()
	}, func() {
		silent.Add(1)
		w.HideMainWindow()
	}, func() {
		exited.Add(1)
	}, "en-US", true)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Dispose()
	w.mw.VisibleChanged().Attach(func() {
		if w.mw.Visible() {
			shown.Add(1)
		}
	})
	w.HideMainWindow()
	hwnd := w.mw.Handle()
	failure := make(chan string, 1)

	w.mw.Starting().Attach(func() {
		go func() {
			items := []uintptr{trayMenuOpenSettings, trayMenuRunSilently, trayMenuExit}
			counts := []*atomic.Int32{&opened, &silent, &exited}
			for i, item := range items {
				win.PostMessage(hwnd, win.WM_APP, 0, win.WM_RBUTTONUP)
				menu := waitForPopupMenu(uint32(os.Getpid()), 2*time.Second)
				if menu == 0 {
					reportTrayMenuFailure(failure, "tray popup did not open", w)
					return
				}
				selectTrayMenuItem(menu, item)
				if !waitForCount(counts[i], 1, 2*time.Second) {
					reportTrayMenuFailure(failure, "tray action callback did not run", w)
					return
				}
				time.Sleep(100 * time.Millisecond)
				if findPopupMenuForProcess(uint32(os.Getpid())) != 0 {
					popupCount.Add(1)
				}
				if item == trayMenuOpenSettings {
					w.Synchronize(w.HideMainWindow)
				}
			}
			w.RequestExplicitClose()
		}()
	})

	w.Run()
	select {
	case message := <-failure:
		t.Error(message)
	default:
	}
	if opened.Load() != 1 || silent.Load() != 1 || exited.Load() != 1 {
		t.Errorf("tray callbacks open=%d silent=%d exit=%d, want 1/1/1", opened.Load(), silent.Load(), exited.Load())
	}
	if shown.Load() == 0 {
		t.Error("tray open settings did not show the settings window")
	}
	if popupCount.Load() != 0 {
		t.Errorf("tray action left/reopened %d popup menus", popupCount.Load())
	}
}

func selectTrayMenuItem(menu win.HWND, item uintptr) {
	if item == trayMenuOpenSettings {
		// MN_SELECTITEM treats zero as no selection unless the position flag
		// is supplied through lParam.
		win.SendMessage(menu, mnSelectItem, 0, 1)
	} else {
		win.SendMessage(menu, mnSelectItem, item, 0)
	}
	win.SendMessage(menu, win.WM_KEYDOWN, win.VK_RETURN, 0)
}

func waitForPopupMenu(pid uint32, timeout time.Duration) win.HWND {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if menu := findPopupMenuForProcess(pid); menu != 0 {
			return menu
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0
}

func waitForCount(count *atomic.Int32, want int32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if count.Load() >= want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func reportTrayMenuFailure(failure chan<- string, message string, w *MainWindow) {
	select {
	case failure <- message:
	default:
	}
	w.RequestExplicitClose()
}

func findPopupMenuForProcess(pid uint32) win.HWND {
	menu := win.FindWindow(syscall.StringToUTF16Ptr("#32768"), nil)
	if menu == 0 {
		return 0
	}
	var menuPID uint32
	win.GetWindowThreadProcessId(menu, &menuPID)
	if menuPID == pid && win.IsWindowVisible(menu) {
		return menu
	}
	return 0
}
