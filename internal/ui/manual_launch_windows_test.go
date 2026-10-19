//go:build windows

package ui

import (
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

// A launch belongs to its program, not to whichever row is selected while
// the worker is running. Other programs must keep their own launch action.
func TestNativeLaunchNowBusyIsPerProgram(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			w := nativeProgramsFixture(3)
			w.settings.Language = language
			w.settings.ManagedApps[1].RunOnStartup = false
			var launched []string
			w.callbacks.OnLaunchNow = func(entry config.ManagedAppEntry) {
				launched = append(launched, entry.ID)
			}
			tt := native.NewTester(w.nativeView, 1200, 850)
			m := i18n.For(language)
			click := func(label string) {
				t.Helper()
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
			}
			click("Program 000")
			click(m.ManagedLaunchNow)
			if !tt.HasText(m.ManagedLaunchNowBusy) {
				t.Fatal("the launching program did not show its busy state")
			}
			click(m.ManagedLaunchNowBusy)
			if len(launched) != 1 || launched[0] != "0" {
				t.Fatalf("busy program accepted a duplicate launch: %v", launched)
			}

			click("Program 001")
			if !tt.HasText(m.ManagedLaunchNow) || tt.HasText(m.ManagedLaunchNowBusy) {
				t.Fatal("another program inherited the first program's busy state")
			}
			click(m.ManagedLaunchNow)
			if len(launched) != 2 || launched[1] != "1" {
				t.Fatalf("another program could not launch independently: %v", launched)
			}
			click("Program 002")
			if !tt.HasText(m.ManagedLaunchNow) || tt.HasText(m.ManagedLaunchNowBusy) {
				t.Fatal("pending launches disabled an unrelated program")
			}
			for _, name := range []string{"Program 000", "Program 001"} {
				click(name)
				if !tt.HasText(m.ManagedLaunchNowBusy) {
					t.Fatalf("%s lost its pending launch state after selection changed", name)
				}
			}

			w.FinishLaunchNow("0")
			w.drain()
			tt.Frame()
			if !tt.HasText(m.ManagedLaunchNowBusy) {
				t.Fatal("finishing the first program unlocked the second program")
			}
			click("Program 000")
			if !tt.HasText(m.ManagedLaunchNow) || tt.HasText(m.ManagedLaunchNowBusy) {
				t.Fatal("the completed program's launch action was not restored")
			}
			click(m.ManagedColumnOrder + " Program 001")
			tt.Key(0, native.KeyHome)
			tt.Key(0, native.KeyEnter)
			click("Program 001")
			if !tt.HasText(m.ManagedLaunchNowBusy) {
				t.Fatalf("reordering a pending program lost its busy state: selected=%d pending=%v apps=%+v", w.desktop.selected, w.launchNowBusy, w.settings.ManagedApps)
			}
			w.FinishLaunchNow("1")
			w.drain()
			tt.Frame()
			click(m.ManagedLaunchNow)
			if len(launched) != 3 || launched[2] != "1" {
				t.Fatalf("completed program could not launch again after reordering: %v", launched)
			}
		})
	}
}

// A running program must expose the stop action without affecting another row.
func TestNativeRunningProgramUsesStopAction(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			w := nativeProgramsFixture(1)
			w.settings.Language = language
			running := true
			stops := 0
			launches := 0
			w.callbacks.OnProgramRunning = func(config.ManagedAppEntry) bool { return running }
			w.callbacks.OnStopNow = func(config.ManagedAppEntry) { stops++ }
			w.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { launches++ }
			tt := native.NewTester(w.nativeView, 1200, 850)
			m := i18n.For(language)
			if err := tt.Click("Program 000"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click(m.ManagedStopProgram); err != nil {
				t.Fatal(err)
			}
			if stops != 1 || !tt.HasText(m.ManagedStopProgramBusy) || tt.HasText(m.ManagedLaunchNow) {
				t.Fatalf("running program did not switch to the stop action: stops=%d texts=%v", stops, tt.Texts())
			}
			running = false
			w.FinishStopNow("0")
			w.drain()
			tt.Frame()
			if !tt.HasText(m.ManagedLaunchNow) || tt.HasText(m.ManagedStopProgram) {
				t.Fatalf("stopped program did not restore launch action: texts=%v", tt.Texts())
			}
			if err := tt.Click(m.ManagedLaunchNow); err != nil {
				t.Fatal(err)
			}
			if launches != 1 {
				t.Fatalf("restored launch action did not launch: launches=%d", launches)
			}
		})
	}
}
