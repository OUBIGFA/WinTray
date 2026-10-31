//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func (w *MainWindow) nativeEditor(c *native.Context, m i18n.Messages) {
	entry, ok := w.nativeSelected()
	if !ok {
		// No placeholder settings: the inspector only edits a real selection.
		native.Column(c).Grow(1).Center().Gap(16).Padding(24).Children(func() {
			native.Icon(c, programIconSVG).Size(40, 40).TextColor(c.Theme().TextMuted)
			native.Text(c, m.ManagedSelectPrompt).FontSize(22).FontWeight(500)
			native.Text(c, m.ManagedSelectHint).TextColor(c.Theme().TextMuted).TextAlign(native.Center)
		})
		return
	}
	_, height := c.Size()
	gap := float32(12)
	if height < 800 {
		gap = 4
	}
	fieldGap := min(gap, 8)
	// Entry identity owns every text/number cursor, including pending edits.
	native.Column(c).Key(entry.ID).Grow(1).Gap(gap).Children(func() {
		native.Row(c).Gap(12).Children(func() {
			if icon := w.desktop.icons[entry.ExePath]; icon != nil {
				native.Image(c, icon).Size(32, 32).Shrink(0)
			} else {
				native.Icon(c, programIconSVG).Size(32, 32).Shrink(0).TextColor(c.Theme().TextMuted)
			}
			native.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				native.Text(c, entry.Name).FontSize(22).FontWeight(500).SingleLine().Tooltip(entry.Name)
				native.Text(c, entry.ExePath).SingleLine().TextColor(c.Theme().TextMuted).Tooltip(entry.ExePath)
			})
			if nativeQuietAction(c, m.BrowseProgram, nil).Tooltip(m.BrowseProgramHint).Clicked() {
				w.nativeChooseProgram(true)
			}
		})
		w.nativeProgramActions(c, m, entry)
		native.Divider(c).Margin(2, 0)
		native.Column(c).Gap(fieldGap).Children(func() {
			if native.Checkbox(c, &entry.RunOnStartup, m.ManagedEnabled).FontWeight(500).Tooltip(m.ManagedEnabledHint).Changed() {
				w.save()
			}
			native.Column(c).Gap(fieldGap).Disabled(!entry.RunOnStartup).Children(func() {
				w.nativeStartMode(c, m, entry)
				native.Field(c, m.ManagedAppArgs, func() {
					input := native.TextInput(c, &entry.Args).Placeholder(m.ManagedArgsPlaceholder).Label(m.ManagedAppArgs)
					if input.Changed() {
						w.desktop.argsDirty = true
					}
					if input.Submitted() {
						w.commitNativeEdits()
					} else if !input.Focused() {
						w.desktop.frameBlurCommits = append(w.desktop.frameBlurCommits, w.commitNativeEdits)
					}
				}).Tooltip(m.ManagedArgsHint)
				if startModeOf(*entry) == startTask {
					nativeHint(c, m.ManagedTaskArgsHint)
				}
			})
		})
		native.Divider(c).Margin(2, 0)
		w.nativeSchedule(c, m, entry)
	})
}

func (w *MainWindow) nativeProgramActions(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry) {
	visible := w.callbacks.OnProgramWindowVisible != nil && w.callbacks.OnProgramWindowVisible(*entry)
	running := w.callbacks.OnProgramRunning != nil && w.callbacks.OnProgramRunning(*entry)
	windowBusy, busy := w.programWindowBusy[entry.ID], w.launchNowBusy[entry.ID]
	windowLabel := m.HostedShowWindow
	if visible {
		windowLabel = m.HostedHideWindow
	}
	if windowBusy {
		windowLabel = m.ManagedWindowBusy
	}
	actionLabel, actionIcon, actionHint := m.ManagedLaunchNow, playIcon, m.ManagedLaunchNowHint
	if running {
		actionLabel, actionIcon, actionHint = m.ManagedStopProgram, stopIcon, m.ManagedStopProgramHint
	}
	if busy {
		actionLabel = m.ManagedLaunchNowBusy
		if running {
			actionLabel = m.ManagedStopProgramBusy
		}
	}
	// Live actions remain separate from (and independent of) startup settings.
	native.Row(c).Gap(8).Disabled(busy || windowBusy).Children(func() {
		var action native.Element
		if running {
			action = nativeAction(c, actionLabel, actionIcon)
		} else {
			action = nativePrimaryAction(c, actionLabel, actionIcon)
		}
		if action.Tooltip(actionHint).Clicked() {
			w.commitNativeEdits()
			if w.launchNowBusy == nil {
				w.launchNowBusy = make(map[string]bool)
			}
			w.launchNowBusy[entry.ID] = true
			if running {
				if w.callbacks.OnStopNow != nil {
					w.callbacks.OnStopNow(*entry)
				}
			} else if w.callbacks.OnLaunchNow != nil {
				w.callbacks.OnLaunchNow(*entry)
			}
		}
		if nativeAction(c, windowLabel, programIconSVG).Tooltip(m.ManagedWindowHint).Clicked() && w.callbacks.OnSetProgramWindowVisible != nil {
			if w.programWindowBusy == nil {
				w.programWindowBusy = make(map[string]bool)
			}
			w.programWindowBusy[entry.ID] = true
			w.callbacks.OnSetProgramWindowVisible(*entry, !visible)
		}
	})
}

