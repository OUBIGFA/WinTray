//go:build windows

package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wintray/internal/config"
)

func queueTestEntry(t *testing.T, name string) config.ManagedAppEntry {
	t.Helper()
	entry := startupTestEntry(t)
	entry.Name = name
	entry.LaunchHiddenInBackground = true
	return entry
}

func queueTestService() *Service {
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	svc.externalStartupLookup = func(string) (string, error) { return "", nil }
	return svc
}

func queueProcessStart(t *testing.T, entry config.ManagedAppEntry) time.Time {
	t.Helper()
	pids := startupTestPIDs(entry.ExePath)
	if len(pids) != 1 {
		t.Fatalf("%s processes = %v, want exactly one", entry.Name, pids)
	}
	created, ok := processStartTime(pids[0])
	if !ok {
		t.Fatalf("could not read creation time for %s", entry.Name)
	}
	return created
}

func TestStartupQueueSpacesActualLaunchesInListOrder(t *testing.T) {
	first := queueTestEntry(t, "first")
	second := queueTestEntry(t, "second")
	third := queueTestEntry(t, "third")
	settings := config.DefaultSettings()
	settings.StartupIntervalSeconds = 1
	settings.ManagedApps = []config.ManagedAppEntry{first, second, third}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	results := queueTestService().StartManagedApps(ctx, settings, nil)
	if len(results) != 3 {
		t.Fatalf("results = %+v, want three launches", results)
	}
	var previous time.Time
	for i, entry := range settings.ManagedApps {
		if results[i].AppName != entry.Name || results[i].Code != ResultStartedHidden {
			t.Fatalf("result %d = %+v, want %s started hidden", i, results[i], entry.Name)
		}
		created := queueProcessStart(t, entry)
		if i > 0 && created.Sub(previous) < time.Second {
			t.Fatalf("%s started only %s after previous process; want >= 1s", entry.Name, created.Sub(previous))
		}
		previous = created
	}
}

