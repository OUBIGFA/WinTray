//go:build windows

package ui

import (
	"fmt"
	"reflect"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestNativeProgramLocationBeforeFilePath(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, width := range []int{860, 1040, 1200} {
			for _, visible := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/visible=%t", language, width, visible), func(t *testing.T) {
					window := nativeProgramsFixture(1)
					window.settings.Language = language
					window.callbacks.OnProgramWindowVisible = func(config.ManagedAppEntry) bool { return visible }
					tester := native.NewTester(window.nativeView, width, 850)
					messages := i18n.For(language)
					if tester.HasText(messages.ReplaceColumn) || tester.HasText(messages.BrowseProgram+" Program 000") {
						t.Fatal("program list still contains a replacement button")
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
					path, hasPath := tester.Find(window.settings.ManagedApps[0].ExePath)
					if !hasShow || !hasFolder || !hasPath || folder.W <= 0 || folder.W > 32 || folder.X+folder.W > path.X || path.X-folder.X-folder.W > 10 || absFloat(folder.Y+folder.H/2-path.Y-path.H/2) > 1 || folder.Y+folder.H > show.Y || path.X+path.W > float32(width) {
						t.Fatalf("folder icon is not directly before the file path: path=%+v folder=%+v show=%+v", path, folder, show)
					}
					captureNativeTest(t, tester, fmt.Sprintf("program-location-%s-%d-%t", language, width, visible))
				})
			}
		}
	}
}

func TestNativeProgramLocationClickOnlyOpensSelectedFolder(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, selected := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s/%d", language, selected), func(t *testing.T) {
				window := nativeProgramsFixture(2)
				window.settings.Language = language
				before := window.Settings()
				var opened []config.ManagedAppEntry
				saves := 0
				window.callbacks.OnOpenProgramLocation = func(entry config.ManagedAppEntry) { opened = append(opened, entry) }
				window.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { t.Fatal("opening a folder launched its program") }
				window.callbacks.OnSave = func(config.Settings) { saves++ }
				tester := native.NewTester(window.nativeView, 1200, 850)
				if err := tester.Click(before.ManagedApps[selected].Name); err != nil {
					t.Fatal(err)
				}
				if err := tester.Click(i18n.For(language).ManagedOpenLocation); err != nil {
					t.Fatal(err)
				}
				if len(opened) != 1 || opened[0] != before.ManagedApps[selected] || saves != 0 || !reflect.DeepEqual(before, window.Settings()) {
					t.Fatalf("location click changed settings or opened another program: opened=%+v saves=%d", opened, saves)
				}
			})
		}
	}
}

func TestNativePathReplacementKeepsOtherProgramSettings(t *testing.T) {
	for _, selected := range []int{0, 1} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			window := nativeProgramsFixture(2)
			window.settings.ManagedApps[0].WorkingDirectory = "C:/Other/data"
			window.settings.ManagedApps[1].ExePath = "shell:AppsFolder\\Example.App_123456789abcd!Reader"
			window.settings.ManagedApps[selected].Args = "--keep-arguments"
			before := window.settings.ManagedApps[selected]
			other := window.settings.ManagedApps[1-selected]
			saves := 0
			window.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { t.Fatal("replacing a program launched it") }
			window.callbacks.OnOpenProgramLocation = func(config.ManagedAppEntry) { t.Fatal("dropping a file opened a folder") }
			window.callbacks.OnSave = func(config.Settings) { saves++ }
			tester := native.NewTester(window.nativeView, 1200, 850)
			if err := tester.Click(before.Name); err != nil {
				t.Fatal(err)
			}
			button, found := tester.Find(i18n.For("zh-CN").ManagedOpenLocation)
			if !found {
				t.Fatal("path replacement button is missing")
			}
			path := droppedTestFile(t, t.TempDir(), "replacement.exe")
			if !tester.DropFiles(button.X+button.W/2, button.Y+button.H/2, path) {
				t.Fatal("path button did not accept the replacement file")
			}
			want := before
			want.Name, want.ExePath = "replacement", path
			if len(window.settings.ManagedApps) != 2 || window.settings.ManagedApps[selected] != want || window.settings.ManagedApps[1-selected] != other || saves != 1 {
				t.Fatalf("path replacement changed more than its target: apps=%+v saves=%d", window.settings.ManagedApps, saves)
			}
		})
	}
}
