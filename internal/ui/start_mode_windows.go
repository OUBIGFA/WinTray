//go:build windows

package ui

import (
	"path/filepath"
	"strings"
	"wintray/internal/config"
)

// A mode maps to the existing flags, keeping old configuration files valid.
type startMode int

const (
	startToTray startMode = iota
	startHidden
	startTask
	startNormal
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
	app.LaunchViaLogonTask = m == startTask
	app.LaunchHiddenInBackground = m == startHidden
	app.TrayBehavior.AutoMinimizeAndHideOnLaunch = m == startToTray
}
func shouldDefaultLaunchHidden(path string) bool {
	return !strings.EqualFold(filepath.Ext(path), ".exe")
}
