//go:build windows

package ui

import (
	"encoding/json"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestWorkingDirectoryEditsFollowSelectedProgram(t *testing.T) {
	w := nativeProgramsFixture(2)
	var saved []byte
	w.callbacks.OnSave = func(s config.Settings) { saved, _ = json.Marshal(s) }
	tt := native.NewTester(w.nativeView, mainWindowMinWidth, mainWindowMinHeight)
	m := i18n.For("zh-CN")
	for _, label := range []string{"Program 000", m.MoreSettings, "工作目录（可选）"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Type(`D:\工作 data`)
	if len(saved) != 0 {
		t.Fatal("directory saved before Enter or leaving the field")
	}
	if err := tt.Click("Program 001"); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		ManagedApps []map[string]any `json:"managedApps"`
	}
	if err := json.Unmarshal(saved, &snapshot); err != nil {
		t.Fatalf("directory was not saved on selection change: %v", err)
	}
	if snapshot.ManagedApps[0]["workingDirectory"] != `D:\工作 data` || snapshot.ManagedApps[1]["workingDirectory"] == `D:\工作 data` {
		t.Fatalf("directory saved to wrong program: %s", saved)
	}
	for _, label := range []string{"Program 000", m.MoreSettings, "工作目录（可选）"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	tt.Key(native.Ctrl, native.KeyA)
	tt.Key(0, native.KeyBackspace)
	tt.Key(0, native.KeyEnter)
	snapshot.ManagedApps = nil
	if err := json.Unmarshal(saved, &snapshot); err != nil {
		t.Fatal(err)
	}
	if value := snapshot.ManagedApps[0]["workingDirectory"]; value != nil && value != "" {
		t.Fatalf("clearing the directory was not saved: %s", saved)
	}
	captureNativeTest(t, tt, "working-directory")
}
