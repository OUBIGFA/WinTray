//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"wintray/internal/i18n"
)

func (w *MainWindow) nativeSettings(c *native.Context, m i18n.Messages) {
	// General preferences and maintenance have different consequences. Keep
	// them in separate flat columns, not a stack of identical setting cards.
	native.Row(c).Grow(1).MinHeight(0).AlignItems(native.Stretch).Children(func() {
		native.Column(c).Grow(1).Basis(0).Padding(24, 32).Gap(20).Children(func() {
			nativeSectionTitle(c, m.SettingsStartupTitle, playIcon)
			w.nativeSetting(c, m.RunAtLogon, m.RunAtLogonHint, false, func() {
				if native.Switch(c, &w.settings.RunAtLogon).Label(m.RunAtLogon).Changed() {
					w.save()
				}
			})
			w.nativeSetting(c, m.StartHidden, m.StartHiddenHint, !w.settings.RunAtLogon, func() {
				if native.Switch(c, &w.settings.StartMinimizedToTray).Label(m.StartHidden).Changed() {
					w.save()
				}
			})
			w.nativeSetting(c, m.ExitOnDone, m.ExitOnDoneHint, !w.settings.RunAtLogon, func() {
				if native.Switch(c, &w.settings.ExitAfterManagedAppsCompleted).Label(m.ExitOnDone).Changed() {
					w.save()
				}
			})
			native.Divider(c)
			nativeSectionTitle(c, m.SettingsTimingTitle, clockIcon)
			w.nativeSetting(c, m.StartupInterval, m.StartupIntervalHint, false, func() {
				w.nativeNumber(c, &w.settings.StartupIntervalSeconds, 0, 120, m.StartupInterval)
			})
			w.nativeSetting(c, m.RetrySeconds, m.RetrySecondsHint, false, func() {
				w.nativeNumber(c, &w.settings.CloseWindowRetrySeconds, 0, 120, m.RetrySeconds)
			})
		})
		native.Divider(c)
		native.Column(c).Width(400).Shrink(0).Background(desktopPanel(c.Theme())).Padding(24, 28).Gap(24).Children(func() {
			nativeSectionTitle(c, m.SettingsTroubleshootTitle, toolsIcon)
			for _, action := range []struct {
				title, hint, label string
				run                func()
			}{
				{m.LogsTitle, m.OpenLogsHint, m.OpenLogs, w.callbacks.OnOpenLogs},
				{m.RemoveLogonTaskTitle, m.RemoveLogonTaskHint, m.RemoveLogonTask, w.callbacks.OnRemoveLogon},
				{m.CleanupRestoreTitle, m.CleanupRestoreHint, m.CleanupRestore, w.callbacks.OnCleanupRestore},
			} {
				native.Column(c).Gap(8).Children(func() {
					native.Text(c, action.title).FontSize(16).FontWeight(500)
					nativeHint(c, action.hint)
					native.Row(c).Children(func() {
						button := native.Button(c, action.label)
						if button.Clicked() && action.run != nil {
							action.run()
						}
					})
				})
				native.Divider(c)
			}
		})
	})
}

func (w *MainWindow) nativeSetting(c *native.Context, title, hint string, disabled bool, controls func()) {
	native.Row(c).Gap(20).Disabled(disabled).Children(func() {
		native.Column(c).Grow(1).Basis(0).Gap(6).Children(func() {
			native.Text(c, title).FontWeight(500)
			nativeHint(c, hint)
		})
		native.Row(c).Shrink(0).Justify(native.End).Children(controls)
	})
}
