//go:build windows

package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/stringutil"
	"wintray/internal/version"
)

type desktopState struct {
	window             *mygo.Window
	initialized        bool
	selected           int
	list               native.ListState
	icons              map[string]*native.Bitmap
	argsDirty          bool
	numberCommits      []func()
	frameNumberCommits []func()
	orderOptions       []string
}

// The gallery's 24-DIP stroked SVG convention follows the toolkit's current
// text color and theme. Parse once, never during a frame.
func nativeIcon(shapes string) *native.SVG {
	return native.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	addIcon        = nativeIcon(`<path d="M12 5v14M5 12h14"/>`)
	settingsIcon   = nativeIcon(`<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>`)
	backIcon       = nativeIcon(`<path d="m12 5-7 7 7 7M5 12h14"/>`)
	playIcon       = nativeIcon(`<path d="m8 5 11 7-11 7z"/>`)
	programIconSVG = nativeIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M7 6.5h.01M10 6.5h.01"/>`)
)

func nativeAction(c *native.Context, label string, icon *native.SVG) *native.Element {
	b := native.Button(c, "").Label(label)
	b.Children(func() {
		if icon != nil {
			native.Icon(c, icon).Size(16, 16)
		}
		text := label
		if icon == backIcon {
			text = strings.TrimPrefix(text, "← ")
		}
		native.Text(c, text).SingleLine()
	})
	return b
}

func nativeHint(c *native.Context, text string) {
	hint := native.Text(c, text).TextColor(c.Theme().TextMuted)
	// Short windows retain every control; long help remains available on
	// hover and to screen readers without introducing page scrolling.
	if _, height := c.Size(); height < 800 {
		hint.SingleLine().Tooltip(text)
	}
}

// Only Table owns a scroll viewport. The two pages, editor and footer have
// bounded layouts so scrolling the list never moves a setting or action.
func (w *MainWindow) nativeView(c *native.Context) {
	w.desktop.frameNumberCommits = nil
	if !w.desktop.initialized {
		w.desktop.initialized = true
		w.desktop.selected = -1
		w.resetNativeList(0)
	}
	if count := len(w.settings.ManagedApps); len(w.desktop.orderOptions) != count {
		w.desktop.orderOptions = make([]string, count)
		for i := range w.desktop.orderOptions {
			w.desktop.orderOptions[i] = strconv.Itoa(i + 1)
		}
	}
	m := i18n.For(w.settings.Language)
	_, height := c.Size()
	padding, gap := float32(18), float32(14)
	if height < 860 {
		padding, gap = 10, 8
	}
	native.Column(c).Fill().Padding(padding, 24).Gap(gap).Children(func() {
		if w.onSettingsPage {
			w.nativeSettings(c, m)
		} else {
			w.nativePrograms(c, m)
		}
		native.Divider(c)
		w.nativeFooter(c, m)
	})
	w.desktop.numberCommits = w.desktop.frameNumberCommits
}

// Drop toolkit layout caches when the desktop closes, preserving position.
func (w *MainWindow) resetNativeList(first int) {
	w.desktop.list = native.ListState{
		Selected: &w.desktop.selected,
		Key:      func(i int) any { return w.settings.ManagedApps[i].ID },
		Label:    func(i int) string { return w.settings.ManagedApps[i].Name },
	}
	w.desktop.list.ScrollTo(first, native.Start)
}

