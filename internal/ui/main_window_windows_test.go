//go:build windows

package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/i18n"
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
	deactivate := activateTestManifest(t)
	defer deactivate()

	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{
		{ID: "1", Name: "Cloud Drive", ExePath: `C:\Apps\Cloud Drive\drive.exe`, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}},
		{ID: "2", Name: "Backup script", ExePath: `C:\Scripts\backup.ps1`, RunOnStartup: true, LaunchHiddenInBackground: true},
		{ID: "3", Name: "Local service", ExePath: `C:\Tools\service.exe`},
	}
	saves, launches := 0, 0
	w, err := NewMainWindow(settings, Callbacks{
		OnSave: func(config.Settings) { saves++ },
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
	w.mw.Starting().Attach(func() {
		go func() {
			time.Sleep(200 * time.Millisecond)
			step(func() {
				if w.removeBtn.Enabled() || w.browseBtn.Enabled() || w.launchNowBtn.Enabled() {
					t.Error("selection actions must be disabled without a selection")
				}
				if !w.addProgramBtn.Visible() || !w.addProgramBtn.Enabled() {
					t.Error("Add Program must always be available")
				}
				if w.languageCombo.Focused() || !w.managedList.Focused() {
					t.Error("opening the window must focus the program list, not the language selector")
				}
				w.managedList.SetCurrentIndex(0)
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "programs-zh")
				if !w.removeBtn.Enabled() || w.pathEdit.Text() != settings.ManagedApps[0].ExePath {
					t.Error("selection did not populate the editor")
				}
				if !w.delayEdit.Enabled() || w.delayEdit.Text() != "0" {
					t.Error("close delay must be editable for a close-window program")
				}
				w.delayEdit.SetText("30")
				w.delayEdit.SendMessage(win.WM_KEYDOWN, win.VK_RETURN, 0)
				if w.settings.ManagedApps[0].TrayBehavior.CloseDelaySeconds != 30 || w.managedListModel.Value(0, 2) != "启动后关闭窗口 (延迟 30 秒)" {
					t.Error("close delay must be saved and shown in the row")
				}
				w.argsEdit.SetFocus()
				if w.managedList.SelectionHiddenWithoutFocus() {
					t.Error("selection must remain visible while editing")
				}
				w.appLaunchHidden.SetChecked(true)
				if w.appAutoHide.Enabled() || w.appAutoHide.Checked() || w.delayEdit.Enabled() || !w.settings.ManagedApps[0].LaunchHiddenInBackground {
					t.Error("hidden launch must disable and clear close-window behavior and its delay")
				}
				w.appPauseTask.SetChecked(true)
				if w.managedList.CurrentIndex() != 0 || w.managedListModel.Value(0, 2) != "已暂停" {
					t.Error("editing rules must preserve the selection and update the row")
				}
				w.launchNowBtn.SendMessage(win.BM_CLICK, 0, 0)
				if launches != 1 || w.launchNowBtn.Enabled() {
					t.Error("paused program should still launch manually, with busy feedback")
				}
				w.setLaunchNowBusy(false)
				w.languageCombo.SetCurrentIndex(1)
				w.mw.SetSize(walk.Size{Width: 980, Height: 720})
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "programs-en-min")
				if w.managedList.CurrentIndex() != 0 || w.managedListModel.Value(0, 2) != "Paused" {
					t.Error("language change must preserve selection and translate the row")
				}
				w.managedList.SetCurrentIndex(1)
				w.removeBtn.SendMessage(win.BM_CLICK, 0, 0)
				if len(w.settings.ManagedApps) != 2 || w.settings.ManagedApps[1].ID != "3" {
					t.Error("Remove must delete only the selected program")
				}
				w.removeBtn.SendMessage(win.BM_CLICK, 0, 0)
				w.managedList.SetCurrentIndex(0)
				w.removeBtn.SendMessage(win.BM_CLICK, 0, 0)
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "empty-en")
				if len(w.settings.ManagedApps) != 0 || !w.emptyList.Visible() || w.managedList.Visible() {
					t.Error("removing the final program must show the empty state")
				}
				if w.removeBtn.Enabled() || w.browseBtn.Enabled() || w.launchNowBtn.Enabled() || w.argsEdit.Enabled() || w.delayEdit.Enabled() {
					t.Error("empty-state selection actions must be disabled")
				}
				if w.noSelectLabel.Text() != i18n.For("en-US").ManagedSelectionHint || saves == 0 {
					t.Error("missing selection guidance or save callbacks")
				}
				w.languageCombo.SetCurrentIndex(0)
				w.mw.SetSize(walk.Size{Width: 1040, Height: 780})
				// Reopening from the tray goes through the same path as the first show.
				w.HideMainWindow()
				w.ShowMainWindow()
			})
			step(func() {
				checkWindowLayout(t, w)
				captureTestWindow(t, w, "empty-zh")
				if w.languageCombo.Focused() || !w.addProgramBtn.Focused() {
					t.Error("reopening with an empty list must focus Add Program, not the language selector")
				}
			})
			w.RequestExplicitClose()
		}()
	})
	w.ShowMainWindow()
	w.Run()
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
	for _, button := range []*walk.PushButton{w.addProgramBtn, w.removeBtn, w.browseBtn, w.launchNowBtn, w.openLogsBtn, w.cleanupBtn, w.checkUpdateBtn, w.exitBtn} {
		if button.Bounds().Width < button.SizeHint().Width {
			t.Errorf("button %q clipped: width=%d needs=%d", button.Text(), button.Bounds().Width, button.SizeHint().Width)
		}
	}
	t.Logf("layout %s: window=%+v list=%+v", w.settings.Language, w.mw.Size(), w.managedList.Bounds())
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
