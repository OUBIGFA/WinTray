//go:build windows

package ui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/stringutil"
	"wintray/internal/version"
)

// startMode is what happens to a program once it has started. Each mode is
// one combination of the stored flags; a hidden launch takes precedence over
// closing the window, as it does when programs are launched. The system
// start mode is the exception: the program's own logon task launches it
// outside WinTray, so no window handling of WinTray's applies to it.
type startMode int

const (
	startToTray startMode = iota
	startHidden
	startTask
	startNormal
)

func startModeOf(app config.ManagedAppEntry) startMode {
	switch {
	case app.LaunchViaLogonTask:
		return startTask
	case app.LaunchHiddenInBackground:
		return startHidden
	case app.TrayBehavior.AutoMinimizeAndHideOnLaunch:
		return startToTray
	default:
		return startNormal
	}
}

func (m startMode) applyTo(app *config.ManagedAppEntry) {
	app.LaunchViaLogonTask = m == startTask
	app.LaunchHiddenInBackground = m == startHidden
	app.TrayBehavior.AutoMinimizeAndHideOnLaunch = m == startToTray
}

const (
	programsListPaneMinWidth = 400
	programsDetailWidth      = 523
	programsDividerGap       = 14
)

// buildProgramsView lays out the page people open WinTray for: the programs
// on the left, the selected program's settings on the right, and a welcome
// state in their place until the first program is added.
func (w *MainWindow) buildProgramsView() error {
	view, err := newColumn(w.mw, 16)
	if err != nil {
		return err
	}
	w.programsView = view
	if err = w.buildLogonNotice(view); err != nil {
		return err
	}
	if w.programsBody, err = newRow(view, 0); err != nil {
		return err
	}
	listPane, err := w.buildListPane(w.programsBody)
	if err != nil {
		return err
	}
	if _, err = walk.NewHSpacerFixed(w.programsBody, programsDividerGap); err != nil {
		return err
	}
	if _, err = newVerticalDivider(w.programsBody); err != nil {
		return err
	}
	if _, err = walk.NewHSpacerFixed(w.programsBody, programsDividerGap); err != nil {
		return err
	}
	if err = w.buildDetailPane(w.programsBody); err != nil {
		return err
	}
	// The editor stays against the right content edge; the list takes any
	// extra width when the user resizes the window.
	layout := w.programsBody.Layout().(*walk.BoxLayout)
	layout.SetStretchFactor(listPane, 1)
	layout.SetStretchFactor(w.detailPane, 0)
	if err = w.buildEmptyState(view); err != nil {
		return err
	}
	if _, err = newDivider(view); err != nil {
		return err
	}
	return w.buildFooter(view)
}

// buildLogonNotice warns, only while it applies, that nothing in the list will
// start at sign-in, and turns WinTray's own autorun back on in one click.
func (w *MainWindow) buildLogonNotice(parent walk.Container) error {
	notice, err := newRow(parent, 12)
	if err != nil {
		return err
	}
	if err = notice.Layout().SetMargins(walk.Margins{HNear: 14, VNear: 8, HFar: 8, VFar: 8}); err != nil {
		return err
	}
	if err = fillBackground(notice, noticeColor); err != nil {
		return err
	}
	w.logonNotice = notice
	if w.logonNoticeText, err = walk.NewLabel(notice); err != nil {
		return err
	}
	w.logonNoticeText.SetTextColor(textColor)
	if err = w.logonNoticeText.SetEllipsisMode(walk.EllipsisEnd); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(notice); err != nil {
		return err
	}
	w.enableLogonBtn, err = newActionButton(notice, func() { w.setRunAtLogon(true) })
	return err
}