func (w *MainWindow) nativePrograms(c *native.Context, m i18n.Messages) {
	native.Row(c).Gap(12).Children(func() {
		native.Text(c, m.WindowTitle).FontSize(22)
		native.Spacer(c)
		lang := m.LanguageZhLabel
		if w.settings.Language == "en-US" {
			lang = m.LanguageEnLabel
		}
		if native.Select(c, &lang, []string{m.LanguageZhLabel, m.LanguageEnLabel}).Width(112).Label(m.LanguageLabel).Changed() {
			w.settings.Language = "zh-CN"
			if lang == m.LanguageEnLabel {
				w.settings.Language = "en-US"
			}
			if w.desktop.window != nil {
				w.desktop.window.SetTitle(i18n.For(w.settings.Language).WindowTitle)
			}
			w.save()
		}
		if nativeAction(c, m.OpenSettings, settingsIcon).Clicked() {
			w.commitNativeEdits()
			w.onSettingsPage = true
		}
	})
	if !w.settings.RunAtLogon {
		native.Row(c).Gap(12).Padding(8, 12).Background(c.Theme().Surface).Children(func() {
			native.Text(c, m.LogonOffNotice).Grow(1)
			if native.Button(c, m.LogonOffEnable).Clicked() {
				w.settings.RunAtLogon = true
				w.save()
			}
		})
	}
	native.Row(c).Grow(1).MinHeight(0).AlignItems(native.Stretch).Gap(14).Children(func() {
		native.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(8).Children(func() {
			if len(w.settings.ManagedApps) == 0 {
				native.Column(c).Grow(1).Center().Gap(14).Children(func() {
					native.Icon(c, programIconSVG).Size(36, 36).TextColor(c.Theme().TextMuted)
					native.Text(c, m.ManagedListEmpty).FontSize(18)
					native.Text(c, m.ManagedListEmptyHint).FillWidth().TextColor(c.Theme().TextMuted)
					if nativeAction(c, m.AddProgram, addIcon).Clicked() {
						w.nativeChooseProgram(false)
					}
				})
				return
			}
			native.Row(c).Children(func() {
				native.Text(c, m.ManagedListTitle).FontSize(16)
				native.Spacer(c)
				if nativeAction(c, m.AddProgram, addIcon).Clicked() {
					w.nativeChooseProgram(false)
				}
			})
			nativeHint(c, m.ManagedListHint)
			cols := []native.TableColumn{
				{ID: "name", Title: m.ManagedColumnName, Fixed: true},
				{ID: "tray", Title: m.TrayBoxHomeTitle, Width: 96, Fixed: true, Align: native.Center},
				{ID: "mode", Title: m.ManagedColumnRule, Width: 150, Fixed: true, Align: native.Center},
				{ID: "order", Title: m.ManagedColumnOrder, Width: 88, Fixed: true, Align: native.Center},
			}
			orderFrom, orderTo := -1, -1
			// Table rows use Accent for selection. Scope the neutral palette to
			// the table itself so its checkboxes and dropdowns keep their theme.
			theme := c.Theme()
			listTheme := *theme
			listTheme.Accent, listTheme.AccentText = theme.SurfacePressed, theme.Text
			c.SetTheme(&listTheme)
			table := native.Table(c, &w.desktop.list, cols, len(w.settings.ManagedApps), func(row, col int) {
				c.SetTheme(theme)
				defer c.SetTheme(&listTheme)
				entry := &w.settings.ManagedApps[row]
				switch col {
				case 3:
					order := strconv.Itoa(row + 1)
					if native.Select(c, &order, w.desktop.orderOptions).MinWidth(0).FillWidth().Padding(6, 8).Gap(6).Label(m.ManagedColumnOrder + " " + entry.Name).Changed() {
						if position, err := strconv.Atoi(order); err == nil {
							orderFrom, orderTo = row, position-1
						}
					}
				case 0:
					native.Row(c).Gap(6).Children(func() {
						if native.Checkbox(c, &entry.RunOnStartup, "").Label(m.ManagedEnabled + " " + entry.Name).Changed() {
							w.save()
						}
						if img := w.desktop.icons[entry.ExePath]; img != nil {
							native.Image(c, img).Size(18, 18)
						} else {
							native.Icon(c, programIconSVG).Size(18, 18)
						}
						name := native.Text(c, entry.Name).SingleLine().Grow(1)
						if !entry.RunOnStartup {
							name.TextColor(c.Theme().TextMuted)
						}
					})
				case 1:
					collected := entry.CollectTrayIcon
					// Disabled on the parent: widget input is handled at creation.
					native.Row(c).Center().Disabled(shouldDefaultLaunchHidden(entry.ExePath)).Children(func() {
						if native.Checkbox(c, &collected, "").Label(m.TrayBoxHomeTitle + " " + entry.Name).Changed() {
							w.nativeCollect(row, collected)
						}
					})
				case 2:
					native.Text(c, i18n.FormatManagedParam(w.settings.Language, *entry)).SingleLine()
				}
			}).Grow(1).MinHeight(0).Label(m.ManagedListTitle)
			c.SetTheme(theme)
			if table.Changed() {
				w.commitNativeEdits()
			}
			if table.Clicked() {
				w.commitNativeEdits()
				w.desktop.selected = -1
			}
			// Finish the keyed rows before changing their order. A mutation from
			// inside a cell would build the moved identity twice in one frame.
			if orderFrom >= 0 {
				w.nativeSetOrder(orderFrom, orderTo)
			}
		})
		native.Divider(c)
		native.Column(c).Width(523).Shrink(0).Gap(12).Children(func() { w.nativeEditor(c, m) })
	})
}

