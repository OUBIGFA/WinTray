//go:build windows

package ui

import (
	"strconv"
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

// settingsContentWidth keeps each setting's name close to its control on a
// wide window, where a full-width row would put them far apart.
const settingsContentWidth = 760

// nestedSettingIndent sets off settings that depend on the row above them.
const nestedSettingIndent = 28

// buildSettingsView gathers everything that is set once or only needed when
// something goes wrong, so the program list stays uncluttered. Every setting
// is one row: its name and explanation on the left, its control on the right.
func (w *MainWindow) buildSettingsView() error {
	view, err := walk.NewScrollView(w.mw)
	if err != nil {
		return err
	}
	// Walk only stretches a scroll view across the window while it may also
	// scroll sideways; the rows never need to, as their text wraps.
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{HFar: 12})
	if err = layout.SetAlignment(walk.AlignHNearVNear); err != nil {
		return err
	}
	if err = view.SetLayout(layout); err != nil {
		return err
	}
	w.settingsView = view
	// A horizontal wrapper makes the width limit effective: Walk's VBox
	// ignores a child's maximum width when stretching it across the page.
	// Matching spacers share the leftover width so the block stays centered.
	widthRow, err := newRow(view, 0)
	if err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(widthRow); err != nil {
		return err
	}
	content, err := newColumn(widthRow, 28)
	if err != nil {
		return err
	}
	content.SetMinMaxSize(walk.Size{}, walk.Size{Width: settingsContentWidth})
	content.SizeChanged().Attach(func() {
		if w.settingsFitPending {
			w.fitSettingsToScreen()
		}
	})
	view.SizeChanged().Attach(func() {
		if w.settingsFitPending {
			w.fitSettingsToScreen()
		}
	})
	if _, err = walk.NewHSpacer(widthRow); err != nil {
		return err
	}
	header, err := newRow(content, 12)
	if err != nil {
		return err
	}
	if w.backBtn, err = newActionButton(header, func() { w.showSettings(false) }); err != nil {
		return err
	}
	if w.settingsTitle, err = newTitleLabel(header, pageTitleSize); err != nil {
		return err
	}
	if _, err = walk.NewHSpacer(header); err != nil {
		return err
	}
	for _, build := range []func(walk.Container) error{
		w.buildLogonSettings,
		w.buildTimingSettings,
		w.buildTroubleshooting,
	} {
		if err = build(content); err != nil {
			return err
		}
	}
	_, err = walk.NewVSpacer(view)
	return err
}

// fitWindowToPages chooses a single initial height for both pages before the
// window is shown. The settings hints have a minimum width close to their
// actual column width, so their height hint reflects wrapped copy.
func (w *MainWindow) fitWindowToPages() {
	bounds := w.mw.BoundsPixels()
	margins := w.mw.Layout().Margins()
	chrome := bounds.Height - w.mw.ClientBoundsPixels().Height
	padding := w.mw.IntFrom96DPI(margins.VNear + margins.VFar)
	home := w.headerRow.SizeHint().Height + w.programsView.SizeHint().Height + w.mw.IntFrom96DPI(w.mw.Layout().Spacing())
	settings := w.settingsView.SizeHint().Height
	w.growWindowToHeight(chrome + padding + max(home, settings))
}

// fitSettingsToScreen checks the actual layout after entering settings in case
// DPI or later content changes require more height than the initial hint.
func (w *MainWindow) fitSettingsToScreen() {
	if !w.settingsFitPending || !w.onSettingsPage || !w.mw.Visible() {
		return
	}
	content := w.startupTitle.Parent().(*walk.Composite).Parent().(*walk.Composite)
	contentHeight := content.BoundsPixels().Height
	viewHeight := w.settingsView.BoundsPixels().Height
	if contentHeight == 0 || viewHeight == 0 {
		return
	}
	missing := contentHeight - viewHeight
	if missing <= 0 {
		w.settingsFitPending = false
		return
	}
	if !w.growWindowToHeight(w.mw.BoundsPixels().Height + missing + 2) {
		w.settingsFitPending = false
	}
}

