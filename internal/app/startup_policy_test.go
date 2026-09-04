package app

import (
	"testing"

	"wintray/internal/config"
)

func TestStartupPolicy_BackgroundLaunchStaysHiddenAndDoesNotSignal(t *testing.T) {
	args := []string{"--autorun", "--background"}
	if shouldShowMainWindow(args) {
		t.Fatal("background launch should not show the main window")
	}
	if shouldSignalRunningInstance(args) {
		t.Fatal("background launch should not activate an existing instance")
	}
}

func TestStartupPolicy_AutorunWithoutBackgroundShowsWindow(t *testing.T) {
	args := []string{"--autorun"}
	if !shouldShowMainWindow(args) {
		t.Fatal("autorun without background should show the main window")
	}
	if !shouldSignalRunningInstance(args) {
		t.Fatal("interactive launch should activate an existing instance")
	}
}

func TestStartupPolicy_AutorunHonorsStartMinimizedSetting(t *testing.T) {
	args := []string{"--autorun"}
	if shouldShowMainWindowForSettings(args, config.Settings{StartMinimizedToTray: true}) {
		t.Fatal("autorun should stay hidden when start-minimized is enabled")
	}
	if !shouldShowMainWindowForSettings(args, config.Settings{StartMinimizedToTray: false}) {
		t.Fatal("autorun should show when start-minimized is disabled")
	}
}