func TestStartupQueueSkipsPausedInvalidAndExistingWithoutConsumingInterval(t *testing.T) {
	paused := queueTestEntry(t, "paused")
	paused.RunOnStartup = false
	invalid := config.ManagedAppEntry{Name: "invalid", ExePath: filepath.Join(t.TempDir(), "invalid.exe"), RunOnStartup: true}
	if err := os.WriteFile(invalid.ExePath, []byte("invalid PE"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := queueTestEntry(t, "existing")
	cmd, err := startProcess(existing.ExePath, existing.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	fresh := queueTestEntry(t, "fresh")
	settings := config.DefaultSettings()
	settings.StartupIntervalSeconds = 120
	settings.ManagedApps = []config.ManagedAppEntry{paused, {Name: "empty", RunOnStartup: true}, invalid, existing, fresh, fresh}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := queueTestService().StartManagedApps(ctx, settings, nil)
	want := []ResultCode{ResultEmptyExePath, ResultProcessStartFailed, ResultAlreadyRunningSkipped, ResultStartedHidden, ResultAlreadyRunningSkipped}
	if len(results) != len(want) {
		t.Fatalf("results = %+v, want paused entry omitted", results)
	}
	for i, code := range want {
		if results[i].Code != code {
			t.Errorf("result %d = %+v, want %s", i, results[i], code)
		}
	}
	queueProcessStart(t, fresh)
	if pids := startupTestPIDs(paused.ExePath); len(pids) != 0 {
		t.Fatalf("paused entry launched: %v", pids)
	}
}

func TestStartupQueueWindowAndExternalWaitsDoNotBlockNextLaunchOrCallback(t *testing.T) {
	external := queueTestEntry(t, "external")
	existing := queueTestEntry(t, "waiting for login")
	existing.LaunchHiddenInBackground = false
	existing.TrayBehavior = config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 60}
	cmd, err := startProcess(existing.ExePath, existing.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	restore := overrideConsoleSeams(t, func(string) bool { return false }, 0, 0, false)
	defer restore()
	fresh := queueTestEntry(t, "fresh")
	settings := config.DefaultSettings()
	settings.StartupIntervalSeconds = 120
	settings.ManagedApps = []config.ManagedAppEntry{external, existing, fresh}
	svc := queueTestService()
	svc.externalStartupLookup = func(path string) (string, error) {
		if path == external.ExePath {
			return `HKCU\Run\test`, nil
		}
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := svc.StartManagedApps(ctx, settings, func(entry config.ManagedAppEntry, result Result) {
		if entry.Name == fresh.Name && result.Managed {
			// This callback must run even while the earlier tasks are waiting.
			cancel()
		}
	})
	if len(results) != 3 || results[2].Code != ResultStartedHidden || results[0].Code != ResultCancelled || results[1].Code != ResultCancelled {
		t.Fatalf("results = %+v, want fresh launch before earlier waits finish", results)
	}
	queueProcessStart(t, fresh)
	if pids := startupTestPIDs(external.ExePath); len(pids) != 0 {
		t.Fatalf("externally owned entry launched by WinTray: %v", pids)
	}
}

func TestStartupQueueCancellationStopsPendingLaunches(t *testing.T) {
	first := queueTestEntry(t, "first")
	second := queueTestEntry(t, "second")
	third := queueTestEntry(t, "third")
	settings := config.DefaultSettings()
	settings.StartupIntervalSeconds = 120
	settings.ManagedApps = []config.ManagedAppEntry{first, second, third}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := queueTestService().StartManagedApps(ctx, settings, func(entry config.ManagedAppEntry, _ Result) {
		if entry.Name == first.Name {
			cancel()
		}
	})
	if len(results) != 3 || results[0].Code != ResultStartedHidden || results[1].Code != ResultCancelled || results[2].Code != ResultCancelled {
		t.Fatalf("results = %+v, want pending launches cancelled", results)
	}
	queueProcessStart(t, first)
	for _, entry := range []config.ManagedAppEntry{second, third} {
		if pids := startupTestPIDs(entry.ExePath); len(pids) != 0 {
			t.Fatalf("%s launched after cancellation: %v", entry.Name, pids)
		}
	}
}

func TestStartupQueueRechecksBeforeLaunchingAfterInterval(t *testing.T) {
	first := queueTestEntry(t, "first")
	second := queueTestEntry(t, "started elsewhere while queued")
	settings := config.DefaultSettings()
	settings.StartupIntervalSeconds = 1
	settings.ManagedApps = []config.ManagedAppEntry{first, second}
	svc := queueTestService()
	svc.externalStartupLookup = func(path string) (string, error) {
		if path == second.ExePath {
			// Appears after the first running check but before the launch slot.
			cmd, err := startProcess(second.ExePath, second.Args, launchNoWindow)
			if err != nil {
				return "", err
			}
			_ = cmd.Process.Release()
		}
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := svc.StartManagedApps(ctx, settings, nil)
	if len(results) != 2 || results[1].Code != ResultAlreadyRunningSkipped {
		t.Fatalf("results=%+v, want queued process adopted instead of duplicated", results)
	}
	queueProcessStart(t, second)
}

func TestStartupQueueZeroIntervalAndEmptyBatch(t *testing.T) {
	settings := config.DefaultSettings()
	settings.StartupIntervalSeconds = 0
	svc := queueTestService()
	if got := svc.StartManagedApps(context.Background(), settings, nil); len(got) != 0 {
		t.Fatalf("empty queue returned %+v", got)
	}
	settings.ManagedApps = []config.ManagedAppEntry{queueTestEntry(t, "first"), queueTestEntry(t, "second")}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := svc.StartManagedApps(ctx, settings, nil)
	for i, result := range results {
		if result.Code != ResultStartedHidden {
			t.Fatalf("zero interval result %d = %+v", i, result)
		}
	}
	first := queueProcessStart(t, settings.ManagedApps[0])
	second := queueProcessStart(t, settings.ManagedApps[1])
	if second.Before(first) {
		t.Fatal("zero interval reversed list order")
	}
}
