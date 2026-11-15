//go:build windows

package ui

import (
	"path/filepath"
	"strings"
	"wintray/internal/config"
	"wintray/internal/shellapp"
)

// A mode maps to the existing flags, keeping old configuration files valid.
type startMode int

const (
	startNormal startMode = iota
	startToTray
	startHidden
	startTask
)

func startModeOf(app config.ManagedAppEntry) startMode {
	switch {
	case app.LaunchViaLogonTask:
		return startTask
	case app.LaunchHiddenInBackground:
		return startHidden
	case app.TrayBehavior.AutoMinimizeAndHideOnLaunch:
		return startToTray
	default:
		return startNormal
	}
}
func (m startMode) applyTo(app *config.ManagedAppEntry) {
	app.RunOnStartup = m != startNormal
	app.LaunchViaLogonTask = m == startTask
	app.LaunchHiddenInBackground = m == startHidden
	app.TrayBehavior.AutoMinimizeAndHideOnLaunch = m == startToTray
}
func shouldDefaultLaunchHidden(path string) bool {
	return !shellapp.IsPath(path) && !strings.EqualFold(filepath.Ext(path), ".exe")
}
