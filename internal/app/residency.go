package app

import "wintray/internal/config"

// exitsAfterStartup reports whether an autorun session may end once managed
// programs are handled. Boxed tray icons are reached through WinTray's own
// icon, so WinTray stays while the box is in use.
func exitsAfterStartup(s config.Settings) bool {
	return s.ExitAfterManagedAppsCompleted && len(config.CollectedTrayIconPaths(s)) == 0
}

// residencyState is owned by the UI thread. Automatic exit must not race with
// startup, a manual launch, or a user who has reopened settings.
type residencyState struct {
	autorun        bool
	startupPending bool
	settingsOpen   bool
	manualLaunches int
	boxActive      bool
	// silent is set when the user chooses background mode explicitly. It
	// keeps hosted icons alive like the logon exit option, in any launch mode.
	silent bool
}

func (s residencyState) hideMainIcon(exitAfterCompleted bool) bool {
	return !s.boxActive && (s.silent || s.autorun && exitAfterCompleted) && !s.settingsOpen
}

func (s residencyState) shouldExit(exitAfterCompleted bool, hostedCount int) bool {
	return s.hideMainIcon(exitAfterCompleted) && !s.startupPending && s.manualLaunches == 0 && hostedCount == 0
}
