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
	"wintray/internal/version"
)

type Callbacks struct {
	OnSave           func(config.Settings)
	OnOpenLogs       func()
	OnCleanupRestore func()
	OnLaunchNow      func(config.ManagedAppEntry)
	OnCheckUpdate    func()
	OnOpenRepository func()
	OnExit           func()
}

type MainWindow struct {
	mw             *walk.MainWindow
	allowClose     bool
	settings       config.Settings
	callbacks      Callbacks
	applyingLocale bool
	updatingEditor bool
	launchNowBusy  bool
	checkingUpdate bool

	globalTitle *walk.Label

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
	launchNowBtn     *walk.PushButton
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
	versionLabel     *walk.Label
	checkUpdateBtn   *walk.PushButton
	githubLink       *walk.ImageView
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

	mw.SetSize(walk.Size{Width: 980, Height: 680})
	if font, fontErr := walk.NewFont("Segoe UI", 9, 0); fontErr == nil {
		mw.SetFont(font)
	}
	if bg, bgErr := walk.NewSolidColorBrush(walk.RGB(248, 249, 251)); bgErr == nil {
		mw.SetBackground(bg)
	}
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{HNear: 24, VNear: 20, HFar: 24, VFar: 20})
	layout.SetSpacing(22)
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
	if err = w.buildFooter(); err != nil {
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
	options, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	optionsLayout := walk.NewVBoxLayout()
	optionsLayout.SetMargins(walk.Margins{})
	optionsLayout.SetSpacing(8)
	if err = options.SetLayout(optionsLayout); err != nil {
		return err
	}

	globalTitle, err := walk.NewLabel(options)
	if err != nil {
		return err
	}
	globalTitle.SetTextColor(walk.RGB(38, 45, 58))
	if font, fontErr := walk.NewFont("Segoe UI", 10, walk.FontBold); fontErr == nil {
		globalTitle.SetFont(font)
	}
	w.globalTitle = globalTitle

	optionsRow, err := walk.NewComposite(options)
	if err != nil {
		return err
	}
	optionsRowLayout := walk.NewHBoxLayout()
	optionsRowLayout.SetMargins(walk.Margins{})
	optionsRowLayout.SetSpacing(18)
	if err = optionsRow.SetLayout(optionsRowLayout); err != nil {
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

	settingsRow, err := walk.NewComposite(options)
	if err != nil {
		return err
	}
	settingsLayout := walk.NewHBoxLayout()
	settingsLayout.SetMargins(walk.Margins{})
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
	title.SetTextColor(walk.RGB(38, 45, 58))
	if font, fontErr := walk.NewFont("Segoe UI", 10, walk.FontBold); fontErr == nil {
		title.SetFont(font)
	}
	w.managedTitle = title

	list, err := walk.NewTableView(w.mw)
	if err != nil {
		return err
	}
	list.SetMinMaxSize(walk.Size{Width: 880, Height: 300}, walk.Size{})
	list.SetColumnsOrderable(false)
	list.SetHeaderHidden(false)
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
	pathCol.SetWidth(540)
	_ = pathCol.SetAlignment(walk.AlignNear)
	if err = list.Columns().Add(pathCol); err != nil {
		return err
	}
	paramCol := walk.NewTableViewColumn()
	paramCol.SetTitle("param")
	paramCol.SetWidth(150)
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
	v.SetMargins(walk.Margins{})
	v.SetSpacing(10)
	if err = editor.SetLayout(v); err != nil {
		return err
	}

	editorTitle, err := walk.NewLabel(editor)
	if err != nil {
		return err
	}
	editorTitle.SetTextAlignment(walk.AlignNear)
	editorTitle.SetTextColor(walk.RGB(38, 45, 58))
	if font, fontErr := walk.NewFont("Segoe UI", 10, walk.FontBold); fontErr == nil {
		editorTitle.SetFont(font)
	}
	w.editorTitle = editorTitle

	noSelectLabel, err := walk.NewLabel(editor)
	if err != nil {
		return err
	}
	noSelectLabel.SetMinMaxSize(walk.Size{Width: 860, Height: 22}, walk.Size{Width: 860, Height: 22})
	noSelectLabel.SetAlwaysConsumeSpace(false)
	noSelectLabel.SetVisible(false)
	noSelectLabel.SetTextColor(walk.RGB(84, 93, 108))
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
	pathEdit.SetMinMaxSize(walk.Size{Width: 560, Height: 0}, walk.Size{})
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
	argsEdit.SetMinMaxSize(walk.Size{Width: 760, Height: 0}, walk.Size{})
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
	hOpt.SetMargins(walk.Margins{})
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

	launchNowBtn, err := walk.NewPushButton(optionsRow)
	if err != nil {
		return err
	}
	launchNowBtn.Clicked().Attach(w.onLaunchNow)
	w.launchNowBtn = launchNowBtn

	return nil
}

func (w *MainWindow) onLaunchNow() {
	app, _, ok := w.selectedManagedApp()
	if !ok || w.launchNowBusy || w.callbacks.OnLaunchNow == nil {
		return
	}
	w.setLaunchNowBusy(true)
	w.callbacks.OnLaunchNow(*app)
}

// SetLaunchNowBusy reflects an in-flight launch on the button so the user can
// tell the click was accepted and does not keep clicking it.
func (w *MainWindow) SetLaunchNowBusy(busy bool) {
	w.synchronize(func() { w.setLaunchNowBusy(busy) })
}

func (w *MainWindow) setLaunchNowBusy(busy bool) {
	if w.launchNowBtn == nil || w.managedList == nil {
		return
	}
	w.launchNowBusy = busy
	msg := i18n.For(w.settings.Language)
	if busy {
		w.launchNowBtn.SetText(msg.ManagedLaunchNowBusy)
	} else {
		w.launchNowBtn.SetText(msg.ManagedLaunchNow)
	}
	_, _, ok := w.selectedManagedApp()
	w.launchNowBtn.SetEnabled(ok && !busy)
}

func (w *MainWindow) buildActions() error {
	row, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	h := walk.NewHBoxLayout()
	h.SetMargins(walk.Margins{})
	h.SetSpacing(8)
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

	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}

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

func (w *MainWindow) buildFooter() error {
	row, err := walk.NewComposite(w.mw)
	if err != nil {
		return err
	}
	h := walk.NewHBoxLayout()
	h.SetMargins(walk.Margins{})
	h.SetSpacing(8)
	if err = row.SetLayout(h); err != nil {
		return err
	}

	versionLabel, err := walk.NewLabel(row)
	if err != nil {
		return err
	}
	versionLabel.SetTextColor(walk.RGB(84, 93, 108))
	w.versionLabel = versionLabel

	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}

	checkUpdateBtn, err := walk.NewPushButton(row)
	if err != nil {
		return err
	}
	checkUpdateBtn.Clicked().Attach(w.onCheckUpdate)
	checkUpdateBtn.SetMinMaxSize(walk.Size{Width: 110, Height: 0}, walk.Size{Width: 110, Height: 0})
	w.checkUpdateBtn = checkUpdateBtn

	githubLink, err := walk.NewImageView(row)
	if err != nil {
		return err
	}
	githubIcon, err := branding.GitHubIcon()
	if err != nil {
		return err
	}
	githubLink.SetMode(walk.ImageViewModeIdeal)
	if err = githubLink.SetImage(githubIcon); err != nil {
		return err
	}
	githubLink.SetCursor(walk.CursorHand())
	githubLink.MouseUp().Attach(func(_, _ int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		if w.callbacks.OnOpenRepository != nil {
			w.callbacks.OnOpenRepository()
		}
	})
	githubLink.SetMinMaxSize(walk.Size{Width: 24, Height: 24}, walk.Size{Width: 24, Height: 24})
	w.githubLink = githubLink

	return nil
}

