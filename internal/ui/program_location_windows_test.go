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

func TestNativeProgramLocationBesideWindowAction(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, width := range []int{1040, 1200} {
			for _, visible := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/visible=%t", language, width, visible), func(t *testing.T) {
					window := nativeProgramsFixture(1)
					window.settings.Language = language
					window.callbacks.OnProgramWindowVisible = func(config.ManagedAppEntry) bool { return visible }
					tester := native.NewTester(window.nativeView, width, 850)
					messages := i18n.For(language)
					if tester.HasText(messages.ManagedOpenLocation) {
						t.Fatal("folder action exists without a selected program")
					}
					if err := tester.Click("Program 000"); err != nil {
						t.Fatal(err)
					}
					label := messages.HostedShowWindow
					if visible {
						label = messages.HostedHideWindow
					}
					show, hasShow := tester.Find(label)
					folder, hasFolder := tester.Find(messages.ManagedOpenLocation)
					remove, hasRemove := tester.Find(messages.ManagedColumnDelete + " Program 000")
					if !hasShow || !hasFolder || !hasRemove || folder.X < show.X+show.W || folder.X+folder.W > remove.X || absFloat(folder.Y+folder.H/2-show.Y-show.H/2) > 1 || folder.W > 40 {
						t.Fatalf("folder icon is not directly right of window action: show=%+v folder=%+v remove=%+v", show, folder, remove)
					}
					captureNativeTest(t, tester, fmt.Sprintf("program-location-%s-%d-%t", language, width, visible))
				})
			}
		}
	}
}

func TestNativeProgramLocationUsesSelectedAppWithoutLaunching(t *testing.T) {
	window := nativeProgramsFixture(2)
	window.settings.ManagedApps[0].WorkingDirectory = "C:/Other/data"
	window.settings.ManagedApps[1].ExePath = "shell:AppsFolder\\Example.App_123456789abcd!Reader"
	before := slices.Clone(window.settings.ManagedApps)
	var opened []config.ManagedAppEntry
	window.callbacks.OnOpenProgramLocation = func(entry config.ManagedAppEntry) { opened = append(opened, entry) }
	window.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { t.Fatal("opening the folder launched the program") }
	window.callbacks.OnSave = func(config.Settings) { t.Fatal("opening the folder changed settings") }
	tester := native.NewTester(window.nativeView, 1200, 850)
	messages := i18n.For("zh-CN")
	for _, entry := range before {
		for _, label := range []string{entry.Name, messages.ManagedOpenLocation} {
			if err := tester.Click(label); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !slices.Equal(opened, before) || !slices.Equal(window.settings.ManagedApps, before) {
		t.Fatalf("folder action changed the target or settings: opened=%+v", opened)
	}
}
