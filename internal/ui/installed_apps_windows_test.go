//go:build windows

package ui

import (
	"path/filepath"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/shellapp"
)

func installedAppsFixture() *MainWindow {
	window := nativeProgramsFixture(0)
	window.desktop.installedApps = &installedAppPicker{
		loaded:   true,
		selected: make(map[string]bool),
		apps: []shellapp.Application{
			{Name: "Desktop Editor", Path: "C:/Apps/Editor.exe", Args: "--profile \"two words\"", WorkingDirectory: "C:/Documents"},
			{Name: "Store Reader", Path: "shell:AppsFolder\\Example.App_123456789abcd!Reader", Args: "--library \"two words\""},
		},
	}
	return window
}

func TestNativeInstalledAppsPreserveLaunchDetailsWithoutStarting(t *testing.T) {
	window := installedAppsFixture()
	launches := 0
	window.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { launches++ }
	window.nativeAddApplications(window.desktop.installedApps.apps)
	if len(window.settings.ManagedApps) != 2 {
		t.Fatalf("added %d apps, want 2", len(window.settings.ManagedApps))
	}
	for index, entry := range window.settings.ManagedApps {
		want := window.desktop.installedApps.apps[index]
		if entry.Name != want.Name || entry.ExePath != want.Path || entry.Args != want.Args || entry.WorkingDirectory != want.WorkingDirectory || startModeOf(entry) != startNormal || config.ShouldLaunchViaWinTray(entry) {
			t.Fatalf("installed app launch details changed: %+v", entry)
		}
	}
	if launches != 0 {
		t.Fatal("adding an installed app launched it")
	}
}

func TestNativeInstalledAppsSearchSelectAndAdd(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			window := installedAppsFixture()
			window.settings.Language = language
			messages := i18n.For(language)
			tester := native.NewTester(window.nativeView, 1200, 850)
			for _, label := range []string{messages.AddInstalledApps, messages.InstalledAppsSearch} {
				if err := tester.Click(label); err != nil {
					t.Fatal(err)
				}
			}
			tester.Type("READER")
			if tester.HasText("Desktop Editor") || !tester.HasText("Store Reader") {
				t.Fatal("installed app search did not filter by name")
			}
			if err := tester.Click("Store Reader"); err != nil {
				t.Fatal(err)
			}
			captureNativeTest(t, tester, "installed-apps-"+language)
			if err := tester.Click(messages.InstalledAppsAdd); err != nil {
				t.Fatal(err)
			}
			if len(window.settings.ManagedApps) != 1 || !shellapp.IsPath(window.settings.ManagedApps[0].ExePath) || window.desktop.installedApps.open {
				t.Fatalf("store selection was not added: %+v", window.settings.ManagedApps)
			}
		})
	}
}

func TestNativeInstalledAppsTypeFilterAndCancel(t *testing.T) {
	window := installedAppsFixture()
	messages := i18n.For("zh-CN")
	tester := native.NewTester(window.nativeView, 860, 680)
	for _, label := range []string{messages.AddInstalledApps, messages.InstalledAppsStore} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if tester.HasText("Desktop Editor") || !tester.HasText("Store Reader") {
		t.Fatal("store-only filter contains desktop programs")
	}
	if err := tester.Click(messages.InstalledAppsDesktop); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("Desktop Editor") || tester.HasText("Store Reader") {
		t.Fatal("desktop-only filter contains store apps")
	}
	captureNativeTest(t, tester, "installed-apps-narrow")
	tester.Key(0, native.KeyEscape)
	if len(window.settings.ManagedApps) != 0 || window.desktop.installedApps.open {
		t.Fatal("cancel added apps or failed to close the picker")
	}
}

func TestNativeInstalledAppModesAndArgumentsPersist(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			window := installedAppsFixture()
			window.settings.Language = language
			store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
			window.callbacks.OnSave = func(settings config.Settings) {
				if err := store.Save(settings); err != nil {
					t.Fatal(err)
				}
			}
			window.nativeAddApplications(window.desktop.installedApps.apps[1:])
			messages := i18n.For(language)
			tester := native.NewTester(window.nativeView, 1200, 850)
			if err := tester.Click("Store Reader"); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []struct {
				label string
				value startMode
			}{
				{messages.ManagedAutoHide, startToTray},
				{messages.ManagedLaunchHidden, startHidden},
				{messages.ManagedTaskLaunch, startTask},
				{messages.ManagedLaunchOnly, startNormal},
			} {
				if err := tester.Click(mode.label); err != nil {
					t.Fatal(err)
				}
				if got := startModeOf(window.settings.ManagedApps[0]); got != mode.value {
					t.Fatalf("store app cannot use startup mode %v: got %v", mode.value, got)
				}
			}
			captureNativeTest(t, tester, "store-modes-"+language)
			if err := tester.Click(messages.MoreSettings); err != nil {
				t.Fatal(err)
			}
			if tester.HasText(messages.ManagedWorkingDirectory) || !tester.HasText(messages.ManagedPackagedDirectory) {
				t.Fatal("store app offers a working directory that Windows cannot apply")
			}
			if err := tester.Click(messages.ManagedAppArgs); err != nil {
				t.Fatal(err)
			}
			tester.Key(native.Ctrl, native.KeyA)
			arguments := `--library "我的 图书" --port 25600`
			tester.Type(arguments)
			tester.Key(0, native.KeyEnter)
			window.commitNativeEdits()
			saved, err := store.LoadWithError()
			if err != nil {
				t.Fatal(err)
			}
			entry := saved.ManagedApps[0]
			if entry.ExePath != "shell:AppsFolder\\Example.App_123456789abcd!Reader" || entry.Args != arguments || config.ShouldLaunchViaWinTray(entry) {
				t.Fatalf("saved store launch settings changed: %+v", entry)
			}
		})
	}
}

func TestNativeInstalledAppsSelectionSurvivesTypeFilter(t *testing.T) {
	window := installedAppsFixture()
	messages := i18n.For("zh-CN")
	tester := native.NewTester(window.nativeView, 1200, 850)
	for _, label := range []string{messages.AddInstalledApps, "Store Reader", messages.InstalledAppsDesktop, "Desktop Editor", messages.InstalledAppsAdd} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if len(window.settings.ManagedApps) != 2 || window.settings.ManagedApps[0].Name != "Desktop Editor" || window.settings.ManagedApps[1].Name != "Store Reader" {
		t.Fatalf("changing the filter lost a selected app: %+v", window.settings.ManagedApps)
	}
}
