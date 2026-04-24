//go:build windows

package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/branding"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/stringutil"
)

type Callbacks struct {
	OnSave           func(config.Settings)
	OnOpenLogs       func()
	OnCleanupRestore func()
	OnExit           func()
}

type MainWindow struct {
	mw             *walk.MainWindow
	allowClose     bool
	settings       config.Settings
	callbacks      Callbacks
	applyingLocale bool
	updatingEditor bool

	managedList      *walk.TableView
	managedListModel *managedListTableModel
	editorTitle      *walk.Label
	noSelectLabel    *walk.Label
	pathLabel        *walk.Label
	pathEdit         *walk.LineEdit
	argsLabel        *walk.Label
	argsEdit         *walk.LineEdit
	browseBtn        *walk.PushButton
	addProgramBtn    *walk.PushButton
	appAutoHide      *walk.CheckBox
	appLaunchHidden  *walk.CheckBox
	appPauseTask     *walk.CheckBox
	retryEdit        *walk.LineEdit
	runAtLogon       *walk.CheckBox
	startHidden      *walk.CheckBox
	exitOnDone       *walk.CheckBox
	retryLabel       *walk.Label
	managedTitle     *walk.Label
	languageLabel    *walk.Label
	languageCombo    *walk.ComboBox
	removeBtn        *walk.PushButton
	openLogsBtn      *walk.PushButton
	cleanupBtn       *walk.PushButton
	exitBtn          *walk.PushButton
}

