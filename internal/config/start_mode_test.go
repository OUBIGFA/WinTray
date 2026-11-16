package config

import (
	"fmt"
	"testing"
)

func TestNormalStartupLeavesOriginalStartupAlone(t *testing.T) {
	for _, legacyStartup := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy_%t", legacyStartup), func(t *testing.T) {
			entry := ManagedAppEntry{
				ID: "normal", ExePath: "Weixin.exe", RunOnStartup: legacyStartup, WeChatAutoLogin: true,
				Schedule: Schedule{StartDelayMinutes: 5, AutoExitMinutes: 30, FrequencyEnabled: true, FrequencyDays: 7, FrequencyRuns: 2},
			}
			if ShouldLaunchViaWinTray(entry) || StartupFrequencyEnabled(entry) || ScheduledStartDelaySeconds(entry) != 0 || ScheduledRunLimit(entry) != 0 {
				t.Errorf("normal startup without a schedule still enables automatic operations: %+v", entry)
			}
			settings := Settings{RunAtLogon: true, ManagedApps: []ManagedAppEntry{entry}}
			if tasks := LogonTaskApps(settings); len(tasks) != 0 {
				t.Errorf("normal startup without a schedule still requests takeover tasks: %+v", tasks)
			}
		})
	}
}

func TestNormalStartupCustomScheduleRemainsActive(t *testing.T) {
	for _, legacyStartup := range []bool{false, true} {
		for _, limited := range []bool{false, true} {
			t.Run(fmt.Sprintf("legacy_%t/limited_%t", legacyStartup, limited), func(t *testing.T) {
				entry := ManagedAppEntry{
					ID: "normal", ExePath: "scheduled.exe", RunOnStartup: legacyStartup,
					Schedule: Schedule{Enabled: true, StartDelayMinutes: 5, AutoExitMinutes: 30, FrequencyEnabled: limited, FrequencyDays: 7, FrequencyRuns: 2},
				}
				if !ShouldLaunchViaWinTray(entry) || StartupFrequencyEnabled(entry) != limited || ScheduledStartDelaySeconds(entry) != 300 || ScheduledRunLimit(entry) != 30 {
					t.Errorf("normal startup disabled the explicit schedule: %+v", entry)
				}
				settings := Settings{RunAtLogon: true, ManagedApps: []ManagedAppEntry{entry}}
				tasks := LogonTaskApps(settings)
				if limited {
					if len(tasks) != 1 || tasks[0].Entry.ID != entry.ID || tasks[0].DelaySeconds != 300 {
						t.Errorf("scheduled frequency control lost its task: %+v", tasks)
					}
				} else if len(tasks) != 0 {
					t.Errorf("custom timing alone requested startup takeover: %+v", tasks)
				}
			})
		}
	}
}

func TestStartupModesReplaceLegacySwitch(t *testing.T) {
	for _, entry := range []ManagedAppEntry{
		{LaunchHiddenInBackground: true},
		{LaunchViaLogonTask: true},
		{TrayBehavior: TrayBehavior{AutoMinimizeAndHideOnLaunch: true}},
	} {
		if !ShouldLaunchViaWinTray(entry) {
			t.Fatalf("configured startup mode was blocked by the removed switch: %+v", entry)
		}
	}
}

func TestLegacyPausedStartupMigratesToNormal(t *testing.T) {
	settings := Settings{SchemaVersion: 3, ManagedApps: []ManagedAppEntry{
		{ExePath: "C:/Apps/paused.exe", LaunchHiddenInBackground: true},
		{ExePath: "C:/Apps/active.exe", RunOnStartup: true, TrayBehavior: TrayBehavior{AutoMinimizeAndHideOnLaunch: true}},
		{ExePath: "C:/Apps/scheduled.exe", LaunchHiddenInBackground: true, Schedule: Schedule{Enabled: true}},
	}}
	got := migrate(settings)
	if got.SchemaVersion < 4 {
		t.Fatalf("schema version = %d, want at least 4", got.SchemaVersion)
	}
	paused, active, scheduled := got.ManagedApps[0], got.ManagedApps[1], got.ManagedApps[2]
	if ShouldLaunchViaWinTray(paused) || paused.LaunchHiddenInBackground || paused.LaunchViaLogonTask || paused.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		t.Fatalf("paused app was re-enabled or kept a stale mode: %+v", paused)
	}
	if !ShouldLaunchViaWinTray(active) || !active.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		t.Fatalf("active startup behavior changed: %+v", active)
	}
	if !ShouldLaunchViaWinTray(scheduled) || !scheduled.LaunchHiddenInBackground {
		t.Fatalf("explicit scheduled behavior changed: %+v", scheduled)
	}
}
