package config

import (
	"path/filepath"
	"strings"
)

// MaxCloseDelaySeconds bounds how long a program may wait between its launch
// and the first action on its window.
const MaxCloseDelaySeconds = 600

const (
	DefaultStartupIntervalSeconds = 3
	MaxStartupIntervalSeconds     = 120
)

// ClampStartupIntervalSeconds bounds spacing between launches. Zero disables
// the wait, but entries are still started in list order.
func ClampStartupIntervalSeconds(seconds int) int {
	return min(max(seconds, 0), MaxStartupIntervalSeconds)
}

type TrayBehavior struct {
	AutoMinimizeAndHideOnLaunch bool `json:"autoMinimizeAndHideOnLaunch"`
	// CloseDelaySeconds keeps the program's windows untouched until its process
	// has been running this long. A login or splash window shown first is left
	// alone (the NT-based QQ quits when its login window is closed), and the
	// close reaches the main window that is up once the delay has passed. The
	// delay counts from the creation of the process whoever started it, so a
	// program started by its own autorun entry is covered too, and a program
	// running longer than the delay is handled right away. It only applies
	// together with AutoMinimizeAndHideOnLaunch.
	CloseDelaySeconds int `json:"closeDelaySeconds"`
}

type ManagedAppEntry struct {
	ID                       string       `json:"id"`
	Name                     string       `json:"name"`
	ExePath                  string       `json:"exePath"`
	Args                     string       `json:"args"`
	RunOnStartup             bool         `json:"runOnStartup"`
	CollectTrayIcon          bool         `json:"collectTrayIcon"`
	LaunchHiddenInBackground bool         `json:"launchHiddenInBackground"`
	TrayBehavior             TrayBehavior `json:"trayBehavior"`
}

type Settings struct {
	SchemaVersion                 int               `json:"schemaVersion"`
	Language                      string            `json:"language"`
	RunAtLogon                    bool              `json:"runAtLogon"`
	StartMinimizedToTray          bool              `json:"startMinimizedToTray"`
	ExitAfterManagedAppsCompleted bool              `json:"exitAfterManagedAppsCompleted"`
	CloseWindowRetrySeconds       int               `json:"closeWindowRetrySeconds"`
	StartupIntervalSeconds        int               `json:"startupIntervalSeconds"`
	ManagedApps                   []ManagedAppEntry `json:"managedApps"`
	// Legacy settings from the first tray-box prototype. Reconciled at startup.
	TrayBoxEnabled bool     `json:"trayBoxEnabled,omitempty"`
	TrayBoxApps    []string `json:"trayBoxApps,omitempty"`
}

func CollectedTrayIconPaths(settings Settings) []string {
	var paths []string
	for _, app := range settings.ManagedApps {
		if app.CollectTrayIcon && strings.EqualFold(filepath.Ext(app.ExePath), ".exe") && !containsPath(paths, app.ExePath) {
			paths = append(paths, app.ExePath)
		}
	}
	return paths
}

func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if strings.EqualFold(p, path) {
			return true
		}
	}
	return false
}

func ShouldLaunchViaWinTray(entry ManagedAppEntry) bool {
	return entry.RunOnStartup
}

// ClampCloseDelaySeconds keeps a close delay inside the supported range.
func ClampCloseDelaySeconds(seconds int) int {
	if seconds < 0 {
		return 0
	}
	if seconds > MaxCloseDelaySeconds {
		return MaxCloseDelaySeconds
	}
	return seconds
}

func DefaultSettings() Settings {
	return Settings{
		SchemaVersion:                 3,
		Language:                      "zh-CN",
		RunAtLogon:                    true,
		StartMinimizedToTray:          true,
		ExitAfterManagedAppsCompleted: true,
		CloseWindowRetrySeconds:       10,
		StartupIntervalSeconds:        DefaultStartupIntervalSeconds,
		ManagedApps:                   make([]ManagedAppEntry, 0),
		TrayBoxApps:                   make([]string, 0),
	}
}
