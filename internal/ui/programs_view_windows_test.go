//go:build windows

package ui

import (
	"testing"

	"wintray/internal/config"
)

func TestStartModeChoiceRoundTrip(t *testing.T) {
	for _, mode := range []startMode{startToTray, startHidden, startTask, startNormal} {
		app := config.ManagedAppEntry{}
		mode.applyTo(&app)
		if got := startModeOf(app); got != mode {
			t.Errorf("startModeOf after choosing %v = %v, want the same mode", mode, got)
		}
	}
}

func TestStartModeTaskReplacesEveryOtherLaunchFlag(t *testing.T) {
	app := config.ManagedAppEntry{
		LaunchHiddenInBackground: true,
		TrayBehavior:             config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true},
	}
	startTask.applyTo(&app)
	if !app.LaunchViaLogonTask || app.LaunchHiddenInBackground || app.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		t.Fatalf("task mode left other launch flags set: %+v", app)
	}
	// Choosing another mode clears the task flag again.
	startToTray.applyTo(&app)
	if app.LaunchViaLogonTask || !app.TrayBehavior.AutoMinimizeAndHideOnLaunch || app.LaunchHiddenInBackground {
		t.Fatalf("close-to-tray mode kept the task flag: %+v", app)
	}
	if got := startModeOf(app); got != startToTray {
		t.Fatalf("startModeOf after switching away = %v, want close to tray", got)
	}
}