func (w *MainWindow) buildListPane(parent walk.Container) (*walk.Composite, error) {
	pane, err := newColumn(parent, 8)
	if err != nil {
		return nil, err
	}
	header, err := newRow(pane, 12)
	if err != nil {
		return nil, err
	}
	if w.managedTitle, err = newSectionTitle(header); err != nil {
		return nil, err
	}
	if _, err = walk.NewHSpacer(header); err != nil {
		return nil, err
	}
	if w.addProgramBtn, err = newActionButton(header, w.onAddProgram); err != nil {
		return nil, err
	}
	if w.managedHint, err = newHint(pane); err != nil {
		return nil, err
	}

	// Taller rows leave room for each program's own icon and a check box that
	// switches it on or off for sign-in without opening its settings.
	list, err := walk.NewTableViewWithCfg(pane, &walk.TableViewCfg{
		Style:           win.LVS_SHOWSELALWAYS,
		CustomRowHeight: w.mw.IntFrom96DPI(30),
	})
	if err != nil {
		return nil, err
	}
	list.SetMinMaxSize(walk.Size{Width: 280, Height: 160}, walk.Size{})
	list.SetColumnsOrderable(false)
	if err = list.SetHeaderHidden(false); err != nil {
		return nil, err
	}
	list.SetGridlines(false)
	if err = list.SetLastColumnStretched(true); err != nil {
		return nil, err
	}
	if err = list.SetSelectionHiddenWithoutFocus(false); err != nil {
		return nil, err
	}
	list.SetCheckBoxes(true)
	for _, width := range []int{170, 112, 145} {
		column := walk.NewTableViewColumn()
		column.SetWidth(width)
		_ = column.SetAlignment(walk.AlignCenter)
		if err = list.Columns().Add(column); err != nil {
			return nil, err
		}
	}
	model := newManagedListTableModel(w.onManagedChecked)
	if err = list.SetModel(model); err != nil {
		return nil, err
	}
	w.managedListModel = model
	if err = installTrayCheckColumn(list, model); err != nil {
		return nil, err
	}
	list.CurrentIndexChanged().Attach(w.syncManagedEditor)
	list.MouseUp().Attach(w.onManagedListMouseDown)
	w.managedList = list
	pane.SetMinMaxSize(walk.Size{Width: programsListPaneMinWidth}, walk.Size{})
	return pane, nil
}

// The TableView routes native list-view clicks through MouseUp.
// Hit-test that child so the tray toggle belongs only to its own column.
func (w *MainWindow) onManagedListMouseDown(x, y int, button walk.MouseButton) {
	if button != walk.LeftButton {
		return
	}
	var listView win.HWND
	for child := win.GetWindow(w.managedList.Handle(), win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		var bounds win.RECT
		if win.GetClientRect(child, &bounds) && bounds.Right > 0 {
			listView = child
			break
		}
	}
	if listView == 0 {
		return
	}
	hit := win.LVHITTESTINFO{Pt: win.POINT{X: int32(x), Y: int32(y)}}
	win.SendMessage(listView, win.LVM_SUBITEMHITTEST, 0, uintptr(unsafe.Pointer(&hit)))
	runtime.KeepAlive(&hit)
	if hit.ISubItem != 1 || hit.IItem < 0 || hit.Flags&win.LVHT_ONITEM == 0 {
		return
	}
	idx := int(hit.IItem)
	if idx >= len(w.settings.ManagedApps) {
		return
	}
	app := &w.settings.ManagedApps[idx]
	if !strings.EqualFold(filepath.Ext(app.ExePath), ".exe") {
		return
	}
	if w.callbacks.OnToggleTrayBox != nil {
		if err := w.callbacks.OnToggleTrayBox(app.ID, !app.CollectTrayIcon); err != nil {
			walk.MsgBox(w.mw, i18n.For(w.settings.Language).TrayBoxFailedTitle, err.Error(), walk.MsgBoxIconWarning)
		}
		return
	}
	app.CollectTrayIcon = !app.CollectTrayIcon
	w.updateManagedRow(idx)
	w.save()
}

func (w *MainWindow) buildDetailPane(parent walk.Container) error {
	pane, err := newColumn(parent, 0)
	if err != nil {
		return err
	}
	w.detailPane = pane
	if err = pane.SetAlignment(walk.AlignHNearVNear); err != nil {
		return err
	}
	pane.SetMinMaxSize(walk.Size{Width: programsDetailWidth}, walk.Size{Width: programsDetailWidth})

	if w.editor, err = newColumn(pane, groupSpacing); err != nil {
		return err
	}
	if err = w.buildIdentity(w.editor); err != nil {
		return err
	}
	if _, err = newDivider(w.editor); err != nil {
		return err
	}
	if err = w.buildSignIn(w.editor); err != nil {
		return err
	}
	if err = w.buildStartMode(w.editor); err != nil {
		return err
	}
	if err = w.buildSchedule(w.editor); err != nil {
		return err
	}
	if err = w.buildArguments(w.editor); err != nil {
		return err
	}
	if _, err = walk.NewVSpacer(w.editor); err != nil {
		return err
	}
	removeRow, err := newRow(w.editor, 0)
	if err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(removeRow); err != nil {
		return err
	}
	w.removeBtn, err = newActionButton(removeRow, w.onRemoveSelected)
	return err
}

