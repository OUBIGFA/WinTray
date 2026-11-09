//go:build windows

package ui

import (
	"fmt"
	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"strconv"
	"strings"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/ui/kit"
	"wintray/internal/ui/kit/icons"
)

// The editor groups settings by purpose. Each selected identity owns its
// input state; switching tabs commits pending values first.
func (w *MainWindow) nativeEditor(c *native.Context, m i18n.Messages) {
	native.Column(c).Fill().Padding(12, 4, 12, 20).Children(func() {
		entry, ok := w.nativeSelected()
		if !ok {
			native.Column(c).Gap(8).Children(func() { kit.Help(c, m.ManagedSelectPrompt); kit.Help(c, m.ManagedSelectHint) })
			return
		}
		if w.desktop.detailFor != entry.ID {
			w.desktop.detailFor = entry.ID
			w.desktop.detailTab = 0
		}
		native.Column(c).Key(entry.ID).FillWidth().Gap(10).Children(func() {
			native.Column(c).FillWidth().Gap(4).Children(func() {
				kit.Heading(c, entry.Name).Role(native.RoleHeading).Level(2).SingleLine().Ellipsis("…").Tooltip(entry.Name).Selectable()
				kit.Mono(c, entry.ExePath).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…").Tooltip(entry.ExePath).Selectable()
			})
			w.nativeProgramActions(c, m, entry)
			kit.SubTabs(c, &w.desktop.detailTab, []string{m.CommonSettings, m.MoreSettings}, func(apply func()) { w.commitNativeEdits(); apply() })
			native.Column(c).FillWidth().Gap(16).Children(func() {
				if w.desktop.detailTab == 0 {
					kit.Group(c, icons.Rocket, m.LaunchBehavior, "", func() {
						if kit.Toggle(c, &entry.RunOnStartup, m.ManagedEnabled, "").MinHeight(40).Padding(6, 16).Changed() {
							w.save()
						}
						native.Column(c).FillWidth().Disabled(!entry.RunOnStartup).Children(func() { w.nativeStartMode(c, m, entry) })
					})
					w.nativeSchedule(c, m, entry)
				} else {
					kit.Panel(c, func() {
						nativeInputField(c, m.ProgramName, "", true, func(handle *native.Handle) { w.nativeProgramName(c, m, entry, handle) })
						nativeInputField(c, m.ManagedAppArgs, m.ManagedArgsHint, true, func(handle *native.Handle) {
							input := native.TextInput(c, &entry.Args).FillWidth().Placeholder(m.ManagedArgsPlaceholder).Label(m.ManagedAppArgs).Bind(handle)
							if input.Changed() {
								w.desktop.argsDirty = true
							}
							if input.Submitted() {
								w.commitNativeEdits()
							} else if !input.Focused() && w.desktop.argsDirty {
								w.desktop.frameBlurCommits = append(w.desktop.frameBlurCommits, w.commitNativeEdits)
							}
							if startModeOf(*entry) == startTask {
								kit.Help(c, m.ManagedTaskArgsHint)
							}
						})
						nativeInputField(c, m.ManagedWorkingDirectory, m.ManagedWorkingDirectoryHint, true, func(handle *native.Handle) {
							native.Row(c).FillWidth().Gap(8).Children(func() {
								input := native.TextInput(c, &entry.WorkingDirectory).Grow(1).Shrink(1).MinWidth(0).Label(m.ManagedWorkingDirectory).Bind(handle)
								if input.Changed() {
									w.desktop.workingDirectoryDirty = true
								}
								if input.Submitted() {
									w.commitNativeEdits()
								} else if !input.Focused() && w.desktop.workingDirectoryDirty {
									w.desktop.frameBlurCommits = append(w.desktop.frameBlurCommits, w.commitNativeEdits)
								}
								if kit.Button(c, m.BrowseWorkingDirectory, icons.FolderOpen, false).Shrink(0).Clicked() {
									w.commitNativeEdits()
									paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w.desktop.window, Title: m.ManagedWorkingDirectory, Directory: true, DefaultPath: entry.LaunchDirectory()})
									if err != nil {
										w.ShowError(m.ManagedWorkingDirectory, err.Error())
									} else if len(paths) > 0 {
										entry.WorkingDirectory = paths[0]
										w.save()
									}
								}
							})
							if startModeOf(*entry) == startTask {
								kit.Help(c, m.ManagedTaskDirectoryHint)
							}
						})
					})
				}
			})
		})
	})
}

