//go:build windows

package tray

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

// Exercise Windows' real modal menu loop, not a fabricated WM_MENURBUTTONUP.
// Input is sent only after locating our popup; restore the cursor afterwards.
func TestBoxRightClickThroughNativeMenuLoop(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	t.Run("right opens native context menu", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_RIGHTDOWN, win.MOUSEEVENTF_RIGHTUP, trayBoxedBase, 0)
	})
	t.Run("left selects normal command", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP, 0, trayBoxedBase)
	})
}

func testBoxMouseThroughNativeMenuLoop(t *testing.T, down, up uintptr, wantRight uint32, wantCommand win.BOOL) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, win.GetModuleHandle(nil), nil)
	if hwnd == 0 {
		t.Fatal("create menu owner")
	}
	defer win.DestroyWindow(hwnd)
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("create menu")
	}
	defer win.DestroyMenu(menu)
	if !appendTrayMenuItem(menu, menuItem{id: trayBoxedBase, text: "Collected test icon"}) {
		t.Fatal("append menu item")
	}
	var cursor win.POINT
	win.GetCursorPos(&cursor)
	defer win.SetCursorPos(cursor.X, cursor.Y)
	win.SetForegroundWindow(hwnd)
	ticks := 0
	var command win.BOOL
	got := trackBoxRightClicks(hwnd, func() {
		ticks++
		if ticks > 20 {
			procEndMenu.Call()
			return
		}
		if ticks != 1 {
			return
		}
		popup := win.FindWindow(syscall.StringToUTF16Ptr("#32768"), nil)
		if popup == 0 || win.GetWindowThreadProcessId(popup, nil) != win.GetCurrentThreadId() {
			return
		}
		var rect win.RECT
		if ok, _, _ := menuUser32.NewProc("GetMenuItemRect").Call(uintptr(hwnd), uintptr(menu), 0, uintptr(unsafe.Pointer(&rect))); ok == 0 {
			return
		}
		pt := win.POINT{X: (rect.Left + rect.Right) / 2, Y: (rect.Top + rect.Bottom) / 2}
		win.SetCursorPos(pt.X, pt.Y)
		menuUser32.NewProc("mouse_event").Call(down, 0, 0, 0, 0)
		menuUser32.NewProc("mouse_event").Call(up, 0, 0, 0, 0)
	}, func() {
		command = win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil)
	})
	if got != wantRight || command != wantCommand {
		t.Fatalf("native right click = %d, normal command = %d, ticks = %d; want %d/%d", got, command, ticks, wantRight, wantCommand)
	}
}

var testMenuOwnerProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
})

// Use a private, never-shown menu owner. Deliver the same owner message that
// Windows sends on a menu right-click, without injecting desktop input.
func TestBoxRightClicksOnlySelectEnabledCollectedItems(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := syscall.StringToUTF16Ptr("WinTrayTestMenuOwner")
	wc := win.WNDCLASSEX{LpfnWndProc: testMenuOwnerProc, HInstance: win.GetModuleHandle(nil), LpszClassName: class}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if win.RegisterClassEx(&wc) == 0 {
		t.Fatal("register test window class failed")
	}
	defer func() {
		r, _, err := menuUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(class)), uintptr(wc.HInstance))
		if r == 0 {
			t.Errorf("unregister test window class: %v", err)
		}
	}()
	hwnd := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, wc.HInstance, nil)
	if hwnd == 0 {
		t.Fatal("create test menu owner failed")
	}
	defer win.DestroyWindow(hwnd)
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("create test menu failed")
	}
	defer win.DestroyMenu(menu)
	for _, item := range []menuItem{
		{id: trayBoxedBase, text: "clickable"},
		{id: trayBoxedBase + 1, text: "not clickable", disabled: true},
		{id: trayActionOpenSettings, text: "settings"},
		{separator: true},
	} {
		if !appendTrayMenuItem(menu, item) {
			t.Fatal("append test menu item failed")
		}
	}
	for position, want := range []uint32{trayBoxedBase, 0, 0, 0} {
		got := trackBoxRightClicks(hwnd, nil, func() {
			win.SendMessage(hwnd, wmMenuRButtonUp, uintptr(position), uintptr(menu))
		})
		if got != want {
			t.Errorf("right-click at position %d = %d, want %d", position, got, want)
		}
	}
	// Closing a menu must remove the subclass and clear the last selection.
	if got := trackBoxRightClicks(hwnd, nil, func() {}); got != 0 {
		t.Errorf("new menu inherited right-click %d", got)
	}
	win.SendMessage(hwnd, wmMenuRButtonUp, 0, uintptr(menu))
	if menuRightClick != 0 {
		t.Error("right-click hook survived after the menu ended")
	}
}
