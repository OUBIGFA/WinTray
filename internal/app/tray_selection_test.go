package app

import (
	"testing"

	"wintray/internal/config"
)

func TestTraySelectionChangeIsPerProgramAndSharesPhysicalIcon(t *testing.T) {
	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{
		{ID: "a", ExePath: `C:\Apps\Test.exe`},
		{ID: "b", ExePath: `c:\apps\TEST.exe`},
		{ID: "c", ExePath: `C:\Apps\Other.exe`},
	}
	first, err := traySelectionChange(settings, "a", true)
	if err != nil || !first.ManagedApps[0].CollectTrayIcon || first.ManagedApps[1].CollectTrayIcon || first.ManagedApps[2].CollectTrayIcon {
		t.Fatalf("first selection = %+v, %v", first.ManagedApps, err)
	}
	if settings.ManagedApps[0].CollectTrayIcon {
		t.Fatal("mutated the settings before saving")
	}
	second, err := traySelectionChange(first, "b", true)
	if err != nil || len(config.CollectedTrayIconPaths(second)) != 1 {
		t.Fatalf("two entries of one program collect one icon: %v, %v", config.CollectedTrayIconPaths(second), err)
	}
	// The program stays collected while another entry still selects it.
	firstOff, err := traySelectionChange(second, "a", false)
	if err != nil || len(config.CollectedTrayIconPaths(firstOff)) != 1 {
		t.Fatalf("first removal = %v, %v", config.CollectedTrayIconPaths(firstOff), err)
	}
	lastOff, err := traySelectionChange(firstOff, "b", false)
	if err != nil || len(config.CollectedTrayIconPaths(lastOff)) != 0 {
		t.Fatalf("last removal = %v, %v", config.CollectedTrayIconPaths(lastOff), err)
	}
}

func TestTraySelectionChangeRejectsUnknownOrNonExecutable(t *testing.T) {
	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{{ID: "script", ExePath: `C:\Scripts\run.ps1`}}
	for _, id := range []string{"missing", "script"} {
		if _, err := traySelectionChange(settings, id, true); err == nil {
			t.Errorf("accepted %s", id)
		}
	}
}
