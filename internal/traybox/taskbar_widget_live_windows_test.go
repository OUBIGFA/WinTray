//go:build windows

package traybox

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lxn/win"
)

// A separately launched, explicitly supplied disposable taskbar widget tests
// the original issue without adding software-name exceptions to production.
func TestIndependentTaskbarWidgetLivePosition(t *testing.T) {
	path := os.Getenv("WINTRAY_TASKBAR_WIDGET_EXE")
	if path == "" {
		t.Skip("set WINTRAY_TASKBAR_WIDGET_EXE to an isolated portable widget")
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	taskbar := currentTaskbar(t)
	var widget win.HWND
	var visit func(win.HWND)
	visit = func(parent win.HWND) {
		var after uintptr
		for {
			hwnd, _, _ := procFindWindowExW.Call(uintptr(parent), after, 0, 0)
			if hwnd == 0 {
				return
			}
			after = hwnd
			var pid uint32
			win.GetWindowThreadProcessId(win.HWND(hwnd), &pid)
			if pid == uint32(cmd.Process.Pid) && win.IsWindowVisible(win.HWND(hwnd)) {
				widget = win.HWND(hwnd)
				return
			}
			visit(win.HWND(hwnd))
			if widget != 0 {
				return
			}
		}
	}
	waitFor(t, "independent taskbar widget embedded", func() bool { visit(taskbar.hwnd); return widget != 0 })
	time.Sleep(2 * time.Second)
	parent := win.GetParent(widget)
	var before win.RECT
	win.GetWindowRect(widget, &before)
	t.Logf("widget hwnd=%#x parent=%#x bounds=%+v", widget, parent, before)
	client := startNativeClient(t, nativeClientPath(t, "x64"))
	b := NewBox(nativeTestSelfPath(t), quietLogger{})
	defer b.Close()
	check := func(stage string) {
		t.Helper()
		time.Sleep(600 * time.Millisecond)
		var now win.RECT
		win.GetWindowRect(widget, &now)
		if !win.IsWindowVisible(widget) || win.GetParent(widget) != parent || now != before || currentTaskbar(t) != taskbar {
			t.Fatalf("%s: widget/taskbar changed: before=%+v now=%+v parent=%#x", stage, before, now, win.GetParent(widget))
		}
		t.Logf("%s: bounds unchanged %+v", stage, now)
	}
	for cycle := 0; cycle < 3; cycle++ {
		if err := b.SetPaths([]string{client.path}); err != nil {
			t.Fatal(err)
		}
		if client.request(t, clientAdd, 0, 0) == 0 {
			t.Fatal("fixture add rejected")
		}
		waitFor(t, "fixture collected", func() bool { return len(b.Icons()) == 1 })
		check("collection active")
		win.PostMessage(b.listener.hwnd, b.listener.taskbarCreated, 0, 0)
		check("after collector maintenance")
		client.request(t, clientDelete, 0, 0)
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		check("collection closed")
	}
}
