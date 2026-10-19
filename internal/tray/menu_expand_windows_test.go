//go:build windows

package tray

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/i18n"
)

var recordedMenuInput []win.MSG
var recordedMenuOwnerMessages []win.MSG
var recordMenuOwnerProc = syscall.NewCallback(func(hwnd win.HWND, message uint32, wp, lp, _ uintptr, _ uintptr) uintptr {
	switch message {
	case win.WM_CANCELMODE, win.WM_ACTIVATE, win.WM_ACTIVATEAPP, win.WM_CAPTURECHANGED, win.WM_EXITMENULOOP:
		recordedMenuOwnerMessages = append(recordedMenuOwnerMessages, win.MSG{HWnd: hwnd, Message: message, WParam: wp, LParam: lp})
	}
	r, _, _ := procDefMenuSubclass.Call(uintptr(hwnd), uintptr(message), wp, lp)
	return r
})
var recordedMenuOwner win.HWND
var recordMenuInputProc = syscall.NewCallback(func(code int32, wParam uintptr, m *win.MSG) uintptr {
	if code == msgfMenu && m.HWnd == recordedMenuOwner && (m.Message >= win.WM_MOUSEFIRST && m.Message <= win.WM_MOUSELAST || m.Message == win.WM_KEYDOWN) {
		recordedMenuInput = append(recordedMenuInput, *m)
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, uintptr(unsafe.Pointer(m)))
	return r
})

