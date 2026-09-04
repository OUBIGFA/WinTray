package app

import (
	"strings"

	"wintray/internal/config"
)

func isBackgroundLaunch(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--background") {
			return true
		}
	}
	return false
}

func isAutorunLaunch(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--autorun") {
			return true
		}
	}
	return false
}

func isCleanupRestoreLaunch(args []string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, "--cleanup-restore") {
			return true
		}
	}
	return false
}

func shouldShowMainWindow(args []string) bool {
	return !isBackgroundLaunch(args)
}

func shouldShowMainWindowForSettings(args []string, settings config.Settings) bool {
	if !shouldShowMainWindow(args) {
		return false
	}
	return !isAutorunLaunch(args) || !settings.StartMinimizedToTray
}

func shouldSignalRunningInstance(args []string) bool {
	return !isBackgroundLaunch(args)
}
