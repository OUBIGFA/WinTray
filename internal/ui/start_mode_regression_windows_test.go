//go:build windows

package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/i18n"
)

func TestNativeStartupModeReplacesSwitch(t *testing.T) {
	w := nativeProgramsFixture(1)
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
	}
	if startModeOf(w.settings.ManagedApps[0]) != startNormal {
		t.Fatal("cannot switch startup mode without the removed startup switch")
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
