//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"wintray/internal/i18n"
	"wintray/internal/ui/kit"
	"wintray/internal/ui/kit/icons"
)

// Settings use the shared page groups and setting rows. WinTray's own
// sign-in and maintenance actions stay available in the same design.
func (w *MainWindow) nativeSettings(c *native.Context, m i18n.Messages) {
	native.Column(c).Grow(1).FillWidth().Padding(16, 0, 20, 0).Children(func() {
		kit.Page(c, func() {
			kit.Group(c, icons.Rocket, m.SettingsStartupTitle, "", func() {
				if kit.Toggle(c, &w.settings.RunAtLogon, m.RunAtLogon, m.RunAtLogonHint).Changed() {
					w.save()
				}
				// Self-explanatory rows need no second line of instructions.
				if kit.Toggle(c, &w.settings.StartMinimizedToTray, m.StartHidden, "").Disabled(!w.settings.RunAtLogon).Changed() {
					w.save()
				}
				if kit.Toggle(c, &w.settings.ExitAfterManagedAppsCompleted, m.ExitOnDone, m.ExitOnDoneHint).Disabled(!w.settings.RunAtLogon).Changed() {
					w.save()
				}
			})
			kit.Group(c, icons.Timer, m.SettingsTimingTitle, "", func() {
				w.nativeNumberRow(c, m.StartupInterval, m.StartupIntervalHint, &w.settings.StartupIntervalSeconds, 0, 120)
				w.nativeNumberRow(c, m.RetrySeconds, m.RetrySecondsHint, &w.settings.CloseWindowRetrySeconds, 0, 120)
			})
			kit.Group(c, icons.Settings2, m.SettingsTroubleshootTitle, "", func() {
				for _, action := range []struct {
					title, hint, label string
					run                func()
				}{
					{m.LogsTitle, "", m.OpenLogs, w.callbacks.OnOpenLogs},
					{m.RemoveLogonTaskTitle, m.RemoveLogonTaskHint, m.RemoveLogonTask, w.callbacks.OnRemoveLogon},
					{m.CleanupRestoreTitle, m.CleanupRestoreHint, m.CleanupRestore, w.callbacks.OnCleanupRestore},
				} {
					kit.Row(c, action.title, action.hint, func() {
						if kit.Button(c, action.label, nil, false).Label(action.label).Clicked() && action.run != nil {
							action.run()
						}
					})
				}
			})
		})
	})
}
