package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"wintray/internal/config"
)

func TestResetSettingsKeepsRecoveryListWhenReleaseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store := config.NewStore(path)
	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{{ID: "one", ExePath: `C:\Apps\example.exe`, CollectTrayIcon: true}}
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("cannot restore icon")
	if err := resetSettings(store, func() error { return releaseErr }); !errors.Is(err, releaseErr) {
		t.Fatalf("reset error = %v, want release error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("failed reset changed recovery settings: %s, error %v", after, err)
	}
	if err := resetSettings(store, func() error { return nil }); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	got, err := store.LoadWithError()
	if err != nil || !reflect.DeepEqual(got, config.DefaultSettings()) {
		t.Fatalf("successful reset = %+v, error %v", got, err)
	}
}

func TestResetSettingsReportsSaveFailure(t *testing.T) {
	// A directory cannot be replaced by the atomic settings-file rename.
	store := config.NewStore(t.TempDir())
	if err := resetSettings(store, func() error { return nil }); err == nil {
		t.Fatal("reset reported success despite a failed settings write")
	}
}
