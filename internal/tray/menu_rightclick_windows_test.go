//go:build windows

package tray

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/i18n"
)

// Exercise Windows' real modal menu loop, not a fabricated WM_MENURBUTTONUP.
// Input is sent only after locating our popup; restore the cursor afterwards.
func TestBoxRightClickThroughNativeMenuLoop(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	t.Run("right opens native context menu", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_RIGHTDOWN, win.MOUSEEVENTF_RIGHTUP, trayBoxedBase, 0, 1, 0, false)
	})
	t.Run("left selects normal command", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP, 0, trayBoxedBase, 1, 0, false)
	})
	t.Run("right after hovering", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_RIGHTDOWN, win.MOUSEEVENTF_RIGHTUP, trayBoxedBase, 0, 6, 0, false)
	})
	t.Run("right held across native refresh", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_RIGHTDOWN, win.MOUSEEVENTF_RIGHTUP, trayBoxedBase, 0, 3, 4, true)
	})
	t.Run("left held across native refresh", func(t *testing.T) {
		testBoxMouseThroughNativeMenuLoop(t, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP, 0, trayBoxedBase, 3, 4, true)
	})
}

func testBoxMouseThroughNativeMenuLoop(t *testing.T, down, up uintptr, wantRight uint32, wantCommand win.BOOL, downTick, holdTicks int, update bool) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
	var cursor win.POINT
	win.GetCursorPos(&cursor)
	defer win.SetCursorPos(cursor.X, cursor.Y)
	win.SetForegroundWindow(hwnd)
	var input []win.MSG
	record := syscall.NewCallback(func(code int32, wp uintptr, m *win.MSG) uintptr {
		if code == msgfMenu && m.Message >= win.WM_MOUSEFIRST && m.Message <= win.WM_MOUSELAST {
			input = append(input, *m)
		}
		r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wp, uintptr(unsafe.Pointer(m)))
		return r
	})
	hook, _, err := procSetWindowsHookEx.Call(^uintptr(0), record, 0, uintptr(win.GetCurrentThreadId()))
	if hook == 0 {
		t.Fatalf("observe test input: %v", err)
	}
	defer procUnhookWindowsHookEx.Call(hook)
	ticks := 0
	var command win.BOOL
	pressed := false
	defer func() {
		if pressed {
			menuUser32.NewProc("mouse_event").Call(up, 0, 0, 0, 0)
		}
	}()
	got, err := trackBoxRightClicks(hwnd, menu, func() {
		ticks++
		if ticks > 30 {
			procEndMenu.Call()
			return
		}
		popup := win.FindWindow(syscall.StringToUTF16Ptr("#32768"), nil)
		if popup == 0 || win.GetWindowThreadProcessId(popup, nil) != win.GetCurrentThreadId() {
			return
		}
		if ticks == 1 {
			var rect win.RECT
			if ok, _, _ := menuUser32.NewProc("GetMenuItemRect").Call(uintptr(hwnd), uintptr(menu), 3, uintptr(unsafe.Pointer(&rect))); ok == 0 {
				t.Error("GetMenuItemRect")
				procEndMenu.Call()
				return
			}
			win.SetCursorPos((rect.Left+rect.Right)/2, (rect.Top+rect.Bottom)/2)
		}
		// Exercise native item updates while the menu owns mouse capture.
		// Do not fabricate WM_MENURBUTTONUP or force a highlighted item.
		if update && ticks > 1 {
			label := syscall.StringToUTF16Ptr(fmt.Sprintf("Collected test icon %d", ticks%2))
			item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STRING, DwTypeData: label}
			if !win.SetMenuItemInfo(menu, trayBoxedBase, false, &item) {
				t.Error("update native menu label")
			}
			redrawOpenBoxMenu()
		}
		if ticks == downTick {
			menuUser32.NewProc("mouse_event").Call(down, 0, 0, 0, 0)
			pressed = true
		}
		if ticks == downTick+holdTicks {
			menuUser32.NewProc("mouse_event").Call(up, 0, 0, 0, 0)
			pressed = false
		}
	}, func() {
		command = win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != wantRight || command != wantCommand {
		for _, m := range input {
			t.Logf("input owner=%#x hwnd=%#x message=%#x point=%+v", hwnd, m.HWnd, m.Message, m.Pt)
		}
		t.Fatalf("native right click = %d, normal command = %d, ticks = %d; want %d/%d", got, command, ticks, wantRight, wantCommand)
	}
}