func (w *MainWindow) nativeSelected() (*config.ManagedAppEntry, bool) {
	i := w.desktop.selected
	if i < 0 || i >= len(w.settings.ManagedApps) {
		return nil, false
	}
	return &w.settings.ManagedApps[i], true
}

func (w *MainWindow) nativeEditor(c *native.Context, m i18n.Messages) {
	entry, ok := w.nativeSelected()
	// The preview is separate from saved entries and has its own widget keys.
	// Keep every setting visible without giving the example any actions.
	var editorKey any = struct{}{}
	if !ok {
		entry = &config.ManagedAppEntry{
			Name: m.ManagedExampleName, ExePath: `C:\Apps\Example\Example.exe`, RunOnStartup: true,
			TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true},
			Schedule:     config.Schedule{AutoExitMinutes: config.DefaultAutoExitMinutes, FrequencyDays: 1, FrequencyRuns: 1},
		}
	} else {
		editorKey = entry.ID
	}
	// A different entry must never inherit the previous entry's text cursor
	// or uncommitted input state.
	_, height := c.Size()
	gap := float32(12)
	if height < 860 {
		gap = 5
	}
	native.Column(c).Key(editorKey).Grow(1).Gap(gap).Disabled(!ok).Children(func() {
		native.Row(c).Gap(8).Children(func() {
			native.Text(c, entry.Name).FontSize(18).SingleLine().Grow(1)
			launchLabel := m.ManagedLaunchNow
			if ok && w.launchNowBusy {
				launchLabel = m.ManagedLaunchNowBusy
			}
			native.Row(c).Disabled(w.launchNowBusy).Children(func() {
				if nativeAction(c, launchLabel, playIcon).Clicked() && w.callbacks.OnLaunchNow != nil {
					w.commitNativeEdits()
					w.launchNowBusy = true
					w.callbacks.OnLaunchNow(*entry)
				}
			})
		})
		native.Row(c).Gap(8).Children(func() {
			if native.Button(c, m.BrowseProgram).Clicked() {
				w.nativeChooseProgram(true)
			}
			native.Text(c, entry.ExePath).SingleLine().Grow(1).TextColor(c.Theme().TextMuted)
		})
		native.Divider(c)
		native.Column(c).Gap(5).Children(func() {
			if native.Checkbox(c, &entry.RunOnStartup, m.ManagedEnabled).Changed() {
				w.save()
			}
			nativeHint(c, m.ManagedEnabledHint)
		})
		native.Column(c).Gap(6).Disabled(!entry.RunOnStartup).Children(func() {
			native.Text(c, m.ManagedModeLabel)
			mode := startModeOf(*entry)
			script := shouldDefaultLaunchHidden(entry.ExePath)
			native.RadioGroup(c, func() {
				for _, choice := range []struct {
					mode startMode
					text string
				}{
					{startToTray, m.ManagedAutoHide}, {startHidden, m.ManagedLaunchHidden}, {startTask, m.ManagedTaskLaunch}, {startNormal, m.ManagedLaunchOnly},
				} {
					native.Row(c).Disabled(script && choice.mode != startHidden).Children(func() {
						if native.Radio(c, &mode, choice.mode, choice.text).Changed() {
							mode.applyTo(entry)
							w.save()
						}
					})
				}
			}).Row().Wrap().Gap(12).Label(m.ManagedModeLabel)
			hint := m.ManagedLaunchOnlyHint
			switch mode {
			case startToTray:
				hint = m.ManagedAutoHideHint
			case startHidden:
				hint = m.ManagedLaunchHiddenHint
			case startTask:
				hint = m.ManagedTaskLaunchHint
			}
			nativeHint(c, hint)
			if mode == startToTray {
				native.Row(c).Gap(8).Padding(0, 16).Children(func() {
					native.Text(c, m.ManagedCloseDelay)
					w.nativeNumber(c, &entry.TrayBehavior.CloseDelaySeconds, 0, 600, m.ManagedCloseDelay)
					native.Text(c, m.SecondsUnit)
				})
				nativeHint(c, m.ManagedCloseDelayHint)
			}
		})
		native.Column(c).Gap(5).Disabled(!entry.RunOnStartup).Children(func() {
			native.Text(c, m.ManagedAppArgs)
			input := native.TextInput(c, &entry.Args).Placeholder(m.ManagedArgsPlaceholder).Label(m.ManagedAppArgs)
			if input.Changed() {
				w.desktop.argsDirty = true
			}
			if input.Submitted() || !input.Focused() {
				w.commitNativeEdits()
			}
			if startModeOf(*entry) == startTask {
				nativeHint(c, m.ManagedTaskArgsHint)
			} else {
				nativeHint(c, m.ManagedArgsHint)
			}
		})
		native.Column(c).Gap(8).Children(func() {
			if native.Checkbox(c, &entry.Schedule.Enabled, m.ManagedSchedule).Changed() {
				w.save()
			}
			if !ok || entry.Schedule.Enabled {
				native.Column(c).Gap(7).Padding(0, 16).Children(func() {
					native.Row(c).Gap(8).Wrap().Children(func() {
						// Schedules can be prepared while sign-in startup is paused.
						native.Row(c).Gap(6).Children(func() {
							native.Text(c, m.ManagedScheduleStart)
							w.nativeNumber(c, &entry.Schedule.StartDelayMinutes, 0, 1440, m.ManagedScheduleStart)
							native.Text(c, m.ManagedScheduleStartUnit)
						})
						native.Text(c, m.ManagedScheduleExit)
						w.nativeNumber(c, &entry.Schedule.AutoExitMinutes, 0, 1440, m.ManagedScheduleExit)
						native.Text(c, m.ManagedScheduleExitUnit)
					})
					nativeHint(c, m.ManagedScheduleHint)
					native.Column(c).Gap(7).Children(func() {
						if native.Checkbox(c, &entry.Schedule.FrequencyEnabled, m.ManagedFrequency).Changed() {
							w.save()
						}
						if !ok || entry.Schedule.FrequencyEnabled {
							native.Row(c).Gap(6).Children(func() {
								w.nativeNumber(c, &entry.Schedule.FrequencyDays, 1, 365, m.ManagedFrequencyDays)
								native.Text(c, m.ManagedFrequencyDays)
								w.nativeNumber(c, &entry.Schedule.FrequencyRuns, 1, 1000, m.ManagedFrequencyRuns)
								native.Text(c, m.ManagedFrequencyRuns)
							})
							nativeHint(c, m.ManagedFrequencyHint)
						}
					})
				})
			}
		})
		native.Spacer(c)
		native.Row(c).Children(func() {
			native.Spacer(c)
			if native.Button(c, m.RemoveSelected).Clicked() {
				w.nativeRemoveSelected()
			}
		})
	})
}

