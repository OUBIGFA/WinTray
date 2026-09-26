package app

import (
	"testing"

	"wintray/internal/config"
)

func TestResidencyAutoExitKeepsOnlyHostedIconsUntilLastProgramEnds(t *testing.T) {
	state := residencyState{autorun: true, startupPending: true}
	if !state.hideMainIcon(true) || state.shouldExit(true, 0) {
		t.Fatal("startup should hide the main icon but keep the process alive")
	}
	state.startupPending = false
	if state.shouldExit(true, 2) || state.shouldExit(true, 1) {
		t.Fatal("hosted programs must retain the main process")
	}
	if !state.shouldExit(true, 0) {
		t.Fatal("last hosted program ending must allow automatic exit")
	}
}

func TestResidencyOpeningSettingsAndManualLaunchPostponeAutomaticExit(t *testing.T) {
	state := residencyState{autorun: true, settingsOpen: true}
	if state.hideMainIcon(true) || state.shouldExit(true, 0) {
		t.Fatal("reopening settings should show the main icon and prevent surprise exit")
	}
	state.manualLaunches = 1
	state.settingsOpen = false
	if !state.hideMainIcon(true) || state.shouldExit(true, 0) {
		t.Fatal("closing settings during a manual launch must leave its worker alive")
	}
	state.manualLaunches = 0
	if state.shouldExit(true, 1) || !state.shouldExit(true, 0) {
		t.Fatal("completed manual launches should keep only actual hosted programs alive")
	}
}

func TestResidencyNormalResidentModeKeepsMainIconAndDoesNotExit(t *testing.T) {
	for _, state := range []residencyState{{}, {autorun: true}, {settingsOpen: true}} {
		if state.hideMainIcon(false) || state.shouldExit(false, 0) {
			t.Fatalf("resident mode exited/hid icon: %+v", state)
		}
	}
	manual := residencyState{}
	if manual.hideMainIcon(true) || manual.shouldExit(true, 0) {
		t.Fatal("ordinary manual invocation must not auto-exit because the logon option is checked")
	}
}

func TestResidencySilentModeKeepsHostedIconsInAnyLaunchMode(t *testing.T) {
	for _, exitAfter := range []bool{false, true} {
		state := residencyState{silent: true}
		if !state.hideMainIcon(exitAfter) || state.shouldExit(exitAfter, 1) {
			t.Fatalf("silent mode must hide the main icon and keep hosted programs (exitAfter=%t)", exitAfter)
		}
		if !state.shouldExit(exitAfter, 0) {
			t.Fatalf("silent mode must exit once nothing is hosted (exitAfter=%t)", exitAfter)
		}
	}
	pending := residencyState{silent: true, autorun: true, startupPending: true, manualLaunches: 1}
	if pending.shouldExit(false, 0) {
		t.Fatal("silent mode must wait for startup and manual launches to hand off")
	}
}

func TestTrayBoxInUseKeepsAutorunSessionResident(t *testing.T) {
	s := config.DefaultSettings()
	if !exitsAfterStartup(s) {
		t.Fatal("default settings exit after startup")
	}
	s.ManagedApps = []config.ManagedAppEntry{{ID: "one", ExePath: `C:\Apps\Listary.exe`}}
	if !exitsAfterStartup(s) {
		t.Fatal("unselected programs must not keep WinTray running")
	}
	s.ManagedApps[0].CollectTrayIcon = true
	if exitsAfterStartup(s) {
		t.Fatal("collected tray icons need WinTray's menu, so it must stay")
	}
	state := residencyState{autorun: true, boxActive: true}
	if state.hideMainIcon(true) || state.shouldExit(true, 0) {
		t.Fatal("boxed icons must remain reachable even after startup")
	}
	state.silent = true
	if state.hideMainIcon(true) || state.shouldExit(true, 0) {
		t.Fatal("silent mode must not hide an active box")
	}
	s.ManagedApps[0].CollectTrayIcon = false
	if !exitsAfterStartup(s) {
		t.Fatal("deselecting the program restores the normal exit setting")
	}
}
