package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreSaveLoadWithError_RoundTripAndAtomicTempCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	want := DefaultSettings()
	want.Language = "en-US"
	want.ManagedApps = []ManagedAppEntry{{Name: "Demo", ExePath: "C:\\\\Demo\\\\demo.exe", RunOnStartup: true}}

	store := NewStore(path)
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	want.Language = "zh-CN"
	if err := store.Save(want); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	got, err := store.LoadWithError()
	if err != nil {
		t.Fatalf("LoadWithError() error = %v", err)
	}
	if got.Language != want.Language || len(got.ManagedApps) != 1 || got.ManagedApps[0].Name != "Demo" {
		t.Fatalf("round-trip mismatch: got=%+v want=%+v", got, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file missing: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".settings-*.tmp")); len(matches) != 0 {
		t.Fatalf("temporary settings files remain: %v", matches)
	}
}

func TestStoreLoadWithError_InvalidFileIsBackedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	data := []byte("{\\\"schemaVersion\\\":")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := NewStore(path).LoadWithError()
	if err == nil || !strings.Contains(err.Error(), "invalid settings") {
		t.Fatalf("LoadWithError() error = %v, want invalid settings error", err)
	}
	if got.SchemaVersion != DefaultSettings().SchemaVersion {
		t.Fatalf("invalid file should return defaults, got schema %d", got.SchemaVersion)
	}
	backups, _ := filepath.Glob(path + ".invalid-*.bak")
	if len(backups) != 1 {
		t.Fatalf("invalid settings backup count = %d, want 1", len(backups))
	}
	backup, readErr := os.ReadFile(backups[0])
	if readErr != nil || string(backup) != string(data) {
		t.Fatalf("backup content mismatch: err=%v content=%q", readErr, backup)
	}
}

func TestStoreLoadWithError_ReadFailureDoesNotLookLikeMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	_, err := NewStore(path).LoadWithError()
	if err == nil {
		t.Fatal("LoadWithError() returned nil error for unreadable settings path")
	}
	if _, marshalErr := json.Marshal(DefaultSettings()); marshalErr != nil {
		t.Fatalf("defaults should remain serializable: %v", marshalErr)
	}
}

func TestShouldLaunchViaWinTray_RunOnStartupIsMasterSwitch(t *testing.T) {
	tests := []struct {
		name  string
		entry ManagedAppEntry
		want  bool
	}{
		{
			name: "paused hidden task does not launch",
			entry: ManagedAppEntry{
				RunOnStartup:             false,
				LaunchHiddenInBackground: true,
			},
			want: false,
		},
		{
			name: "paused auto hide task does not launch",
			entry: ManagedAppEntry{
				RunOnStartup: false,
				TrayBehavior: TrayBehavior{AutoMinimizeAndHideOnLaunch: true},
			},
			want: false,
		},
		{
			name: "enabled launch only task launches",
			entry: ManagedAppEntry{
				RunOnStartup: true,
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldLaunchViaWinTray(tc.entry)
			if got != tc.want {
				t.Fatalf("ShouldLaunchViaWinTray(%+v) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
}

func TestMigrate_LegacySchemaEnablesRunOnStartup(t *testing.T) {
	input := Settings{
		SchemaVersion: 1,
		Language:      "en-US",
		ManagedApps: []ManagedAppEntry{
			{Name: "App A", RunOnStartup: false},
			{Name: "App B"},
		},
	}

	got := migrate(input)

	if got.SchemaVersion != 3 {
		t.Fatalf("migrate schema version = %d, want 3", got.SchemaVersion)
	}
	for i, app := range got.ManagedApps {
		if !app.RunOnStartup {
			t.Fatalf("managed app %d RunOnStartup = false, want true", i)
		}
	}
}

func TestMigrate_SchemaV2PreservesRunOnStartupFalse(t *testing.T) {
	input := Settings{
		SchemaVersion: 2,
		Language:      "en-US",
		ManagedApps: []ManagedAppEntry{
			{Name: "App A", RunOnStartup: false},
			{Name: "App B", RunOnStartup: true},
		},
	}

	got := migrate(input)

	if got.SchemaVersion != 3 {
		t.Fatalf("migrate schema version = %d, want 3", got.SchemaVersion)
	}
	if got.ManagedApps[0].RunOnStartup {
		t.Fatalf("managed app 0 RunOnStartup = true, want false")
	}
	if !got.ManagedApps[1].RunOnStartup {
		t.Fatalf("managed app 1 RunOnStartup = false, want true")
	}
}

func TestMigrate_SchemaV2DefaultsExeToAutoHide(t *testing.T) {
	input := Settings{
		SchemaVersion: 2,
		Language:      "zh-CN",
		ManagedApps: []ManagedAppEntry{
			{
				Name:                     "eCloud",
				ExePath:                  `D:\Software\ecloud\eCloud.exe`,
				RunOnStartup:             true,
				LaunchHiddenInBackground: false,
				TrayBehavior:             TrayBehavior{AutoMinimizeAndHideOnLaunch: false},
			},
			{
				Name:                     "openclaw",
				ExePath:                  `C:\Users\bigfa\AppData\Roaming\npm\openclaw.cmd`,
				RunOnStartup:             true,
				LaunchHiddenInBackground: true,
				TrayBehavior:             TrayBehavior{AutoMinimizeAndHideOnLaunch: false},
			},
		},
	}

	got := migrate(input)
	if got.SchemaVersion != 3 {
		t.Fatalf("migrate schema version = %d, want 3", got.SchemaVersion)
	}
	if !got.ManagedApps[0].TrayBehavior.AutoMinimizeAndHideOnLaunch {
		t.Fatalf("exe managed app auto minimize = false, want true")
	}
	if got.ManagedApps[1].TrayBehavior.AutoMinimizeAndHideOnLaunch {
		t.Fatalf("hidden cmd managed app auto minimize = true, want false")
	}
}

func TestMigrate_NormalizesLanguageAndRetryBounds(t *testing.T) {
	tests := []struct {
		name       string
		retry      int
		wantRetry  int
		wantLang   string
		inputLang  string
		schemaVers int
	}{
		{
			name:       "retry lower bound",
			retry:      -1,
			wantRetry:  0,
			inputLang:  "fr-FR",
			wantLang:   "zh-CN",
			schemaVers: 2,
		},
		{
			name:       "retry upper bound",
			retry:      999,
			wantRetry:  120,
			inputLang:  "de-DE",
			wantLang:   "zh-CN",
			schemaVers: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := Settings{
				SchemaVersion:           tc.schemaVers,
				Language:                tc.inputLang,
				CloseWindowRetrySeconds: tc.retry,
				ManagedApps: []ManagedAppEntry{
					{},
				},
			}

			got := migrate(input)

			if got.Language != tc.wantLang {
				t.Fatalf("language = %q, want %q", got.Language, tc.wantLang)
			}
			if got.CloseWindowRetrySeconds != tc.wantRetry {
				t.Fatalf("closeWindowRetrySeconds = %d, want %d", got.CloseWindowRetrySeconds, tc.wantRetry)
			}
		})
	}
}
