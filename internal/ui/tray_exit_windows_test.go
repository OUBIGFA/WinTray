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
	"wintray/internal/traybox"
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
	if runWindowTestInChild(t) {
		return
	}
	runTestMainThread(func() { testTrayMenuActions(t) })
}

func testTrayMenuActions(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w, err := NewMainWindow(config.DefaultSettings(), Callbacks{})
	if err != nil {
		t.Fatal(err)
	}

	var opened, silent, exited atomic.Int32
	var shown, popupCount atomic.Int32
	box := traybox.NewBox("", nil)
	var controller *tray.Controller
	defer func() {
		if controller != nil {
			controller.Dispose()
		}
	}()
	failure := make(chan string, 1)
	w.OnStarting(func() {
		controller, err = tray.New(w.Native(), func() {
			opened.Add(1)
			w.ShowMainWindow()
			w.Synchronize(func() {
				if w.desktop.window != nil && w.desktop.window.IsVisible() {
					shown.Add(1)
				}
			})
		}, func() {
			silent.Add(1)
			w.HideMainWindow()
		}, func() {
			exited.Add(1)
		}, "en-US", true, box, nil)
		if err != nil {
			t.Fatal(err)
		}
		w.HideMainWindow()
		hwnd := w.mw.Handle()
		go func() {
			// The exit callback deliberately does not dispose the controller:
			// a declined exit (for example a failed icon restore) must be retryable.
			items := []uintptr{trayMenuOpenSettings, trayMenuRunSilently, trayMenuExit, trayMenuExit}
			counts := []*atomic.Int32{&opened, &silent, &exited, &exited}
			for i, item := range items {
				win.PostMessage(hwnd, win.WM_APP, 0, win.WM_RBUTTONUP)
				menu := waitForPopupMenu(uint32(os.Getpid()), 2*time.Second)
				if menu == 0 {
					reportTrayMenuFailure(failure, "tray popup did not open", w)
					return
				}
				if hMenu := win.HMENU(win.SendMessage(menu, 0x1e1, 0, 0)); win.GetMenuItemCount(hMenu) != 3 {
					reportTrayMenuFailure(failure, "unselected programs added entries to the tray menu", w)
					return
				}
				selectTrayMenuItem(menu, item)
				want := int32(1)
				if i == 3 {
					want = 2
				}
				if !waitForCount(counts[i], want, 2*time.Second) {
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

	if code := w.Run(); code != 0 {
		t.Errorf("desktop exit %d", code)
	}
	select {
	case message := <-failure:
		t.Error(message)
	default:
	}
	if opened.Load() != 1 || silent.Load() != 1 || exited.Load() != 2 {
		t.Errorf("tray callbacks open=%d silent=%d exit=%d, want 1/1/2", opened.Load(), silent.Load(), exited.Load())
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
