//go:build windows

package orchestrator

import (
	"context"
	"github.com/lxn/win"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"wintray/internal/config"
)

func qqFixtureEntry(t *testing.T, mode string) (config.ManagedAppEntry, string) {
	t.Helper()
	entry, file := weChatFixtureEntry(t, "qq-"+mode)
	data, err := os.ReadFile(entry.ExePath)
	if err != nil {
		t.Fatal(err)
	}
	entry.ExePath = filepath.Join(filepath.Dir(entry.ExePath), "QQ.exe")
	if err := os.WriteFile(entry.ExePath, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pid := findRunningProcessByIdentity(entry.ExePath, ""); pid != 0 {
			p, err := os.FindProcess(int(pid))
			if err == nil {
				_ = p.Kill()
				_, _ = p.Wait()
			}
		}
	})
	entry.WeChatAutoLogin = false
	entry.QQWaitForLogin = true
	entry.TrayBehavior.CloseDelaySeconds = 600
	return entry, file
}

// A real HWND/UIA provider covers the transition, refused close, and restoration.
// QQ must never receive a click on its login button, even while we keep waiting.
func TestQQNativeWaitAndRetryUntilDismissed(t *testing.T) {
	entry, file := qqFixtureEntry(t, "retry")
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	svc.externalStartupLookup = func(string) (string, error) { return "", nil }
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	done := make(chan Result, 1)
	go func() { done <- svc.StartAndManage(ctx, entry, 0) }()
	state := awaitStartupFixture(t, file)
	time.Sleep(1100 * time.Millisecond)
	if !isWindowVisible(state.Forms[0]) || visibilityOwner(state.Forms[0]) != 0 {
		t.Fatal("QQ login was hidden before authentication")
	}
	if clicks := win.SendMessage(win.HWND(state.Owner), win.WM_APP+20, 0, 0); clicks != 0 {
		t.Fatalf("QQ login clicked %d times", clicks)
	}
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	result := <-done
	if !result.Managed || result.Action != "close" || isWindowVisible(state.Forms[1]) || !svc.IsProgramRunning(entry) {
		t.Fatalf("QQ main not safely dismissed: %+v", result)
	}
	if attempts := win.SendMessage(win.HWND(state.Owner), win.WM_APP+22, 0, 0); attempts < 2 {
		t.Fatal("refused close did not retry")
	}
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	time.Sleep(700 * time.Millisecond)
	if !isWindowVisible(state.Forms[1]) {
		t.Fatal("completed startup dismissed a manually reopened window")
	}
}

func TestQQNativeUnknownWindowStaysVisible(t *testing.T) {
	for _, mode := range []string{"unknown", "unknown-title", "unknown-label"} {
		t.Run(mode, func(t *testing.T) {
			entry, file := qqFixtureEntry(t, mode)
			cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
			if err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Release()
			state := awaitStartupFixture(t, file)
			win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
			svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			defer cancel()
			result := svc.StartNow(ctx, entry, 0)
			if result.Managed || !isWindowVisible(state.Forms[1]) || !svc.IsProgramRunning(entry) {
				t.Fatalf("unknown QQ window handled: %+v", result)
			}
		})
	}
}

func TestQQWaitStopsWhenProcessExits(t *testing.T) {
	entry, file := qqFixtureEntry(t, "phone")
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	awaitStartupFixture(t, file)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan Result, 1)
	go func() { done <- svc.StartNow(ctx, entry, 0) }()
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.Managed {
			t.Fatalf("exited QQ reported as managed: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("QQ login observation survived process exit")
	}
}

func TestLoginMainAlreadyHiddenCompletes(t *testing.T) {
	entry, file := qqFixtureEntry(t, "phone")
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	state := awaitStartupFixture(t, file)
	// The main window was positively recognized, then the application itself
	// hid it before WinTray's dismissal worker started.
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	win.ShowWindow(win.HWND(state.Forms[1]), win.SW_HIDE)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = svc.dismissLoginMain(ctx, entry.ExePath, HiddenWindow{state.Forms[1], uint32(cmd.Process.Pid)})
	if err != nil {
		t.Fatalf("already hidden recognized main kept waiting: %v", err)
	}
	if !svc.IsProgramRunning(entry) {
		t.Fatal("already hidden program was ended")
	}
}

func TestQQDoesNotChooseBetweenAccounts(t *testing.T) {
	entry, file := qqFixtureEntry(t, "phone")
	first, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Process.Release()
	state := awaitStartupFixture(t, file)
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	secondFile := filepath.Join(filepath.Dir(file), "second.json")
	second, err := startProcess(entry.ExePath, "-test.run=^TestWeChatUIAProcess$ -- "+syscall.EscapeArg(secondFile)+" qq-phone", launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Process.Release()
	defer second.Process.Kill()
	secondState := awaitStartupFixture(t, secondFile)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if recognized, err := observeLoginMain(ctx, entry.ExePath, HiddenWindow{state.Forms[1], uint32(first.Process.Pid)}); err != nil || !recognized {
		t.Fatalf("main fixture not observable: %t %v", recognized, err)
	}
	main, err := waitQQLogin(ctx, entry.ExePath)
	if err == nil || main.Handle != 0 {
		t.Fatalf("selected another account's main while login was open: %+v %v", main, err)
	}
	if !isWindowVisible(state.Forms[1]) || !isWindowVisible(secondState.Forms[0]) {
		t.Fatal("multi-account observation changed visibility")
	}
}
