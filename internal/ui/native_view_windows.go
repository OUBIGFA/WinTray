//go:build windows

package ui

import (
	"fmt"
	"os"
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
	frameBlurCommits   []func()
	exitPressed        bool
	orderOptions       []string
}

// One 24-DIP SVG vocabulary keeps toolbar, empty-state and row icons at the
// same visual weight. The toolkit supplies rendering and interaction states.
func nativeIcon(shapes string) *native.SVG {
	return native.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	addIcon      = nativeIcon(`<path d="M12 5v14M5 12h14"/>`)
	gripIcon     = nativeIcon(`<circle cx="9" cy="5" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="19" r="1"/>`)
	settingsIcon = nativeIcon(`<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>`)
	backIcon     = nativeIcon(`<path d="m12 5-7 7 7 7M5 12h14"/>`)
	playIcon     = nativeIcon(`<path d="m8 5 11 7-11 7z"/>`)
	stopIcon     = nativeIcon(`<rect x="6" y="6" width="12" height="12" rx="1"/>`)
	deleteIcon   = nativeIcon(`<path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13M10 11v5M14 11v5"/>`)
	chevronIcon  = nativeIcon(`<path d="m4 8 8 8 8-8"/>`)
	helpIcon     = nativeIcon(`<circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 0 1 5 0c0 2-2.5 2-2.5 4M12 17h.01"/>`)
	clockIcon    = nativeIcon(`<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`)
	trayIcon     = nativeIcon(`<path d="M4 13h4l2 3h4l2-3h4M4 13l2-8h12l2 8v6H4z"/>`)
	toolsIcon    = nativeIcon(`<path d="m14 6 4 4M4 20l8-8M14 3a6 6 0 0 0-6 7L3 15a3 3 0 0 0 4 4l5-5a6 6 0 0 0 8-7l-4 4-3-3 4-4z"/>`)
	// Lucide Languages (ISC); the notice is kept in icons/LICENSE.lucide.
	translateIcon  = nativeIcon(`<path d="m5 8 6 6M4 14l6-6 2-3M2 5h12M7 2h1m14 20-5-10-5 10M14 18h6"/>`)
	programIconSVG = nativeIcon(`<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18M7 6.5h.01M10 6.5h.01"/>`)
)

func nativeAction(c *native.Context, label string, icon *native.SVG) native.Element {
	return native.Button(c, "").Label(label).Children(func() {
		if icon != nil {
			native.Icon(c, icon).Size(16, 16)
		}
		native.Text(c, strings.TrimPrefix(label, "← ")).SingleLine()
	})
}

func nativeHint(c *native.Context, text string) {
	hint := native.Text(c, text).TextColor(c.Theme().TextMuted)
	if _, height := c.Size(); height < 800 {
		// Keep controls on screen at the minimum window height. The complete
		// explanation remains available through the tooltip and accessibility.
		hint.SingleLine().Tooltip(text)
	}
}

// Only the program table scrolls. Page chrome and the inspector have bounded
// layouts, so list navigation never moves the selected program's controls.
func (w *MainWindow) nativeView(c *native.Context) {
	w.desktop.frameNumberCommits = nil
	w.desktop.frameBlurCommits = nil
	w.desktop.exitPressed = false
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
	var dropped []string
	native.Column(c).Fill().Children(func() {
		w.nativeHeader(c, m)
		native.Divider(c)
		if w.onSettingsPage {
			w.nativeSettings(c, m)
		} else {
			dropped = w.nativePrograms(c, m)
		}
		native.Divider(c)
		w.nativeFooter(c, m)
	})
	w.desktop.numberCommits = w.desktop.frameNumberCommits
	// Read the footer's exit gesture before blur saves. Pointer-down moves
	// focus first; do not block its matching click in a save/validation call.
	if !w.desktop.exitPressed && !w.closing.Load() {
		for _, commit := range w.desktop.frameBlurCommits {
			commit()
		}
	}
	if len(dropped) > 0 {
		// Finish rows and editor before changing their identities.
		w.nativeDropPaths(dropped)
	}
}