// buildSignIn switches the selected program on or off for sign-in, the same
// as its check box in the list.
func (w *MainWindow) buildSignIn(parent walk.Container) error {
	block, err := newColumn(parent, fieldSpacing)
	if err != nil {
		return err
	}
	if w.appEnabledLabel, err = newFieldLabel(block); err != nil {
		return err
	}
	if w.appEnabled, err = walk.NewCheckBox(block); err != nil {
		return err
	}
	w.appEnabled.CheckedChanged().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, idx, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		app.RunOnStartup = w.appEnabled.Checked()
		w.updateManagedRow(idx)
		w.save()
	})
	w.appEnabledHint, err = newWrappedHint(block)
	return err
}

// buildIdentity shows which program is being edited, with its most used
// action next to its name and the rarely needed file change as a link before
// its path.
func (w *MainWindow) buildIdentity(parent walk.Container) error {
	identity, err := newColumn(parent, 4)
	if err != nil {
		return err
	}
	nameRow, err := newRow(identity, 12)
	if err != nil {
		return err
	}
	if w.appName, err = newTitleLabel(nameRow, programTitleSize); err != nil {
		return err
	}
	if err = w.appName.SetEllipsisMode(walk.EllipsisEnd); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(nameRow); err != nil {
		return err
	}
	if w.launchNowBtn, err = newActionButton(nameRow, w.onLaunchNow); err != nil {
		return err
	}
	pathRow, err := newRow(identity, 12)
	if err != nil {
		return err
	}
	if w.browseLink, err = walk.NewLinkLabel(pathRow); err != nil {
		return err
	}
	if err = w.browseLink.SetAlwaysConsumeSpace(true); err != nil {
		return err
	}
	w.browseLink.LinkActivated().Attach(func(*walk.LinkLabelLink) { w.onSelectProgramForSelected() })
	if w.appPath, err = newHint(pathRow); err != nil {
		return err
	}
	if err = w.appPath.SetEllipsisMode(walk.EllipsisPath); err != nil {
		return err
	}
	_, err = walk.NewHSpacer(pathRow)
	return err
}

// buildStartMode offers the start modes as one choice, described in words
// below it, instead of check boxes that exclude each other.
func (w *MainWindow) buildStartMode(parent walk.Container) error {
	block, err := newColumn(parent, fieldSpacing)
	if err != nil {
		return err
	}
	if w.modeLabel, err = newFieldLabel(block); err != nil {
		return err
	}
	row, err := newRow(block, 0)
	if err != nil {
		return err
	}
	// The buttons get a composite of their own: radio buttons form one group
	// while they are adjacent siblings, and arrow keys then cycle through
	// exactly these four.
	choices, err := newRow(row, 24)
	if err != nil {
		return err
	}
	if w.modeTray, err = w.newModeButton(choices, startToTray); err != nil {
		return err
	}
	if w.modeHidden, err = w.newModeButton(choices, startHidden); err != nil {
		return err
	}
	if w.modeTask, err = w.newModeButton(choices, startTask); err != nil {
		return err
	}
	if w.modeNormal, err = w.newModeButton(choices, startNormal); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}
	if w.modeHint, err = newWrappedHint(block); err != nil {
		return err
	}
	return w.buildCloseDelay(block)
}

func (w *MainWindow) newModeButton(parent walk.Container, mode startMode) (*walk.RadioButton, error) {
	button, err := walk.NewRadioButton(parent)
	if err != nil {
		return nil, err
	}
	button.CheckedChanged().Attach(func() {
		if !w.updatingEditor && button.Checked() {
			w.setStartMode(mode)
		}
	})
	return button, nil
}

