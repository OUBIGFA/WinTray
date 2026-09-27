//go:build windows

package tray

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

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