// Exercise Windows' real modal loop and native submenu arrow. Mouse input is
// sent only after finding our row, and every press and cursor move is undone.
func TestBoxExpandThroughNativeMenuLoop(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	for _, tc := range []struct {
		name, gesture string
		arrow         bool
		hold, update  bool
		want          boxMenuChoice
	}{
		{"hover arrow", "hover", true, false, false, boxMenuChoice{trayBoxedBase, true}},
		{"click arrow", "left", true, false, false, boxMenuChoice{trayBoxedBase, true}},
		{"click label", "left", false, false, false, boxMenuChoice{trayBoxedBase, false}},
		{"hover label does not expand", "hover", false, false, false, boxMenuChoice{}},
		{"right label does not expand", "right", false, false, false, boxMenuChoice{}},
		{"right arrow does not expand", "right", true, false, false, boxMenuChoice{}},
		{"held arrow across refresh", "left", true, true, true, boxMenuChoice{trayBoxedBase, true}},
		{"held label across refresh", "left", false, true, true, boxMenuChoice{trayBoxedBase, false}},
		{"leave arrow cancels hover", "leave", true, false, false, boxMenuChoice{}},
		{"disabled arrow does not expand", "disabled", true, false, false, boxMenuChoice{}},
		{"release outside cancels click", "cancel", false, true, false, boxMenuChoice{}},
		{"keyboard enter opens program", "enter", false, false, false, boxMenuChoice{trayBoxedBase, false}},
		{"keyboard right expands", "right-key", false, false, false, boxMenuChoice{trayBoxedBase, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
			var cursor win.POINT
			win.GetCursorPos(&cursor)
			if tc.gesture != "enter" && tc.gesture != "right-key" {
				defer win.SetCursorPos(cursor.X, cursor.Y)
			}
			win.SetForegroundWindow(hwnd)
			ticks := 0
			var command win.BOOL
			var rect win.RECT
			pressed := false
			down, up := uintptr(win.MOUSEEVENTF_LEFTDOWN), uintptr(win.MOUSEEVENTF_LEFTUP)
			if tc.gesture == "right" {
				down, up = win.MOUSEEVENTF_RIGHTDOWN, win.MOUSEEVENTF_RIGHTUP
			}
			mouse := menuUser32.NewProc("mouse_event")
			defer func() {
				if pressed {
					mouse.Call(up, 0, 0, 0, 0)
				}
			}()
			choice, err := trackBoxMenu(hwnd, menu, func() {
				ticks++
				if ticks >= 24 {
					procEndMenu.Call()
					return
				}
				if ticks == 1 {
					// Keep tests independent of a user's unusually long/zero menu
					// delay, without changing the actual Windows preference.
					activeBoxMenu.hoverDelay = 150 * time.Millisecond
					if ok, _, _ := procGetMenuItemRect.Call(uintptr(hwnd), uintptr(menu), 3, uintptr(unsafe.Pointer(&rect))); ok == 0 {
						t.Error("GetMenuItemRect")
						procEndMenu.Call()
						return
					}
					x := (rect.Left + rect.Right) / 2
					if tc.arrow {
						x = rect.Right - 8
					}
					if tc.gesture != "enter" && tc.gesture != "right-key" {
						win.SetCursorPos(x, (rect.Top+rect.Bottom)/2)
					}
				}
				if tc.update && ticks > 1 {
					label := syscall.StringToUTF16Ptr(fmt.Sprintf("Collected test icon %d", ticks%2))
					item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STRING, DwTypeData: label}
					if !win.SetMenuItemInfo(menu, trayBoxedBase, false, &item) {
						t.Error("update native menu label")
					}
					redrawOpenBoxMenu()
				}
				switch tc.gesture {
				case "left", "right", "cancel":
					if ticks == 2 {
						mouse.Call(down, 0, 0, 0, 0)
						pressed = true
					}
					upTick := 2
					if tc.hold {
						upTick = 7
					}
					if ticks == upTick {
						if tc.gesture == "cancel" {
							win.SetCursorPos(rect.Left-20, rect.Top-20)
						}
						mouse.Call(up, 0, 0, 0, 0)
						pressed = false
					}
				case "disabled":
					if ticks == 2 {
						item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STATE, FState: win.MFS_DISABLED}
						if !win.SetMenuItemInfo(menu, trayBoxedBase, false, &item) {
							t.Error("disable collected row")
						}
					}
				case "leave":
					if ticks == 2 {
						win.SetCursorPos((rect.Left+rect.Right)/2, (rect.Top+rect.Bottom)/2)
					}
				case "enter", "right-key":
					// Select through the popup's normal arrow navigation.
					// A posted End key need not select a native menu row.
					if ticks >= 2 && ticks <= 4 {
						win.PostMessage(hwnd, win.WM_KEYDOWN, win.VK_DOWN, 0)
					}
					if ticks == 5 {
						key := uintptr(win.VK_RETURN)
						if tc.gesture == "right-key" {
							key = win.VK_RIGHT
						}
						win.PostMessage(hwnd, win.WM_KEYDOWN, key, 0)
					}
				}
			}, func() {
				command = win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil)
			})
			if err != nil {
				t.Fatal(err)
			}
			if choice != tc.want || command != 0 || tc.want.id == 0 && ticks < 8 {
				for _, input := range recordedMenuInput {
					t.Logf("native input: message=%#x wParam=%#x point=%+v", input.Message, input.WParam, input.Pt)
				}
				t.Fatalf("choice=%+v command=%d ticks=%d, want %+v and no WinTray command", choice, command, ticks, tc.want)
			}
			if activeBoxMenu != nil {
				t.Fatal("input handler survived menu dismissal")
			}
		})
	}
}

func newBoxMenuLoopFixture(t *testing.T, language string) (win.HWND, win.HMENU) {
	t.Helper()
	// Cleanup must still run on the thread that owns these native handles.
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	// TrackPopupMenu leaves an outside click for the owner's normal loop.
	// These fixtures have no outer loop, so drain its pending input before
	// showing the next menu instead of replaying the same dismissal.
	drainMenuTestInput()
	t.Cleanup(drainMenuTestInput)
	hwnd := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, win.GetModuleHandle(nil), nil)
	if hwnd == 0 {
		t.Fatal("create menu owner")
	}
	t.Cleanup(func() { win.DestroyWindow(hwnd) })
	recordedMenuOwner, recordedMenuInput, recordedMenuOwnerMessages = hwnd, nil, nil
	if ok, _, _ := procSetMenuSubclass.Call(uintptr(hwnd), recordMenuOwnerProc, 0x5754, 0); ok == 0 {
		t.Fatal("record owner menu notifications")
	}
	t.Cleanup(func() { procRemoveMenuSubclass.Call(uintptr(hwnd), recordMenuOwnerProc, 0x5754) })
	hook, _, err := procSetWindowsHookEx.Call(^uintptr(0), recordMenuInputProc, 0, uintptr(win.GetCurrentThreadId()))
	if hook == 0 {
		t.Fatalf("record menu input: %v", err)
	}
	t.Cleanup(func() { procUnhookWindowsHookEx.Call(hook) })
	menu := win.CreatePopupMenu()
	if menu == 0 {
		t.Fatal("create menu")
	}
	t.Cleanup(func() { win.DestroyMenu(menu) })
	if _, ok := appendContextMenu(menu, i18n.For(language), nil); !ok {
		t.Fatal("append WinTray actions")
	}
	sub := win.CreatePopupMenu()
	if sub == 0 {
		t.Fatal("create native expansion arrow")
	}
	if !appendTrayMenuItem(menu, menuItem{separator: true}) ||
		!appendTrayMenuItem(menu, menuItem{id: trayBoxedBase, text: "Collected test icon", submenu: sub}) {
		win.DestroyMenu(sub)
		t.Fatal("append collected menu item")
	}
	return hwnd, menu
}