// buildCloseDelay sits under the start modes and can only be edited for
// programs closed to the tray, the one mode it applies to.
func (w *MainWindow) buildCloseDelay(parent walk.Container) error {
	var err error
	if w.delayBlock, err = newIndentedColumn(parent, optionIndent, fieldSpacing); err != nil {
		return err
	}
	row, err := newRow(w.delayBlock, 8)
	if err != nil {
		return err
	}
	if w.delayLabel, err = newFieldLabel(row); err != nil {
		return err
	}
	if w.delayEdit, w.delayUnit, err = newNumberEdit(row); err != nil {
		return err
	}
	// A greedy trailing spacer absorbs this row's excess width. Without it
	// the fixed-size editor and unit get centered in equal slots and drift
	// apart (verified by TestCloseDelayRowGeometry).
	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}
	if w.delayHint, err = newWrappedHint(w.delayBlock); err != nil {
		return err
	}
	w.delayEdit.EditingFinished().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, idx, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		v, convErr := strconv.Atoi(strings.TrimSpace(w.delayEdit.Text()))
		if convErr != nil {
			walk.MsgBox(w.mw, w.mw.Title(), i18n.For(w.settings.Language).ManagedCloseDelayInvalid, walk.MsgBoxIconWarning)
			v = app.TrayBehavior.CloseDelaySeconds
		}
		v = config.ClampCloseDelaySeconds(v)
		w.delayEdit.SetText(strconv.Itoa(v))
		if v == app.TrayBehavior.CloseDelaySeconds {
			return
		}
		app.TrayBehavior.CloseDelaySeconds = v
		w.updateManagedRow(idx)
		w.save()
	})
	return nil
}

// optionIndent sets options that only apply with the setting above them in
// from its left edge.
const optionIndent = 20

// buildSchedule offers a start delay after sign-in and a run time after which
// the program is ended, both editable only once the schedule is switched on.
func (w *MainWindow) buildSchedule(parent walk.Container) error {
	block, err := newColumn(parent, fieldSpacing)
	if err != nil {
		return err
	}
	if w.scheduleRow, err = newRow(block, 12); err != nil {
		return err
	}
	if w.scheduleEnabled, err = walk.NewCheckBox(w.scheduleRow); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(w.scheduleRow); err != nil {
		return err
	}
	w.scheduleEnabled.CheckedChanged().Attach(func() {
		w.scheduleBlock.SetEnabled(w.scheduleEnabled.Checked())
		if w.updatingEditor {
			return
		}
		app, idx, ok := w.selectedManagedApp()
		if !ok || app.Schedule.Enabled == w.scheduleEnabled.Checked() {
			return
		}
		app.Schedule.Enabled = w.scheduleEnabled.Checked()
		w.updateManagedRow(idx)
		w.save()
	})
	if w.scheduleBlock, err = newIndentedColumn(block, optionIndent, fieldSpacing); err != nil {
		return err
	}
	row, err := newRow(w.scheduleBlock, 8)
	if err != nil {
		return err
	}
	if w.scheduleStart, err = newHint(row); err != nil {
		return err
	}
	if w.startDelayEdit, w.startDelayUnit, err = newNumberEdit(row); err != nil {
		return err
	}
	if _, err = walk.NewHSpacerFixed(row, 16); err != nil {
		return err
	}
	if w.scheduleExit, err = newHint(row); err != nil {
		return err
	}
	if w.autoExitEdit, w.autoExitUnit, err = newNumberEdit(row); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}
	if w.scheduleHint, err = newWrappedHint(w.scheduleBlock); err != nil {
		return err
	}
	w.attachScheduleMinutes(w.startDelayEdit, func(s *config.Schedule) *int { return &s.StartDelayMinutes })
	w.attachScheduleMinutes(w.autoExitEdit, func(s *config.Schedule) *int { return &s.AutoExitMinutes })
	return nil
}

// attachScheduleMinutes saves one scheduled time when its editor is left.
func (w *MainWindow) attachScheduleMinutes(edit *walk.LineEdit, field func(*config.Schedule) *int) {
	edit.EditingFinished().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, idx, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		stored := field(&app.Schedule)
		v, convErr := strconv.Atoi(strings.TrimSpace(edit.Text()))
		if convErr != nil {
			walk.MsgBox(w.mw, w.mw.Title(), i18n.For(w.settings.Language).ManagedScheduleInvalid, walk.MsgBoxIconWarning)
			v = *stored
		}
		v = config.ClampScheduleMinutes(v)
		edit.SetText(strconv.Itoa(v))
		if v == *stored {
			return
		}
		*stored = v
		w.updateManagedRow(idx)
		w.save()
	})
}