func NewMainWindow(initial config.Settings, callbacks Callbacks) (*MainWindow, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	if appIcon, iconErr := branding.AppIcon(); iconErr == nil && appIcon != nil {
		if err = mw.SetIcon(appIcon); err != nil {
			return nil, err
		}
	}
	w := &MainWindow{mw: mw, settings: initial, callbacks: callbacks}

	mw.SetSize(walk.Size{Width: 940, Height: 620})
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{HNear: 14, VNear: 14, HFar: 14, VFar: 14})
	layout.SetSpacing(10)
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
	optionsLayout.SetSpacing(12)
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
	settingsLayout.SetSpacing(10)
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
	retryEdit.SetMinMaxSize(walk.Size{Width: 72, Height: 0}, walk.Size{Width: 72, Height: 0})
	retryEdit.SetText(strconv.Itoa(w.settings.CloseWindowRetrySeconds))
	retryEdit.EditingFinished().Attach(func() {
		v, convErr := strconv.Atoi(retryEdit.Text())
		if convErr != nil {
			if i18n.Resolve(w.settings.Language) == i18n.LangEnUS {
				walk.MsgBox(w.mw, w.mw.Title(), "Retry seconds must be a number between 0 and 120.", walk.MsgBoxIconWarning)
			} else {
				walk.MsgBox(w.mw, w.mw.Title(), "重试秒数必须是 0 到 120 的数字。", walk.MsgBoxIconWarning)
			}
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
	languageCombo.SetMinMaxSize(walk.Size{Width: 150, Height: 0}, walk.Size{Width: 150, Height: 0})
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

	list, err := walk.NewTableView(w.mw)
	if err != nil {
		return err
	}
	list.SetMinMaxSize(walk.Size{Width: 860, Height: 320}, walk.Size{})
	list.SetColumnsOrderable(false)
	list.SetHeaderHidden(true)
	list.SetGridlines(false)
	list.SetLastColumnStretched(true)
	list.SetSelectionHiddenWithoutFocus(true)

	nameCol := walk.NewTableViewColumn()
	nameCol.SetTitle("name")
	nameCol.SetWidth(170)
	_ = nameCol.SetAlignment(walk.AlignNear)
	if err = list.Columns().Add(nameCol); err != nil {
		return err
	}
	pathCol := walk.NewTableViewColumn()
	pathCol.SetTitle("path")
	pathCol.SetWidth(510)
	_ = pathCol.SetAlignment(walk.AlignNear)
	if err = list.Columns().Add(pathCol); err != nil {
		return err
	}
	paramCol := walk.NewTableViewColumn()
	paramCol.SetTitle("param")
	paramCol.SetWidth(160)
	_ = paramCol.SetAlignment(walk.AlignNear)
	if err = list.Columns().Add(paramCol); err != nil {
		return err
	}

	model := newManagedListTableModel()
	if err = list.SetModel(model); err != nil {
		return err
	}
	w.managedListModel = model
	list.CurrentIndexChanged().Attach(func() {
		w.syncManagedEditor()
	})
	list.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		if w.tableViewHitOnItem(x, y) {
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
	v.SetSpacing(8)
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
	noSelectLabel.SetMinMaxSize(walk.Size{Width: 860, Height: 22}, walk.Size{Width: 860, Height: 22})
	noSelectLabel.SetAlwaysConsumeSpace(false)
	noSelectLabel.SetVisible(false)
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
	pathLabel.SetMinMaxSize(walk.Size{Width: 96, Height: 0}, walk.Size{Width: 96, Height: 0})

	pathEdit, err := walk.NewLineEdit(pathRow)
	if err != nil {
		return err
	}
	pathEdit.SetReadOnly(true)
	pathEdit.SetMinMaxSize(walk.Size{Width: 520, Height: 0}, walk.Size{Width: 520, Height: 0})
	w.pathEdit = pathEdit

	browseBtn, err := walk.NewPushButton(pathRow)
	if err != nil {
		return err
	}
	browseBtn.Clicked().Attach(w.onSelectProgramForSelected)
	w.browseBtn = browseBtn

	addProgramBtn, err := walk.NewPushButton(pathRow)
	if err != nil {
		return err
	}
	addProgramBtn.Clicked().Attach(w.onAddProgram)
	w.addProgramBtn = addProgramBtn

	argsRow, err := walk.NewComposite(editor)
	if err != nil {
		return err
	}
	hArgs := walk.NewHBoxLayout()
	hArgs.SetSpacing(8)
	if err = argsRow.SetLayout(hArgs); err != nil {
		return err
	}

	argsLabel, err := walk.NewLabel(argsRow)
	if err != nil {
		return err
	}
	w.argsLabel = argsLabel
	argsLabel.SetMinMaxSize(walk.Size{Width: 96, Height: 0}, walk.Size{Width: 96, Height: 0})

	argsEdit, err := walk.NewLineEdit(argsRow)
	if err != nil {
		return err
	}
	argsEdit.SetMinMaxSize(walk.Size{Width: 760, Height: 0}, walk.Size{Width: 760, Height: 0})
	argsEdit.EditingFinished().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, _, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		app.Args = argsEdit.Text()
		w.save()
	})
	w.argsEdit = argsEdit

	optionsRow, err := walk.NewComposite(editor)
	if err != nil {
		return err
	}
	hOpt := walk.NewHBoxLayout()
	hOpt.SetSpacing(12)
	if err = optionsRow.SetLayout(hOpt); err != nil {
		return err
	}

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
		if app.LaunchHiddenInBackground {
			appAutoHide.SetChecked(false)
		}
		app.TrayBehavior.AutoMinimizeAndHideOnLaunch = appAutoHide.Checked()
		w.refreshManagedList()
		w.save()
	})
	w.appAutoHide = appAutoHide

	appLaunchHidden, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	appLaunchHidden.CheckedChanged().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, _, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		checked := appLaunchHidden.Checked()
		app.LaunchHiddenInBackground = checked
		if checked {
			app.TrayBehavior.AutoMinimizeAndHideOnLaunch = false
			w.appAutoHide.SetChecked(false)
			w.appAutoHide.SetEnabled(false)
		} else {
			w.appAutoHide.SetEnabled(true)
		}
		w.refreshManagedList()
		w.save()
	})
	w.appLaunchHidden = appLaunchHidden

	appPauseTask, err := walk.NewCheckBox(optionsRow)
	if err != nil {
		return err
	}
	appPauseTask.CheckedChanged().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, _, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		app.RunOnStartup = !appPauseTask.Checked()
		w.refreshManagedList()
		w.save()
	})
	w.appPauseTask = appPauseTask

	return nil
}

