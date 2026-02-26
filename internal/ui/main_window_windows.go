//go:build windows

package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

type Callbacks struct {
	OnSave     func(config.Settings)
	OnOpenLogs func()
	OnExit     func()
}

type MainWindow struct {
	mw             *walk.MainWindow
	allowClose     bool
	settings       config.Settings
	callbacks      Callbacks
	applyingLocale bool
	updatingEditor bool

	managedList   *walk.ListBox
	editorTitle   *walk.Label
	noSelectLabel *walk.Label
	pathLabel     *walk.Label
	pathEdit      *walk.LineEdit
	browseBtn     *walk.PushButton
	appRunOnStart *walk.CheckBox
	appAutoHide   *walk.CheckBox
	retryEdit     *walk.LineEdit
	runAtLogon    *walk.CheckBox
	startHidden   *walk.CheckBox
	exitOnDone    *walk.CheckBox
	retryLabel    *walk.Label
	managedTitle  *walk.Label
	languageLabel *walk.Label
	languageCombo *walk.ComboBox
	removeBtn     *walk.PushButton
	openLogsBtn   *walk.PushButton
	exitBtn       *walk.PushButton
}

func NewMainWindow(initial config.Settings, callbacks Callbacks) (*MainWindow, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	w := &MainWindow{mw: mw, settings: initial, callbacks: callbacks}

	mw.SetSize(walk.Size{Width: 920, Height: 600})
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{HNear: 12, VNear: 12, HFar: 12, VFar: 12})
	layout.SetSpacing(8)
	if err = mw.SetLayout(layout); err != nil {
		return nil, err
	}

	if err = w.buildTopOptions(); err != nil {
		return nil, err
	}
	if err = w.buildManagedEditor(); err != nil {
		return nil, err
	}
	if err = w.buildManagedList(); err != nil {
		return nil, err
	}
	if err = w.buildActions(); err != nil {
		return nil, err
	}

	w.applyLanguage(w.settings.Language)
	w.refreshManagedList()

	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !w.allowClose {
			*canceled = true
			w.mw.Hide()
			w.mw.SetVisible(false)
		}
	})

	mw.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		if w.managedList == nil {
			return
		}
		bounds := w.managedList.Bounds()
		if x >= bounds.X && x < bounds.X+bounds.Width && y >= bounds.Y && y < bounds.Y+bounds.Height {
			return
		}
		w.clearManagedSelection()
	})

	return w, nil
}

func (w *MainWindow) buildTopOptions() error {
	optionsRow, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	optionsLayout := walk.NewHBoxLayout()
	optionsLayout.SetSpacing(16)
	if err = optionsRow.SetLayout(optionsLayout); err != nil {
		return err
	}

	if _, err = walk.NewHSpacer(optionsRow); err != nil {
		return err
	}

	runAtLogon, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	runAtLogon.SetChecked(w.settings.RunAtLogon)
	runAtLogon.CheckedChanged().Attach(func() {
		w.settings.RunAtLogon = runAtLogon.Checked()
		w.save()
	})
	w.runAtLogon = runAtLogon

	startHidden, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	startHidden.SetChecked(w.settings.StartMinimizedToTray)
	startHidden.CheckedChanged().Attach(func() {
		w.settings.StartMinimizedToTray = startHidden.Checked()
		w.save()
	})
	w.startHidden = startHidden

	exitOnDone, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	exitOnDone.SetChecked(w.settings.ExitAfterManagedAppsCompleted)
	exitOnDone.CheckedChanged().Attach(func() {
		w.settings.ExitAfterManagedAppsCompleted = exitOnDone.Checked()
		w.save()
	})
	w.exitOnDone = exitOnDone

	if _, err = walk.NewHSpacer(optionsRow); err != nil {
		return err
	}

	settingsRow, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	settingsLayout := walk.NewHBoxLayout()
	settingsLayout.SetSpacing(8)
	if err = settingsRow.SetLayout(settingsLayout); err != nil {
		return err
	}

	retryLabel, err := walk.NewLabel(settingsRow)
	if err != nil {
		return err
	}
	w.retryLabel = retryLabel

	retryEdit, err := walk.NewLineEdit(settingsRow)
	if err != nil {
		return err
	}
	retryEdit.SetMinMaxSize(walk.Size{Width: 64, Height: 0}, walk.Size{Width: 64, Height: 0})
	retryEdit.SetText(strconv.Itoa(w.settings.CloseWindowRetrySeconds))
	retryEdit.EditingFinished().Attach(func() {
		v, convErr := strconv.Atoi(retryEdit.Text())
		if convErr != nil {
			v = w.settings.CloseWindowRetrySeconds
		}
		if v < 0 {
			v = 0
		}
		if v > 120 {
			v = 120
		}
		w.settings.CloseWindowRetrySeconds = v
		retryEdit.SetText(strconv.Itoa(v))
		w.save()
	})
	w.retryEdit = retryEdit

	if _, err = walk.NewHSpacer(settingsRow); err != nil {
		return err
	}

	languageLabel, err := walk.NewLabel(settingsRow)
	if err != nil {
		return err
	}
	w.languageLabel = languageLabel

	languageCombo, err := walk.NewComboBox(settingsRow)
	if err != nil {
		return err
	}
	languageCombo.SetMinMaxSize(walk.Size{Width: 140, Height: 0}, walk.Size{Width: 140, Height: 0})
	_ = languageCombo.SetModel([]string{i18n.For("zh-CN").LanguageZhLabel, i18n.For("zh-CN").LanguageEnLabel})
	languageCombo.CurrentIndexChanged().Attach(func() {
		if w.applyingLocale {
			return
		}
		idx := languageCombo.CurrentIndex()
		if idx == 1 {
			w.settings.Language = string(i18n.LangEnUS)
		} else {
			w.settings.Language = string(i18n.LangZhCN)
		}
		w.applyLanguage(w.settings.Language)
		w.refreshManagedList()
		w.save()
	})
	w.languageCombo = languageCombo

	return nil
}

