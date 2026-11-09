//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"testing"
	"wintray/internal/i18n"
)

// Scroll only the target pane until the real control is fully visible.
func revealNativeControl(t *testing.T, tt *native.Tester, label string, x, y float32) {
	t.Helper()
	for i := 0; i < 20; i++ {
		if r, ok := tt.Find(label); ok && r.W > 0 && r.H > 0 {
			return
		}
		tt.Scroll(x, y, 0, 80)
	}
	t.Fatalf("control is not reachable by scrolling: %q", label)
}

// Exercise the actual view: the two settings tabs must keep the same
// selected program and commit edits before changing the visible controls.
func TestProgramSettingsTabs(t *testing.T) {
	w := nativeProgramsFixture(2)
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	if tab, ok := tt.Find("程序信息"); !ok {
		t.Fatal("missing details tab")
	} else {
		tt.ClickAt(tab.X+tab.W/2, tab.Y+tab.H/2)
	}
	captureNativeTest(t, tt, "program-details")
	m := i18n.For("zh-CN")
	if err := tt.Click(m.ProgramName); err != nil {
		t.Fatal(err)
	}
	tt.Key(native.Ctrl, native.KeyA)
	tt.Type("Renamed program")
	if err := tt.Click(m.ManagedAppArgs); err != nil {
		t.Fatal(err)
	}
	tt.Type("--keep-tab-edit")
	if err := tt.Click("启动与计划"); err != nil {
		t.Fatal(err)
	}
	if w.settings.ManagedApps[0].Args != "--keep-tab-edit" {
		t.Fatal("tab change lost arguments")
	}
	if w.settings.ManagedApps[0].Name != "Renamed program" {
		t.Fatal("tab change lost the program name")
	}
	if !tt.HasText(m.ManagedModeLabel) || tt.HasText(m.ManagedAppArgs) {
		t.Fatal("settings are not grouped in tabs")
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(m.StartupInterval) {
		t.Fatal("startup settings unavailable")
	}
	if err := tt.Click("程序列表"); err != nil {
		t.Fatal(err)
	}
	if w.desktop.selected != 0 || !tt.HasText("启动与计划") {
		t.Fatal("page tabs lost program selection")
	}
}
