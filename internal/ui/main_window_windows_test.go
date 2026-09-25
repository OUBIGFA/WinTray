//go:build windows

package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/version"
)

// This opt-in test opens only the settings UI, with in-memory callbacks. It does
// not start managed programs, create a tray icon or touch user settings/autorun.
// Run on an interactive Windows desktop with WINTRAY_UI_TEST=1.
func TestMainWindowInteractions(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	discardStaleMessages()
	defer discardStaleMessages()
	deactivate := activateTestManifest(t)
	defer deactivate()

	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{
		{ID: "1", Name: "Cloud Drive", ExePath: `C:\Apps\Cloud Drive\drive.exe`, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}},
		{ID: "2", Name: "Backup script", ExePath: `C:\Scripts\backup.ps1`, RunOnStartup: true, LaunchHiddenInBackground: true},
		{ID: "3", Name: "Local service", ExePath: `C:\Tools\service.exe`},
	}
	saves, launches, updateChecks := 0, 0, 0
	w, err := NewMainWindow(settings, Callbacks{
		OnSave:        func(config.Settings) { saves++ },
		OnCheckUpdate: func() { updateChecks++ },
		OnLaunchNow: func(app config.ManagedAppEntry) {
			launches++
			if app.ID != "1" {
				t.Errorf("launched %q, want selected program 1", app.ID)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.mw.Dispose()

	// Run assertions on the UI thread, leaving the real message/layout loop time
	// to settle between steps. No mocked controls or source-text assertions.
	step := func(f func()) {
		done := make(chan struct{})
		w.synchronize(func() { defer close(done); f() })
		<-done
		time.Sleep(200 * time.Millisecond)
	}
	click := func(button walk.Widget) { button.SendMessage(win.BM_CLICK, 0, 0) }
	zh, en := i18n.For("zh-CN"), i18n.For("en-US")
	var baseBody, baseDetail, baseList walk.Rectangle
	var baseNameRow, baseDelay walk.Rectangle
	w.mw.Starting().Attach(func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			step(func() {
				baseBody, baseDetail, baseList = w.programsBody.Bounds(), w.detailPane.Bounds(), w.managedList.Bounds()
				listPane := w.managedList.Parent().Bounds()
				divider := w.programsBody.Children().At(2).Bounds()
				leftGap := divider.X - listPane.X - listPane.Width
				rightGap := w.detailPane.Bounds().X - divider.X - divider.Width
				if leftGap != programsDividerGap || rightGap != programsDividerGap {
					t.Errorf("divider spacing: left=%d right=%d, want %d", leftGap, rightGap, programsDividerGap)
				}
				if w.managedList.Bounds().Height < 300 {
					t.Errorf("program list no longer fills the home pane: %+v", w.managedList.Bounds())
				}
				baseNameRow, baseDelay = w.appName.Parent().Bounds(), w.delayEdit.Bounds()
				if w.managedHint.Text() != zh.ManagedListHint {
					t.Errorf("startup list hint = %q", w.managedHint.Text())
				}
				if !w.editor.Visible() || w.editor.Enabled() || w.launchNowBtn.Enabled() || w.argsEdit.Enabled() || w.appEnabled.Enabled() {
					t.Error("without a selection the editor must remain visible but not editable")
				}
				if w.appName.Text() != "" || w.appPath.Text() != "" || w.argsEdit.Text() != "" || w.delayEdit.Text() != "0" || !w.modeTray.Checked() || w.appEnabled.Checked() {
					t.Error("without a selection the editor must show defaults")
				}
				if !w.addProgramBtn.Visible() || !w.addProgramBtn.Enabled() {
					t.Error("Add Program must always be available")
				}
				if !w.managedList.Focused() {
					t.Error("opening the window must focus the program list")
				}
				if w.logonNotice.Visible() || w.settingsView.Visible() {
					t.Error("the program list must open without the sign-in notice or the settings page")
				}
				w.managedList.SetCurrentIndex(0)
			})
			step(func() {
				if w.programsBody.Bounds().X != baseBody.X || w.programsBody.Bounds().Width != baseBody.Width ||
					w.detailPane.Bounds().X != baseDetail.X || w.detailPane.Bounds().Width != baseDetail.Width ||
					w.managedList.Bounds().X != baseList.X || w.managedList.Bounds().Width != baseList.Width {
					t.Errorf("selecting a program moved the fixed home columns: before body=%+v detail=%+v list=%+v after body=%+v detail=%+v list=%+v", baseBody, baseDetail, baseList, w.programsBody.Bounds(), w.detailPane.Bounds(), w.managedList.Bounds())
				}
				if w.detailPane.Bounds().Y != baseDetail.Y || w.appName.Parent().Bounds().Y != baseNameRow.Y || w.delayEdit.Bounds().X != baseDelay.X {
					t.Errorf("selecting a program moved editor controls: detail=%+v name row=%+v delay=%+v", w.detailPane.Bounds(), w.appName.Parent().Bounds(), w.delayEdit.Bounds())
				}
				if w.argsHint.Text() != zh.ManagedArgsHint {
					t.Errorf("argument hint = %q, want %q", w.argsHint.Text(), zh.ManagedArgsHint)
				}
				if w.argsHint.Bounds().Y < w.argsEdit.Bounds().Y+w.argsEdit.Bounds().Height {
					t.Errorf("argument hint is not below its input: edit=%+v hint=%+v", w.argsEdit.Bounds(), w.argsHint.Bounds())
				}
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "programs-zh")
				if !w.editor.Visible() || w.appName.Text() != "Cloud Drive" || w.appPath.Text() != settings.ManagedApps[0].ExePath {
					t.Error("selection did not populate the editor")
				}
				if !w.appEnabled.Checked() || !w.modeTray.Checked() || w.modeNormal.Checked() || w.modeHidden.Checked() {
					t.Error("the editor must show the program's start settings")
				}
				// Each setting starts at the left edge of the editor, with its
				// name above its control.
				for _, widget := range []walk.Widget{w.appEnabledLabel, w.appEnabled, w.appEnabledHint, w.modeLabel, w.delayLabel, w.argsLabel} {
					if x := widget.Bounds().X; x != 0 {
						t.Errorf("%T with text starting at x=%d, want it at the left edge of its group", widget, x)
					}
				}
				if !w.delayBlock.Visible() || w.delayEdit.Text() != "0" {
					t.Error("close delay must be editable for a program closed to the tray")
				}
				for _, tc := range []struct {
					text string
					want int
				}{{"999", 600}, {"30", 30}} {
					w.delayEdit.SetText(tc.text)
					w.delayEdit.SendMessage(win.WM_KEYDOWN, win.VK_RETURN, 0)
					if got := w.settings.ManagedApps[0].TrayBehavior.CloseDelaySeconds; got != tc.want {
						t.Errorf("close delay %q saved as %d, want %d", tc.text, got, tc.want)
					}
				}
				if w.managedListModel.Value(0, 1) != fmt.Sprintf(zh.ManagedAutoHideDelayed, 30) {
					t.Error("the row must show the close delay")
				}
				w.argsEdit.SetFocus()
				if w.managedList.SelectionHiddenWithoutFocus() {
					t.Error("selection must remain visible while editing")
				}
				click(w.modeHidden)
				if w.programsBody.Bounds().X != baseBody.X || w.programsBody.Bounds().Width != baseBody.Width || w.detailPane.Bounds().X != baseDetail.X || w.detailPane.Bounds().Width != baseDetail.Width || w.managedList.Bounds().X != baseList.X || w.managedList.Bounds().Width != baseList.Width {
					t.Errorf("changing start mode moved the fixed home columns")
				}
				if w.detailPane.Bounds().Y != baseDetail.Y || w.appName.Parent().Bounds().Y != baseNameRow.Y || w.delayEdit.Bounds().X != baseDelay.X {
					t.Errorf("changing start mode moved editor controls")
				}
				app := w.settings.ManagedApps[0]
				if !app.LaunchHiddenInBackground || app.TrayBehavior.AutoMinimizeAndHideOnLaunch || w.modeTray.Checked() {
					t.Error("choosing Run hidden must replace closing to the tray")
				}
				if w.delayBlock.Visible() || w.modeHint.Text() != zh.ManagedLaunchHiddenHint || w.managedListModel.Value(0, 1) != zh.ManagedLaunchHidden {
					t.Error("the mode hint, close delay and row must follow the chosen mode")
				}
				click(w.appEnabled)
				if w.settings.ManagedApps[0].RunOnStartup || w.managedListModel.Checked(0) || w.managedListModel.Value(0, 1) != zh.ManagedListParamPausedTemplate {
					t.Error("switching sign-in off must update the program and its row")
				}
				if w.managedList.CurrentIndex() != 0 {
					t.Error("editing must preserve the selection")
				}
				// The list's check box writes through the model, as a click on it does.
				_ = w.managedListModel.SetChecked(0, true)
				if !w.settings.ManagedApps[0].RunOnStartup || !w.appEnabled.Checked() {
					t.Error("the list check box must switch the program on and update the editor")
				}
				_ = w.managedListModel.SetChecked(0, false)
				click(w.launchNowBtn)
				if launches != 1 || w.launchNowBtn.Enabled() {
					t.Error("a program switched off should still launch manually, with busy feedback")
				}
				w.setLaunchNowBusy(false)
				before := saves
				click(w.checkUpdateBtn)
				click(w.checkUpdateBtn)
				if updateChecks != 1 || w.checkUpdateBtn.Enabled() || w.checkUpdateBtn.Text() != zh.CheckUpdateBusy {
					t.Error("the home update button must start one check and show busy feedback")
				}
				if saves != before {
					t.Error("checking for updates must not change settings")
				}
				click(w.settingsBtn)
			})
			step(func() {
				if !w.onSettingsPage || !w.settingsView.Visible() || w.programsView.Visible() || w.headerRow.Visible() || w.settingsTitle.Text() != zh.SettingsTitle {
					t.Error("Settings must open the settings page")
				}
				if !w.backBtn.Visible() || !w.backBtn.Focused() || w.settingsBtn.Visible() {
					t.Error("the settings page must offer and focus the way back")
				}
				for _, toggle := range []*walk.CheckBox{w.runAtLogon, w.hideAtLogon, w.exitOnDone} {
					if toggle.Text() != "" {
						t.Errorf("settings toggle must show only a check box, got %q", toggle.Text())
					}
				}
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "settings-zh")
				if w.intervalEdit.Text() != "3" {
					t.Errorf("default launch interval = %q, want 3", w.intervalEdit.Text())
				}
				for _, tc := range []struct {
					text string
					want int
				}{{"7", 7}, {"0", 0}, {"999", 120}, {"-1", 0}} {
					w.intervalEdit.SetText(tc.text)
					w.intervalEdit.SendMessage(win.WM_KEYDOWN, win.VK_RETURN, 0)
					if w.settings.StartupIntervalSeconds != tc.want {
						t.Errorf("launch interval %q saved as %d, want %d", tc.text, w.settings.StartupIntervalSeconds, tc.want)
					}
				}
				click(w.runAtLogon)
				if w.settings.RunAtLogon || w.hideAtLogonRow.row.Enabled() || w.runAtLogon.Checked() || w.runAtLogon.Text() != "" {
					t.Error("turning sign-in off must save it, show it and disable the options that depend on it")
				}
				click(w.backBtn)
			})
			step(func() {
				if !w.languageCombo.Visible() {
					t.Error("returning home must make the language selector available")
				}
				before := saves
				w.languageCombo.SetCurrentIndex(1)
				if saves != before+1 || w.settings.Language != "en-US" {
					t.Error("changing language on the home page must save it exactly once")
				}
				if w.checkUpdateBtn.Enabled() || w.checkUpdateBtn.Text() != en.CheckUpdateBusy {
					t.Error("changing language must translate and preserve an in-flight update check")
				}
				if w.argsHint.Text() != en.ManagedArgsHint {
					t.Errorf("English argument hint = %q, want %q", w.argsHint.Text(), en.ManagedArgsHint)
				}
				click(w.settingsBtn)
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "settings-en")
				if w.settings.Language != "en-US" || w.settingsTitle.Text() != en.SettingsTitle || w.logonRow.title.Text() != en.RunAtLogon || w.runAtLogon.Text() != "" {
					t.Error("the language change must apply at once")
				}
				w.mw.SetSize(walk.Size{Width: 880, Height: 720})
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "settings-en-min")
				// Completion can arrive while the home page is hidden.
				w.SetCheckUpdateBusy(false)
				click(w.backBtn)
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "programs-en-min")
				if w.onSettingsPage || !w.programsView.Visible() {
					t.Error("Back must return to the program list")
				}
				if !w.checkUpdateBtn.Enabled() || w.checkUpdateBtn.Text() != en.CheckUpdate {
					t.Error("an update completed on the settings page must re-enable its home button")
				}
				click(w.checkUpdateBtn)
				if updateChecks != 2 {
					t.Error("the update button must allow a new check after completion")
				}
				w.SetCheckUpdateBusy(false)
				if w.managedList.CurrentIndex() != 0 || w.managedListModel.Value(0, 1) != en.ManagedListParamPausedTemplate {
					t.Error("the settings page must preserve the selection, and the language change translate the row")
				}
				if !w.logonNotice.Visible() {
					t.Error("the program list must warn while WinTray does not run at sign-in")
				}
				click(w.enableLogonBtn)
				if !w.settings.RunAtLogon || w.logonNotice.Visible() || !w.runAtLogon.Checked() || !w.hideAtLogonRow.row.Enabled() {
					t.Error("the notice must turn running at sign-in back on")
				}
				w.managedList.SetCurrentIndex(1)
				click(w.removeBtn)
				if len(w.settings.ManagedApps) != 2 || w.settings.ManagedApps[1].ID != "3" {
					t.Error("Remove must delete only the selected program")
				}
				click(w.removeBtn)
				w.managedList.SetCurrentIndex(0)
				click(w.removeBtn)
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "empty-en")
				if len(w.settings.ManagedApps) != 0 || !w.emptyList.Visible() || w.programsBody.Visible() {
					t.Error("removing the final program must show the empty state")
				}
				if !w.emptyAddBtn.Focused() || w.emptyTitle.Text() != en.ManagedListEmpty || saves == 0 {
					t.Error("the empty state must offer Add Program, and changes must be saved")
				}
				w.languageCombo.SetCurrentIndex(0)
				click(w.settingsBtn)
				w.mw.SetSize(walk.Size{Width: 1040, Height: 780})
				// Reopening from the tray goes through the same path as the first
				// show, and starts on the program list.
				w.HideMainWindow()
				w.ShowMainWindow()
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "empty-zh")
				if w.onSettingsPage || !w.emptyList.Visible() || !w.emptyAddBtn.Focused() {
					t.Error("reopening must show the program list and focus Add Program")
				}
			})
			w.RequestExplicitClose()
		}()
	})
	w.ShowMainWindow()
	w.Run()
}

