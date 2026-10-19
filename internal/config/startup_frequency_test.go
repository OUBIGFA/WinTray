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
	_ "time/tzdata" // Keep DST cases independent of the host's installed zone database.
)

func TestStartupFrequencyPersistsUntilLocalMidnight(t *testing.T) {
	var entry ManagedAppEntry
	if err := json.Unmarshal([]byte(`{"id":"daily","exePath":"C:\\Apps\\daily.exe","runOnStartup":true,"schedule":{"enabled":true,"frequencyEnabled":true,"frequencyDays":1,"frequencyRuns":1}}`), &entry); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "history.json")
	beijing := time.FixedZone("Beijing", 8*60*60)
	now := time.Date(2026, 9, 29, 23, 59, 0, 0, beijing)
	history := NewStartupHistory(path)
	launches := 0
	launch := func() error { launches++; return nil }
	if ok, err := history.Launch(entry, now, launch); err != nil || !ok {
		t.Fatalf("first launch: %t %v", ok, err)
	}
	// Restarting preserves the current local day's count, but the next
	// midnight starts a new day even when less than 24 hours have elapsed.
	if ok, err := NewStartupHistory(path).Launch(entry, now.Add(30*time.Second), launch); err != nil || ok {
		t.Fatalf("limit after restart: %t %v", ok, err)
	}
	midnight := now.Add(time.Minute)
	if ok, err := history.Launch(entry, midnight, launch); err != nil || !ok {
		t.Fatalf("local midnight: %t %v", ok, err)
	}
	if ok, err := NewStartupHistory(path).Launch(entry, midnight.Add(time.Nanosecond), launch); err != nil || ok {
		t.Fatalf("launch at the midnight boundary was not counted: %t %v", ok, err)
	}
	if launches != 2 {
		t.Fatalf("launched %d times, want 2", launches)
	}
}

// Stored timestamps keep their original offsets. The current local calendar,
// not the stored timestamp's date or the UTC date, decides the counting window.
func TestStartupFrequencyUsesCurrentLocalCalendar(t *testing.T) {
	for _, zone := range []struct {
		name   string
		offset int
	}{{"Beijing", 8 * 60 * 60}, {"Pacific", -7 * 60 * 60}, {"Nepal", (5*60 + 45) * 60}} {
		t.Run(zone.name, func(t *testing.T) {
			local := time.FixedZone(zone.name, zone.offset)
			entry := ManagedAppEntry{ExePath: `C:\Apps\daily.exe`, RunOnStartup: true, Schedule: Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}}
			path := filepath.Join(t.TempDir(), "history.json")
			stamp := time.Date(2026, 9, 29, 23, 0, 0, 0, local)
			launch := func() error { return nil }
			if ok, err := NewStartupHistory(path).Launch(entry, stamp.UTC(), launch); err != nil || !ok {
				t.Fatalf("save UTC history: %t %v", ok, err)
			}
			if ok, err := NewStartupHistory(path).Launch(entry, stamp.Add(30*time.Minute), launch); err != nil || ok {
				t.Fatalf("same local day: %t %v", ok, err)
			}
			if ok, err := NewStartupHistory(path).Launch(entry, stamp.Add(time.Hour), launch); err != nil || !ok {
				t.Fatalf("next local day: %t %v", ok, err)
			}
		})
	}
}

func TestStartupFrequencyIncludesTodayAndPreviousCalendarDays(t *testing.T) {
	local := time.FixedZone("Beijing", 8*60*60)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, local)
	cutoff := time.Date(2026, 9, 30, 0, 0, 0, 0, local)
	for _, tc := range []struct {
		name  string
		stamp time.Time
		allow bool
	}{
		{"before oldest day", cutoff.Add(-time.Nanosecond), true},
		{"oldest day midnight", cutoff, false},
		{"previous day", cutoff.Add(18 * time.Hour), false},
		{"today", now.Add(-time.Hour), false},
		{"future after clock rollback", now.AddDate(0, 0, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := ManagedAppEntry{ExePath: `C:\Apps\daily.exe`, RunOnStartup: true, Schedule: Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 2, FrequencyRuns: 1}}
			history := NewStartupHistory(filepath.Join(t.TempDir(), "history.json"))
			launch := func() error { return nil }
			if ok, err := history.Launch(entry, tc.stamp.UTC(), launch); err != nil || !ok {
				t.Fatalf("seed launch: %t %v", ok, err)
			}
			if ok, err := history.Launch(entry, now, launch); err != nil || ok != tc.allow {
				t.Fatalf("two-calendar-day window: allowed=%t err=%v, want %t", ok, err, tc.allow)
			}
		})
	}
}

func TestStartupFrequencyCalendarDaysHandleDST(t *testing.T) {
	local, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		stamp, now time.Time
		allow      bool
	}{
		{"23-hour day ends", time.Date(2026, 3, 8, 0, 0, 0, 0, local), time.Date(2026, 3, 9, 0, 0, 0, 0, local), true},
		{"25-hour day still active", time.Date(2026, 11, 1, 0, 0, 0, 0, local), time.Date(2026, 11, 1, 23, 30, 0, 0, local), false},
		{"25-hour day ends", time.Date(2026, 11, 1, 0, 0, 0, 0, local), time.Date(2026, 11, 2, 0, 0, 0, 0, local), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := ManagedAppEntry{ExePath: `C:\Apps\daily.exe`, RunOnStartup: true, Schedule: Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}}
			history := NewStartupHistory(filepath.Join(t.TempDir(), "history.json"))
			launch := func() error { return nil }
			if ok, err := history.Launch(entry, tc.stamp, launch); err != nil || !ok {
				t.Fatalf("seed launch: %t %v", ok, err)
			}
			if ok, err := history.Launch(entry, tc.now, launch); err != nil || ok != tc.allow {
				t.Fatalf("calendar window after %v: allowed=%t err=%v, want %t", tc.now.Sub(tc.stamp), ok, err, tc.allow)
			}
		})
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

func TestFrequencySettingsRemainIndependentOfStartupSwitch(t *testing.T) {
	var entry ManagedAppEntry
	if err := json.Unmarshal([]byte(`{"runOnStartup":true,"schedule":{"enabled":true,"autoExitMinutes":5,"frequencyEnabled":true,"frequencyDays":1,"frequencyRuns":1}}`), &entry); err != nil {
		t.Fatal(err)
	}
	if !StartupFrequencyEnabled(entry) {
		t.Fatal("frequency must apply to startup")
	}
	entry.RunOnStartup = false
	if !StartupFrequencyEnabled(entry) || ScheduledRunLimit(entry) != 5 {
		t.Fatal("custom frequency must remain active when startup switch is off")
	}
}
