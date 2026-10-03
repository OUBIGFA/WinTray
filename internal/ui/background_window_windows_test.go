//go:build windows

package ui

import (
	"os"
	"runtime"
	"testing"

	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestBackgroundWindowBuildsLatestSettingsOnFirstOpen(t *testing.T) {
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
	settings.ManagedApps = []config.ManagedAppEntry{{ID: "app", Name: "Deferred program", ExePath: `C:\Apps\deferred.exe`}}
	saves := 0
	w, err := NewBackgroundWindow(settings, Callbacks{OnSave: func(config.Settings) { saves++ }})
	if err != nil {
		t.Fatal(err)
	}
	defer w.mw.Dispose()
	if w.managedList != nil || w.settingsView != nil {
		t.Fatal("background startup eagerly constructed settings controls")
	}
	// State arriving before the UI exists must survive first construction.
	w.setLaunchNowBusy(true)
	w.setCheckUpdateBusy(true)
	w.applyLanguage("en-US")
	w.SetCollectedTrayIcon("app", true)
	w.TurnOffRunAtLogon()
	w.mw.Starting().Attach(func() {
		w.ShowMainWindow()
		w.synchronize(func() {
			if w.managedList == nil || w.settingsView == nil {
				t.Error("first open did not construct settings")
				w.RequestExplicitClose()
				return
			}
			if w.managedListModel.RowCount() != 1 || !w.managedListModel.rows[0].Collected || w.runAtLogon.Checked() {
				t.Error("changes made in the background were lost")
			}
			en := i18n.For("en-US")
			if w.settingsBtn.Text() != en.OpenSettings || w.checkUpdateBtn.Enabled() || w.launchNowBtn.Text() != en.ManagedLaunchNowBusy {
				t.Error("language or pending operations were lost on first open")
			}
			if saves != 1 {
				t.Errorf("opening settings caused unexpected saves: %d", saves)
			}
			list := w.managedList
			w.HideMainWindow()
			w.ShowMainWindow()
			w.synchronize(func() {
				if w.managedList != list {
					t.Error("reopening duplicated the settings controls")
				}
				w.RequestExplicitClose()
			})
		})
	})
	w.Run()
}