// TestSettingsFirstOpenFitsContent verifies that entering settings at a small
// window expands it before requiring a scroll, when the monitor has room.
func TestSettingsFirstOpenFitsContent(t *testing.T) {
	testSettingsFirstOpenFitsContent(t, "")
}

func TestSettingsFirstOpenFitsLongCopy(t *testing.T) {
	testSettingsFirstOpenFitsContent(t, strings.Repeat("Slow programs may take longer to show a window. ", 30))
}

func TestSettingsFirstOpenCapsAtWorkArea(t *testing.T) {
	testSettingsFirstOpenFitsContent(t, strings.Repeat("Slow programs may take longer to show a window. ", 65))
}

func testSettingsFirstOpenFitsContent(t *testing.T, extraHint string) {
	t.Helper()
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	discardStaleMessages()
	defer discardStaleMessages()
	deactivate := activateTestManifest(t)
	defer deactivate()

	settings := config.DefaultSettings()
	settings.Language = "en-US"
	w, err := NewMainWindow(settings, Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.mw.Dispose()
	if extraHint != "" {
		w.retryRow.hint.SetText(w.retryRow.hint.Text() + extraHint)
	}
	w.mw.SetSize(walk.Size{Width: 880, Height: 720})
	initialHeight := w.mw.BoundsPixels().Height
	shownHeight := 0
	w.mw.Starting().Attach(func() {
		shownHeight = w.mw.BoundsPixels().Height
		go func() {
			time.Sleep(200 * time.Millisecond)
			w.synchronize(func() { w.showSettings(true) })
			time.Sleep(300 * time.Millisecond)
			w.synchronize(func() {
				content := w.startupTitle.Parent().(*walk.Composite).Parent().(*walk.Composite)
				monitor := win.MonitorFromWindow(w.mw.Handle(), win.MONITOR_DEFAULTTONEAREST)
				info := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
				if !win.GetMonitorInfo(monitor, &info) {
					t.Error("cannot measure monitor work area")
				}
				shot := "settings-first-open"
				if len(extraHint) > 2000 {
					shot = "settings-first-open-capped"
				} else if extraHint != "" {
					shot = "settings-first-open-long"
				}
				captureTestWindow(t, w, shot)
				if shownHeight != w.mw.BoundsPixels().Height {
					t.Errorf("page switch changed window height: home=%d settings=%d", shownHeight, w.mw.BoundsPixels().Height)
				}
				if content.BoundsPixels().Height > w.settingsView.BoundsPixels().Height && w.mw.BoundsPixels().Height < int(info.RcWork.Bottom-info.RcWork.Top) {
					t.Errorf("first settings view scrolls despite available screen space: content=%+v view=%+v window=%+v work=%+v", content.BoundsPixels(), w.settingsView.BoundsPixels(), w.mw.BoundsPixels(), info.RcWork)
				}
				if w.mw.BoundsPixels().Height > int(info.RcWork.Bottom-info.RcWork.Top) && initialHeight <= int(info.RcWork.Bottom-info.RcWork.Top) {
					t.Errorf("settings window exceeds the monitor work area: window=%+v work=%+v", w.mw.BoundsPixels(), info.RcWork)
				}
				if extraHint != "" && int(info.RcWork.Bottom-info.RcWork.Top) > initialHeight+100 && w.mw.BoundsPixels().Height <= initialHeight+100 {
					t.Error("longer copy did not increase the initial settings window height")
				}
				checkWindowLayout(t, w)
				w.showSettings(false)
				if w.mw.BoundsPixels().Height != shownHeight {
					t.Errorf("returning home changed window height: first=%d returned=%d", shownHeight, w.mw.BoundsPixels().Height)
				}
				w.RequestExplicitClose()
			})
		}()
	})
	w.ShowMainWindow()
	w.Run()
}

// TestCloseDelayRowGeometry verifies on a real window that the close-delay
// editor and its unit pack to the left of their row, below the label, instead
// of drifting apart when nothing in the row can absorb the excess width.
func TestCloseDelayRowGeometry(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	discardStaleMessages()
	defer discardStaleMessages()
	deactivate := activateTestManifest(t)
	defer deactivate()

	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{
		{ID: "1", Name: "Cloud Drive", ExePath: `C:\Apps\Cloud Drive\drive.exe`, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 20}},
	}
	w, err := NewMainWindow(settings, Callbacks{OnSave: func(config.Settings) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.mw.Dispose()

	step := func(f func()) {
		done := make(chan struct{})
		w.synchronize(func() { defer close(done); f() })
		<-done
		time.Sleep(200 * time.Millisecond)
	}
	w.mw.Starting().Attach(func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			step(func() {
				w.managedList.SetCurrentIndex(0)
			})
			step(func() {
				lbl := w.delayLabel.Bounds()
				row := w.delayEdit.Parent().Bounds()
				edit := w.delayEdit.Bounds()
				unit := w.delayUnit.Bounds()
				gap := unit.X - (edit.X + edit.Width)
				if w.delayEdit.Text() != "20" || row.Y < lbl.Y+lbl.Height {
					t.Error("the close delay must show its value below its label")
				}
				// Spacing is 8 at 96 DPI; allow generous DPI headroom but fail
				// on the hundreds of pixels the default centering inserts.
				if edit.X > 1 || gap < -1 || gap > 40 {
					t.Errorf("close-delay editor and unit drifted apart: edit=%+v gap=%d", edit, gap)
				}
				captureTestWindow(t, w, "delay-row")
			})
			w.RequestExplicitClose()
		}()
	})
	w.ShowMainWindow()
	w.Run()
}

