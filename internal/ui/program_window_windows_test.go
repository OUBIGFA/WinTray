//go:build windows

package ui

import (
	"fmt"
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestNativeWindowActionBesideLaunchNow(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		for _, size := range [][2]int{{1040, 720}, {1200, 850}} {
			t.Run(fmt.Sprintf("%s/%dx%d", lang, size[0], size[1]), func(t *testing.T) {
				w := nativeProgramsFixture(1)
				w.settings.Language = lang
				w.settings.ManagedApps[0].Name = "Background script with a long program name"
				w.settings.ManagedApps[0].ExePath = `C:\Scripts\background.py`
				w.settings.ManagedApps[0].LaunchHiddenInBackground = true
				w.settings.ManagedApps[0].TrayBehavior.AutoMinimizeAndHideOnLaunch = false
				tt := native.NewTester(w.nativeView, size[0], size[1])
				if err := tt.Click(w.settings.ManagedApps[0].Name); err != nil {
					t.Fatal(err)
				}
				m := i18n.For(lang)
				show, found := tt.Find(m.HostedShowWindow)
				launch, hasLaunch := tt.Find(m.ManagedLaunchNow)
				if !found || !hasLaunch || show.Y != launch.Y || show.X+show.W > launch.X || launch.X+launch.W > float32(size[0]) || show.H < 24 {
					t.Fatalf("window action not visible beside Launch Now: show=%+v launch=%+v", show, launch)
				}
				tt.Move(-1, -1)
				captureNativeTest(t, tt, fmt.Sprintf("window-action-%s-%d", lang, size[0]))
			})
		}
	}
}

func TestNativeWindowActionPreservesStartupAndTracksLiveWindow(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			w := nativeProgramsFixture(2)
			w.settings.Language = lang
			w.settings.ManagedApps[0].RunOnStartup = false
			before := slices.Clone(w.settings.ManagedApps)
			visible := map[string]bool{}
			var calls []string
			w.callbacks.OnSave = func(config.Settings) { t.Error("window action saved startup settings") }
			w.callbacks.OnProgramWindowVisible = func(entry config.ManagedAppEntry) bool { return visible[entry.ID] }
			w.callbacks.OnSetProgramWindowVisible = func(entry config.ManagedAppEntry, show bool) {
				calls = append(calls, fmt.Sprintf("%s/%t", entry.ID, show))
				visible[entry.ID] = show
			}
			tt := native.NewTester(w.nativeView, 1200, 850)
			m := i18n.For(lang)
			click := func(label string) {
				t.Helper()
				if err := tt.Click(label); err != nil {
					t.Fatal(err)
				}
			}
			// The illustrative entry has no actions before selection.
			click(m.HostedShowWindow)
			if len(calls) != 0 {
				t.Fatal("unselected example accepted a window action")
			}
			click("Program 000")
			click(m.HostedShowWindow)
			click(m.ManagedWindowBusy)
			if len(calls) != 1 {
				t.Fatalf("busy window action repeated: %v", calls)
			}
			click("Program 001")
			click(m.HostedShowWindow)
			if len(calls) != 2 {
				t.Fatalf("another program inherited busy state: %v", calls)
			}
			w.FinishProgramWindow("0")
			w.drain()
			tt.Frame()
			if !tt.HasText(m.ManagedWindowBusy) {
				t.Fatal("finishing another program unlocked this one")
			}
			click("Program 000")
			click(m.HostedHideWindow)
			w.FinishProgramWindow("0")
			w.drain()
			tt.Frame()
			// A tray/menu change is reflected through the live query, not the
			// previous button click. No global timer or process scan is needed.
			visible["0"] = true
			tt.Frame()
			if !tt.HasText(m.HostedHideWindow) {
				t.Fatal("button label ignored external window visibility")
			}
			visible["0"] = false
			tt.Frame()
			if !tt.HasText(m.HostedShowWindow) {
				t.Fatal("hidden/minimized window retained Hide Window")
			}
			if !slices.Equal(calls, []string{"0/true", "1/true", "0/false"}) || !slices.Equal(before, w.settings.ManagedApps) {
				t.Fatalf("window action changed program or settings: calls=%v apps=%+v", calls, w.settings.ManagedApps)
			}
		})
	}
}
