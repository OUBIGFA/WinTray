package config

import "testing"

func TestPackagedAppUsesWindowsLaunchDirectory(t *testing.T) {
	entry := ManagedAppEntry{ExePath: "shell:AppsFolder\\Example.App_123456789abcd!App", WorkingDirectory: "C:/old-folder"}
	if got := entry.LaunchDirectory(); got != "" {
		t.Fatalf("packaged launch directory = %q; Windows must select it", got)
	}
}

func TestPackagedAppCanUseLogonTaskAndFrequencyLimit(t *testing.T) {
	entry := ManagedAppEntry{ID: "packaged", Name: "Store app", ExePath: "shell:AppsFolder\\Example.App_123456789abcd!App", LaunchViaLogonTask: true}
	settings := DefaultSettings()
	settings.ManagedApps = []ManagedAppEntry{entry}
	if got := LogonTaskApps(settings); len(got) != 1 {
		t.Fatalf("packaged logon task count = %d, want 1", len(got))
	}
	settings.ManagedApps[0].LaunchViaLogonTask = false
	settings.ManagedApps[0].Schedule = Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}
	if got := LogonTaskApps(settings); len(got) != 1 {
		t.Fatalf("packaged frequency task count = %d, want 1", len(got))
	}
}

func TestPackagedAppTaskModeSurvivesSave(t *testing.T) {
	settings := DefaultSettings()
	settings.ManagedApps = []ManagedAppEntry{{ExePath: "shell:AppsFolder\\Example.App_123456789abcd!App", LaunchViaLogonTask: true}}
	got := migrate(settings).ManagedApps[0]
	if !got.LaunchViaLogonTask || !ShouldLaunchViaWinTray(got) {
		t.Fatalf("saved store app lost its selected startup mode: %+v", got)
	}
}

func TestPackagedAppCollectionRetainsApplicationIdentity(t *testing.T) {
	settings := DefaultSettings()
	path := "shell:AppsFolder\\Example.App_123456789abcd!Reader"
	settings.ManagedApps = []ManagedAppEntry{{ExePath: path, CollectTrayIcon: true}, {ExePath: path, CollectTrayIcon: true}}
	paths := CollectedTrayIconPaths(settings)
	if len(paths) != 1 || paths[0] != path {
		t.Fatalf("store app did not reach tray collection: %q", paths)
	}
}