func (w *MainWindow) onCheckUpdate() {
	if w.checkingUpdate || w.callbacks.OnCheckUpdate == nil {
		return
	}
	w.setCheckUpdateBusy(true)
	w.callbacks.OnCheckUpdate()
}

// SetCheckUpdateBusy reflects an in-flight update check on the button.
func (w *MainWindow) SetCheckUpdateBusy(busy bool) {
	w.synchronize(func() { w.setCheckUpdateBusy(busy) })
}

func (w *MainWindow) setCheckUpdateBusy(busy bool) {
	if w.checkUpdateBtn == nil {
		return
	}
	w.checkingUpdate = busy
	msg := i18n.For(w.settings.Language)
	if busy {
		w.checkUpdateBtn.SetText(msg.CheckUpdateBusy)
	} else {
		w.checkUpdateBtn.SetText(msg.CheckUpdate)
	}
	w.checkUpdateBtn.SetEnabled(!busy)
}

func (w *MainWindow) applyLanguage(language string) {
	msg := i18n.For(language)
	w.settings.Language = string(i18n.Resolve(language))
	w.applyingLocale = true
	defer func() { w.applyingLocale = false }()

	w.mw.SetTitle(msg.WindowTitle)
	w.globalTitle.SetText(msg.GlobalSettingsTitle)
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
	w.setLaunchNowBusy(w.launchNowBusy)
	w.languageLabel.SetText(msg.LanguageLabel)
	w.removeBtn.SetText(msg.RemoveSelected)
	w.openLogsBtn.SetText(msg.OpenLogs)
	w.cleanupBtn.SetText(msg.CleanupRestore)
	w.exitBtn.SetText(msg.ExitApp)
	w.versionLabel.SetText(fmt.Sprintf(msg.VersionLabel, version.Number))
	w.githubLink.SetToolTipText(msg.GitHubTooltip)
	w.setCheckUpdateBusy(w.checkingUpdate)
	_ = w.languageCombo.SetModel([]string{msg.LanguageZhLabel, msg.LanguageEnLabel})
	if w.settings.Language == string(i18n.LangEnUS) {
		w.languageCombo.SetCurrentIndex(1)
	} else {
		w.languageCombo.SetCurrentIndex(0)
	}
	if w.managedList != nil {
		w.managedList.Columns().At(0).SetTitle(msg.ManagedColumnName)
		w.managedList.Columns().At(1).SetTitle(msg.ManagedColumnPath)
		w.managedList.Columns().At(2).SetTitle(msg.ManagedColumnRule)
	}
	w.syncManagedEditor()
}

