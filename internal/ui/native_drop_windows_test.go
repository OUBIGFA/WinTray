//go:build windows

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func droppedTestFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("drop test; must not be executed"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNativeDroppedProgramsAppendAndPersist(t *testing.T) {
	for _, count := range []int{0, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			w := nativeProgramsFixture(count)
			before := slices.Clone(w.settings.ManagedApps)
			store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
			saves, launches := 0, 0
			w.callbacks.OnSave = func(settings config.Settings) {
				saves++
				if err := store.Save(settings); err != nil {
					t.Fatal(err)
				}
			}
			w.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { launches++ }
			tt := native.NewTester(w.nativeView, 1200, 850)
			dir := t.TempDir()
			var paths []string
			for _, name := range []string{"中文 程序.EXE", "batch.bat", "command.CMD", "powershell.ps1", "python.py", "pythonw.PYW"} {
				paths = append(paths, droppedTestFile(t, dir, name))
			}
			w.nativeDropPaths(paths)
			tt.Frame()
			if saves != 1 || launches != 0 {
				t.Fatalf("drop saves=%d launches=%d, want one save and no launch", saves, launches)
			}
			saved, err := store.LoadWithError()
			if err != nil {
				t.Fatal(err)
			}
			apps := saved.ManagedApps
			if len(apps) != count+len(paths) || !slices.Equal(apps[:count], before) {
				t.Fatalf("drop changed the existing list: %+v", apps)
			}
			ids := make(map[string]bool)
			for i, path := range paths {
				entry := apps[count+i]
				name := filepath.Base(path)
				name = name[:len(name)-len(filepath.Ext(path))]
				if entry.ID == "" || ids[entry.ID] || entry.Name != name || entry.ExePath != path || !entry.RunOnStartup {
					t.Errorf("wrong dropped entry: %+v", entry)
				}
				ids[entry.ID] = true
				hidden := i != 0
				if entry.LaunchHiddenInBackground != hidden || entry.TrayBehavior.AutoMinimizeAndHideOnLaunch == hidden || entry.LaunchViaLogonTask || entry.CollectTrayIcon {
					t.Errorf("drop changed default launch mode: %+v", entry)
				}
				wantSchedule := config.Schedule{AutoExitMinutes: config.DefaultAutoExitMinutes, FrequencyDays: 1, FrequencyRuns: 1}
				if entry.Schedule != wantSchedule {
					t.Errorf("drop changed schedule defaults: %+v", entry.Schedule)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "drop test; must not be executed" {
					t.Errorf("drop moved or changed source file %q: %v", path, err)
				}
			}
			last := len(apps) - 1
			if w.desktop.selected != last || !tt.HasText(paths[len(paths)-1]) || len(w.desktop.orderOptions) != len(apps) {
				t.Fatal("drop did not select the last added program and refresh its editor/order options")
			}
			if !reflect.DeepEqual(saved, w.Settings()) {
				t.Fatal("saved settings differ from the displayed settings")
			}
		})
	}
}

func TestNativeDroppedProgramsRejectInvalidItems(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			w := nativeProgramsFixture(1)
			tt := native.NewTester(w.nativeView, 1200, 850)
			if err := tt.Click("Program 000"); err != nil {
				t.Fatal(err)
			}
			before := w.Settings()
			saves := 0
			w.callbacks.OnSave = func(config.Settings) { saves++ }
			dir := t.TempDir()
			folder := filepath.Join(dir, "folder.exe")
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			paths := []string{"", folder, filepath.Join(dir, "missing.exe"), droppedTestFile(t, dir, "notes.txt"), droppedTestFile(t, dir, "shortcut.lnk")}
			valid := droppedTestFile(t, dir, "valid.cmd")
			if mixed {
				paths = slices.Insert(paths, 2, valid)
			}
			w.nativeDropPaths(paths)
			tt.Frame()
			if mixed {
				if saves != 1 || len(w.settings.ManagedApps) != 2 || w.settings.ManagedApps[1].ExePath != valid {
					t.Fatalf("mixed drop did not add only the valid file: saves=%d apps=%+v", saves, w.settings.ManagedApps)
				}
			} else if saves != 0 || !reflect.DeepEqual(before, w.Settings()) || w.desktop.selected != 0 {
				t.Fatal("invalid drop changed settings or selection")
			}
		})
	}
}

// Appending can reallocate ManagedApps. Pending editor values must reach the
// original entry before that happens, not a stale pointer after the append.
func TestNativeDropCommitsPendingEditor(t *testing.T) {
	w := nativeProgramsFixture(1)
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	label := i18n.For("zh-CN").ManagedAppArgs
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Type("--keep-original")
	if !tt.Focused(label) || !w.desktop.argsDirty {
		t.Fatal("test did not leave the arguments edit pending")
	}
	var saved config.Settings
	w.callbacks.OnSave = func(settings config.Settings) { saved = settings }
	w.nativeDropPaths([]string{droppedTestFile(t, t.TempDir(), "new.exe")})
	tt.Frame()
	if len(saved.ManagedApps) != 2 || saved.ManagedApps[0].Args != "--keep-original" || saved.ManagedApps[1].Args != "" || w.desktop.argsDirty {
		t.Fatalf("drop lost or transferred the pending edit: %+v", saved.ManagedApps)
	}
}

func TestNativeEmptyDropIsNoOp(t *testing.T) {
	w := nativeProgramsFixture(1)
	native.NewTester(w.nativeView, 1200, 850)
	before := w.Settings()
	w.callbacks.OnSave = func(config.Settings) { t.Error("empty drop saved settings") }
	w.nativeDropPaths(nil)
	if !reflect.DeepEqual(before, w.Settings()) || w.desktop.selected != -1 {
		t.Fatal("empty drop changed the list or selection")
	}
}
