//go:build windows

package tray

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/traybox"
)

type menuLiveLogger struct{ t *testing.T }

func (l menuLiveLogger) Info(message string) { l.t.Log(message) }
func (l menuLiveLogger) Warn(message string) { l.t.Log(message) }

// Dump unfiltered menu windows when a live fixture cannot observe its popup.
func logMenuWindows(t *testing.T, pid uint32) {
	proc := syscall.NewCallback(func(hwnd win.HWND, _ uintptr) uintptr {
		var owner uint32
		win.GetWindowThreadProcessId(hwnd, &owner)
		if owner != pid {
			return 1
		}
		var name [128]uint16
		win.GetClassName(hwnd, &name[0], len(name))
		if windows.UTF16ToString(name[:]) == "#32768" {
			var rect win.RECT
			ok := win.GetWindowRect(hwnd, &rect)
			t.Logf("raw native hwnd=%#x visible=%t style=%#x rect-ok=%t rect=%+v", hwnd, win.IsWindowVisible(hwnd), uint32(win.GetWindowLong(hwnd, win.GWL_STYLE)), ok, rect)
		}
		return 1
	})
	procEnumWindows.Call(proc, 0)
}

// Explicit opt-in: exercises the original menus of already-running programs.
// It changes no saved settings, launches no managed program and restores the
// collected icons on exit. Close WinTray first so only one broker owns them.
func TestExistingCollectedMenuLifecycle(t *testing.T) {
	paths := strings.Split(os.Getenv("WINTRAY_MENU_EXISTING_PATHS"), "|")
	if len(paths) == 1 && paths[0] == "" {
		t.Skip("set WINTRAY_MENU_EXISTING_PATHS with WinTray closed")
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("verify ordinary user privilege")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	self := os.Getenv("WINTRAY_MENU_SELF_PATH")
	if self == "" {
		self = "../../dist/WinTray.exe"
	}
	self, err := filepath.Abs(self)
	if err != nil {
		t.Fatal(err)
	}
	box := traybox.NewBox(self, menuLiveLogger{t})
	defer func() {
		if err := box.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := box.SetPaths(paths); err != nil {
		t.Fatal(err)
	}
	var icons []traybox.Icon
	waitMenuCondition(t, "selected running icons", func() bool {
		icons = nil
		for _, path := range paths {
			for _, icon := range box.Icons() {
				if strings.EqualFold(path, icon.ExePath) {
					icons = append(icons, icon)
					break
				}
			}
		}
		return len(icons) == len(paths)
	})
	hwnd, menu := newBoxMenuLoopFixture(t, "en-US")
	for i := 1; i < len(icons); i++ {
		sub := win.CreatePopupMenu()
		if sub == 0 || !appendTrayMenuItem(menu, menuItem{id: uint32(trayBoxedBase + i), text: icons[i].DisplayName(), submenu: sub}) {
			t.Fatal("append existing program")
		}
	}
	var cursor win.POINT
	win.GetCursorPos(&cursor)
	defer win.SetCursorPos(cursor.X, cursor.Y)
	win.SetForegroundWindow(hwnd)
	index, stage, ticks := 0, 0, 0
	switching := os.Getenv("WINTRAY_MENU_EXISTING_SWITCH") == "1"
	var stageAt time.Time
	var observed []programMenuWindow
	var finalErr error
	advance := func(next int) { stage, stageAt = next, time.Now() }
	rowAt := func(i int) win.RECT {
		var row win.RECT
		if ok, _, _ := procGetMenuItemRect.Call(uintptr(hwnd), uintptr(menu), uintptr(i+3), uintptr(unsafe.Pointer(&row))); ok == 0 {
			finalErr = fmt.Errorf("row %d unavailable", i)
			procEndMenu.Call()
		}
		return row
	}
	checkClosed := func() {
		for _, popup := range observed {
			if win.IsWindowVisible(popup.hwnd) {
				finalErr = fmt.Errorf("popup remains visible: %s", icons[index].DisplayName())
			}
			if popup.native && queryMenuThread(popup.thread).menuOwner == popup.gui.menuOwner {
				finalErr = fmt.Errorf("native menu loop remains active: %s", icons[index].DisplayName())
			}
		}
		t.Logf("closed %s; WinTray parent still active", icons[index].DisplayName())
	}
	controller := &Controller{box: box}
	_, err = trackBoxMenu(hwnd, menu, func() {
		ticks++
		if stage == 99 {
			return
		}
		if ticks > 600 || !stageAt.IsZero() && time.Since(stageAt) > 5*time.Second {
			finalErr = fmt.Errorf("existing menu stalled: %s stage=%d", icons[index].DisplayName(), stage)
			var pointer win.POINT
			win.GetCursorPos(&pointer)
			var pid uint32
			win.GetWindowThreadProcessId(icons[index].CallbackWindow(), &pid)
			t.Logf("cursor=%+v hover=%d suppressed=%d popup=%t candidates=%+v", pointer, activeBoxMenu.hoverID, activeBoxMenu.rightSuppressed, activeBoxMenu.popup != nil, scanProgramMenuWindows(pid))
			logMenuWindows(t, pid)
			if activeBoxMenu.popup != nil {
				t.Logf("popup=%+v candidates=%+v", activeBoxMenu.popup.windows, scanProgramMenuWindows(activeBoxMenu.popup.pid))
			}
			procEndMenu.Call()
			return
		}
		switch stage {
		case 0:
			row := rowAt(index)
			win.SetCursorPos(row.Right-2, (row.Top+row.Bottom)/2)
			advance(1)
		case 1:
			p := activeBoxMenu.popup
			if p == nil || len(p.windows) == 0 {
				return
			}
			observed = append([]programMenuWindow(nil), p.windows...)
			for _, popup := range observed {
				var class [128]uint16
				win.GetClassName(popup.hwnd, &class[0], len(class))
				t.Logf("opened %s pid=%d popup=%#x class=%s native-owner=%#x", icons[index].DisplayName(), p.pid, popup.hwnd, windows.UTF16ToString(class[:]), popup.gui.menuOwner)
			}
			r := observed[0].rect
			win.SetCursorPos((r.Left+r.Right)/2, (r.Top+r.Bottom)/2)
			advance(2)
		case 2:
			if time.Since(stageAt) < 250*time.Millisecond {
				return
			}
			if activeBoxMenu.popup == nil {
				finalErr = fmt.Errorf("menu closed while pointer was inside: %s", icons[index].DisplayName())
				procEndMenu.Call()
				return
			}
			if switching && index+1 < len(icons) {
				row := rowAt(index + 1)
				// Use a still-visible part of the next arrow. Some original
				// menus overlap a few pixels of the parent that launched them.
				for x := row.Right - 2; x >= row.Right-(row.Bottom-row.Top); x-- {
					point := win.POINT{X: x, Y: (row.Top + row.Bottom) / 2}
					covered := false
					for _, popup := range activeBoxMenu.popup.windows {
						covered = covered || pointInMenuRect(popup.rect, point)
					}
					if !covered {
						win.SetCursorPos(point.X, point.Y)
						advance(4)
						return
					}
				}
				win.SetCursorPos(row.Left+8, (row.Top+row.Bottom)/2)
				advance(5)
				return
			}
			win.SetCursorPos(20, 700)
			advance(3)
		case 3:
			if activeBoxMenu.popup != nil {
				return
			}
			checkClosed()
			index++
			if finalErr != nil || index == len(icons) {
				stage = 99
				procEndMenu.Call()
				return
			}
			advance(0)
		case 4:
			if activeBoxMenu.expandedID != uint32(trayBoxedBase+index+1) {
				return
			}
			checkClosed()
			if finalErr != nil {
				procEndMenu.Call()
				return
			}
			index++
			advance(1)
		case 5:
			if activeBoxMenu.popup != nil {
				return
			}
			row := rowAt(index + 1)
			win.SetCursorPos(row.Right-2, (row.Top+row.Bottom)/2)
			advance(4)
		}
	}, func() { win.TrackPopupMenuEx(menu, trayMenuFlags, 150, 250, hwnd, nil) }, func(id uint32) (boxMenuExpansion, error) {
		icon, resolveErr := controller.currentBoxIcon(icons, id)
		if resolveErr != nil {
			return boxMenuExpansion{}, resolveErr
		}
		t.Logf("request expansion: %s owner=%#x", icon.DisplayName(), icon.CallbackWindow())
		return boxMenuExpansion{owner: icon.CallbackWindow(), open: func() error { return traybox.Activate(icon, traybox.ActionRightClick) }, report: func(err error) { t.Logf("close diagnostic: %s: %v", icon.DisplayName(), err) }}, nil
	})
	if err != nil || finalErr != nil || stage != 99 {
		t.Fatalf("existing menus: stage=%d err=%v close=%v", stage, err, finalErr)
	}
	waitMenuCondition(t, "existing popup cleanup", func() bool { return retiringProgramMenu == nil })
}