type numberEditor struct {
	text  string
	dirty bool
}

// Keep the original compact numeric field and commit once on blur/Enter.
// The toolkit's NumberInput includes two extra buttons and cannot fit these
// existing paired schedule rows without changing their layout.
func (w *MainWindow) nativeNumber(c *native.Context, value *int, low, high int, label string) {
	row := native.Row(c).Shrink(0)
	state := native.Local(row, "number", func() numberEditor { return numberEditor{text: strconv.Itoa(*value)} })
	commit := func() {
		if !state.dirty {
			return
		}
		v, err := strconv.Atoi(strings.TrimSpace(state.text))
		if err != nil {
			body := fmt.Sprintf("请输入 %d–%d 之间的整数。", low, high)
			if w.settings.Language == "en-US" {
				body = fmt.Sprintf("Enter a whole number from %d to %d.", low, high)
			}
			w.ShowError(label, body)
		} else {
			v = min(high, max(low, v))
			if v != *value {
				*value = v
				w.save()
			}
		}
		state.dirty = false
		state.text = strconv.Itoa(*value)
	}
	w.desktop.frameNumberCommits = append(w.desktop.frameNumberCommits, commit)
	row.Children(func() {
		input := native.TextInput(c, &state.text).Width(68).Label(label)
		if input.Changed() {
			state.dirty = true
		}
		for _, key := range []struct {
			key  native.Key
			step int
		}{{native.KeyUp, 1}, {native.KeyDown, -1}} {
			if input.Shortcut(0, key.key) {
				*value = min(high, max(low, *value+key.step))
				state.text = strconv.Itoa(*value)
				state.dirty = false
				w.save()
			}
		}
		if state.dirty && (input.Submitted() || !input.Focused()) {
			commit()
		}
		if !state.dirty && !input.Focused() {
			state.text = strconv.Itoa(*value)
		}
	})
}

