//go:build windows

package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	native "github.com/egoist/mygo/ui"

	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestNativeDisablingSchedulePersistsWithoutClearingValues(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, runOnStartup := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/startup_%t", language, runOnStartup), func(t *testing.T) {
				w := nativeProgramsFixture(2)
				w.settings.Language = language
				w.settings.ManagedApps[0].RunOnStartup = runOnStartup
				schedule := config.Schedule{
					Enabled: true, StartDelayMinutes: 10, AutoExitMinutes: 30,
					FrequencyEnabled: true, FrequencyDays: 7, FrequencyRuns: 2,
				}
				for i := range w.settings.ManagedApps {
					w.settings.ManagedApps[i].Schedule = schedule
				}
				store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
				w.callbacks.OnSave = func(s config.Settings) {
					if err := store.Save(s); err != nil {
						t.Fatal(err)
					}
				}
				w.save()
				tt := native.NewTester(w.nativeView, 1200, 850)
				m := i18n.For(language)
				for _, label := range []string{"Program 000", m.ManagedSchedule} {
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
				}
				assertDisabled := func(stage string, settings config.Settings) {
					t.Helper()
					entry := settings.ManagedApps[0]
					want := schedule
					want.Enabled = false
					if entry.Schedule != want || entry.RunOnStartup != runOnStartup {
						t.Fatalf("%s changed more than the schedule switch: %+v", stage, entry)
					}
					if config.ScheduledRunLimit(entry) != 0 || config.ScheduledStartDelaySeconds(entry) != 0 || config.StartupFrequencyEnabled(entry) {
						t.Fatalf("%s kept disabled schedule actions active: %+v", stage, entry.Schedule)
					}
					if settings.ManagedApps[1].Schedule != schedule {
						t.Fatalf("%s changed another program's schedule", stage)
					}
				}
				assertDisabled("immediately after click", w.Settings())
				if _, visible := tt.Find(m.ManagedScheduleExit); visible {
					t.Fatal("disabled schedule still shows its time fields")
				}
				// Rebuilding the editor and committing old controls must not
				// restore the switch or another program's schedule state.
				for _, label := range []string{"Program 001", "Program 000"} {
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
				}
				w.commitNativeEdits()
				assertDisabled("after selection change", w.Settings())
				reloaded, err := store.LoadWithError()
				if err != nil {
					t.Fatal(err)
				}
				assertDisabled("after reload", reloaded)
			})
		}
	}
}
