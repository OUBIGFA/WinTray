//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"wintray/internal/config"
)

// A real mygo window, isolated from user settings and startup registration.
func TestBackgroundWindowBuildsLatestSettingsOnFirstOpen(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive desktop")
	}
	if runWindowTestInChild(t) {
		return
	}
	runTestMainThread(func() {
		initial := nativeProgramsFixture(3).settings
		initial.ManagedApps[0].ExePath = filepath.Join(os.Getenv("WINDIR"), "notepad.exe")
		initial.ManagedApps[0].Schedule = config.Schedule{Enabled: true, AutoExitMinutes: 30, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}
		saved := 0
		w, err := NewBackgroundWindow(initial, Callbacks{OnSave: func(config.Settings) { saved++ }})
		if err != nil {
			t.Error(err)
			return
		}
		if w.mw != nil || w.desktop.window != nil {
			t.Error("background constructor created desktop resources")
		}
		w.SetLanguage("en-US")
		w.SetCollectedTrayIcon("0", true)
		w.TurnOffRunAtLogon()
		w.OnStarting(func() {
			go func() {
				step := func(f func()) {
					done := make(chan struct{})
					w.Synchronize(func() { defer close(done); f() })
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("desktop did not dispatch")
					}
				}
				capture := func(name string) {
					if dir := os.Getenv("WINTRAY_UI_SCREENSHOTS"); dir != "" {
						data, err := w.desktop.window.CapturePage()
						if err != nil {
							t.Error(err)
							return
						}
						if err := os.MkdirAll(dir, 0755); err != nil {
							t.Error(err)
							return
						}
						if err := os.WriteFile(filepath.Join(dir, name+".png"), data, 0600); err != nil {
							t.Error(err)
						}
					}
				}
				w.ShowMainWindow()
				time.Sleep(150 * time.Millisecond)
				step(func() {
					if w.desktop.window == nil || !w.desktop.window.IsVisible() {
						t.Error("desktop was not shown")
						return
					}
					if w.settings.Language != "en-US" || w.settings.RunAtLogon || !w.settings.ManagedApps[0].CollectTrayIcon {
						t.Error("background edits lost")
					}
					w.desktop.selected = 0
				})
				time.Sleep(100 * time.Millisecond)
				step(func() { capture("live-programs"); w.onSettingsPage = true })
				time.Sleep(100 * time.Millisecond)
				step(func() {
					capture("live-settings")
					w.HideMainWindow()
					if w.desktop.window != nil || len(w.desktop.icons) != 0 {
						t.Error("hidden desktop retained rendering resources")
					}
				})
				w.ShowMainWindow()
				time.Sleep(100 * time.Millisecond)
				step(func() {
					if w.desktop.window == nil || !w.desktop.window.IsVisible() || w.onSettingsPage {
						t.Error("reopen did not restore programs page")
					}
					if saved != 1 {
						t.Errorf("opening caused extra saves: %d", saved)
					}
				})
				w.RequestExplicitClose()
			}()
		})
		if code := w.Run(); code != 0 {
			t.Errorf("desktop exit %d", code)
		}
	})
}