func (w *MainWindow) nativeHeader(c *native.Context, m i18n.Messages) {
	native.Row(c).Padding(14, 24).Gap(16).Children(func() {
		if w.onSettingsPage {
			if nativeQuietAction(c, m.BackToPrograms, backIcon).Clicked() {
				w.commitNativeEdits()
				w.onSettingsPage = false
			}
			native.Text(c, m.SettingsTitle).FontSize(24).FontWeight(500)
		} else {
			native.Text(c, m.ManagedListTitle).FontSize(24).FontWeight(500)
			native.Text(c, fmt.Sprintf(m.ManagedProgramCount, len(w.settings.ManagedApps))).TextColor(c.Theme().TextMuted)
		}
		native.Spacer(c)
		native.Toolbar(c, func() {
			if !w.onSettingsPage && nativeAction(c, m.OpenSettings, settingsIcon).Clicked() {
				w.commitNativeEdits()
				w.onSettingsPage = true
			}
			if native.Button(c, "").Padding(8).Label(m.LanguageLabel).Tooltip(m.SwitchLanguageHint).Children(func() {
				native.Icon(c, translateIcon).Size(20, 20)
			}).Clicked() {
				w.nativeSwitchLanguage()
			}
		}).Padding(0).Gap(8).Justify(native.End).Label(m.SettingsTitle)
	})
}

// Drop toolkit layout caches when the desktop closes, preserving position.
func (w *MainWindow) resetNativeList(first int) {
	w.desktop.list = native.ListState{
		Selected: &w.desktop.selected,
		Key:      func(i int) any { return w.settings.ManagedApps[i].ID },
		Label:    func(i int) string { return w.settings.ManagedApps[i].Name },
		Reorder: func(rows []int, before int) {
			// Mygo reports an insertion boundary; the order selector uses the
			// final index. This single-selection table always moves one row.
			if len(rows) != 1 {
				return
			}
			to := before
			if rows[0] < to {
				to--
			}
			w.nativeSetOrder(rows[0], to)
		},
	}
	w.desktop.list.ScrollTo(first, native.Start)
}

func (w *MainWindow) nativePrograms(c *native.Context, m i18n.Messages) []string {
	var dropped []string
	if !w.settings.RunAtLogon {
		native.Row(c).Gap(12).Padding(8, 24).Background(c.Theme().Surface).Children(func() {
			native.Text(c, m.LogonOffNotice).Grow(1)
			if native.Button(c, m.LogonOffEnable).Clicked() {
				w.settings.RunAtLogon = true
				w.save()
			}
		})
		native.Divider(c)
	}
	native.Row(c).Grow(1).MinHeight(0).AlignItems(native.Stretch).Children(func() {
		list := native.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(16, 16, 0, 24).Gap(12)
		dropped = list.DroppedFiles()
		if list.FileDragOver() {
			list.Background(desktopSelection(c.Theme()))
		}
		list.Children(func() {
			native.Row(c).Gap(12).Children(func() {
				native.Text(c, m.ManagedListHint).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(m.ManagedListHint).Grow(1).MinWidth(0)
				if nativePrimaryAction(c, m.AddProgram, addIcon).Clicked() {
					w.nativeChooseProgram(false)
				}
			})
			if len(w.settings.ManagedApps) == 0 {
				native.Column(c).Grow(1).Center().Gap(16).Padding(24).Children(func() {
					native.Icon(c, addIcon).Size(40, 40).TextColor(c.Theme().TextMuted)
					native.Text(c, m.ManagedListEmpty).FontSize(22).FontWeight(500)
					native.Text(c, m.ManagedListEmptyHint).TextAlign(native.Center).TextColor(c.Theme().TextMuted)
				})
				return
			}
			w.nativeProgramTable(c, m)
		})
		native.Divider(c)
		// A fixed inspector keeps the established left/right split and leaves
		// numeric controls usable while the name column absorbs width changes.
		padding := float32(16)
		if _, height := c.Size(); height < 800 {
			padding = 12
		}
		native.Column(c).Width(506).Shrink(0).Padding(padding, 20).Background(desktopPanel(c.Theme())).Children(func() { w.nativeEditor(c, m) })
	})
	return dropped
}

func (w *MainWindow) nativeSelected() (*config.ManagedAppEntry, bool) {
	i := w.desktop.selected
	if i < 0 || i >= len(w.settings.ManagedApps) {
		return nil, false
	}
	return &w.settings.ManagedApps[i], true
}

type numberEditor struct {
	value float64
	dirty bool
}

