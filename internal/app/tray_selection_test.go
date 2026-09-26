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
	first, _, before, after, err := traySelectionChange(settings, "a", true)
	if err != nil || before || !after || !first.ManagedApps[0].CollectTrayIcon || first.ManagedApps[1].CollectTrayIcon || first.ManagedApps[2].CollectTrayIcon {
		t.Fatalf("first selection = %+v, %t -> %t, %v", first.ManagedApps, before, after, err)
	}
	if settings.ManagedApps[0].CollectTrayIcon {
		t.Fatal("mutated the settings before saving")
	}
	second, _, before, after, err := traySelectionChange(first, "b", true)
	if err != nil || !before || !after {
		t.Fatalf("second selection = %t -> %t, %v", before, after, err)
	}
	firstOff, _, before, after, err := traySelectionChange(second, "a", false)
	if err != nil || !before || !after || !firstOff.ManagedApps[1].CollectTrayIcon {
		t.Fatalf("first removal = %+v, %t -> %t, %v", firstOff.ManagedApps, before, after, err)
	}
	_, _, before, after, err = traySelectionChange(firstOff, "b", false)
	if err != nil || !before || after {
		t.Fatalf("last removal = %t -> %t, %v", before, after, err)
	}
}

func TestTraySelectionChangeRejectsUnknownOrNonExecutable(t *testing.T) {
	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{{ID: "script", ExePath: `C:\Scripts\run.ps1`}}
	for _, id := range []string{"missing", "script"} {
		if _, _, _, _, err := traySelectionChange(settings, id, true); err == nil {
			t.Errorf("accepted %s", id)
		}
	}
}