func (w *MainWindow) buildActions() error {
	row, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	h := walk.NewHBoxLayout()
	h.SetSpacing(10)
	if err = row.SetLayout(h); err != nil {
		return err
	}

	removeBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	removeBtn.Clicked().Attach(w.onRemoveSelected)
	removeBtn.SetMinMaxSize(walk.Size{Width: 110, Height: 0}, walk.Size{Width: 110, Height: 0})
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
	openLogsBtn.SetMinMaxSize(walk.Size{Width: 110, Height: 0}, walk.Size{Width: 110, Height: 0})
	w.openLogsBtn = openLogsBtn

	cleanupBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	cleanupBtn.Clicked().Attach(func() {
		if w.callbacks.OnCleanupRestore != nil {
			w.callbacks.OnCleanupRestore()
		}
	})
	cleanupBtn.SetMinMaxSize(walk.Size{Width: 150, Height: 0}, walk.Size{Width: 150, Height: 0})
	w.cleanupBtn = cleanupBtn

	exitBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	exitBtn.Clicked().Attach(func() {
		if w.callbacks.OnExit != nil {
			w.callbacks.OnExit()
		}
	})
	exitBtn.SetMinMaxSize(walk.Size{Width: 110, Height: 0}, walk.Size{Width: 110, Height: 0})
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
	w.argsLabel.SetText(msg.ManagedAppArgs)
	w.browseBtn.SetText(msg.AddProgram)
	w.addProgramBtn.SetText(msg.AddProgram)
	w.appAutoHide.SetText(msg.ManagedAutoHide)
	w.appLaunchHidden.SetText(msg.ManagedLaunchHidden)
	w.appPauseTask.SetText(msg.ManagedPauseTask)
	w.languageLabel.SetText(msg.LanguageLabel)
	w.removeBtn.SetText(msg.RemoveSelected)
	w.openLogsBtn.SetText(msg.OpenLogs)
	w.cleanupBtn.SetText(msg.CleanupRestore)
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
	name := stringutil.TrimExt(filepath.Base(dlg.FilePath))
	if name == "" {
		name = msg.NewAppName
	}
	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	launchHiddenByDefault := shouldDefaultLaunchHidden(dlg.FilePath)
	w.settings.ManagedApps = append(w.settings.ManagedApps, config.ManagedAppEntry{
		ID:           id,
		Name:         name,
		ExePath:      dlg.FilePath,
		Args:         "",
		RunOnStartup: true,
		WindowMatch: config.WindowMatchRule{
			Strategy: config.MatchProcessNameThenTitle,
		},
		LaunchHiddenInBackground: launchHiddenByDefault,
		TrayBehavior:             config.TrayBehavior{AutoMinimizeAndHideOnLaunch: !launchHiddenByDefault},
	})
	w.refreshManagedList()
	w.managedList.SetCurrentIndex(len(w.settings.ManagedApps) - 1)
	w.syncManagedEditor()
	w.save()
}

func (w *MainWindow) onSelectProgramForSelected() {
	app, idx, ok := w.selectedManagedApp()
	if !ok {
		// No item selected — fall back to adding a new entry
		w.onAddProgram()
		return
	}
	msg := i18n.For(w.settings.Language)
	dlg := new(walk.FileDialog)
	dlg.Title = msg.SelectManagedExe
	dlg.Filter = fmt.Sprintf("%s|%s", msg.ExeFilter, msg.AllFilesFilter)
	result, err := dlg.ShowOpen(w.mw)
	if err != nil || !result {
		return
	}
	app.ExePath = dlg.FilePath
	name := stringutil.TrimExt(filepath.Base(dlg.FilePath))
	if name != "" {
		app.Name = name
	}
	if shouldDefaultLaunchHidden(dlg.FilePath) {
		app.LaunchHiddenInBackground = true
		app.TrayBehavior.AutoMinimizeAndHideOnLaunch = false
	}
	w.refreshManagedList()
	w.managedList.SetCurrentIndex(idx)
	w.syncManagedEditor()
	w.save()
}

