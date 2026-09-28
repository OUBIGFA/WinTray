//go:build windows

package ui

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/tray"
	"wintray/internal/traybox"
)

type hostedCollectionLogger struct{ t *testing.T }

func (l hostedCollectionLogger) Info(s string) { l.t.Log(s) }
func (l hostedCollectionLogger) Warn(s string) { l.t.Log(s) }

// Use a disposable off-screen window and the test process as the hosted
// program. Exercise real NotifyIcon registration and callbacks without
// starting/stopping a user's console service or changing saved settings.
func TestHostedIconsCanBeCollectedAndReleased(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	// Walk keeps process-wide notification-icon state. Give the real tray
	// lifecycle its own process, like the main-session lifecycle tests, so
	// it cannot leave queued work in another test's layout/message loop.
	if os.Getenv("WINTRAY_HOST_COLLECTION_CHILD") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(self, "-test.run=^TestHostedIconsCanBeCollectedAndReleased$", "-test.v", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "WINTRAY_HOST_COLLECTION_CHILD=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hosted collection test: %v\n%s", err, output)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	discardStaleMessages()
	defer discardStaleMessages()
	deactivate := activateTestManifest(t)
	defer deactivate()
	loop, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Dispose()
	hwnd := win.CreateWindowEx(win.WS_EX_TOOLWINDOW|win.WS_EX_NOACTIVATE, syscall.StringToUTF16Ptr("STATIC"), nil, win.WS_POPUP, -10000, -10000, 10, 10, 0, 0, 0, nil)
	if hwnd == 0 {
		t.Fatal("create hosted test window")
	}
	defer win.DestroyWindow(hwnd)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logger := hostedCollectionLogger{t}
	box := traybox.NewBox(self, logger)
	defer func() {
		if err := box.Close(); err != nil {
			t.Error(err)
		}
	}()
	host := tray.NewHost("zh-CN", logger, nil)
	host.SetTrayBox(box)
	defer host.RestoreAll()
	path := `C:\WinTray-test-only\hosted-service.exe`
	if err := box.SetPaths([]string{path}); err != nil {
		t.Fatal(err)
	}
	if err := host.TryAdd(tray.HostedWindow{Name: "WinTray collection test", ExePath: path, ProcessID: uint32(os.Getpid()), Handle: uintptr(hwnd)}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer func() {
			loop.Synchronize(func() { loop.Close() })
			close(done)
		}()
		ui := func(f func()) {
			completed := make(chan struct{})
			loop.Synchronize(func() { defer close(completed); f() })
			<-completed
		}
		await := func(what string, condition func() bool) bool {
			t.Helper()
			deadline := time.Now().Add(4 * time.Second)
			for {
				var ready bool
				ui(func() { ready = condition() })
				if ready {
					return true
				}
				if time.Now().After(deadline) {
					t.Errorf("timed out: %s", what)
					return false
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		for cycle := 0; cycle < 2; cycle++ {
			if !await("hosted icon collected", func() bool { return len(box.Icons()) == 1 }) {
				return
			}
			var icon traybox.Icon
			ui(func() { icon = box.Icons()[0] })
			if icon.DisplayName() != "WinTray collection test" || icon.Image == nil {
				t.Errorf("hosted icon lost name/image: %+v", icon)
				return
			}
			if err := traybox.Activate(icon, traybox.ActionClick); err != nil {
				t.Error(err)
				return
			}
			if !await("collected click shows window", func() bool { return win.IsWindowVisible(hwnd) }) {
				return
			}
			if err := traybox.Activate(icon, traybox.ActionClick); err != nil {
				t.Error(err)
				return
			}
			if !await("collected click hides window", func() bool { return !win.IsWindowVisible(hwnd) }) {
				return
			}
			ui(func() { err = box.SetPaths(nil) })
			if err != nil {
				t.Error(err)
				return
			}
			var collected int
			ui(func() { collected = len(box.Icons()) })
			if host.Count() != 1 || collected != 0 {
				t.Error("deselecting collection must preserve hosting")
				return
			}
			ui(func() { err = box.SetPaths([]string{path}) })
			if err != nil {
				t.Error(err)
				return
			}
		}
		if !await("reselected hosted icon", func() bool { return len(box.Icons()) == 1 }) {
			return
		}
		ui(host.RestoreAll)
		await("release restores window and removes collection", func() bool { return win.IsWindowVisible(hwnd) && len(box.Icons()) == 0 })
	}()
	loop.Run()
	<-done
}
