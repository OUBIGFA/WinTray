//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"wintray/internal/config"
)

func startupTestEntry(t *testing.T) config.ManagedAppEntry {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	entry := config.ManagedAppEntry{
		Name: "QQ", ExePath: filepath.Join(dir, "qq.exe"), RunOnStartup: true,
		Args: runnerHelperArgs("wait", filepath.Join(dir, "release")),
	}
	if err := os.WriteFile(entry.ExePath, image, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, pid := range startupTestPIDs(entry.ExePath) {
			p, err := os.FindProcess(int(pid))
			if err == nil {
				_ = p.Kill()
				_, _ = p.Wait()
			}
		}
	})
	return entry
}

func startupTestPIDs(path string) []uint32 {
	var pids []uint32
	forEachRunningProcessByIdentity(path, "qq", func(pid uint32) bool {
		pids = append(pids, pid)
		return true
	})
	return pids
}

func TestExternalStartupLateProcessIsAdoptedWithoutDuplicate(t *testing.T) {
	entry := startupTestEntry(t)
	entry.TrayBehavior = config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 1}
	restore := overrideConsoleSeams(t, func(string) bool { return false }, 0, 0, false)
	defer restore()
	mgr := &timedManager{}
	svc := NewService(&launchedWindowEnumerator{exePath: entry.ExePath}, mgr, &testLogger{})
	svc.externalStartupWait = 3 * time.Second
	checked := make(chan struct{})
	svc.externalStartupLookup = func(string) (string, error) {
		close(checked)
		return `HKCU\Run\QQNT`, nil
	}
	// Simulate Windows starting the app after WinTray has found no process.
	started := make(chan *exec.Cmd, 1)
	failed := make(chan error, 1)
	go func() {
		<-checked
		time.Sleep(300 * time.Millisecond)
		cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
		if err != nil {
			failed <- err
			return
		}
		started <- cmd
	}()
	got := svc.StartAndManage(context.Background(), entry, 2)
	var cmd *exec.Cmd
	select {
	case cmd = <-started:
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("external process was not started")
	}
	defer cmd.Process.Release()
	pids := startupTestPIDs(entry.ExePath)
	if len(pids) != 1 || pids[0] != uint32(cmd.Process.Pid) {
		t.Fatalf("running processes = %v, want only external pid %d", pids, cmd.Process.Pid)
	}
	if got.Code != ResultAlreadyRunningManaged || len(mgr.closeCalls) != 1 {
		t.Fatalf("result=%+v close=%v, want adopted window closed once", got, mgr.closeCalls)
	}
	created, ok := processStartTime(uint32(cmd.Process.Pid))
	if !ok || mgr.closeAt.Sub(created) < time.Second {
		t.Fatalf("login window closed too early: created=%v closed=%v known=%t", created, mgr.closeAt, ok)
	}
}

func TestExternalStartupTimeoutNeverFallsBackToLaunching(t *testing.T) {
	entry := startupTestEntry(t)
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	svc.externalStartupLookup = func(string) (string, error) { return `HKCU\Run\QQNT`, nil }
	svc.externalStartupWait = 50 * time.Millisecond
	got := svc.StartAndManage(context.Background(), entry, 0)
	if got.Managed || got.Code != ResultExternalStartupTimeout {
		t.Fatalf("result=%+v, want explicit external startup timeout", got)
	}
	if pids := startupTestPIDs(entry.ExePath); len(pids) != 0 {
		t.Fatalf("WinTray launched fallback process(es): %v", pids)
	}
	// An arbitrarily late Windows launch must still leave only one instance.
	cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	if pids := startupTestPIDs(entry.ExePath); len(pids) != 1 {
		t.Fatalf("late external launch resulted in %v, want one process", pids)
	}
}

func TestStartupCancelledOrUninspectableDoesNotLaunch(t *testing.T) {
	for _, tc := range []struct {
		name          string
		cancelBefore  bool
		cancelWaiting bool
		probeError    bool
		want          ResultCode
	}{
		{name: "cancel before startup", cancelBefore: true, want: ResultCancelled},
		{name: "cancel while waiting", cancelWaiting: true, want: ResultCancelled},
		{name: "cannot inspect startup", probeError: true, want: ResultStartupCheckFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := startupTestEntry(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelBefore {
				cancel()
			}
			svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
			svc.externalStartupLookup = func(string) (string, error) {
				if tc.probeError {
					return "", errors.New("access denied")
				}
				if tc.cancelWaiting {
					time.AfterFunc(50*time.Millisecond, cancel)
				}
				return `HKCU\Run\QQNT`, nil
			}
			before := time.Now()
			got := svc.StartAndManage(ctx, entry, 0)
			if got.Managed || got.Code != tc.want || time.Since(before) > time.Second {
				t.Fatalf("result=%+v elapsed=%s, want prompt %s", got, time.Since(before), tc.want)
			}
			if pids := startupTestPIDs(entry.ExePath); len(pids) != 0 {
				t.Fatalf("unexpected launch: %v", pids)
			}
		})
	}
}

func TestStartupWithoutEnabledRunEntryAndManualStartStillLaunch(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "no enabled Run entry", true: "manual ignores external startup"}[manual], func(t *testing.T) {
			entry := startupTestEntry(t)
			entry.LaunchHiddenInBackground = true
			svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
			svc.externalStartupLookup = func(string) (string, error) {
				if manual {
					t.Fatal("manual launch must not wait for a future logon startup")
				}
				return "", nil
			}
			var got Result
			if manual {
				got = svc.StartNow(context.Background(), entry, 0)
			} else {
				got = svc.StartAndManage(context.Background(), entry, 0)
			}
			if got.Code != ResultStartedHidden || len(startupTestPIDs(entry.ExePath)) != 1 {
				t.Fatalf("result=%+v, want one normally launched process", got)
			}
			// A second request adopts the existing process, including without a GUI.
			again := svc.StartNow(context.Background(), entry, 0)
			if again.Code != ResultAlreadyRunningSkipped || len(startupTestPIDs(entry.ExePath)) != 1 {
				t.Fatalf("second request=%+v, want existing process without a duplicate", again)
			}
		})
	}
}

type callbackEnumerator func() []ManagedWindowInfo

func (e callbackEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo { return e() }

func TestStartupRechecksAfterBaselineBeforeLaunching(t *testing.T) {
	entry := startupTestEntry(t)
	scans := 0
	enum := callbackEnumerator(func() []ManagedWindowInfo {
		scans++
		if scans == 2 { // baseline scan, after the initial running check
			cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
			if err != nil {
				t.Fatal(err)
			}
			_ = cmd.Process.Release()
		}
		return nil
	})
	svc := NewService(enum, &testManager{}, &testLogger{})
	got := svc.StartNow(context.Background(), entry, 0)
	if got.Code != ResultAlreadyRunningSkipped || len(startupTestPIDs(entry.ExePath)) != 1 {
		t.Fatalf("result=%+v processes=%v, want adoption after baseline scan", got, startupTestPIDs(entry.ExePath))
	}
}