func (w *MainWindow) buildManagedList() error {
	title, err := walk.NewLabel(w.mw)
	if err != nil {
		return err
	}
	w.managedTitle = title

	list, err := walk.NewListBox(w.mw)
	if err != nil {
		return err
	}
	list.SetMinMaxSize(walk.Size{Width: 860, Height: 340}, walk.Size{})
	list.CurrentIndexChanged().Attach(func() {
		w.syncManagedEditor()
	})
	list.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		if w.listClickHitsItem(x, y) {
			return
		}
		w.clearManagedSelection()
	})
	w.managedList = list

	return nil
}

func (w *MainWindow) buildManagedEditor() error {
	editor, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	v := walk.NewVBoxLayout()
	v.SetSpacing(6)
	if err = editor.SetLayout(v); err != nil {
		return err
	}

	editorTitleRow, err := walk.NewComposite(editor)
	if err != nil {
		return err
	}
	hTitle := walk.NewHBoxLayout()
	hTitle.SetSpacing(0)
	if err = editorTitleRow.SetLayout(hTitle); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(editorTitleRow); err != nil {
		return err
	}

	editorTitle, err := walk.NewLabel(editorTitleRow)
	if err != nil {
		return err
	}
	editorTitle.SetTextAlignment(walk.AlignCenter)
	editorTitle.SetMinMaxSize(walk.Size{Width: 240, Height: 0}, walk.Size{Width: 240, Height: 0})
	w.editorTitle = editorTitle

	if _, err = walk.NewHSpacer(editorTitleRow); err != nil {
		return err
	}

	noSelectLabel, err := walk.NewLabel(editor)
	if err != nil {
		return err
	}
	w.noSelectLabel = noSelectLabel

	pathRow, err := walk.NewComposite(editor)
	if err != nil {
		return err
	}
	hPath := walk.NewHBoxLayout()
	hPath.SetSpacing(8)
	if err = pathRow.SetLayout(hPath); err != nil {
		return err
	}

	pathLabel, err := walk.NewLabel(pathRow)
	if err != nil {
		return err
	}
	w.pathLabel = pathLabel

	pathEdit, err := walk.NewLineEdit(pathRow)
	if err != nil {
		return err
	}
	pathEdit.SetReadOnly(true)
	pathEdit.SetMinMaxSize(walk.Size{Width: 620, Height: 0}, walk.Size{Width: 620, Height: 0})
	w.pathEdit = pathEdit

	browseBtn, err := walk.NewPushButton(pathRow)
	if err != nil {
		return err
	}
	browseBtn.Clicked().Attach(w.onSelectProgramForSelected)
	w.browseBtn = browseBtn

	optionsRow, err := walk.NewComposite(editor)
	if err != nil {
		return err
	}
	hOpt := walk.NewHBoxLayout()
	hOpt.SetSpacing(16)
	if err = optionsRow.SetLayout(hOpt); err != nil {
		return err
	}

	appRunOnStart, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	appRunOnStart.CheckedChanged().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, _, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		app.RunOnStartup = appRunOnStart.Checked()
		w.refreshManagedList()
		w.save()
	})
	w.appRunOnStart = appRunOnStart

	appAutoHide, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	appAutoHide.CheckedChanged().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, _, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		app.TrayBehavior.AutoMinimizeAndHideOnLaunch = appAutoHide.Checked()
		w.refreshManagedList()
		w.save()
	})
	w.appAutoHide = appAutoHide

	return nil
}