// TestBlankAreaClickLeavesEditor posts clicks to the windows the system would
// deliver them to: a hint's text, the window margin, the list rows and the
// space below them, the slot of a disabled button and an enabled field. Only
// blank areas may reset the editor, and only after committing the text
// still being typed.
func TestBlankAreaClickLeavesEditor(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// The UI tests take turns on pooled OS threads.
	discardStaleMessages()
	defer discardStaleMessages()
	deactivate := activateTestManifest(t)
	defer deactivate()

	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{
		{ID: "1", Name: "Cloud Drive", ExePath: `C:\Apps\Cloud Drive\drive.exe`, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}},
		{ID: "2", Name: "Local service", ExePath: `C:\Tools\service.exe`, RunOnStartup: true},
	}
	w, err := NewMainWindow(settings, Callbacks{OnSave: func(config.Settings) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.mw.Dispose()

	step := func(f func()) {
		done := make(chan struct{})
		w.synchronize(func() { defer close(done); f() })
		<-done
		time.Sleep(200 * time.Millisecond)
	}
	click := func(hwnd win.HWND, x, y int) {
		pos := uintptr(win.MAKELONG(uint16(x), uint16(y)))
		win.PostMessage(hwnd, win.WM_LBUTTONDOWN, win.MK_LBUTTON, pos)
		win.PostMessage(hwnd, win.WM_LBUTTONUP, 0, pos)
	}
	editorIsEmpty := func() bool {
		return w.managedList.CurrentIndex() < 0 && w.editor.Visible() && !w.editor.Enabled() && !w.browseLink.Visible() &&
			w.appName.Text() == "" && w.appPath.Text() == "" && w.argsEdit.Text() == "" &&
			w.delayEdit.Text() == "0" && !w.appEnabled.Checked() && w.modeTray.Checked() &&
			!w.launchNowBtn.Enabled() && !w.removeBtn.Enabled()
	}
	// Walk draws the list rows in a native listview beside a zero-width one
	// kept for frozen columns.
	rowView := func() win.HWND {
		for child := win.GetWindow(w.managedList.Handle(), win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
			var r win.RECT
			if win.GetClientRect(child, &r) && r.Right > 0 {
				return child
			}
		}
		t.Error("test setup: the list's row view was not found")
		return 0
	}
	w.mw.Starting().Attach(func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			step(func() {
				w.managedList.SetCurrentIndex(0)
			})
			step(func() {
				w.argsEdit.SetFocus()
				w.argsEdit.SetText("--minimized")
				click(win.GetWindow(w.delayHint.Handle(), win.GW_CHILD), 2, 2)
			})
			step(func() {
				if got := w.settings.ManagedApps[0].Args; got != "--minimized" {
					t.Errorf("arguments being typed = %q after a blank click, want them committed", got)
				}
				if !editorIsEmpty() {
					t.Error("clicking a hint must reset and disable the program editor")
				}
				captureTestWindow(t, w, "programs-unselected")
				if w.argsEdit.Focused() {
					t.Error("a blank click must take focus out of the field being edited")
				}
				w.managedList.SetCurrentIndex(1)
				// A launch in progress disables Launch Now.
				w.setLaunchNowBusy(true)
			})
			step(func() {
				if w.launchNowBtn.Enabled() {
					t.Error("test setup: Launch Now should be disabled")
				}
				launch := w.launchNowBtn.BoundsPixels()
				click(w.launchNowBtn.Parent().Handle(), launch.X+launch.Width/2, launch.Y+launch.Height/2)
				click(w.argsEdit.Handle(), 4, 4)
			})
			step(func() {
				w.setLaunchNowBusy(false)
				if w.managedList.CurrentIndex() != 1 || w.appPath.Text() != settings.ManagedApps[1].ExePath {
					t.Error("clicking a disabled button or an enabled field must keep the program selected")
				}
				// The window margin belongs to the client area behind every page.
				click(win.GetParent(w.pageTitle.Parent().Handle()), 2, 2)
			})
			step(func() {
				if !editorIsEmpty() {
					t.Error("clicking the window margin must return the program editor to its empty state")
				}
				w.managedList.SetCurrentIndex(0)
			})
			step(func() {
				rows := rowView()
				var bounds win.RECT
				win.GetClientRect(rows, &bounds)
				click(rows, 40, int(bounds.Bottom)-8)
			})
			step(func() {
				if !editorIsEmpty() {
					t.Error("clicking the list below its rows must return the program editor to its empty state")
				}
				rows := rowView()
				item := win.RECT{Left: win.LVIR_BOUNDS}
				win.SendMessage(rows, win.LVM_GETITEMRECT, 1, uintptr(unsafe.Pointer(&item)))
				click(rows, int(item.Left+item.Right)/2, int(item.Top+item.Bottom)/2)
			})
			step(func() {
				if w.managedList.CurrentIndex() != 1 || w.appPath.Text() != settings.ManagedApps[1].ExePath {
					t.Error("clicking a program row must select it and fill the editor")
				}
				w.showSettings(true)
				click(w.logonRow.row.Handle(), 2, 2)
			})
			step(func() {
				w.showSettings(false)
				if w.managedList.CurrentIndex() != 1 {
					t.Error("a blank click on the settings page must keep the program selected")
				}
			})
			w.RequestExplicitClose()
		}()
	})
	w.ShowMainWindow()
	w.Run()
}