func shouldDefaultLaunchHidden(path string) bool {
	return strings.ToLower(filepath.Ext(path)) != ".exe"
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
	if w.managedList == nil || w.managedListModel == nil {
		return
	}
	selected := w.managedList.CurrentIndex()
	rows := make([]managedListRow, 0, len(w.settings.ManagedApps))
	for _, app := range w.settings.ManagedApps {
		rows = append(rows, managedListRow{
			Name:  app.Name,
			Path:  app.ExePath,
			Param: i18n.FormatManagedParam(w.settings.Language, app),
		})
	}
	w.managedListModel.SetRows(rows)
	if len(rows) == 0 {
		w.managedList.SetCurrentIndex(-1)
		w.syncManagedEditor()
		return
	}
	if selected >= len(rows) {
		selected = len(rows) - 1
	}
	w.managedList.SetCurrentIndex(selected)
	w.syncManagedEditor()
}

func (w *MainWindow) syncManagedEditor() {
	if w.pathEdit == nil || w.argsEdit == nil || w.appAutoHide == nil || w.appLaunchHidden == nil || w.appPauseTask == nil || w.addProgramBtn == nil {
		return
	}
	app, _, ok := w.selectedManagedApp()
	w.updatingEditor = true
	defer func() { w.updatingEditor = false }()

	msg := i18n.For(w.settings.Language)
	w.pathEdit.SetEnabled(ok)
	w.argsEdit.SetEnabled(ok)
	w.argsEdit.SetReadOnly(!ok)
	w.browseBtn.SetEnabled(true)
	w.addProgramBtn.SetEnabled(true)
	w.appAutoHide.SetEnabled(ok)
	w.appLaunchHidden.SetEnabled(ok)
	w.appPauseTask.SetEnabled(ok)
	w.noSelectLabel.SetVisible(false)
	if ok {
		w.browseBtn.SetText(msg.ModifyProgram)
		w.addProgramBtn.SetText(msg.AddProgram)
		w.addProgramBtn.SetVisible(true)
	} else {
		w.browseBtn.SetText(msg.AddProgram)
		w.addProgramBtn.SetVisible(false)
	}

	if !ok {
		w.pathEdit.SetText("")
		w.argsEdit.SetText("")
		w.appAutoHide.SetChecked(false)
		w.appLaunchHidden.SetChecked(false)
		w.appPauseTask.SetChecked(false)
		return
	}

	w.pathEdit.SetText(app.ExePath)
	w.argsEdit.SetText(app.Args)
	w.appAutoHide.SetChecked(app.TrayBehavior.AutoMinimizeAndHideOnLaunch)
	w.appLaunchHidden.SetChecked(app.LaunchHiddenInBackground)
	w.appPauseTask.SetChecked(!app.RunOnStartup)
	w.appAutoHide.SetEnabled(!app.LaunchHiddenInBackground)
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
		hwnd := w.mw.Handle()
		if hwnd != 0 {
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.ShowWindow(hwnd, win.SW_SHOW)
			win.SetForegroundWindow(hwnd)
		}
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

func (w *MainWindow) clearManagedSelection() {
	if w.managedList == nil {
		return
	}
	_ = w.managedList.SetSelectedIndexes([]int{})
	_ = w.managedList.SetCurrentIndex(-1)
	w.syncManagedEditor()
}

func (w *MainWindow) tableViewHitOnItem(x, y int) bool {
	if w.managedList == nil {
		return false
	}
	hti := win.LVHITTESTINFO{Pt: win.POINT{X: int32(x), Y: int32(y)}}
	w.managedList.SendMessage(win.LVM_HITTEST, 0, uintptr(unsafe.Pointer(&hti)))
	if hti.IItem < 0 {
		return false
	}
	return (hti.Flags & win.LVHT_ONITEM) != 0
}

type managedListRow struct {
	Name  string
	Path  string
	Param string
}

type managedListTableModel struct {
	walk.TableModelBase
	rows []managedListRow
}

func newManagedListTableModel() *managedListTableModel {
	return &managedListTableModel{rows: make([]managedListRow, 0)}
}

func (m *managedListTableModel) RowCount() int {
	return len(m.rows)
}

func (m *managedListTableModel) Value(row, col int) any {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	switch col {
	case 0:
		return m.rows[row].Name
	case 1:
		return m.rows[row].Path
	case 2:
		return m.rows[row].Param
	default:
		return ""
	}
}

func (m *managedListTableModel) SetRows(rows []managedListRow) {
	m.rows = rows
	m.PublishRowsReset()
}