func (w *MainWindow) nativeStartMode(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry) {
	mode := startModeOf(*entry)
	script := shouldDefaultLaunchHidden(entry.ExePath)
	choices := []struct {
		mode startMode
		text string
	}{
		{startToTray, m.ManagedAutoHide}, {startHidden, m.ManagedLaunchHidden},
		{startTask, m.ManagedTaskLaunch}, {startNormal, m.ManagedLaunchOnly},
	}
	// A two-column radio group preserves native arrow-key selection and fits
	// complete English labels without making the editor or page scroll.
	native.RadioGroup(c, func() {
		for row := 0; row < 2; row++ {
			native.Row(c).Gap(8).Children(func() {
				for _, choice := range choices[row*2 : row*2+2] {
					native.Row(c).Grow(1).Basis(0).Padding(4, 8).Radius(4).Background(c.Theme().Background).Disabled(script && choice.mode != startHidden).Children(func() {
						if native.Radio(c, &mode, choice.mode, choice.text).Changed() {
							mode.applyTo(entry)
							w.save()
						}
					})
				}
			})
		}
	}).Gap(4).Label(m.ManagedModeLabel)
	hint := m.ManagedLaunchOnlyHint
	switch mode {
	case startToTray:
		hint = m.ManagedAutoHideTip
	case startHidden:
		hint = m.ManagedLaunchHiddenHint
	case startTask:
		hint = m.ManagedTaskLaunchTip
	}
	nativeHint(c, hint)
	if mode == startToTray {
		native.Row(c).Gap(8).Children(func() {
			native.Text(c, m.ManagedCloseDelay).Tooltip(m.ManagedCloseDelayHint)
			w.nativeNumber(c, &entry.TrayBehavior.CloseDelaySeconds, 0, 600, m.ManagedCloseDelay)
			native.Text(c, m.SecondsUnit).TextColor(c.Theme().TextMuted)
			nativeHelp(c, m.ManagedCloseDelayHint)
		})
	}
	if config.IsWeChatExecutable(entry.ExePath) {
		// Hidden launches cannot provide a login UI; keep the saved choice.
		native.Row(c).Gap(8).Disabled(mode == startHidden).Children(func() {
			if native.Checkbox(c, &entry.WeChatAutoLogin, m.ManagedWeChatAutoLogin).Tooltip(m.ManagedWeChatAutoLoginHint).Changed() {
				w.save()
			}
			nativeHelp(c, m.ManagedWeChatAutoLoginHint)
		})
	}
}

func (w *MainWindow) nativeSchedule(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry) {
	gap := float32(8)
	if _, height := c.Size(); height < 800 {
		gap = 4
	}
	native.Column(c).Gap(gap).Children(func() {
		if native.Checkbox(c, &entry.Schedule.Enabled, m.ManagedSchedule).FontWeight(500).Tooltip(m.ManagedScheduleHint).Changed() {
			w.save()
		}
		if !entry.Schedule.Enabled {
			nativeHint(c, m.ManagedScheduleOffHint)
			return
		}
		// Timing stays editable when sign-in startup is paused. Labels above
		// the real NumberInputs leave enough room for both steppers.
		native.Row(c).Gap(16).AlignItems(native.Start).Children(func() {
			native.Column(c).Grow(1).Basis(0).Children(func() {
				native.Field(c, m.ManagedScheduleStart, func() {
					w.nativeNumber(c, &entry.Schedule.StartDelayMinutes, 0, 1440, m.ManagedScheduleStart)
				})
			})
			native.Column(c).Grow(1).Basis(0).Children(func() {
				native.Field(c, m.ManagedScheduleExit, func() {
					w.nativeNumber(c, &entry.Schedule.AutoExitMinutes, 0, 1440, m.ManagedScheduleExit)
				})
			})
		})
		if _, height := c.Size(); height >= 800 {
			nativeHint(c, m.ManagedScheduleRangeHint)
		}
		if native.Checkbox(c, &entry.Schedule.FrequencyEnabled, m.ManagedFrequency).Tooltip(m.ManagedFrequencyHint).Changed() {
			w.save()
		}
		if entry.Schedule.FrequencyEnabled {
			native.Row(c).Gap(8).Children(func() {
				w.nativeNumber(c, &entry.Schedule.FrequencyDays, 1, 365, m.ManagedFrequencyDays)
				native.Text(c, m.ManagedFrequencyDays)
				w.nativeNumber(c, &entry.Schedule.FrequencyRuns, 1, 1000, m.ManagedFrequencyRuns)
				native.Text(c, m.ManagedFrequencyRuns)
			})
			if _, height := c.Size(); height >= 800 {
				nativeHint(c, m.ManagedFrequencyHint)
			}
		}
	})
}

// Tooltip content is also the accessible label; Tab can reach the help even
// when a compact window cannot display a long explanation alongside a field.
func nativeHelp(c *native.Context, hint string) {
	native.Toolbar(c, func() {
		native.Button(c, "").Label(hint).Tooltip(hint).Padding(4).Children(func() {
			native.Icon(c, helpIcon).Size(16, 16).TextColor(c.Theme().TextMuted)
		})
	}).Padding(0)
}