func drainMenuTestInput() {
	var message win.MSG
	for win.PeekMessage(&message, 0, win.WM_MOUSEFIRST, win.WM_MOUSELAST, win.PM_REMOVE) {
		win.DispatchMessage(&message)
	}
	for win.PeekMessage(&message, 0, win.WM_KEYFIRST, win.WM_KEYLAST, win.PM_REMOVE) {
		win.DispatchMessage(&message)
	}
}

func TestBoxExpandRegionUsesNativeRowSize(t *testing.T) {
	for _, rect := range []win.RECT{{Left: 100, Top: 200, Right: 300, Bottom: 222}, {Left: -500, Top: -200, Right: -100, Bottom: -156}} {
		for _, tc := range []struct {
			point win.POINT
			want  bool
		}{
			{win.POINT{X: rect.Left + 8, Y: rect.Top + 8}, false},
			{win.POINT{X: rect.Right - 8, Y: rect.Top + 8}, true},
			{win.POINT{X: rect.Right, Y: rect.Top + 8}, false},
			{win.POINT{X: rect.Right - 8, Y: rect.Top - 1}, false},
			{win.POINT{X: rect.Right - 8, Y: rect.Bottom}, false},
		} {
			if got := inMenuExpandRegion(rect, tc.point); got != tc.want {
				t.Errorf("arrow hit at %+v in %+v = %t, want %t", tc.point, rect, got, tc.want)
			}
		}
	}
}

// The current native row ID/state, not a stale highlight, decides whether a
// row is usable. No desktop input is needed to test disabled/gone entries.
func TestBoxMenuOnlySelectsEnabledCollectedItems(t *testing.T) {
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
			t.Errorf("command at position %d = %d, want %d", position, got, want)
		}
	}
}

func TestBoxMenuFilterIsScopedAndRemovedAfterTracking(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
	filter := menuUser32.NewProc("CallMsgFilterW")
	call := func(message win.MSG, code int32) bool {
		result, _, _ := filter.Call(uintptr(unsafe.Pointer(&message)), uintptr(code))
		return result != 0
	}
	ticks := 0
	timer := win.MSG{HWnd: hwnd, Message: win.WM_TIMER, WParam: boxRefreshTimer}
	got, err := trackBoxMenu(hwnd, menu, func() { ticks++ }, func() {
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
		if _, err := trackBoxMenu(hwnd, menu, nil, func() { t.Error("nested menu was tracked") }); err == nil || activeBoxMenu != outer {
			t.Error("nested tracking replaced the active menu handler")
		}
		if !call(timer, msgfMenu) || ticks != 2 {
			t.Error("nested tracking removed the active menu handler")
		}
	})
	if err != nil || got != (boxMenuChoice{}) {
		t.Fatalf("tracking = %+v, %v", got, err)
	}
	if activeBoxMenu != nil || call(timer, msgfMenu) || ticks != 2 {
		t.Error("menu filter survived after tracking ended")
	}
	// Reusing this owner for another menu must not keep the old observer.
	probe := &boxMenuTracking{owner: hwnd, menu: menu, selected: -1}
	activeBoxMenu = probe
	win.SendMessage(hwnd, win.WM_MENUSELECT, uintptr(win.MF_POPUP<<16|3), uintptr(menu))
	activeBoxMenu = nil
	if probe.selected != -1 {
		t.Error("menu selection observer survived after tracking ended")
	}
}
