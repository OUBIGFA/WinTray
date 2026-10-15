//go:build windows

package ui

import (
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/tray"
)

// A second shell notification can arrive inside TrackPopupMenuEx's modal loop.
// It must not remove the handler that still owns the first menu. Only this
// test's windows receive messages; no desktop input or real program is used.
func TestTrayMenuRepeatedOpenKeepsRightClick(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	if runWindowTestInChild(t) {
		return
	}
	runTestMainThread(func() {
		w, err := NewMainWindow(config.DefaultSettings(), Callbacks{})
		if err != nil {
			t.Error(err)
			return
		}
		var controller *tray.Controller
		defer func() {
			if controller != nil {
				controller.Dispose()
			}
		}()
		failure := make(chan string, 1)
		w.OnStarting(func() {
			controller, err = tray.New(w.Native(), nil, nil, nil, "en-US", false, nil, nil)
			if err != nil {
				reportTrayMenuFailure(failure, err.Error(), w)
				return
			}
			controller.SetDispatcher(w.Synchronize)
			hwnd := w.Native().Handle()
			go func() {
				defer w.RequestExplicitClose()
				win.PostMessage(hwnd, win.WM_APP, 0, win.WM_RBUTTONUP)
				popup := waitForPopupMenu(uint32(os.Getpid()), 2*time.Second)
				if popup == 0 {
					reportTrayMenuFailure(failure, "tray popup did not open", w)
					return
				}
				menu := win.HMENU(win.SendMessage(popup, 0x1e1, 0, 0))
				// Give an existing enabled row a collected-icon command ID. The
				// controller has no real icons, so closing it activates nothing.
				item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_ID, WID: 100}
				if !win.SetMenuItemInfo(menu, 1, true, &item) {
					reportTrayMenuFailure(failure, "could not prepare collected command", w)
					return
				}
				for range 3 {
					win.PostMessage(hwnd, win.WM_APP, 0, win.WM_RBUTTONUP)
				}
				processed := make(chan struct{})
				w.Synchronize(func() { close(processed) })
				select {
				case <-processed:
				case <-time.After(2 * time.Second):
					reportTrayMenuFailure(failure, "repeated tray notification blocked the menu loop", w)
					return
				}
				w.Synchronize(func() {
					// Feed a raw release through the thread's real menu filter.
					// This tests handler lifetime without moving the desktop mouse.
					user32 := windows.NewLazySystemDLL("user32.dll")
					var rect win.RECT
					if ok, _, _ := user32.NewProc("GetMenuItemRect").Call(uintptr(hwnd), uintptr(menu), 1, uintptr(unsafe.Pointer(&rect))); ok == 0 {
						reportTrayMenuFailure(failure, "could not locate collected command", w)
						return
					}
					message := win.MSG{HWnd: hwnd, Message: win.WM_RBUTTONUP,
						Pt: win.POINT{X: (rect.Left + rect.Right) / 2, Y: (rect.Top + rect.Bottom) / 2}}
					user32.NewProc("CallMsgFilterW").Call(uintptr(unsafe.Pointer(&message)), 2) // MSGF_MENU
				})
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					if findPopupMenuForProcess(uint32(os.Getpid())) == 0 {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				reportTrayMenuFailure(failure, "repeated tray notification removed the active menu's right-click handler", w)
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
	})
}