// growWindowToHeight preserves the window width and never exceeds the current
// monitor's work area. Beyond that height the settings page can scroll.
func (w *MainWindow) growWindowToHeight(requested int) bool {
	bounds := w.mw.BoundsPixels()
	if requested <= bounds.Height {
		return false
	}
	monitor := win.MonitorFromWindow(w.mw.Handle(), win.MONITOR_DEFAULTTONEAREST)
	info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
	if monitor == 0 || !win.GetMonitorInfo(monitor, &info) {
		return false
	}
	work := info.RcWork
	height := min(requested, int(work.Bottom-work.Top))
	if height <= bounds.Height {
		return false
	}
	bounds.Y = max(int(work.Top), min(bounds.Y-(height-bounds.Height)/2, int(work.Bottom)-height))
	bounds.Height = height
	return w.mw.SetBoundsPixels(bounds) == nil
}

// newSettingsSection is a titled group of setting rows, separated by lines.
func newSettingsSection(parent walk.Container) (*walk.Composite, *walk.Label, error) {
	section, err := newColumn(parent, 0)
	if err != nil {
		return nil, nil, err
	}
	title, err := newSectionTitle(section)
	if err != nil {
		return nil, nil, err
	}
	_, err = walk.NewVSpacerFixed(section, 4)
	return section, title, err
}

// addSettingRow adds a row to a section, below a divider unless it is the
// section's first row.
func addSettingRow(section *walk.Composite, indent int, first bool) (*settingRow, error) {
	if !first {
		if _, err := newDivider(section); err != nil {
			return nil, err
		}
	}
	return newSettingRow(section, indent)
}

func (w *MainWindow) buildLogonSettings(parent walk.Container) error {
	section, title, err := newSettingsSection(parent)
	if err != nil {
		return err
	}
	w.startupTitle = title
	if w.logonRow, err = addSettingRow(section, 0, true); err != nil {
		return err
	}
	if w.runAtLogon, err = newToggle(w.logonRow.controls, w.setRunAtLogon); err != nil {
		return err
	}
	// Both options below only take effect when WinTray runs at sign-in, so
	// they are indented under it and greyed out while it is off.
	if w.hideAtLogonRow, err = addSettingRow(section, nestedSettingIndent, false); err != nil {
		return err
	}
	if w.hideAtLogon, err = newToggle(w.hideAtLogonRow.controls, func(on bool) {
		if w.settings.StartMinimizedToTray != on {
			w.settings.StartMinimizedToTray = on
			w.save()
		}
	}); err != nil {
		return err
	}
	if w.exitOnDoneRow, err = addSettingRow(section, nestedSettingIndent, false); err != nil {
		return err
	}
	w.exitOnDone, err = newToggle(w.exitOnDoneRow.controls, func(on bool) {
		if w.settings.ExitAfterManagedAppsCompleted != on {
			w.settings.ExitAfterManagedAppsCompleted = on
			w.save()
		}
	})
	return err
}

// setRunAtLogon is shared by the settings switch and the notice on the
// program list that offers to turn running at sign-in back on. Showing the
// stored state again through syncLogonState changes and saves nothing.
func (w *MainWindow) setRunAtLogon(on bool) {
	if w.settings.RunAtLogon == on {
		return
	}
	w.settings.RunAtLogon = on
	w.syncLogonState()
	w.save()
}

func (w *MainWindow) syncLogonState() {
	wasSuspended := w.mw.Suspended()
	w.mw.SetSuspended(true)
	defer w.mw.SetSuspended(wasSuspended)
	w.runAtLogon.SetChecked(w.settings.RunAtLogon)
	w.hideAtLogon.SetChecked(w.settings.StartMinimizedToTray)
	w.exitOnDone.SetChecked(w.settings.ExitAfterManagedAppsCompleted)
	w.hideAtLogonRow.setEnabled(w.settings.RunAtLogon)
	w.exitOnDoneRow.setEnabled(w.settings.RunAtLogon)
	w.logonNotice.SetVisible(!w.settings.RunAtLogon)
}