// Keep the fixture on its UI thread until cleanup has destroyed the windows.
func newBoxMenuLoopFixture(t *testing.T, language string) (win.HWND, win.HMENU) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	hwnd := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, win.GetModuleHandle(nil), nil)
	if hwnd == 0 {
		t.Fatal("create menu owner")
	}
	t.Cleanup(func() { win.DestroyWindow(hwnd) })
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("create menu")
	}
	t.Cleanup(func() { win.DestroyMenu(menu) })
	if _, ok := appendContextMenu(menu, i18n.For(language), nil); !ok {
		t.Fatal("append WinTray actions")
	}
	if !appendTrayMenuItem(menu, menuItem{separator: true}) ||
		!appendTrayMenuItem(menu, menuItem{id: trayBoxedBase, text: "Collected test icon"}) {
		t.Fatal("append menu item")
	}
	return hwnd, menu
}

// Hover and the old arrow area are ordinary native row input now. Right
// clicks on disabled rows or WinTray actions must not execute any command.
func TestBoxMenuNativeNavigationAndDismissal(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	for _, tc := range []struct {
		name                  string
		row                   uintptr
		key                   uintptr
		right, disabled, edge bool
		want                  win.BOOL
	}{
		{name: "hover label", row: 3},
		{name: "hover trailing edge", row: 3, edge: true},
		{name: "right run silently", row: 0, right: true},
		{name: "right exit", row: 1, right: true},
		{name: "right disabled program", row: 3, right: true, disabled: true},
		{name: "keyboard enter", row: 3, key: win.VK_RETURN, want: trayBoxedBase},
		{name: "keyboard right does not expand", row: 3, key: win.VK_RIGHT},
		{name: "keyboard escape", row: 3, key: win.VK_ESCAPE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
			if tc.disabled {
				procEnableMenuItem.Call(uintptr(menu), trayBoxedBase, win.MF_BYCOMMAND|win.MF_GRAYED)
			}
			var cursor win.POINT
			win.GetCursorPos(&cursor)
			defer win.SetCursorPos(cursor.X, cursor.Y)
			win.SetForegroundWindow(hwnd)
			ticks := 0
			var command win.BOOL
			got, err := trackBoxRightClicks(hwnd, menu, func() {
				ticks++
				if ticks == 1 {
					var rect win.RECT
					if ok, _, _ := procGetMenuItemRect.Call(uintptr(hwnd), uintptr(menu), tc.row, uintptr(unsafe.Pointer(&rect))); ok == 0 {
						t.Error("read menu row")
						procEndMenu.Call()
						return
					}
					x := (rect.Left + rect.Right) / 2
					if tc.edge {
						x = rect.Right - 2
					}
					if tc.key != 0 {
						win.SetCursorPos(20, 20)
					} else {
						win.SetCursorPos(x, (rect.Top+rect.Bottom)/2)
					}
				}
				if tc.key != 0 && ticks >= 2 && ticks <= 4 {
					// Select all three enabled rows through native navigation;
					// no hover selection or undocumented menu messages are used.
					win.PostMessage(hwnd, win.WM_KEYDOWN, win.VK_DOWN, 0)
				}
				if tc.key != 0 && ticks == 5 {
					win.PostMessage(hwnd, win.WM_KEYDOWN, tc.key, 0)
				}
				if ticks == 4 && tc.right {
					mouse := menuUser32.NewProc("mouse_event")
					mouse.Call(win.MOUSEEVENTF_RIGHTDOWN, 0, 0, 0, 0)
					mouse.Call(win.MOUSEEVENTF_RIGHTUP, 0, 0, 0, 0)
				}
				if ticks == 15 {
					procEndMenu.Call()
				}
			}, func() {
				command = win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil)
			})
			if err != nil || got != 0 || command != tc.want {
				t.Fatalf("right=%d command=%d ticks=%d err=%v, want command=%d", got, command, ticks, err, tc.want)
			}
			if !tc.right && tc.want == 0 && tc.key != win.VK_ESCAPE && ticks != 15 {
				t.Fatalf("hover/navigation ended the menu after %d ticks", ticks)
			}
		})
	}
}