func (w *MainWindow) nativeSettings(c *native.Context, m i18n.Messages) {
	native.Column(c).Grow(1).MaxWidth(900).FillWidth().Margin(0, native.Auto).Gap(16).Children(func() {
		native.Row(c).Gap(12).Children(func() {
			if nativeAction(c, m.BackToPrograms, backIcon).Clicked() {
				w.onSettingsPage = false
			}
			native.Text(c, m.SettingsTitle).FontSize(22)
		})
		native.Text(c, m.SettingsStartupTitle).FontSize(16)
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
		native.Text(c, m.SettingsTimingTitle).FontSize(16)
		w.nativeSetting(c, m.StartupInterval, m.StartupIntervalHint, false, func() {
			w.nativeNumber(c, &w.settings.StartupIntervalSeconds, 0, 120, m.StartupInterval)
			native.Text(c, m.SecondsUnit)
		})
		w.nativeSetting(c, m.RetrySeconds, m.RetrySecondsHint, false, func() {
			w.nativeNumber(c, &w.settings.CloseWindowRetrySeconds, 0, 120, m.RetrySeconds)
			native.Text(c, m.SecondsUnit)
		})
		native.Text(c, m.SettingsTroubleshootTitle).FontSize(16)
		for _, action := range []struct {
			title, hint, label string
			run                func()
		}{
			{m.LogsTitle, m.OpenLogsHint, m.OpenLogs, w.callbacks.OnOpenLogs},
			{m.RemoveLogonTaskTitle, m.RemoveLogonTaskHint, m.RemoveLogonTask, w.callbacks.OnRemoveLogon},
			{m.CleanupRestoreTitle, m.CleanupRestoreHint, m.CleanupRestore, w.callbacks.OnCleanupRestore},
		} {
			w.nativeSetting(c, action.title, action.hint, false, func() {
				if native.Button(c, action.label).Clicked() && action.run != nil {
					action.run()
				}
			})
		}
		native.Spacer(c)
	})
}

func (w *MainWindow) nativeSetting(c *native.Context, title, hint string, disabled bool, controls func()) {
	native.Row(c).Gap(24).Disabled(disabled).Children(func() {
		native.Column(c).Grow(1).Basis(0).Gap(4).Children(func() { native.Text(c, title); nativeHint(c, hint) })
		native.Row(c).Width(180).Justify(native.End).Gap(8).Children(controls)
	})
}

func (w *MainWindow) nativeFooter(c *native.Context, m i18n.Messages) {
	native.Row(c).Gap(12).Children(func() {
		if !w.onSettingsPage {
			native.Text(c, fmt.Sprintf(m.VersionLabel, version.Number)).TextColor(c.Theme().TextMuted)
			if native.Button(c, m.GitHubLink).Clicked() && w.callbacks.OnOpenRepository != nil {
				w.callbacks.OnOpenRepository()
			}
			native.Row(c).Disabled(w.checkingUpdate).Children(func() {
				label := m.CheckUpdate
				if w.checkingUpdate {
					label = m.CheckUpdateBusy
				}
				if native.Button(c, label).Clicked() && w.callbacks.OnCheckUpdate != nil {
					w.checkingUpdate = true
					w.callbacks.OnCheckUpdate()
				}
			})
		}
		native.Spacer(c)
		if native.Button(c, m.RunSilently).Clicked() && w.callbacks.OnRunSilently != nil {
			w.commitNativeEdits()
			w.callbacks.OnRunSilently()
		}
		if native.Button(c, m.ExitApp).Clicked() && w.callbacks.OnExit != nil {
			w.commitNativeEdits()
			w.callbacks.OnExit()
		}
	})
}

func (w *MainWindow) commitNativeEdits() {
	for _, commit := range w.desktop.numberCommits {
		commit()
	}
	for _, commit := range w.desktop.frameNumberCommits {
		commit()
	}
	if w.desktop.argsDirty {
		w.desktop.argsDirty = false
		w.save()
	}
}

