package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestStorePreservesWorkingDirectory(t *testing.T) {
	// Load as JSON so this also catches an older reader silently dropping the field.
	var settings Settings
	if err := json.Unmarshal([]byte(`{"managedApps":[{"id":"one","exePath":"C:\\Apps\\app.exe","workingDirectory":"D:\\工作 data"}]}`), &settings); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(t.TempDir(), "settings.json"))
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadWithError()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(loaded.ManagedApps[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["workingDirectory"] != `D:\工作 data` {
		t.Fatalf("working directory was lost after save/reload: %s", data)
	}
}

func TestLaunchDirectoryDefaultsAndRelativePaths(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", `C:\Apps`},
		{"   ", `C:\Apps`},
		{`D:\工作 data`, `D:\工作 data`},
		{` "D:\工作 data" `, `D:\工作 data`},
		{`..\Data`, `C:\Data`},
	} {
		entry := ManagedAppEntry{ExePath: `C:\Apps\app.exe`, WorkingDirectory: tc.input}
		if got := entry.LaunchDirectory(); got != tc.want {
			t.Errorf("directory %q: got %q, want %q", tc.input, got, tc.want)
		}
	}
	var old ManagedAppEntry
	if err := json.Unmarshal([]byte(`{"exePath":"C:\\Apps\\app.exe"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.LaunchDirectory() != `C:\Apps` {
		t.Fatalf("old entry changed directory: %+v", old)
	}
}
