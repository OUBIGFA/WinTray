//go:build windows

package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIntegration_HiddenConsoleLaunch starts a real console program with a
// hidden console and checks that its console window exists, is invisible and
// can be shown again — the contract the tray host relies on.
func TestIntegration_HiddenConsoleLaunch(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "ping.exe")
	if _, err := os.Stat(ping); err != nil {
		t.Skipf("ping.exe not available: %v", err)
	}
	if !isConsoleExecutable(ping) {
		t.Fatalf("%s should be detected as a console executable", ping)
	}

	cmd, err := startProcess(ping, "-t 127.0.0.1", launchHiddenConsole)
	if err != nil {
		t.Fatalf("start hidden console: %v", err)
	}
	pid := uint32(cmd.Process.Pid)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	hwnd := waitForConsoleWindow(context.Background(), pid, consoleWindowWait)
	if hwnd == 0 {
		t.Fatalf("console window of pid %d not found within %s", pid, consoleWindowWait)
	}
	if isWindowVisible(hwnd) {
		t.Fatalf("console window 0x%X should start hidden", hwnd)
	}

	mgr := NewWin32WindowManager()
	if _, _, _ = procShowWindowAsync.Call(hwnd, 5 /* SW_SHOW */); !waitVisible(hwnd, true) {
		t.Fatalf("console window 0x%X did not become visible after SW_SHOW", hwnd)
	}
	if ok, err := mgr.HideWindow(hwnd); !ok {
		t.Fatalf("HideWindow: %v", err)
	}
	if !waitVisible(hwnd, false) {
		t.Fatalf("console window 0x%X did not hide again", hwnd)
	}
	if FindConsoleWindow(pid) != hwnd {
		t.Fatalf("FindConsoleWindow(%d) should keep returning the hidden window 0x%X", pid, hwnd)
	}
}

func waitVisible(hwnd uintptr, want bool) bool {
	for i := 0; i < 20; i++ {
		if isWindowVisible(hwnd) == want {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