func (w *MainWindow) buildArguments(parent walk.Container) error {
	block, err := newColumn(parent, fieldSpacing)
	if err != nil {
		return err
	}
	if w.argsLabel, err = newFieldLabel(block); err != nil {
		return err
	}
	if w.argsEdit, err = walk.NewLineEdit(block); err != nil {
		return err
	}
	w.argsEdit.SetMinMaxSize(walk.Size{Height: 28}, walk.Size{Height: 28})
	if w.argsHint, err = newWrappedHint(block); err != nil {
		return err
	}
	w.argsEdit.EditingFinished().Attach(func() {
		if w.updatingEditor {
			return
		}
		app, _, ok := w.selectedManagedApp()
		if !ok {
			return
		}
		app.Args = w.argsEdit.Text()
		w.save()
	})
	return nil
}

func (w *MainWindow) buildEmptyState(parent walk.Container) error {
	var err error
	if w.emptyList, err = newColumn(parent, 12); err != nil {
		return err
	}
	if err = w.emptyList.Layout().(*walk.BoxLayout).SetAlignment(walk.AlignHCenterVCenter); err != nil {
		return err
	}
	if _, err = walk.NewVSpacer(w.emptyList); err != nil {
		return err
	}
	if w.emptyTitle, err = newSectionTitle(w.emptyList); err != nil {
		return err
	}
	if err = w.emptyTitle.SetTextAlignment(walk.AlignCenter); err != nil {
		return err
	}
	// Keep the explanation to a readable line length, centered.
	hintRow, err := newRow(w.emptyList, 0)
	if err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(hintRow); err != nil {
		return err
	}
	if w.emptyHint, err = newWrappedHint(hintRow); err != nil {
		return err
	}
	if err = w.emptyHint.SetTextAlignment(walk.AlignHCenterVNear); err != nil {
		return err
	}
	w.emptyHint.SetMinMaxSize(walk.Size{Width: 360}, walk.Size{Width: 560})
	if _, err = walk.NewHSpacer(hintRow); err != nil {
		return err
	}
	if _, err = walk.NewVSpacerFixed(w.emptyList, 4); err != nil {
		return err
	}
	buttonRow, err := newRow(w.emptyList, 0)
	if err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(buttonRow); err != nil {
		return err
	}
	if w.emptyAddBtn, err = newActionButton(buttonRow, w.onAddProgram); err != nil {
		return err
	}
	w.emptyAddBtn.SetMinMaxSize(walk.Size{Width: 140, Height: 34}, walk.Size{Height: 34})
	if _, err = walk.NewHSpacer(buttonRow); err != nil {
		return err
	}
	_, err = walk.NewVSpacer(w.emptyList)
	return err
}

// buildFooter shows the version with its update check on the left, and keeps
// the two ways of leaving WinTray on the right, apart from the settings of any
// program. Closing the window only hides it to the tray.
func (w *MainWindow) buildFooter(parent walk.Container) error {
	row, err := newRow(parent, 8)
	if err != nil {
		return err
	}
	if w.versionLabel, err = newFieldLabel(row); err != nil {
		return err
	}
	w.versionLabel.SetTextColor(secondaryColor)
	if w.githubLink, err = walk.NewLinkLabel(row); err != nil {
		return err
	}
	w.githubLink.LinkActivated().Attach(func(*walk.LinkLabelLink) {
		if w.callbacks.OnOpenRepository != nil {
			w.callbacks.OnOpenRepository()
		}
	})
	if _, err = walk.NewHSpacerFixed(row, 4); err != nil {
		return err
	}
	if w.checkUpdateBtn, err = newActionButton(row, w.onCheckUpdate); err != nil {
		return err
	}
	w.checkUpdateBtn.SetMinMaxSize(walk.Size{Width: 132}, walk.Size{})
	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}
	if w.silentBtn, err = newActionButton(row, func() {
		if w.callbacks.OnRunSilently != nil {
			w.callbacks.OnRunSilently()
		}
	}); err != nil {
		return err
	}
	w.exitBtn, err = newActionButton(row, func() {
		if w.callbacks.OnExit != nil {
			w.callbacks.OnExit()
		}
	})
	return err
}

