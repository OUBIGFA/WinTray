//go:build windows

package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"wintray/internal/config"
)

func TestPrepareUninstallRestoresStartupAndPreservesPortableSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	path, err := config.SettingsPathWithError()
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "WinTray", "settings.json") {
		t.Fatalf("changed shared settings path: %s", path)
	}
	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{{ID: "existing", Name: "Portable program", ExePath: `C:\Apps\existing.exe`, RunOnStartup: true}}
	store := config.NewStore(path)
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	var calls []string
	if err := prepareUninstall(func() error { calls = append(calls, "restore"); return nil }, func() error { calls = append(calls, "remove"); return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !reflect.DeepEqual(calls, []string{"restore", "remove"}) || string(before) != string(after) {
		t.Fatal("uninstall changed the shared profile or skipped startup restoration")
	}
}

func TestPrepareUninstallStopsOnRestorationFailure(t *testing.T) {
	failure := errors.New("startup cannot be restored")
	called := false
	err := prepareUninstall(func() error { return failure }, func() error { called = true; return nil })
	if !errors.Is(err, failure) || called {
		t.Fatal("uninstall continued after failed restoration")
	}
}