type programNameEditor struct {
	text  string
	dirty bool
}

// Only user edits write back; a replaced program refreshes an untouched field.
// Keep an empty rename in the field rather than persisting an unnamed entry.
func (w *MainWindow) nativeProgramName(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry, handle *native.Handle) {
	box := native.Column(c).FillWidth()
	state := native.Local(box, "name", func() programNameEditor { return programNameEditor{text: entry.Name} })
	commit := func() {
		if !state.dirty {
			return
		}
		if name := strings.TrimSpace(state.text); name != "" {
			state.dirty = false
			if name != entry.Name {
				entry.Name = name
				w.save()
			}
		}
	}
	w.desktop.frameNumberCommits = append(w.desktop.frameNumberCommits, commit)
	box.Children(func() {
		input := native.TextInput(c, &state.text).FillWidth().Label(m.ProgramName).Bind(handle)
		if input.Changed() {
			state.dirty = true
		}
		if strings.TrimSpace(state.text) == "" {
			input.Error(m.ProgramNameRequired)
		}
		if input.Submitted() {
			commit()
		} else if !input.Focused() {
			w.desktop.frameBlurCommits = append(w.desktop.frameBlurCommits, commit)
		}
		if !state.dirty && !input.Focused() {
			state.text = entry.Name
		}
	})
}

func (w *MainWindow) nativeProgramActions(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry) {
	visible := w.callbacks.OnProgramWindowVisible != nil && w.callbacks.OnProgramWindowVisible(*entry)
	running := w.programRunning(*entry)
	windowBusy, busy := w.programWindowBusy[entry.ID], w.launchNowBusy[entry.ID]
	windowLabel := m.HostedShowWindow
	if visible {
		windowLabel = m.HostedHideWindow
	}
	if windowBusy {
		windowLabel = m.ManagedWindowBusy
	}
	actionLabel, actionIcon, actionHint := m.ManagedLaunchNow, icons.Play, m.ManagedLaunchNowHint
	if running {
		actionLabel, actionIcon, actionHint = m.ManagedStopProgram, icons.Square, m.ManagedStopProgramHint
	}
	if busy {
		actionLabel = m.ManagedLaunchNowBusy
		if running {
			actionLabel = m.ManagedStopProgramBusy
		}
	}
	// Live actions remain separate from (and independent of) startup settings.
	native.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
		action := nativeAction(c, actionLabel, actionIcon)
		if action.Disabled(busy || windowBusy).Tooltip(actionHint).Clicked() {
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
		if nativeAction(c, windowLabel, icons.PanelTop).Disabled(busy || windowBusy).Tooltip(m.ManagedWindowHint).Clicked() && w.callbacks.OnSetProgramWindowVisible != nil {
			if w.programWindowBusy == nil {
				w.programWindowBusy = make(map[string]bool)
			}
			w.programWindowBusy[entry.ID] = true
			w.callbacks.OnSetProgramWindowVisible(*entry, !visible)
		}
		// Keep confirmation bound to the selected ID even if the list reorders.
		id := entry.ID
		kit.Button(c, m.ManagedColumnDelete, icons.Trash2, false).Label(m.ManagedColumnDelete + " " + entry.Name).Menu(func(menu *native.Menu) {
			menu.Item(fmt.Sprintf(m.RemoveConfirmPrompt, strings.ReplaceAll(entry.Name, "&", "&&"))).Disabled(true)
			menu.Separator()
			if menu.Item(m.RemoveConfirmLabel).Chosen() {
				w.desktop.removeID = id
			}
		})
	})
}

