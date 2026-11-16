//go:build windows

package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestNativeStartupModeReplacesSwitch(t *testing.T) {
	w := nativeProgramsFixture(1)
	schedule := config.Schedule{Enabled: true, StartDelayMinutes: 5, AutoExitMinutes: 30}
	w.settings.ManagedApps[0].Schedule = schedule
	tester := native.NewTester(w.nativeView, 1200, 850)
	if err := tester.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	messages := i18n.For("zh-CN")
	if _, found := tester.Find(messages.ManagedEnabled); found {
		t.Fatal("removed per-program startup switch is still visible")
	}
	normal, found := tester.Find(messages.ManagedLaunchOnly)
	tray, hasTray := tester.Find(messages.ManagedAutoHide)
	if !found || !hasTray || normal.X >= tray.X {
		t.Fatalf("normal mode must be the first choice: normal=%+v tray=%+v", normal, tray)
	}
	for _, label := range []string{messages.ManagedLaunchOnly, messages.ManagedLaunchHidden, messages.ManagedLaunchOnly} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		entry := w.Settings().ManagedApps[0]
		if entry.Schedule != schedule || !config.ShouldLaunchViaWinTray(entry) {
			t.Fatalf("switching startup mode disabled the custom schedule: %+v", entry)
		}
	}
	if startModeOf(w.settings.ManagedApps[0]) != startNormal {
		t.Fatal("cannot switch startup mode without the removed startup switch")
	}
	if err := tester.Click(messages.ManagedSchedule); err != nil {
		t.Fatal(err)
	}
	entry := w.Settings().ManagedApps[0]
	schedule.Enabled = false
	if entry.Schedule != schedule || config.ShouldLaunchViaWinTray(entry) || len(config.LogonTaskApps(w.Settings())) != 0 {
		t.Fatalf("normal mode kept automatic startup after disabling its schedule: %+v", entry)
	}
}

func TestNativeAddedProgramsDefaultToNormal(t *testing.T) {
	w := nativeProgramsFixture(0)
	w.nativeAddPaths([]string{"C:/Apps/demo.exe", "C:/Scripts/demo.cmd"}, false)
	for _, entry := range w.settings.ManagedApps {
		if startModeOf(entry) != startNormal || entry.RunOnStartup {
			t.Fatalf("new program does not default to normal: %+v", entry)
		}
	}
}
