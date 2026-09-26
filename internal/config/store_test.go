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
	want.ManagedApps = []ManagedAppEntry{{Name: "Demo", ExePath: "C:\\\\Demo\\\\demo.exe", RunOnStartup: true, TrayBehavior: TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 45}}}

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
	if got.Language != want.Language || len(got.ManagedApps) != 1 || got.ManagedApps[0].Name != "Demo" || got.ManagedApps[0].TrayBehavior.CloseDelaySeconds != 45 {
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

func TestStartupIntervalDefaultsBoundsAndRoundTrip(t *testing.T) {
	if got := DefaultSettings().StartupIntervalSeconds; got != 3 {
		t.Fatalf("default startup interval = %d, want 3 seconds", got)
	}
	for _, tc := range []struct {
		name  string
		field string
		want  int
	}{
		{name: "legacy missing field", want: 3},
		{name: "zero disables wait", field: `,"startupIntervalSeconds":0`, want: 0},
		{name: "custom interval", field: `,"startupIntervalSeconds":15`, want: 15},
		{name: "negative clamped", field: `,"startupIntervalSeconds":-1`, want: 0},
		{name: "too large clamped", field: `,"startupIntervalSeconds":999`, want: 120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(`{"schemaVersion":3,"language":"en-US"`+tc.field+`}`), 0o600); err != nil {
				t.Fatal(err)
			}
			store := NewStore(path)
			settings, err := store.LoadWithError()
			if err != nil || settings.StartupIntervalSeconds != tc.want {
				t.Fatalf("loaded interval = %d, err=%v, want %d", settings.StartupIntervalSeconds, err, tc.want)
			}
			if err := store.Save(settings); err != nil {
				t.Fatal(err)
			}
			again, err := store.LoadWithError()
			if err != nil || again.StartupIntervalSeconds != tc.want {
				t.Fatalf("round-trip interval = %d, err=%v, want %d", again.StartupIntervalSeconds, err, tc.want)
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

func TestLoadWithError_ReadsCloseDelaySecondsFromTrayBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	data := `{"schemaVersion":3,"language":"zh-CN","managedApps":[` +
		`{"name":"QQ","exePath":"C:\\QQ\\QQ.exe","runOnStartup":true,"trayBehavior":{"autoMinimizeAndHideOnLaunch":true,"closeDelaySeconds":30}},` +
		`{"name":"Legacy","exePath":"C:\\Legacy\\app.exe","runOnStartup":true,"trayBehavior":{"autoMinimizeAndHideOnLaunch":true}}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := NewStore(path).LoadWithError()
	if err != nil {
		t.Fatalf("LoadWithError() error = %v", err)
	}
	if len(got.ManagedApps) != 2 {
		t.Fatalf("managed apps = %d, want 2", len(got.ManagedApps))
	}
	if got.ManagedApps[0].TrayBehavior.CloseDelaySeconds != 30 {
		t.Fatalf("QQ closeDelaySeconds = %d, want 30", got.ManagedApps[0].TrayBehavior.CloseDelaySeconds)
	}
	if got.ManagedApps[1].TrayBehavior.CloseDelaySeconds != 0 {
		t.Fatalf("entry without closeDelaySeconds = %d, want 0", got.ManagedApps[1].TrayBehavior.CloseDelaySeconds)
	}
}

func TestMigrate_ClampsCloseDelaySeconds(t *testing.T) {
	input := Settings{
		SchemaVersion: 3,
		Language:      "zh-CN",
		ManagedApps: []ManagedAppEntry{
			{Name: "Negative", TrayBehavior: TrayBehavior{CloseDelaySeconds: -5}},
			{Name: "Too long", TrayBehavior: TrayBehavior{CloseDelaySeconds: MaxCloseDelaySeconds + 1}},
			{Name: "In range", TrayBehavior: TrayBehavior{CloseDelaySeconds: 30}},
		},
	}

	got := migrate(input)

	want := []int{0, MaxCloseDelaySeconds, 30}
	for i, app := range got.ManagedApps {
		if app.TrayBehavior.CloseDelaySeconds != want[i] {
			t.Errorf("%s closeDelaySeconds = %d, want %d", app.Name, app.TrayBehavior.CloseDelaySeconds, want[i])
		}
	}
}

func TestLoadWithError_PerProgramTrayIconCollection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":3,"language":"zh-CN","managedApps":[{"id":"a","exePath":"C:\\Apps\\One.exe"},{"id":"b","exePath":"C:\\Apps\\Two.exe"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	got, err := store.LoadWithError()
	if err != nil || got.ManagedApps[0].CollectTrayIcon || got.ManagedApps[1].CollectTrayIcon {
		t.Fatalf("default collection = %+v, %v", got.ManagedApps, err)
	}
	got.ManagedApps[0].CollectTrayIcon = true
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	again, err := store.LoadWithError()
	if err != nil || !again.ManagedApps[0].CollectTrayIcon || again.ManagedApps[1].CollectTrayIcon {
		t.Fatalf("collection round-trip = %+v, %v", again.ManagedApps, err)
	}
	if paths := CollectedTrayIconPaths(again); len(paths) != 1 || paths[0] != `C:\Apps\One.exe` {
		t.Fatalf("selected icon paths = %v", paths)
	}
}

func TestCollectedTrayIconPathsFiltersScriptsAndDuplicateExecutables(t *testing.T) {
	s := DefaultSettings()
	s.ManagedApps = []ManagedAppEntry{
		{ExePath: `C:\Apps\One.exe`, CollectTrayIcon: true},
		{ExePath: `c:\apps\ONE.exe`, CollectTrayIcon: true},
		{ExePath: `C:\Scripts\run.ps1`, CollectTrayIcon: true},
		{ExePath: `C:\Apps\Two.exe`},
	}
	paths := CollectedTrayIconPaths(s)
	if len(paths) != 1 || paths[0] != s.ManagedApps[0].ExePath {
		t.Fatalf("collected icon paths = %v", paths)
	}
}

func TestLoadWithError_MigratesGlobalTrayCollectionByExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	data := `{"schemaVersion":3,"language":"zh-CN","trayBoxEnabled":true,"trayBoxApps":["C:\\Apps\\One.exe","C:\\Apps\\Absent.exe"],"managedApps":[{"id":"a","exePath":"c:\\apps\\ONE.exe"},{"id":"b","exePath":"C:\\Apps\\Two.exe"}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(path).LoadWithError()
	if err != nil || !got.ManagedApps[0].CollectTrayIcon || got.ManagedApps[1].CollectTrayIcon || !got.TrayBoxEnabled || len(got.TrayBoxApps) != 2 {
		t.Fatalf("legacy migration = %+v, %v", got, err)
	}
}
