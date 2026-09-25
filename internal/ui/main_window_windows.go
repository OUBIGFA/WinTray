//go:build windows

package ui

import (
	"context"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/branding"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

type Callbacks struct {
	OnSave           func(config.Settings)
	OnOpenLogs       func()
	OnCleanupRestore func()
	OnLaunchNow      func(config.ManagedAppEntry)
	OnCheckUpdate    func()
	OnOpenRepository func()
	OnExit           func()
	OnRunSilently    func()
	OnHideToTray     func()
}

// MainWindow has two pages: the program list, which is what people open
// WinTray for, with the language, version and update check at hand; and a
// settings page for options that are set once, such as running at sign-in,
// timing and troubleshooting.
type MainWindow struct {
	mw             *walk.MainWindow
	allowClose     bool
	settings       config.Settings
	callbacks      Callbacks
	applyingLocale bool
	updatingEditor bool
	launchNowBusy  bool
	checkingUpdate bool
	// onSettingsPage tracks the current page itself: while the window is
	// hidden, all of its children report themselves as invisible.
	onSettingsPage     bool
	settingsFitPending bool

	headerRow     *walk.Composite
	backBtn       *walk.PushButton
	pageTitle     *walk.Label
	settingsTitle *walk.Label
	languageCombo *walk.ComboBox
	settingsBtn   *walk.PushButton

	programsView     *walk.Composite
	logonNotice      *walk.Composite
	logonNoticeText  *walk.Label
	enableLogonBtn   *walk.PushButton
	programsBody     *walk.Composite
	managedTitle     *walk.Label
	managedHint      *walk.Label
	addProgramBtn    *walk.PushButton
	managedList      *walk.TableView
	managedListModel *managedListTableModel
	emptyList        *walk.Composite
	emptyTitle       *walk.Label
	emptyHint        *walk.TextLabel
	emptyAddBtn      *walk.PushButton
	detailPane       *walk.Composite
	editor           *walk.Composite
	appName          *walk.Label
	appPath          *walk.Label
	browseLink       *walk.LinkLabel
	launchNowBtn     *walk.PushButton
	appEnabledLabel  *walk.Label
	appEnabled       *walk.CheckBox
	appEnabledHint   *walk.TextLabel
	modeLabel        *walk.Label
	modeTray         *walk.RadioButton
	modeNormal       *walk.RadioButton
	modeHidden       *walk.RadioButton
	modeHint         *walk.TextLabel
	delayBlock       *walk.Composite
	delayLabel       *walk.Label
	delayEdit        *walk.LineEdit
	delayUnit        *walk.Label
	delayHint        *walk.TextLabel
	argsLabel        *walk.Label
	argsEdit         *walk.LineEdit
	argsHint         *walk.TextLabel
	removeBtn        *walk.PushButton
	versionLabel     *walk.Label
	githubLink       *walk.LinkLabel
	checkUpdateBtn   *walk.PushButton
	silentBtn        *walk.PushButton
	exitBtn          *walk.PushButton

	settingsView      *walk.ScrollView
	startupTitle      *walk.Label
	logonRow          *settingRow
	runAtLogon        *walk.CheckBox
	hideAtLogonRow    *settingRow
	hideAtLogon       *walk.CheckBox
	exitOnDoneRow     *settingRow
	exitOnDone        *walk.CheckBox
	timingTitle       *walk.Label
	intervalRow       *settingRow
	intervalEdit      *walk.LineEdit
	intervalUnit      *walk.Label
	retryRow          *settingRow
	retryEdit         *walk.LineEdit
	retryUnit         *walk.Label
	troubleshootTitle *walk.Label
	logsRow           *settingRow
	openLogsBtn       *walk.PushButton
	cleanupRow        *settingRow
	cleanupBtn        *walk.PushButton

	blankSurfaces map[win.HWND]walk.Window
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
	mw.SetSuspended(true)
	defer mw.SetSuspended(false)

	mw.SetSize(walk.Size{Width: 1040, Height: 780})
	mw.SetMinMaxSize(walk.Size{Width: 1040, Height: 720}, walk.Size{})
	if font, fontErr := walk.NewFont(uiFontFamily, bodyTextSize, 0); fontErr == nil {
		mw.SetFont(font)
	}
	if bg, bgErr := walk.NewSolidColorBrush(walk.RGB(248, 249, 251)); bgErr == nil {
		mw.SetBackground(bg)
	}
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{HNear: 24, VNear: 16, HFar: 24, VFar: 18})
	layout.SetSpacing(16)
	if err = mw.SetLayout(layout); err != nil {
		return nil, err
	}

	if err = w.buildHeader(); err != nil {
		return nil, err
	}
	if err = w.buildProgramsView(); err != nil {
		return nil, err
	}
	if err = w.buildSettingsView(); err != nil {
		return nil, err
	}
	if err = w.installBlankClickReset(); err != nil {
		return nil, err
	}

	w.showSettings(false)
	w.syncLogonState()
	w.applyLanguage(w.settings.Language)
	w.refreshManagedList()

	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if !w.allowClose {
			*canceled = true
			w.mw.Hide()
			w.mw.SetVisible(false)
			if w.callbacks.OnHideToTray != nil {
				w.callbacks.OnHideToTray()
			}
		}
	})

	return w, nil
}