// NumberInput owns parsing, range constraints, keyboard stepping and buttons.
// Defer the settings write until blur/Enter/close, not on every typed digit.
func (w *MainWindow) nativeNumber(c *native.Context, value *int, low, high int, label string) {
	row := native.Row(c).Shrink(0)
	state := native.Local(row, "number", func() numberEditor { return numberEditor{value: float64(*value)} })
	commit := func() {
		if !state.dirty {
			return
		}
		state.dirty = false
		v := int(state.value)
		if v != *value {
			*value = v
			w.save()
		}
	}
	w.desktop.frameNumberCommits = append(w.desktop.frameNumberCommits, commit)
	row.Children(func() {
		input := native.NumberInput(c, &state.value, float64(low), float64(high), 1).Label(label)
		if input.Changed() {
			state.dirty = true
		}
		if state.dirty {
			if input.Shortcut(0, native.KeyEnter) {
				commit()
			} else if !input.FocusWithin() {
				w.desktop.frameBlurCommits = append(w.desktop.frameBlurCommits, commit)
			}
		}
		if !state.dirty && !input.FocusWithin() {
			state.value = float64(*value)
		}
	})
}

func (w *MainWindow) nativeFooter(c *native.Context, m i18n.Messages) {
	native.Row(c).Gap(12).Padding(10, 24).Children(func() {
		native.Text(c, fmt.Sprintf(m.VersionLabel, version.Number)).TextColor(c.Theme().TextMuted)
		native.Toolbar(c, func() {
			if native.Button(c, m.GitHubLink).Clicked() && w.callbacks.OnOpenRepository != nil {
				w.callbacks.OnOpenRepository()
			}
			label := m.CheckUpdate
			if w.checkingUpdate {
				label = m.CheckUpdateBusy
			}
			if native.Button(c, label).Disabled(w.checkingUpdate).Clicked() && w.callbacks.OnCheckUpdate != nil {
				w.checkingUpdate = true
				w.callbacks.OnCheckUpdate()
			}
		}).Padding(0)
		native.Spacer(c)
		if nativeAction(c, m.RunSilently, trayIcon).Clicked() && w.callbacks.OnRunSilently != nil {
			w.commitNativeEdits()
			w.callbacks.OnRunSilently()
		}
		exit := nativeQuietAction(c, m.ExitApp, nil)
		w.desktop.exitPressed = exit.Pressed()
		if exit.Clicked() && w.callbacks.OnExit != nil {
			// The application must arm its exit deadline before saving an edit.
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

// nativeRemove runs only after the confirmation menu has selected an ID and
// the table has finished building. Selection or reordering cannot retarget it.
func (w *MainWindow) nativeRemove(id string) {
	i := slices.IndexFunc(w.settings.ManagedApps, func(entry config.ManagedAppEntry) bool { return entry.ID == id })
	if i < 0 {
		return
	}
	entry := w.settings.ManagedApps[i]
	if entry.CollectTrayIcon && !w.nativeCollect(i, false) {
		return
	}
	w.commitNativeEdits()
	w.settings.ManagedApps = slices.Delete(w.settings.ManagedApps, i, i+1)
	switch {
	case w.desktop.selected == i:
		w.desktop.selected = -1
	case w.desktop.selected > i:
		w.desktop.selected--
	}
	w.desktop.orderOptions = nil
	// Two entries may use the same executable. Keep their shared icon until
	// the last one is removed.
	if !slices.ContainsFunc(w.settings.ManagedApps, func(other config.ManagedAppEntry) bool { return other.ExePath == entry.ExePath }) {
		delete(w.desktop.icons, entry.ExePath)
	}
	w.save()
}

func (w *MainWindow) nativeSwitchLanguage() {
	w.commitNativeEdits()
	if i18n.Resolve(w.settings.Language) == i18n.LangEnUS {
		w.settings.Language = string(i18n.LangZhCN)
	} else {
		w.settings.Language = string(i18n.LangEnUS)
	}
	if w.desktop.window != nil {
		w.desktop.window.SetTitle(i18n.For(w.settings.Language).WindowTitle)
	}
	w.save()
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
	if len(paths) > 0 {
		w.nativeAddPaths(paths, replace)
	}
}

// Dropping files records their paths, never moves or runs them. Directories
// and unsupported types are reported together without rejecting valid files.
func (w *MainWindow) nativeDropPaths(paths []string) {
	var accepted, rejected []string
	for _, path := range paths {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd", ".ps1", ".py", ".pyw":
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				accepted = append(accepted, path)
				continue
			}
		}
		rejected = append(rejected, path)
	}
	w.nativeAddPaths(accepted, false)
	if len(rejected) > 0 {
		m := i18n.For(w.settings.Language)
		w.ShowError(m.AddProgram, fmt.Sprintf(m.ManagedDropRejected, strings.Join(rejected, "\r\n")))
	}
}

func (w *MainWindow) nativeAddPaths(paths []string, replace bool) {
	if len(paths) == 0 {
		return
	}
	w.commitNativeEdits()
	if replace {
		entry, ok := w.nativeSelected()
		if !ok {
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
