package app

// residencyState is owned by the UI thread. Automatic exit must not race with
// startup, a manual launch, or a user who has reopened settings.
type residencyState struct {
	autorun        bool
	startupPending bool
	settingsOpen   bool
	manualLaunches int
	// silent is set when the user chooses background mode explicitly. It
	// keeps hosted icons alive like the logon exit option, in any launch mode.
	silent bool
}

func (s residencyState) hideMainIcon(exitAfterCompleted bool) bool {
	return (s.silent || s.autorun && exitAfterCompleted) && !s.settingsOpen
}

func (s residencyState) shouldExit(exitAfterCompleted bool, hostedCount int) bool {
	return s.hideMainIcon(exitAfterCompleted) && !s.startupPending && s.manualLaunches == 0 && hostedCount == 0
}