// The native row ID/state, not its current highlight, determines whether a
// collected icon accepts a right-click. No desktop input is needed here.
func TestBoxRightClicksOnlySelectEnabledCollectedItems(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("create test menu failed")
	}
	defer win.DestroyMenu(menu)
	for _, item := range []menuItem{
		{id: trayActionRunSilently, text: "run silently"},
		{id: trayActionExit, text: "exit"},
		{separator: true},
		{id: trayBoxedBase, text: "clickable"},
		{id: trayBoxedBase + 1, text: "not clickable", disabled: true},
		{id: trayIDLimit, text: "outside collected range"},
	} {
		if !appendTrayMenuItem(menu, item) {
			t.Fatal("append test menu item failed")
		}
	}
	for position, want := range []uint32{0, 0, 0, trayBoxedBase, 0, 0, 0} {
		if got := collectedMenuCommand(menu, uint32(position)); got != want {
			t.Errorf("right-click at position %d = %d, want %d", position, got, want)
		}
	}
}

func TestBoxMenuFilterIsScopedAndRemovedAfterTracking(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, win.GetModuleHandle(nil), nil)
	if hwnd == 0 {
		t.Fatal("create test menu owner")
	}
	defer win.DestroyWindow(hwnd)
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("create test menu")
	}
	defer win.DestroyMenu(menu)
	filter := menuUser32.NewProc("CallMsgFilterW")
	call := func(message win.MSG, code int32) bool {
		result, _, _ := filter.Call(uintptr(unsafe.Pointer(&message)), uintptr(code))
		return result != 0
	}
	ticks := 0
	timer := win.MSG{HWnd: hwnd, Message: win.WM_TIMER, WParam: boxRefreshTimer}
	got, err := trackBoxRightClicks(hwnd, menu, func() { ticks++ }, func() {
		for _, tc := range []struct {
			message win.MSG
			code    int32
		}{
			{timer, 0}, // a dialog's modal loop is not this menu
			{timer, -1},
			{win.MSG{Message: win.WM_TIMER, WParam: boxRefreshTimer}, msgfMenu},
			{win.MSG{HWnd: hwnd, Message: win.WM_TIMER, WParam: boxRefreshTimer + 1}, msgfMenu},
			{win.MSG{HWnd: hwnd, Message: win.WM_LBUTTONUP}, msgfMenu},
		} {
			if call(tc.message, tc.code) || ticks != 0 {
				t.Errorf("unrelated message consumed: code=%d message=%+v ticks=%d", tc.code, tc.message, ticks)
			}
		}
		if !call(timer, msgfMenu) || ticks != 1 {
			t.Error("active menu timer did not refresh exactly once")
		}
		outer := activeBoxMenu
		if _, err := trackBoxRightClicks(hwnd, menu, nil, func() { t.Error("nested menu was tracked") }); err == nil || activeBoxMenu != outer {
			t.Error("nested tracking replaced the active menu handler")
		}
		if !call(timer, msgfMenu) || ticks != 2 {
			t.Error("nested tracking removed the active menu handler")
		}
	})
	if err != nil || got != 0 {
		t.Fatalf("tracking = %d, %v", got, err)
	}
	if activeBoxMenu != nil || call(timer, msgfMenu) || ticks != 2 {
		t.Error("menu filter survived after tracking ended")
	}
}
