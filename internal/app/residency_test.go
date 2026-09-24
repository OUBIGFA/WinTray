package app

import "testing"

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
