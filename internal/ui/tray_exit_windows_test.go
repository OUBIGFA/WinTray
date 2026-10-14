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

// Tray menu order: run silently, exit, then collected icons when present.
const (
	trayMenuRunSilently = 0
	trayMenuExit        = 1
	mnSelectItem        = 0x1E5
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
			// Opening the main window remains a left-click action, not a menu row.
			win.PostMessage(hwnd, win.WM_APP, 0, win.WM_LBUTTONDOWN)
			win.PostMessage(hwnd, win.WM_APP, 0, win.WM_LBUTTONUP)
			if !waitForCount(&shown, 1, 2*time.Second) {
				reportTrayMenuFailure(failure, "left-click did not show the main window", w)
				return
			}
			if findPopupMenuForProcess(uint32(os.Getpid())) != 0 {
				reportTrayMenuFailure(failure, "left-click opened a popup menu", w)
				return
			}
			// The exit callback deliberately does not dispose the controller:
			// a declined exit (for example a failed icon restore) must be retryable.
			items := []uintptr{trayMenuRunSilently, trayMenuExit, trayMenuExit}
			counts := []*atomic.Int32{&silent, &exited, &exited}
			for i, item := range items {
				win.PostMessage(hwnd, win.WM_APP, 0, win.WM_RBUTTONUP)
				menu := waitForPopupMenu(uint32(os.Getpid()), 2*time.Second)
				if menu == 0 {
					reportTrayMenuFailure(failure, "tray popup did not open", w)
					return
				}
				if hMenu := win.HMENU(win.SendMessage(menu, 0x1e1, 0, 0)); win.GetMenuItemCount(hMenu) != 2 {
					reportTrayMenuFailure(failure, "tray menu without collected icons must contain only run silently and exit", w)
					return
				}
				selectTrayMenuItem(menu, item)
				want := int32(1)
				if i == 2 {
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
		t.Error("left-click did not show the main window")
	}
	if popupCount.Load() != 0 {
		t.Errorf("tray action left/reopened %d popup menus", popupCount.Load())
	}
}

func selectTrayMenuItem(menu win.HWND, item uintptr) {
	if item == 0 {
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
	// Let the modal menu loop end before the queued window close can run.
	if menu := findPopupMenuForProcess(uint32(os.Getpid())); menu != 0 {
		win.PostMessage(menu, win.WM_KEYDOWN, win.VK_ESCAPE, 0)
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