// buildHeader holds the program page title, language and settings button.
func (w *MainWindow) buildHeader() error {
	row, err := newRow(w.mw, 12)
	if err != nil {
		return err
	}
	w.headerRow = row
	if w.pageTitle, err = newTitleLabel(row, pageTitleSize); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}
	if w.languageCombo, err = walk.NewComboBox(row); err != nil {
		return err
	}
	w.languageCombo.SetMinMaxSize(walk.Size{Width: 110}, walk.Size{Width: 110})
	w.languageCombo.CurrentIndexChanged().Attach(func() {
		if w.applyingLocale {
			return
		}
		language := "zh-CN"
		if w.languageCombo.CurrentIndex() == 1 {
			language = "en-US"
		}
		w.applyLanguage(language)
		w.refreshManagedList()
		w.save()
	})
	w.settingsBtn, err = newActionButton(row, func() { w.showSettings(true) })
	return err
}

// showSettings switches between the program list and the settings page. The
// program selection is kept, so going back returns to the same program.
func (w *MainWindow) showSettings(show bool) {
	wasSuspended := w.mw.Suspended()
	w.mw.SetSuspended(true)
	defer w.mw.SetSuspended(wasSuspended)
	w.onSettingsPage = show
	w.headerRow.SetVisible(!show)
	w.programsView.SetVisible(!show)
	w.settingsView.SetVisible(show)
	w.applyPageTitle()
	if show && w.mw.Visible() {
		w.settingsFitPending = true
		// Let Walk finish laying out the newly visible page before measuring it.
		w.synchronize(w.fitSettingsToScreen)
	}
	if w.mw.Visible() {
		w.focusDefaultControl()
	}
}

func (w *MainWindow) applyPageTitle() {
	msg := i18n.For(w.settings.Language)
	w.pageTitle.SetText(msg.WindowTitle)
	w.settingsTitle.SetText(msg.SettingsTitle)
}

func (w *MainWindow) applyLanguage(language string) {
	wasSuspended := w.mw.Suspended()
	w.mw.SetSuspended(true)
	defer w.mw.SetSuspended(wasSuspended)
	msg := i18n.For(language)
	w.settings.Language = string(i18n.Resolve(language))
	w.applyingLocale = true
	defer func() { w.applyingLocale = false }()

	w.mw.SetTitle(msg.WindowTitle)
	w.applyPageTitle()
	w.backBtn.SetText(msg.BackToPrograms)
	w.settingsBtn.SetText(msg.OpenSettings)
	_ = w.languageCombo.SetModel([]string{msg.LanguageZhLabel, msg.LanguageEnLabel})
	if w.settings.Language == string(i18n.LangEnUS) {
		w.languageCombo.SetCurrentIndex(1)
	} else {
		w.languageCombo.SetCurrentIndex(0)
	}
	w.languageCombo.SetToolTipText(msg.LanguageLabel)
	_ = w.languageCombo.Accessibility().SetName(msg.LanguageLabel)
	w.applyProgramsLanguage(msg)
	w.applySettingsLanguage(msg)
	w.syncManagedEditor()
}

func (w *MainWindow) SetLanguage(language string) {
	w.synchronize(func() {
		w.applyLanguage(language)
		w.refreshManagedList()
	})
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

// Synchronize schedules application-owned state changes on the UI thread.
func (w *MainWindow) Synchronize(f func()) { w.synchronize(f) }

func (w *MainWindow) ShowInfo(title, body string) {
	w.synchronize(func() {
		walk.MsgBox(w.mw, title, body, walk.MsgBoxIconInformation)
	})
}

// ConfirmContext asks on the UI thread without trapping a worker if shutdown
// ends the message loop before the question is displayed.
func (w *MainWindow) ConfirmContext(ctx context.Context, title, body string) bool {
	answer := make(chan bool, 1)
	w.synchronize(func() {
		if ctx.Err() == nil {
			answer <- walk.MsgBox(w.mw, title, body, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes
		}
	})
	select {
	case yes := <-answer:
		return yes
	case <-ctx.Done():
		return false
	}
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

// ShowMainWindow brings the window to the front. A window reopened from the
// tray starts on the program list, whichever page it was closed on.
func (w *MainWindow) ShowMainWindow() {
	w.synchronize(func() {
		if !w.mw.Visible() && w.onSettingsPage {
			w.showSettings(false)
		}
		w.fitWindowToPages()
		hwnd := w.mw.Handle()
		if hwnd != 0 {
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.ShowWindow(hwnd, win.SW_SHOW)
			win.SetForegroundWindow(hwnd)
		}
		w.mw.Show()
		w.mw.SetVisible(true)
		w.focusDefaultControl()
	})
}

// focusDefaultControl gives keyboard focus to the program list, to the add
// button while the list is empty, or to the back button on the settings page.
// Focus has to land on a child: walk otherwise moves it to the first tab stop
// after a layout pass, whose text then shows up selected. Moving focus is also
// what commits a field still being edited.
func (w *MainWindow) focusDefaultControl() {
	switch {
	case w.onSettingsPage:
		_ = w.backBtn.SetFocus()
	case w.managedList != nil && w.managedList.Visible():
		_ = w.managedList.SetFocus()
	case w.emptyAddBtn != nil:
		_ = w.emptyAddBtn.SetFocus()
	}
}

func (w *MainWindow) HideMainWindow() {
	w.mw.Hide()
}

func (w *MainWindow) Run() int {
	w.fitWindowToPages()
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