func (w *MainWindow) nativeCollect(row int, on bool) bool {
	if row < 0 || row >= len(w.settings.ManagedApps) {
		return false
	}
	entry := &w.settings.ManagedApps[row]
	if on && shouldDefaultLaunchHidden(entry.ExePath) {
		return false
	}
	if w.callbacks.OnToggleTrayBox != nil {
		if err := w.callbacks.OnToggleTrayBox(entry.ID, on); err != nil {
			w.ShowError(i18n.For(w.settings.Language).TrayBoxFailedTitle, err.Error())
			return false
		}
		entry.CollectTrayIcon = on
	} else {
		entry.CollectTrayIcon = on
		w.save()
	}
	return true
}

func (w *MainWindow) nativeRemoveSelected() {
	entry, ok := w.nativeSelected()
	if !ok {
		return
	}
	i := w.desktop.selected
	removedPath := entry.ExePath
	if entry.CollectTrayIcon && !w.nativeCollect(i, false) {
		return
	}
	w.commitNativeEdits()
	w.settings.ManagedApps = slices.Delete(w.settings.ManagedApps, i, i+1)
	w.desktop.selected = -1
	w.save()
	delete(w.desktop.icons, removedPath)
}

// The selected number denotes the final position in the saved start order.
func (w *MainWindow) nativeSetOrder(from, to int) {
	if from < 0 || from >= len(w.settings.ManagedApps) || to < 0 || to >= len(w.settings.ManagedApps) {
		return
	}
	if from == to {
		return
	}
	w.commitNativeEdits()
	entry := w.settings.ManagedApps[from]
	w.settings.ManagedApps = slices.Delete(w.settings.ManagedApps, from, from+1)
	w.settings.ManagedApps = slices.Insert(w.settings.ManagedApps, to, entry)
	w.desktop.selected = to
	w.desktop.list.ScrollIntoView(to)
	w.save()
}

func (w *MainWindow) nativeChooseProgram(replace bool) {
	m := i18n.For(w.settings.Language)
	opts := mygo.OpenDialogOptions{Parent: w.desktop.window, Title: m.SelectManagedExe, Multiple: !replace, Filters: []mygo.FileFilter{
		{Name: m.SelectManagedExe, Extensions: []string{"exe", "bat", "cmd", "ps1", "py", "pyw"}}, {Name: "*.*", Extensions: []string{"*"}},
	}}
	if entry, ok := w.nativeSelected(); replace && ok {
		opts.Title = m.SelectReplacementExe
		opts.DefaultPath = entry.ExePath
	}
	paths, err := mygo.Dialog.Open(opts)
	if err != nil {
		w.ShowError(m.WindowTitle, err.Error())
		return
	}
	if len(paths) == 0 {
		return
	}
	w.nativeAddPaths(paths, replace)
}

func (w *MainWindow) nativeAddPaths(paths []string, replace bool) {
	w.commitNativeEdits()
	if replace {
		entry, ok := w.nativeSelected()
		if !ok || len(paths) == 0 {
			return
		}
		if entry.CollectTrayIcon && !w.nativeCollect(w.desktop.selected, false) {
			return
		}
		entry.ExePath, entry.Name = paths[0], stringutil.TrimExt(filepath.Base(paths[0]))
		if shouldDefaultLaunchHidden(entry.ExePath) {
			startHidden.applyTo(entry)
		}
	} else {
		base := time.Now().UnixNano()
		for i, path := range paths {
			hidden := shouldDefaultLaunchHidden(path)
			w.settings.ManagedApps = append(w.settings.ManagedApps, config.ManagedAppEntry{
				ID: strconv.FormatInt(base+int64(i), 10), Name: stringutil.TrimExt(filepath.Base(path)), ExePath: path, RunOnStartup: true,
				LaunchHiddenInBackground: hidden, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: !hidden},
				Schedule: config.Schedule{AutoExitMinutes: 30, FrequencyDays: 1, FrequencyRuns: 1},
			})
		}
		w.desktop.selected = len(w.settings.ManagedApps) - 1
		w.desktop.list.ScrollIntoView(w.desktop.selected)
	}
	w.save()
	if w.desktop.window != nil {
		w.loadNativeIcons()
	}
}
