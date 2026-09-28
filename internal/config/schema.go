package config

import (
	"encoding/json"
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

// LogonTaskDelaySeconds is when a task-launched program starts after
// sign-in. Launch at boot means at the moment of sign-in, like a native logon
// task: no base delay and no staggering behind other programs. Only a start
// delay the user scheduled explicitly holds it back.
func LogonTaskDelaySeconds(entry ManagedAppEntry) int {
	return ScheduledStartDelaySeconds(entry)
}

// LogonTaskApp pairs an enabled task-launched entry with the delay its logon
// task uses at sign-in.
type LogonTaskApp struct {
	Entry        ManagedAppEntry
	DelaySeconds int
}

// LogonTaskApps returns the enabled task-launched entries in list order with
// the delay each one's logon task uses. Entries paused for sign-in get no
// task, so Windows cannot start them behind WinTray's back.
func LogonTaskApps(settings Settings) []LogonTaskApp {
	apps := make([]LogonTaskApp, 0)
	for i := range settings.ManagedApps {
		entry := settings.ManagedApps[i]
		if !entry.RunOnStartup || !entry.LaunchViaLogonTask {
			continue
		}
		apps = append(apps, LogonTaskApp{Entry: entry, DelaySeconds: LogonTaskDelaySeconds(entry)})
	}
	return apps
}

type TrayBehavior struct {
	AutoMinimizeAndHideOnLaunch bool `json:"autoMinimizeAndHideOnLaunch"`
	// CloseDelaySeconds defers native close requests until the process has
	// been running this long. Startup UI may be visually shielded meanwhile,
	// without changing its native visibility or closing its login window (the
	// NT-based QQ quits when its login window is closed). The
	// delay counts from the creation of the process whoever started it, so a
	// program started by its own autorun entry is covered too, and a program
	// running longer than the delay is handled right away. It only applies
	// together with AutoMinimizeAndHideOnLaunch.
	CloseDelaySeconds int `json:"closeDelaySeconds"`
}

const (
	// MaxScheduleMinutes bounds both scheduled times: a day.
	MaxScheduleMinutes = 1440
	// DefaultAutoExitMinutes is the run time offered when a schedule is
	// first switched on.
	DefaultAutoExitMinutes = 30
)

// Schedule suits programs that only need to run for a while, such as daily
// check-in tools: they start some minutes after sign-in, out of the way of
// the programs that matter then, and are ended once they have run long enough.
// Like a task's "stop the task if it runs longer than" setting, the run time
// counts from the creation of the program's process and ends it for good.
type Schedule struct {
	Enabled bool `json:"enabled"`
	// StartDelayMinutes counts from sign-in.
	StartDelayMinutes int `json:"startDelayMinutes"`
	// AutoExitMinutes is how long the program may run; 0 never ends it.
	AutoExitMinutes int `json:"autoExitMinutes"`
}

// ClampScheduleMinutes keeps a scheduled time inside the supported range.
func ClampScheduleMinutes(minutes int) int {
	return min(max(minutes, 0), MaxScheduleMinutes)
}

// ScheduledStartDelaySeconds is how long after sign-in the entry may start.
func ScheduledStartDelaySeconds(entry ManagedAppEntry) int {
	if !entry.Schedule.Enabled {
		return 0
	}
	return ClampScheduleMinutes(entry.Schedule.StartDelayMinutes) * 60
}

// ScheduledRunLimit returns how many minutes the entry's program may run, or
// 0 when it is never ended.
func ScheduledRunLimit(entry ManagedAppEntry) int {
	if !entry.Schedule.Enabled {
		return 0
	}
	return ClampScheduleMinutes(entry.Schedule.AutoExitMinutes)
}

type ManagedAppEntry struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ExePath         string `json:"exePath"`
	Args            string `json:"args"`
	RunOnStartup    bool   `json:"runOnStartup"`
	CollectTrayIcon bool   `json:"collectTrayIcon"`
	// LaunchViaLogonTask replaces only the sign-in trigger. It reuses an app's
	// native task, or migrates its original user startup entry without changing
	// arguments, window behaviour or privileges. If no original entry exists,
	// it registers the configured executable and arguments. WinTray observes
	// task launches rather than starting a second instance.
	// Disabling this mode releases the task and restores migrated triggers.
	LaunchViaLogonTask       bool         `json:"launchViaLogonTask"`
	LaunchHiddenInBackground bool         `json:"launchHiddenInBackground"`
	TrayBehavior             TrayBehavior `json:"trayBehavior"`
	Schedule                 Schedule     `json:"schedule"`
}

// UnmarshalJSON gives entries saved before schedules existed the default run
// time, so switching a schedule on starts from it rather than from "never".
func (e *ManagedAppEntry) UnmarshalJSON(b []byte) error {
	type plain ManagedAppEntry
	decoded := plain{Schedule: Schedule{AutoExitMinutes: DefaultAutoExitMinutes}}
	if err := json.Unmarshal(b, &decoded); err != nil {
		return err
	}
	*e = ManagedAppEntry(decoded)
	return nil
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
	}
}