func (w *MainWindow) nativeStartMode(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry) {
	mode := startModeOf(*entry)
	current := strconv.Itoa(int(mode))
	choices := []kit.SelectOption{{Value: "0", Label: m.ManagedAutoHide}, {Value: "1", Label: m.ManagedLaunchHidden}, {Value: "2", Label: m.ManagedTaskLaunch}, {Value: "3", Label: m.ManagedLaunchOnly}}
	if shouldDefaultLaunchHidden(entry.ExePath) {
		choices = choices[1:2]
	}
	kit.Stack(c, m.ManagedModeLabel, "", func() {
		native.Row(c).FillWidth().Children(func() {
			native.Box(c).Shrink(1).MinWidth(0).Children(func() {
				if kit.Segmented(c, &current, choices, m.ManagedModeLabel) {
					choice, _ := strconv.Atoi(current)
					startMode(choice).applyTo(entry)
					w.save()
				}
			})
		})
	}).Padding(6, 16)
	if mode == startToTray {
		kit.Indented(c, func() {
			w.nativeNumberRow(c, m.ManagedCloseDelay, m.ManagedCloseDelayHint, &entry.TrayBehavior.CloseDelaySeconds, 0, 600)
		})
	}
	if config.IsWeChatExecutable(entry.ExePath) {
		if kit.Toggle(c, &entry.WeChatAutoLogin, m.ManagedWeChatAutoLogin, m.ManagedWeChatAutoLoginHint).MinHeight(40).Padding(6, 16).Disabled(mode == startHidden).Changed() {
			w.save()
		}
	}
}

func (w *MainWindow) nativeSchedule(c *native.Context, m i18n.Messages, entry *config.ManagedAppEntry) {
	plan := &entry.Schedule
	kit.Group(c, icons.CalendarClock, m.ScheduleTitle, "", func() {
		if kit.Toggle(c, &plan.Enabled, m.ManagedSchedule, "").MinHeight(40).Padding(6, 16).Changed() {
			w.save()
		}
		if plan.Enabled {
			kit.Indented(c, func() {
				// Pair related values so an expanded schedule fits beside the
				// list at the same window size as the application settings.
				native.Row(c).FillWidth().Children(func() {
					w.nativeScheduleNumber(c, m.ManagedScheduleStart, m.ManagedScheduleRangeHint, &plan.StartDelayMinutes, 0, 1440)
					w.nativeScheduleNumber(c, m.ManagedScheduleExit, m.ManagedScheduleRangeHint, &plan.AutoExitMinutes, 0, 1440)
				})
			})
		}
		hint := ""
		if !plan.Enabled {
			hint = m.ScheduleRequired
		}
		if kit.Toggle(c, &plan.FrequencyEnabled, m.ManagedFrequency, hint).MinHeight(40).Padding(6, 16).Disabled(!plan.Enabled).Tooltip(m.ManagedFrequencyHint).Changed() {
			w.save()
		}
		if plan.Enabled && plan.FrequencyEnabled {
			kit.Indented(c, func() {
				native.Row(c).FillWidth().Children(func() {
					w.nativeScheduleNumber(c, m.FrequencyDaysLabel, m.ManagedFrequencyHint, &plan.FrequencyDays, 1, 365)
					w.nativeScheduleNumber(c, m.FrequencyRunsLabel, m.ManagedFrequencyHint, &plan.FrequencyRuns, 1, 1000)
				})
			})
		}
	})
}

func (w *MainWindow) nativeScheduleNumber(c *native.Context, label, hint string, value *int, low, high int) {
	field := native.Row(c).Basis(0).Grow(1).Padding(6, 16).Gap(8).AlignItems(native.Center).Tooltip(hint)
	handle := native.Local(field, "field-handle", func() native.Handle { return native.Handle{} })
	field.Children(func() {
		if kit.Label(c, label).Basis(0).Grow(1).Clicked() {
			handle.Focus()
		}
		w.nativeNumber(c, value, low, high, label, handle).Width(80)
	})
}

func (w *MainWindow) nativeNumberRow(c *native.Context, label, hint string, value *int, low, high int) {
	nativeInputField(c, label, "", false, func(handle *native.Handle) { w.nativeNumber(c, value, low, high, label, handle) }).Tooltip(hint)
}

// Clicking a field caption focuses its input.
func nativeInputField(c *native.Context, label, hint string, stacked bool, control func(*native.Handle)) native.Element {
	var field native.Element
	if stacked {
		field = native.Column(c).FillWidth().Padding(12, 16).Gap(8)
	} else {
		field = native.Row(c).FillWidth().MinHeight(kit.RowMinHeight).Padding(10, 16).GapX(16).GapY(8).AlignItems(native.Center).Wrap()
	}
	handle := native.Local(field, "field-handle", func() native.Handle { return native.Handle{} })
	return field.Children(func() {
		text := kit.Label(c, label)
		if !stacked {
			text.Basis(220).Grow(1).Shrink(1)
		}
		if text.Clicked() {
			handle.Focus()
		}
		control(handle)
		if hint != "" {
			kit.Help(c, hint)
		}
	})
}
