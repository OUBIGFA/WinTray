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
	"wintray/internal/ui/kit"
	"wintray/internal/ui/kit/icons"
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
	pageTab            int
	detailTab          int
	detailFor          string
	split              kit.SplitState
	removeID           string
}

func nativeAction(c *native.Context, label string, icon *native.SVG) native.Element {
	return kit.Button(c, strings.TrimPrefix(label, "← "), icon, false).Label(label)
}

// Only the program table scrolls. Page chrome and the inspector have bounded
// layouts, so list navigation never moves the selected program's controls.
func (w *MainWindow) nativeView(c *native.Context) {
	c.SetTheme(kit.Theme(c.Preferences(), c.Theme().Dark))
	w.desktop.frameNumberCommits = nil
	w.desktop.frameBlurCommits = nil
	w.desktop.exitPressed = false
	if !w.desktop.initialized {
		w.desktop.initialized = true
		w.desktop.selected = -1
		w.resetNativeList(0)
		w.desktop.split.Percent = 50
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
		dropped = w.nativePrograms(c, m)
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
	if id := w.desktop.removeID; id != "" {
		w.desktop.removeID = ""
		w.nativeRemove(id)
	}
}

func (w *MainWindow) nativeHeader(c *native.Context, m i18n.Messages) {
	native.Row(c).Padding(12, 24).Gap(16).Children(func() {
		kit.Title(c, m.ManagedListTitle)
		kit.Caption(c, fmt.Sprintf(m.ManagedProgramCount, len(w.settings.ManagedApps)))
		native.Spacer(c)
		native.Toolbar(c, func() {
			if native.Button(c, "").Padding(8).Label(m.LanguageLabel).Tooltip(m.SwitchLanguageHint).Children(func() {
				native.Icon(c, icons.Languages).Size(20, 20)
			}).Clicked() {
				w.nativeSwitchLanguage()
			}
		}).Padding(0).Gap(8).Justify(native.End)
	})
}

// Drop toolkit layout caches when the desktop closes, preserving position.
func (w *MainWindow) resetNativeList(first int) {
	w.desktop.list = native.ListState{
		Selected: &w.desktop.selected,
		Key:      func(i int) any { return w.settings.ManagedApps[i].ID },
		Label: func(i int) string {
			return i18n.For(w.settings.Language).ProgramsTab + ": " + w.settings.ManagedApps[i].Name
		},
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

// Only the program list scrolls. Page tabs, Add and the application footer
// stay fixed.
func (w *MainWindow) nativePrograms(c *native.Context, m i18n.Messages) []string {
	var dropped []string
	native.Column(c).Grow(1).MinHeight(0).Padding(0, 24, 16, 24).Gap(16).Children(func() {
		kit.Tabs(c, &w.desktop.pageTab, []kit.Tab{{Label: m.ProgramsTab, Icon: icons.List}, {Label: m.SettingsTab, Icon: icons.Settings2}}, func(apply func()) { w.commitNativeEdits(); apply() })
		if w.desktop.pageTab == 1 {
			w.nativeSettings(c, m)
			return
		}
		native.Row(c).Gap(16).Children(func() {
			if nativePrimaryAction(c, m.AddProgram, icons.Plus).Clicked() {
				w.nativeChooseProgram(false)
			}
			if !w.settings.RunAtLogon {
				kit.Help(c, m.LogonOffNotice).Grow(1)
				if kit.Button(c, m.LogonOffEnable, nil, false).Label(m.LogonOffEnable).Clicked() {
					w.settings.RunAtLogon = true
					w.save()
				}
			} else {
				kit.Help(c, m.ManagedListHint).SingleLine().MinWidth(0).Tooltip(m.ManagedListHint)
			}
		})
		area := native.Column(c).Grow(1).MinHeight(0).BorderWidth(1, 0, 0, 0).BorderColor(kit.Colors(c).Hairline)
		area.Children(func() {
			width, _ := c.Size()
			if width < 908 {
				native.Column(c).FillWidth().Height(180).Shrink(0).Children(func() { dropped = w.nativeProgramTable(c, m) })
				native.Column(c).Grow(1).MinHeight(0).BorderWidth(1, 0, 0, 0).BorderColor(kit.Colors(c).Hairline).Children(func() { w.nativeEditor(c, m) })
			} else {
				// Let startup choices wrap while retaining room for the paired
				// schedule fields when the user narrows the editor.
				kit.Split(c, &w.desktop.split, kit.SplitOptions{Label: m.ResizePanels, MinFirst: 35, MinSecond: max(30, 560/(width-48)*100)}, func() { dropped = w.nativeProgramTable(c, m) }, func() { w.nativeEditor(c, m) }).Grow(1).MinHeight(0)
			}
		})
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
	text  string
	dirty bool
}

// Use the shared 120-DIP text field and arrow-key stepping. Only valid whole
// numbers commit on Enter, blur or close; an invalid entry stays visible.
func (w *MainWindow) nativeNumber(c *native.Context, value *int, low, high int, label string, handles ...*native.Handle) native.Element {
	box := native.Column(c).Width(120).Shrink(0).Gap(4)
	state := native.Local(box, "number", func() numberEditor { return numberEditor{text: strconv.Itoa(*value)} })
	parse := func() (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(state.text))
		return n, err == nil && n >= low && n <= high
	}
	commit := func() {
		if !state.dirty {
			return
		}
		if n, ok := parse(); ok {
			state.dirty = false
			if n != *value {
				*value = n
				w.save()
			}
		}
	}
	w.desktop.frameNumberCommits = append(w.desktop.frameNumberCommits, commit)
	box.Children(func() {
		input := native.TextInput(c, &state.text).FillWidth().Label(label)
		if len(handles) > 0 {
			input.Bind(handles[0])
		}
		if input.Changed() {
			state.dirty = true
		}
		step := func(delta int) {
			n, ok := parse()
			if !ok {
				n = *value
			}
			state.text = strconv.Itoa(min(high, max(low, n+delta)))
			state.dirty = true
		}
		if input.Shortcut(0, native.KeyUp) {
			step(1)
		}
		if input.Shortcut(0, native.KeyDown) {
			step(-1)
		}
		if _, ok := parse(); !ok {
			input.Error(fmt.Sprintf("%d–%d", low, high))
		}
		if state.dirty {
			if input.Submitted() {
				commit()
			} else if !input.Focused() {
				w.desktop.frameBlurCommits = append(w.desktop.frameBlurCommits, commit)
			}
		}
		if !state.dirty && !input.Focused() {
			state.text = strconv.Itoa(*value)
		}
	})
	return box
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
		if nativeAction(c, m.RunSilently, icons.PanelTopClose).Clicked() && w.callbacks.OnRunSilently != nil {
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
		{Name: m.SelectManagedExe, Extensions: []string{"exe", "bat", "cmd", "ps1", "py", "pyw", "ahk"}}, {Name: "*.*", Extensions: []string{"*"}},
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
		case ".exe", ".bat", ".cmd", ".ps1", ".py", ".pyw", ".ahk":
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

// A file dropped on Change replaces only that row and never launches it.
func (w *MainWindow) nativeReplacePaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	path := paths[0]
	supported := false
	switch strings.ToLower(filepath.Ext(path)) {
	case ".exe", ".bat", ".cmd", ".ps1", ".py", ".pyw", ".ahk":
		info, err := os.Stat(path)
		supported = err == nil && info.Mode().IsRegular()
	}
	if !supported {
		m := i18n.For(w.settings.Language)
		w.ShowError(m.BrowseProgram, fmt.Sprintf(m.ManagedDropRejected, path))
		return
	}
	w.nativeAddPaths(paths[:1], true)
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
