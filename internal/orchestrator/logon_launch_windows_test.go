//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"wintray/internal/config"
)

func TestLaunchNowInTaskModeUsesRegisteredLaunchAndDoesNotRevealExistingApp(t *testing.T) {
	entry := startupTestEntry(t)
	originalArgs := entry.Args
	entry.Args = "--not-the-original-startup-command"
	entry.LaunchViaLogonTask = true
	// Even stale contradictory flags must not make this mode handle windows.
	entry.LaunchHiddenInBackground = true
	entry.TrayBehavior.AutoMinimizeAndHideOnLaunch = true
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	calls := 0
	svc.logonTaskLaunch = func(app config.ManagedAppEntry) error {
		calls++
		cmd, err := startProcess(app.ExePath, originalArgs, launchNoWindow)
		if err == nil {
			_ = cmd.Process.Release()
		}
		return err
	}
	result := svc.StartNow(context.Background(), entry, 0)
	if result.Code != ResultStartedOnly || !result.Managed || calls != 1 || len(startupTestPIDs(entry.ExePath)) != 1 {
		t.Fatalf("original task not used: result=%+v calls=%d", result, calls)
	}
	result = svc.StartNow(context.Background(), entry, 0)
	if result.Code != ResultAlreadyRunningSkipped || calls != 1 || len(startupTestPIDs(entry.ExePath)) != 1 {
		t.Fatalf("already running app was relaunched/revealed: result=%+v calls=%d", result, calls)
	}
}

func TestLaunchNowTaskErrorsAndMissingProcessNeverFallBackToBareExe(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted but no process", true: "rejected"}[rejected], func(t *testing.T) {
			entry := startupTestEntry(t)
			entry.LaunchViaLogonTask = true
			svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
			svc.logonTaskLaunchWait = 20 * time.Millisecond
			svc.logonTaskLaunch = func(config.ManagedAppEntry) error {
				if rejected {
					return errors.New("original task is unavailable")
				}
				return nil
			}
			result := svc.StartNow(context.Background(), entry, 0)
			if result.Managed || len(startupTestPIDs(entry.ExePath)) != 0 {
				t.Fatalf("false success or bare-executable fallback: %+v", result)
			}
			want := ResultExternalStartupTimeout
			if rejected {
				want = ResultProcessStartFailed
			}
			if result.Code != want {
				t.Fatalf("result=%+v, want %s", result, want)
			}
		})
	}
}
