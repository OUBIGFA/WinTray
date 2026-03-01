package config

import "testing"

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

	if got.SchemaVersion != 2 {
		t.Fatalf("migrate schema version = %d, want 2", got.SchemaVersion)
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

	if got.SchemaVersion != 2 {
		t.Fatalf("migrate schema version = %d, want 2", got.SchemaVersion)
	}
	if got.ManagedApps[0].RunOnStartup {
		t.Fatalf("managed app 0 RunOnStartup = true, want false")
	}
	if !got.ManagedApps[1].RunOnStartup {
		t.Fatalf("managed app 1 RunOnStartup = false, want true")
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
