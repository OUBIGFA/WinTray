//go:build windows

package traybox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

// Opt-in because this moves the pointer and temporarily pins two disposable
// icons. Never launch WinTray alongside it; no existing app icon is dragged.
func TestTaskbarIconsLiveReorderWithCollector(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_DRAG_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_DRAG_LIVE_TEST=1 with WinTray closed and leave the mouse idle")
	}
	tray := explorerTray(0)
	if tray == 0 || win.FindWindow(syscall.StringToUTF16Ptr(trayWindowClass), nil) != tray {
		t.Fatal("Explorer must own the first taskbar window before the test")
	}
	var bounds win.RECT
	if !win.GetWindowRect(tray, &bounds) || bounds.Right-bounds.Left <= bounds.Bottom-bounds.Top {
		t.Skip("live drag fixture requires a horizontal Windows 11 taskbar")
	}
	f := startFakeShell(t)
	for i, name := range []string{"WinTray drag test A", "WinTray drag test B"} {
		nid := win.NOTIFYICONDATA{HWnd: f.program, UID: uint32(801 + i), UFlags: win.NIF_MESSAGE | win.NIF_ICON | win.NIF_TIP,
			UCallbackMessage: testCallback, HIcon: win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))}
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		copy(nid.SzTip[:], syscall.StringToUTF16(name))
		if !win.Shell_NotifyIcon(win.NIM_ADD, &nid) {
			t.Fatal("add live drag test icon")
		}
		defer win.Shell_NotifyIcon(win.NIM_DELETE, &nid)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "taskbar_drag.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	run := func(action, stage string) {
		t.Helper()
		cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
			"-Taskbar", fmt.Sprint(uintptr(tray)), "-Owner", fmt.Sprint(uintptr(f.program)), "-Action", action)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", stage, err, output)
		}
		var result struct{ Pinned, Swapped bool }
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("%s: %v\n%s", stage, err, output)
		}
		if action == "Pin" && !result.Pinned || action == "Swap" && !result.Swapped {
			t.Fatalf("%s: taskbar icons did not swap: %s", stage, output)
		}
		t.Logf("%s: %s", stage, output)
	}
	run("Pin", "fixture")
	run("Swap", "without collector")
	l, err := startListener(listenerConfig{className: trayWindowClass, target: explorerTray, raise: true}, "",
		[]string{`C:\WinTray-test-only\nothing.exe`}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.stop(); err != nil {
			t.Error(err)
		}
	}()
	run("Swap", "collector running")
	// Simulate the broadcast on our listener only; do not restart Explorer or
	// ask unrelated programs to re-register their icons in an automated test.
	win.SendMessage(l.hwnd, l.taskbarCreated, 0, 0)
	run("Swap", "after TaskbarCreated")
	run("Swap", "after priority restoration")
}
