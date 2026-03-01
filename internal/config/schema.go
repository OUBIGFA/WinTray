package config

type MatchStrategy string

const (
	MatchProcessNameThenTitle MatchStrategy = "processNameThenTitle"
	MatchTitleContains        MatchStrategy = "titleContains"
	MatchClassName            MatchStrategy = "className"
	MatchAny                  MatchStrategy = "any"
)

type WindowMatchRule struct {
	Strategy MatchStrategy `json:"strategy"`
}

type TrayBehavior struct {
	AutoMinimizeAndHideOnLaunch bool `json:"autoMinimizeAndHideOnLaunch"`
}

type ManagedAppEntry struct {
	ID                       string          `json:"id"`
	Name                     string          `json:"name"`
	ExePath                  string          `json:"exePath"`
	Args                     string          `json:"args"`
	RunOnStartup             bool            `json:"runOnStartup"`
	LaunchHiddenInBackground bool            `json:"launchHiddenInBackground"`
	WindowMatch              WindowMatchRule `json:"windowMatch"`
	TrayBehavior             TrayBehavior    `json:"trayBehavior"`
}

type Settings struct {
	SchemaVersion                 int               `json:"schemaVersion"`
	Language                      string            `json:"language"`
	RunAtLogon                    bool              `json:"runAtLogon"`
	StartMinimizedToTray          bool              `json:"startMinimizedToTray"`
	ExitAfterManagedAppsCompleted bool              `json:"exitAfterManagedAppsCompleted"`
	CloseWindowRetrySeconds       int               `json:"closeWindowRetrySeconds"`
	ManagedApps                   []ManagedAppEntry `json:"managedApps"`
}

func DefaultSettings() Settings {
	return Settings{
		SchemaVersion:                 1,
		Language:                      "zh-CN",
		RunAtLogon:                    true,
		StartMinimizedToTray:          false,
		ExitAfterManagedAppsCompleted: false,
		CloseWindowRetrySeconds:       10,
		ManagedApps:                   make([]ManagedAppEntry, 0),
	}
}