func (w *MainWindow) buildTimingSettings(parent walk.Container) error {
	section, title, err := newSettingsSection(parent)
	if err != nil {
		return err
	}
	w.timingTitle = title
	if w.intervalRow, w.intervalEdit, w.intervalUnit, err = newSecondsRow(section, true); err != nil {
		return err
	}
	w.intervalEdit.SetText(strconv.Itoa(w.settings.StartupIntervalSeconds))
	w.intervalEdit.EditingFinished().Attach(func() {
		v, convErr := strconv.Atoi(strings.TrimSpace(w.intervalEdit.Text()))
		if convErr != nil {
			walk.MsgBox(w.mw, w.mw.Title(), i18n.For(w.settings.Language).StartupIntervalInvalid, walk.MsgBoxIconWarning)
			v = w.settings.StartupIntervalSeconds
		}
		v = config.ClampStartupIntervalSeconds(v)
		w.settings.StartupIntervalSeconds = v
		w.intervalEdit.SetText(strconv.Itoa(v))
		w.save()
	})

	if w.retryRow, w.retryEdit, w.retryUnit, err = newSecondsRow(section, false); err != nil {
		return err
	}
	w.retryEdit.SetText(strconv.Itoa(w.settings.CloseWindowRetrySeconds))
	w.retryEdit.EditingFinished().Attach(func() {
		v, convErr := strconv.Atoi(strings.TrimSpace(w.retryEdit.Text()))
		if convErr != nil {
			walk.MsgBox(w.mw, w.mw.Title(), i18n.For(w.settings.Language).RetrySecondsInvalid, walk.MsgBoxIconWarning)
			v = w.settings.CloseWindowRetrySeconds
		}
		v = min(max(v, 0), 120)
		w.settings.CloseWindowRetrySeconds = v
		w.retryEdit.SetText(strconv.Itoa(v))
		w.save()
	})
	return nil
}

// newSecondsRow is a setting row whose control is a number of seconds.
func newSecondsRow(section *walk.Composite, first bool) (*settingRow, *walk.LineEdit, *walk.Label, error) {
	row, err := addSettingRow(section, 0, first)
	if err != nil {
		return nil, nil, nil, err
	}
	edit, unit, err := newNumberEdit(row.controls)
	return row, edit, unit, err
}

func (w *MainWindow) buildTroubleshooting(parent walk.Container) error {
	section, title, err := newSettingsSection(parent)
	if err != nil {
		return err
	}
	w.troubleshootTitle = title
	if w.logsRow, err = addSettingRow(section, 0, true); err != nil {
		return err
	}
	if w.openLogsBtn, err = newActionButton(w.logsRow.controls, func() {
		if w.callbacks.OnOpenLogs != nil {
			w.callbacks.OnOpenLogs()
		}
	}); err != nil {
		return err
	}
	if w.cleanupRow, err = addSettingRow(section, 0, false); err != nil {
		return err
	}
	w.cleanupBtn, err = newActionButton(w.cleanupRow.controls, func() {
		if w.callbacks.OnCleanupRestore != nil {
			w.callbacks.OnCleanupRestore()
		}
	})
	return err
}

func (w *MainWindow) applySettingsLanguage(msg i18n.Messages) {
	w.startupTitle.SetText(msg.SettingsStartupTitle)
	for _, s := range []struct {
		row    *settingRow
		toggle *walk.CheckBox
		title  string
		hint   string
	}{
		{w.logonRow, w.runAtLogon, msg.RunAtLogon, msg.RunAtLogonHint},
		{w.hideAtLogonRow, w.hideAtLogon, msg.StartHidden, msg.StartHiddenHint},
		{w.exitOnDoneRow, w.exitOnDone, msg.ExitOnDone, msg.ExitOnDoneHint},
	} {
		s.row.title.SetText(s.title)
		s.row.hint.SetText(s.hint)
		_ = s.toggle.Accessibility().SetName(s.title)
	}
	w.timingTitle.SetText(msg.SettingsTimingTitle)
	w.intervalRow.title.SetText(msg.StartupInterval)
	w.intervalRow.hint.SetText(msg.StartupIntervalHint)
	w.intervalUnit.SetText(msg.SecondsUnit)
	w.retryRow.title.SetText(msg.RetrySeconds)
	w.retryRow.hint.SetText(msg.RetrySecondsHint)
	w.retryUnit.SetText(msg.SecondsUnit)

	w.troubleshootTitle.SetText(msg.SettingsTroubleshootTitle)
	w.logsRow.title.SetText(msg.LogsTitle)
	w.logsRow.hint.SetText(msg.OpenLogsHint)
	w.openLogsBtn.SetText(msg.OpenLogs)
	w.cleanupRow.title.SetText(msg.CleanupRestoreTitle)
	w.cleanupRow.hint.SetText(msg.CleanupRestoreHint)
	w.cleanupBtn.SetText(msg.CleanupRestore)
}