// discardStaleMessages empties what RequestExplicitClose leaves on a pooled OS
// thread: messages for the destroyed window and the WM_QUIT that only surfaces
// behind them. The next window on the thread would otherwise end its loop
// early, and its test would pass without checking anything, or hang. Peeking
// cannot remove WM_PAINT, which only comes after any pending WM_QUIT.
func discardStaleMessages() {
	var msg win.MSG
	for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) && msg.Message != win.WM_QUIT && msg.Message != win.WM_PAINT {
	}
}

func activateTestManifest(t *testing.T) func() {
	t.Helper()
	path, err := filepath.Abs("../../build/WinTray.exe.manifest")
	if err != nil {
		t.Fatal(err)
	}
	source, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := win.CreateActCtx(&win.ACTCTX{Source: source})
	if ctx == win.HANDLE(windows.InvalidHandle) {
		t.Fatal("could not create the common-controls activation context")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	cookie, ok := win.ActivateActCtx(ctx)
	if !ok {
		kernel.NewProc("ReleaseActCtx").Call(uintptr(ctx))
		t.Fatal("could not activate the common-controls manifest")
	}
	return func() {
		kernel.NewProc("DeactivateActCtx").Call(0, cookie)
		kernel.NewProc("ReleaseActCtx").Call(uintptr(ctx))
	}
}

// checkWindowLayout checks the visible page: every control inside its parent,
// and no button, label or choice cut short in either language or size.
func checkWindowLayout(t *testing.T, w *MainWindow) {
	t.Helper()
	w.mw.ForEachDescendant(func(widget walk.Widget) bool {
		if !widget.Visible() {
			return false
		}
		bounds := widget.Bounds()
		parent := widget.Parent().ClientBounds()
		if bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.Width > parent.Width+1 || bounds.Y+bounds.Height > parent.Height+1 {
			t.Errorf("%T outside parent: bounds=%+v parent=%+v", widget, bounds, parent)
		}
		return true
	})
	clipped := func(kind string, widget walk.Widget, text string) {
		if widget.Visible() && widget.Bounds().Width < widget.SizeHint().Width {
			t.Errorf("%s %q clipped: width=%d needs=%d", kind, text, widget.Bounds().Width, widget.SizeHint().Width)
		}
	}
	for _, button := range []*walk.PushButton{w.backBtn, w.settingsBtn, w.enableLogonBtn, w.addProgramBtn, w.emptyAddBtn, w.removeBtn, w.launchNowBtn, w.silentBtn, w.exitBtn, w.checkUpdateBtn, w.openLogsBtn, w.cleanupBtn} {
		clipped("button", button, button.Text())
	}
	for _, label := range []*walk.Label{w.pageTitle, w.settingsTitle, w.versionLabel, w.intervalUnit, w.retryUnit, w.delayUnit} {
		clipped("label", label, label.Text())
	}
	clipped("link", w.githubLink, w.githubLink.Text())
	for _, widget := range []walk.Widget{w.languageCombo, w.versionLabel, w.githubLink, w.checkUpdateBtn} {
		if widget.Visible() != !w.onSettingsPage {
			t.Errorf("%T visibility = %t, want home utilities visible only on the home page", widget, widget.Visible())
		}
	}
	if want := fmt.Sprintf(i18n.For(w.settings.Language).VersionLabel, version.Number); w.versionLabel.Text() != want {
		t.Errorf("version label = %q, want %q", w.versionLabel.Text(), want)
	}
	rows := []*settingRow{w.logonRow, w.hideAtLogonRow, w.exitOnDoneRow, w.intervalRow, w.retryRow, w.logsRow, w.cleanupRow}
	for _, row := range rows {
		clipped("setting", row.title, row.title.Text())
	}
	// Every setting's control lines up on the right of its row, nested
	// settings included, so each one reads as belonging to its row.
	if w.onSettingsPage {
		content := w.startupTitle.Parent().(*walk.Composite).Parent().(*walk.Composite)
		wrapper := content.Parent()
		left := content.Bounds().X
		rightMargin := wrapper.ClientBounds().Width - left - content.Bounds().Width
		if diff := left - rightMargin; diff < -1 || diff > 1 {
			t.Errorf("settings block not centered: left=%d right=%d content=%+v wrapper=%+v", left, rightMargin, content.Bounds(), wrapper.ClientBounds())
		}
		header := w.backBtn.Parent().(*walk.Composite)
		if header.Parent() != content || header.Bounds().X != 0 || w.backBtn.Bounds().X != 0 || w.settingsTitle.Bounds().X <= w.backBtn.Bounds().X {
			t.Errorf("settings header must start at the content's left edge: back=%+v title=%+v", w.backBtn.Bounds(), w.settingsTitle.Bounds())
		}
		right := func(row *settingRow) int { return row.controls.Bounds().X + row.controls.Bounds().Width }
		for _, row := range rows[1:] {
			if diff := right(row) - right(rows[0]); diff < -1 || diff > 1 {
				t.Errorf("control of %q ends at %d, want %d like the others", row.title.Text(), right(row), right(rows[0]))
			}
		}
	}
	for _, box := range []*walk.CheckBox{w.appEnabled, w.runAtLogon, w.hideAtLogon, w.exitOnDone} {
		clipped("check box", box, box.Text())
	}
	for _, button := range []*walk.RadioButton{w.modeTray, w.modeNormal, w.modeHidden} {
		clipped("start mode", button, button.Text())
	}
}

func captureTestWindow(t *testing.T, w *MainWindow, name string) {
	t.Helper()
	dir := os.Getenv("WINTRAY_UI_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Error(err)
		return
	}
	bitmap, err := walk.NewBitmapFromWindow(w.mw)
	if err != nil {
		t.Error(err)
		return
	}
	defer bitmap.Dispose()
	img, err := bitmap.ToImage()
	if err != nil {
		t.Error(err)
		return
	}
	// GDI window captures do not populate the alpha channel.
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	file, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s.png", name)))
	if err != nil {
		t.Error(err)
		return
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Error(err)
	}
}
