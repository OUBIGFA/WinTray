//go:build windows

package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"wintray/internal/config"
)

// Optional real-runtime coverage uses a portable interpreter supplied by the
// caller. HKCR is overridden only inside this child, not in the user's session.
func TestAutoHotkeyRuntime(t *testing.T) {
	interpreter := os.Getenv("WINTRAY_AHK_TEST_EXE")
	if interpreter == "" {
		t.Skip("set WINTRAY_AHK_TEST_EXE to a portable AutoHotkey v1/v2 executable")
	}
	if len(os.Args) != 4 || os.Args[2] != "--" || os.Args[3] != "ahk-runtime" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, self, "-test.run=^TestAutoHotkeyRuntime$", "--", "ahk-runtime")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("AutoHotkey runtime child: %v\n%s", err, output)
		}
		return
	}
	isolatedScriptAssociation(t, syscall.EscapeArg(interpreter)+` "%1" %*`)
	dir := t.TempDir()
	entry := config.ManagedAppEntry{ID: "ahk", Name: "hotkeys", ExePath: filepath.Join(dir, "中文 hotkeys.ahk"), RunOnStartup: true, LaunchHiddenInBackground: true}
	other := entry
	other.ID, other.ExePath = "other", filepath.Join(dir, "other.ahk")
	other.Args = syscall.EscapeArg(entry.ExePath)
	// This command syntax is valid in both runtimes. No single-instance guard:
	// duplicate prevention must come from WinTray, not from AutoHotkey.
	for _, path := range []string{entry.ExePath, other.ExePath} {
		if err := os.WriteFile(path, []byte("#SingleInstance Off\nSleep 20000\nExitApp\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { terminateProcessTrees(runningProgramPIDs(path, filepath.Base(path))) })
	}
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, item := range []config.ManagedAppEntry{entry, other} {
		if result := svc.StartNow(ctx, item, 1); !result.Managed || result.Code != ResultStartedHidden {
			t.Fatalf("real script did not start: %+v", result)
		}
	}
	if !svc.IsProgramRunning(entry) || !svc.IsProgramRunning(other) {
		t.Fatal("running AutoHotkey scripts were not recognized")
	}
	if result := svc.StartNow(ctx, entry, 1); !result.Managed || result.Code != ResultAlreadyRunningSkipped {
		t.Fatalf("relaunch did not detect existing script: %+v", result)
	}
	if pids := runningProgramPIDs(entry.ExePath, entry.Name); len(pids) != 1 {
		t.Fatalf("script launched more than once: %v", pids)
	}
	if err := svc.StopNow(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if svc.IsProgramRunning(entry) || !svc.IsProgramRunning(other) {
		t.Fatal("stop retained the target or terminated the other script")
	}
}