func (w *MainWindow) buildActions() error {
	row, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	h := walk.NewHBoxLayout()
	h.SetSpacing(8)
	if err = row.SetLayout(h); err != nil {
		return err
	}

	removeBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	removeBtn.Clicked().Attach(w.onRemoveSelected)
	w.removeBtn = removeBtn

	openLogsBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	openLogsBtn.Clicked().Attach(func() {
		if w.callbacks.OnOpenLogs != nil {
			w.callbacks.OnOpenLogs()
		}
	})
	w.openLogsBtn = openLogsBtn

	exitBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	exitBtn.Clicked().Attach(func() {
		if w.callbacks.OnExit != nil {
			w.callbacks.OnExit()
		}
	})
	w.exitBtn = exitBtn

	return nil
}

func (w *MainWindow) applyLanguage(language string) {
	msg := i18n.For(language)
	w.settings.Language = string(i18n.Resolve(language))
	w.applyingLocale = true
	defer func() { w.applyingLocale = false }()

	w.mw.SetTitle(msg.WindowTitle)
	w.runAtLogon.SetText(msg.RunAtLogon)
	w.startHidden.SetText(msg.StartHidden)
	w.exitOnDone.SetText(msg.ExitOnDone)
	w.retryLabel.SetText(msg.RetrySeconds)
	w.managedTitle.SetText(msg.ManagedListTitle)
	w.editorTitle.SetText(msg.ManagedEditorTitle)
	w.pathLabel.SetText(msg.ManagedAppPath)
	w.browseBtn.SetText(msg.SelectProgram)
	w.appRunOnStart.SetText(msg.ManagedRunOnStartup)
	w.appAutoHide.SetText(msg.ManagedAutoHide)
	w.noSelectLabel.SetText(msg.ManagedNoSelectionHint)
	w.languageLabel.SetText(msg.LanguageLabel)
	w.removeBtn.SetText(msg.RemoveSelected)
	w.openLogsBtn.SetText(msg.OpenLogs)
	w.exitBtn.SetText(msg.ExitApp)
	_ = w.languageCombo.SetModel([]string{msg.LanguageZhLabel, msg.LanguageEnLabel})
	if w.settings.Language == string(i18n.LangEnUS) {
		w.languageCombo.SetCurrentIndex(1)
	} else {
		w.languageCombo.SetCurrentIndex(0)
	}
	w.syncManagedEditor()
}

func (w *MainWindow) SetLanguage(language string) {
	w.mw.Synchronize(func() {
		w.applyLanguage(language)
		w.refreshManagedList()
	})
}

func (w *MainWindow) onAddProgram() {
	msg := i18n.For(w.settings.Language)
	dlg := new(walk.FileDialog)
	dlg.Title = msg.SelectManagedExe
	dlg.Filter = fmt.Sprintf("%s|%s", msg.ExeFilter, msg.AllFilesFilter)
	ok, err := dlg.ShowOpen(w.mw)
	if err != nil || !ok {
		return
	}
	name := trimExt(filepath.Base(dlg.FilePath))
	if name == "" {
		name = msg.NewAppName
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	w.settings.ManagedApps = append(w.settings.ManagedApps, config.ManagedAppEntry{
		ID:           id,
		Name:         name,
		ExePath:      dlg.FilePath,
		RunOnStartup: true,
		WindowMatch: config.WindowMatchRule{
			Strategy: config.MatchProcessNameThenTitle,
		},
		TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true},
	})
	w.refreshManagedList()
	w.managedList.SetCurrentIndex(len(w.settings.ManagedApps) - 1)
	w.syncManagedEditor()
	w.save()
}

func (w *MainWindow) onSelectProgramForSelected() {
	w.onAddProgram()
}

func (w *MainWindow) onRemoveSelected() {
	idx := w.managedList.CurrentIndex()
	if idx < 0 || idx >= len(w.settings.ManagedApps) {
		return
	}
	w.settings.ManagedApps = append(w.settings.ManagedApps[:idx], w.settings.ManagedApps[idx+1:]...)
	w.refreshManagedList()
	w.syncManagedEditor()
	w.save()
}

