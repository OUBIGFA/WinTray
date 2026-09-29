package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupFrequencyPersistsAndExpires(t *testing.T) {
	var entry ManagedAppEntry
	if err := json.Unmarshal([]byte(`{"id":"daily","exePath":"C:\\Apps\\daily.exe","runOnStartup":true,"schedule":{"enabled":true,"frequencyEnabled":true,"frequencyDays":1,"frequencyRuns":1}}`), &entry); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "history.json")
	now := time.Date(2026, 9, 29, 23, 59, 0, 0, time.UTC)
	history := NewStartupHistory(path)
	launches := 0
	launch := func() error { launches++; return nil }
	if ok, err := history.Launch(entry, now, launch); err != nil || !ok {
		t.Fatalf("first launch: %t %v", ok, err)
	}
	// A new store simulates restarting WinTray; midnight does not reset a rolling day.
	if ok, err := NewStartupHistory(path).Launch(entry, now.Add(2*time.Minute), launch); err != nil || ok {
		t.Fatalf("limit after restart: %t %v", ok, err)
	}
	if ok, err := history.Launch(entry, now.Add(24*time.Hour), launch); err != nil || !ok {
		t.Fatalf("expired window: %t %v", ok, err)
	}
	if launches != 2 {
		t.Fatalf("launched %d times, want 2", launches)
	}
}

func TestStartupFrequencyRefundsFailedLaunchAndSerializesRequests(t *testing.T) {
	entry := ManagedAppEntry{ExePath: `C:\Apps\daily.exe`, RunOnStartup: true, Schedule: Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 2, FrequencyRuns: 3}}
	path := filepath.Join(t.TempDir(), "history.json")
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	failure := errors.New("could not create process")
	if ok, err := NewStartupHistory(path).Launch(entry, now, func() error { return failure }); ok || !errors.Is(err, failure) {
		t.Fatalf("failed launch: %t %v", ok, err)
	}
	var launches atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NewStartupHistory(path).Launch(entry, now, func() error { launches.Add(1); return nil })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if launches.Load() != 3 {
		t.Fatalf("concurrent launches=%d, want 3", launches.Load())
	}
	if ok, err := NewStartupHistory(path).Launch(entry, now.Add(-time.Hour), func() error { t.Error("clock rollback reset the quota"); return nil }); ok || err != nil {
		t.Fatalf("clock rollback: %t %v", ok, err)
	}
	if err := os.WriteFile(path, []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := NewStartupHistory(path).Launch(entry, now, func() error { t.Error("corruption allowed a launch"); return nil }); ok || err == nil {
		t.Fatal("corrupt history must fail closed")
	}
}

func TestFrequencySettingsPreserveIndependentAutoExit(t *testing.T) {
	var entry ManagedAppEntry
	if err := json.Unmarshal([]byte(`{"runOnStartup":true,"schedule":{"enabled":true,"autoExitMinutes":5,"frequencyEnabled":true,"frequencyDays":1,"frequencyRuns":1}}`), &entry); err != nil {
		t.Fatal(err)
	}
	if !StartupFrequencyEnabled(entry) {
		t.Fatal("frequency must apply to startup")
	}
	entry.RunOnStartup = false
	if StartupFrequencyEnabled(entry) || ScheduledRunLimit(entry) != 5 {
		t.Fatal("disabling startup must retain auto-exit only")
	}
}