func (w *MainWindow) applyProgramsLanguage(msg i18n.Messages) {
	w.logonNoticeText.SetText(msg.LogonOffNotice)
	w.enableLogonBtn.SetText(msg.LogonOffEnable)
	w.managedTitle.SetText(msg.ManagedListTitle)
	w.managedHint.SetText(msg.ManagedListHint)
	for _, button := range []*walk.PushButton{w.addProgramBtn, w.emptyAddBtn} {
		button.SetText(msg.AddProgram)
		button.SetToolTipText(msg.AddProgramHint)
	}
	w.emptyTitle.SetText(msg.ManagedListEmpty)
	w.emptyHint.SetText(msg.ManagedListEmptyHint)
	_ = w.browseLink.SetText(fmt.Sprintf("<a>%s</a>", msg.BrowseProgram))
	w.browseLink.SetToolTipText(msg.BrowseProgramHint)
	w.setLaunchNowBusy(w.launchNowBusy)
	w.launchNowBtn.SetToolTipText(msg.ManagedLaunchNowHint)
	w.appEnabledLabel.SetText(msg.ManagedEnabledLabel)
	w.appEnabled.SetText(msg.ManagedEnabled)
	w.appEnabledHint.SetText(msg.ManagedEnabledHint)
	w.modeLabel.SetText(msg.ManagedModeLabel)
	w.modeTray.SetText(msg.ManagedAutoHide)
	w.modeTray.SetToolTipText(msg.ManagedAutoHideTip)
	w.modeTask.SetText(msg.ManagedTaskLaunch)
	w.modeTask.SetToolTipText(msg.ManagedTaskLaunchTip)
	w.modeNormal.SetText(msg.ManagedLaunchOnly)
	w.modeHidden.SetText(msg.ManagedLaunchHidden)
	w.delayLabel.SetText(msg.ManagedCloseDelay)
	w.delayUnit.SetText(msg.SecondsUnit)
	w.delayHint.SetText(msg.ManagedCloseDelayHint)
	w.scheduleEnabled.SetText(msg.ManagedSchedule)
	w.scheduleEnabled.SetToolTipText(msg.ManagedSchedule)
	_ = w.scheduleEnabled.Accessibility().SetName(msg.ManagedSchedule)
	w.scheduleStart.SetText(msg.ManagedScheduleStart)
	w.startDelayUnit.SetText(msg.ManagedScheduleStartUnit)
	w.scheduleExit.SetText(msg.ManagedScheduleExit)
	w.autoExitUnit.SetText(msg.ManagedScheduleExitUnit)
	w.scheduleHint.SetText(msg.ManagedScheduleHint)
	w.argsLabel.SetText(msg.ManagedAppArgs)
	w.argsHint.SetText(msg.ManagedArgsHint)
	w.argsEdit.SetCueBanner(msg.ManagedArgsPlaceholder)
	w.removeBtn.SetText(msg.RemoveSelected)
	w.removeBtn.SetToolTipText(msg.RemoveSelectedHint)
	w.versionLabel.SetText(fmt.Sprintf(msg.VersionLabel, version.Number))
	_ = w.githubLink.SetText(fmt.Sprintf("<a>%s</a>", msg.GitHubLink))
	w.setCheckUpdateBusy(w.checkingUpdate)
	w.silentBtn.SetText(msg.RunSilently)
	w.silentBtn.SetToolTipText(msg.RunSilentlyHint)
	w.exitBtn.SetText(msg.ExitApp)
	w.managedList.Columns().At(0).SetTitle(msg.ManagedColumnName)
	w.managedList.Columns().At(1).SetTitle(msg.TrayBoxHomeTitle)
	w.managedList.Columns().At(2).SetTitle(msg.ManagedColumnRule)
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

// onAddProgram adds every file picked at once, which saves setting up several
// programs one dialog at a time, and selects the last one for editing.
func (w *MainWindow) onAddProgram() {
	msg := i18n.For(w.settings.Language)
	dlg := &walk.FileDialog{
		Title:  msg.SelectManagedExe,
		Filter: fmt.Sprintf("%s|%s", msg.ExeFilter, msg.AllFilesFilter),
	}
	ok, err := dlg.ShowOpenMultiple(w.mw)
	if err != nil || !ok || len(dlg.FilePaths) == 0 {
		return
	}
	base := time.Now().UnixNano()
	for i, path := range dlg.FilePaths {
		name := stringutil.TrimExt(filepath.Base(path))
		if name == "" {
			name = msg.NewAppName
		}
		launchHiddenByDefault := shouldDefaultLaunchHidden(path)
		w.settings.ManagedApps = append(w.settings.ManagedApps, config.ManagedAppEntry{
			ID:                       strconv.FormatInt(base+int64(i), 10),
			Name:                     name,
			ExePath:                  path,
			RunOnStartup:             true,
			LaunchHiddenInBackground: launchHiddenByDefault,
			TrayBehavior:             config.TrayBehavior{AutoMinimizeAndHideOnLaunch: !launchHiddenByDefault},
			Schedule:                 config.Schedule{AutoExitMinutes: config.DefaultAutoExitMinutes},
		})
	}
	w.refreshManagedList()
	w.managedList.SetCurrentIndex(len(w.settings.ManagedApps) - 1)
	w.syncManagedEditor()
	w.focusDefaultControl()
	w.save()
}

func (w *MainWindow) onSelectProgramForSelected() {
	app, idx, ok := w.selectedManagedApp()
	if !ok {
		return
	}
	msg := i18n.For(w.settings.Language)
	dlg := &walk.FileDialog{
		Title:          msg.SelectReplacementExe,
		Filter:         fmt.Sprintf("%s|%s", msg.ExeFilter, msg.AllFilesFilter),
		InitialDirPath: filepath.Dir(app.ExePath),
	}
	result, err := dlg.ShowOpen(w.mw)
	if err != nil || !result {
		return
	}
	if !w.stopCollecting(app) {
		return
	}
	app.ExePath = dlg.FilePath
	name := stringutil.TrimExt(filepath.Base(dlg.FilePath))
	if name != "" {
		app.Name = name
	}
	if shouldDefaultLaunchHidden(dlg.FilePath) {
		startHidden.applyTo(app)
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
	if !w.stopCollecting(&w.settings.ManagedApps[idx]) {
		return
	}
	w.settings.ManagedApps = append(w.settings.ManagedApps[:idx], w.settings.ManagedApps[idx+1:]...)
	w.refreshManagedList()
	// The button just used may have been hidden with the last program.
	w.focusDefaultControl()
	w.save()
}

func (w *MainWindow) stopCollecting(app *config.ManagedAppEntry) bool {
	if !app.CollectTrayIcon {
		return true
	}
	if w.callbacks.OnToggleTrayBox != nil {
		if err := w.callbacks.OnToggleTrayBox(app.ID, false); err != nil {
			walk.MsgBox(w.mw, i18n.For(w.settings.Language).TrayBoxFailedTitle, err.Error(), walk.MsgBoxIconWarning)
			return false
		}
	} else {
		app.CollectTrayIcon = false
	}
	return true
}

// onManagedChecked switches a program on or off for sign-in from its check box
// in the list.
func (w *MainWindow) onManagedChecked(idx int, checked bool) {
	if idx < 0 || idx >= len(w.settings.ManagedApps) {
		return
	}
	w.settings.ManagedApps[idx].RunOnStartup = checked
	w.updateManagedRow(idx)
	if _, selected, ok := w.selectedManagedApp(); ok && selected == idx {
		w.updatingEditor = true
		w.appEnabled.SetChecked(checked)
		w.updatingEditor = false
	}
	w.save()
}

func (w *MainWindow) setStartMode(mode startMode) {
	app, idx, ok := w.selectedManagedApp()
	if !ok || startModeOf(*app) == mode {
		return
	}
	mode.applyTo(app)
	w.updateManagedRow(idx)
	w.showStartMode(mode)
	w.save()
}

// showStartMode explains the chosen mode and shows the close delay only where
// it has an effect.
func (w *MainWindow) showStartMode(mode startMode) {
	msg := i18n.For(w.settings.Language)
	switch mode {
	case startToTray:
		w.modeHint.SetText(msg.ManagedAutoHideHint)
	case startHidden:
		w.modeHint.SetText(msg.ManagedLaunchHiddenHint)
	case startTask:
		w.modeHint.SetText(msg.ManagedTaskLaunchHint)
	default:
		w.modeHint.SetText(msg.ManagedLaunchOnlyHint)
	}
	w.argsEdit.SetReadOnly(false)
	if mode == startTask {
		w.argsHint.SetText(msg.ManagedTaskArgsHint)
		w.launchNowBtn.SetToolTipText(msg.ManagedTaskLaunchNowHint)
	} else {
		w.argsHint.SetText(msg.ManagedArgsHint)
		w.launchNowBtn.SetToolTipText(msg.ManagedLaunchNowHint)
	}
	w.delayBlock.SetEnabled(mode == startToTray)
}

func (w *MainWindow) managedRow(app config.ManagedAppEntry) managedListRow {
	return managedListRow{
		Name:       app.Name,
		Path:       app.ExePath,
		Mode:       i18n.FormatManagedParam(w.settings.Language, app),
		Enabled:    app.RunOnStartup,
		Collected:  app.CollectTrayIcon,
		CanCollect: strings.EqualFold(filepath.Ext(app.ExePath), ".exe"),
	}
}

// updateManagedRow redraws one program's row, keeping selection and focus.
func (w *MainWindow) updateManagedRow(idx int) {
	w.managedListModel.SetRow(idx, w.managedRow(w.settings.ManagedApps[idx]))
}

func (w *MainWindow) refreshManagedList() {
	if w.managedList == nil || w.managedListModel == nil {
		return
	}
	wasSuspended := w.mw.Suspended()
	w.mw.SetSuspended(true)
	defer w.mw.SetSuspended(wasSuspended)
	selected := w.managedList.CurrentIndex()
	rows := make([]managedListRow, 0, len(w.settings.ManagedApps))
	for _, app := range w.settings.ManagedApps {
		rows = append(rows, w.managedRow(app))
	}
	w.managedListModel.SetRows(rows)
	w.programsBody.SetVisible(len(rows) > 0)
	w.emptyList.SetVisible(len(rows) == 0)
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

// syncManagedEditor keeps the right-hand layout in place even without a
// selection. Program-specific fields reset to defaults and cannot be edited.
func (w *MainWindow) syncManagedEditor() {
	if w.editor == nil || w.managedList == nil {
		return
	}
	app, _, ok := w.selectedManagedApp()
	w.updatingEditor = true
	defer func() { w.updatingEditor = false }()

	w.editor.SetEnabled(ok)
	if ok {
		isExe := strings.EqualFold(filepath.Ext(app.ExePath), ".exe")
		w.modeTask.SetEnabled(isExe)
	} else {
		w.modeTask.SetEnabled(false)
	}
	w.browseLink.SetEnabled(ok)
	w.browseLink.SetVisible(ok)
	w.launchNowBtn.SetEnabled(ok && !w.launchNowBusy)
	if !ok {
		w.appName.SetText("")
		w.appPath.SetText("")
		w.appEnabled.SetChecked(false)
		w.modeTray.SetChecked(true)
		w.modeNormal.SetChecked(false)
		w.modeHidden.SetChecked(false)
		w.modeTask.SetChecked(false)
		w.showStartMode(startToTray)
		w.argsEdit.SetText("")
		w.delayEdit.SetText("0")
		w.scheduleEnabled.SetChecked(false)
		w.scheduleBlock.SetEnabled(false)
		w.startDelayEdit.SetText("0")
		w.autoExitEdit.SetText(strconv.Itoa(config.DefaultAutoExitMinutes))
		return
	}

	w.appName.SetText(app.Name)
	w.appPath.SetText(app.ExePath)
	w.appEnabled.SetChecked(app.RunOnStartup)
	mode := startModeOf(*app)
	w.modeTray.SetChecked(mode == startToTray)
	w.modeNormal.SetChecked(mode == startNormal)
	w.modeHidden.SetChecked(mode == startHidden)
	w.modeTask.SetChecked(mode == startTask)
	w.showStartMode(mode)
	w.delayEdit.SetText(strconv.Itoa(app.TrayBehavior.CloseDelaySeconds))
	w.scheduleEnabled.SetChecked(app.Schedule.Enabled)
	w.scheduleBlock.SetEnabled(app.Schedule.Enabled)
	w.startDelayEdit.SetText(strconv.Itoa(app.Schedule.StartDelayMinutes))
	w.autoExitEdit.SetText(strconv.Itoa(app.Schedule.AutoExitMinutes))
	w.argsEdit.SetText(app.Args)
}

func (w *MainWindow) selectedManagedApp() (*config.ManagedAppEntry, int, bool) {
	idx := w.managedList.CurrentIndex()
	if idx < 0 || idx >= len(w.settings.ManagedApps) {
		return nil, -1, false
	}
	return &w.settings.ManagedApps[idx], idx, true
}

func (w *MainWindow) clearManagedSelection() {
	if w.managedList == nil {
		return
	}
	_ = w.managedList.SetSelectedIndexes([]int{})
	_ = w.managedList.SetCurrentIndex(-1)
	w.syncManagedEditor()
}