func (w *MainWindow) refreshManagedList() {
	selected := w.managedList.CurrentIndex()
	items := make([]string, 0, len(w.settings.ManagedApps))
	for _, app := range w.settings.ManagedApps {
		items = append(items, i18n.FormatManagedListItem(w.settings.Language, app))
	}
	_ = w.managedList.SetModel(items)
	if len(items) == 0 {
		w.managedList.SetCurrentIndex(-1)
		w.syncManagedEditor()
		return
	}
	if selected < 0 || selected >= len(items) {
		selected = 0
	}
	w.managedList.SetCurrentIndex(selected)
	w.syncManagedEditor()
}

func (w *MainWindow) syncManagedEditor() {
	if w.pathEdit == nil || w.appRunOnStart == nil || w.appAutoHide == nil {
		return
	}
	app, _, ok := w.selectedManagedApp()
	w.updatingEditor = true
	defer func() { w.updatingEditor = false }()

	w.pathEdit.SetEnabled(ok)
	w.browseBtn.SetEnabled(true)
	w.appRunOnStart.SetEnabled(ok)
	w.appAutoHide.SetEnabled(ok)
	w.noSelectLabel.SetVisible(!ok)

	if !ok {
		w.pathEdit.SetText("")
		w.appRunOnStart.SetChecked(false)
		w.appAutoHide.SetChecked(false)
		return
	}

	w.pathEdit.SetText(app.ExePath)
	w.appRunOnStart.SetChecked(app.RunOnStartup)
	w.appAutoHide.SetChecked(app.TrayBehavior.AutoMinimizeAndHideOnLaunch)
}

func (w *MainWindow) selectedManagedApp() (*config.ManagedAppEntry, int, bool) {
	idx := w.managedList.CurrentIndex()
	if idx < 0 || idx >= len(w.settings.ManagedApps) {
		return nil, -1, false
	}
	return &w.settings.ManagedApps[idx], idx, true
}

func (w *MainWindow) ShowInfo(title, body string) {
	w.mw.Synchronize(func() {
		walk.MsgBox(w.mw, title, body, walk.MsgBoxIconInformation)
	})
}

func (w *MainWindow) ShowError(title, body string) {
	w.mw.Synchronize(func() {
		walk.MsgBox(w.mw, title, body, walk.MsgBoxIconError)
	})
}

func (w *MainWindow) save() {
	if w.callbacks.OnSave != nil {
		w.callbacks.OnSave(w.settings)
	}
}

func (w *MainWindow) ShowMainWindow() {
	w.mw.Synchronize(func() {
		w.mw.Show()
		w.mw.SetVisible(true)
		w.mw.SetFocus()
	})
}

func (w *MainWindow) HideMainWindow() {
	w.mw.Hide()
}

func (w *MainWindow) Run() int {
	return w.mw.Run()
}

func (w *MainWindow) RequestExplicitClose() {
	if w == nil || w.mw == nil {
		return
	}
	w.mw.Synchronize(func() {
		if w.mw.IsDisposed() {
			return
		}
		w.allowClose = true
		w.mw.Close()
		if app := walk.App(); app != nil {
			app.Exit(0)
		}
	})
}

func (w *MainWindow) Native() *walk.MainWindow {
	return w.mw
}

func (w *MainWindow) Settings() config.Settings {
	return w.settings
}

func trimExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)]
}

func (w *MainWindow) clearManagedSelection() {
	if w.managedList == nil {
		return
	}
	if w.managedList.CurrentIndex() == -1 {
		return
	}
	w.managedList.SetCurrentIndex(-1)
	w.syncManagedEditor()
}

func (w *MainWindow) listClickHitsItem(x, y int) bool {
	if w.managedList == nil {
		return false
	}
	if len(w.settings.ManagedApps) == 0 {
		return false
	}
	point := win.MAKELONG(uint16(x), uint16(y))
	result := w.managedList.SendMessage(win.LB_ITEMFROMPOINT, 0, uintptr(point))
	outside := ((result >> 16) & 0xFFFF) != 0
	if outside {
		return false
	}
	index := int(result & 0xFFFF)
	if index < 0 || index >= len(w.settings.ManagedApps) {
		return false
	}
	var rect win.RECT
	ret := w.managedList.SendMessage(win.LB_GETITEMRECT, uintptr(index), uintptr(unsafe.Pointer(&rect)))
	if int32(ret) == win.LB_ERR {
		return false
	}
	if y < int(rect.Top) || y >= int(rect.Bottom) {
		return false
	}
	return true
}