func (w *MainWindow) SetLanguage(language string) {
	w.synchronize(func() {
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
		// No item selected; fall back to adding a new entry.
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
	if w.pathEdit == nil || w.argsEdit == nil || w.appAutoHide == nil || w.appLaunchHidden == nil || w.appPauseTask == nil || w.launchNowBtn == nil || w.addProgramBtn == nil {
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
	w.launchNowBtn.SetEnabled(ok && !w.launchNowBusy)
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

// synchronize queues f on the UI thread and wakes the message loop. walk only
// drains its queue after it has dispatched a window message, so an idle window
// would otherwise hold background results (status updates, dialogs) until the
// user happened to touch the UI again.
func (w *MainWindow) synchronize(f func()) {
	w.mw.Synchronize(f)
	if hwnd := w.mw.Handle(); hwnd != 0 {
		win.PostMessage(hwnd, win.WM_NULL, 0, 0)
	}
}

func (w *MainWindow) ShowInfo(title, body string) {
	w.synchronize(func() {
		walk.MsgBox(w.mw, title, body, walk.MsgBoxIconInformation)
	})
}

// Confirm asks a yes/no question on the UI thread and blocks until answered,
// so background workers can prompt the user.
func (w *MainWindow) Confirm(title, body string) bool {
	answer := make(chan bool, 1)
	w.synchronize(func() {
		answer <- walk.MsgBox(w.mw, title, body, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
	})
	return <-answer
}

func (w *MainWindow) ShowError(title, body string) {
	w.synchronize(func() {
		walk.MsgBox(w.mw, title, body, walk.MsgBoxIconError)
	})
}

func (w *MainWindow) save() {
	if w.callbacks.OnSave != nil {
		w.callbacks.OnSave(w.settings)
	}
}

func (w *MainWindow) ShowMainWindow() {
	w.synchronize(func() {
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
	w.synchronize(func() {
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
