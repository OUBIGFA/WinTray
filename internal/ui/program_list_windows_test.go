//go:build windows

package ui

import (
	"fmt"
	"strconv"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/i18n"
)

func TestNativeOrderSelectKeepsNormalSizeAndDropdownBehavior(t *testing.T) {
	value := "1"
	options := make([]string, 100)
	for i := range options {
		options[i] = strconv.Itoa(i + 1)
	}
	changed := 0
	tt := native.NewTester(func(c *native.Context) {
		native.Column(c).Padding(20).Width(108).Children(func() {
			if nativeOrderSelect(c, &value, options, "order").Changed() {
				changed++
			}
		})
	}, 240, 400)
	box, ok := tt.Find("order")
	if !ok || box.W < 68 || box.H < 30 {
		t.Fatalf("order select was compressed: %+v", box)
	}
	if err := tt.Click("order"); err != nil {
		t.Fatal(err)
	}
	if len(tt.Menu()) != 0 || !tt.HasText("100") {
		t.Fatal("order select was replaced with a system menu button")
	}
	tt.Key(0, native.KeyEnd)
	last, ok := tt.Find("100")
	if !ok || last.Y < box.Y+box.H || last.Y+last.H > 400 {
		t.Fatalf("keyboard selection did not scroll the last option into view: %+v", last)
	}
	tt.Key(0, native.KeyEnter)
	if value != "100" || changed != 1 {
		t.Fatalf("selecting the last order = %q, changed=%d", value, changed)
	}
	if err := tt.Click("order"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyHome)
	tt.Key(0, native.KeyEscape)
	if value != "100" || changed != 1 {
		t.Fatal("dismissing the dropdown changed the order")
	}
}

// A full default-sized page is the visual check; a cropped control is not
// enough to judge whether row spacing, headers and the editor still agree.
func TestNativeProgramListDefaultLayout(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			w := nativeProgramsFixture(13)
			w.settings.Language = language
			w.settings.ManagedApps[0].Schedule.Enabled = true
			w.settings.ManagedApps[0].Schedule.FrequencyEnabled = true
			w.settings.ManagedApps[0].TrayBehavior.CloseDelaySeconds = 15
			for i := range w.settings.ManagedApps {
				if language == "zh-CN" {
					w.settings.ManagedApps[i].Name = fmt.Sprintf("示例程序 %02d", i+1)
				}
			}
			tt := native.NewTester(w.nativeView, 1200, 850)
			if err := tt.Click(w.settings.ManagedApps[0].Name); err != nil {
				t.Fatal(err)
			}
			tt.Move(-1, -1)
			m := i18n.For(language)
			if _, hasTitle := tt.Find(m.WindowTitle); hasTitle || !tt.HasText(m.OpenSettings) || !tt.HasText(m.ManagedFrequencyHint) {
				t.Fatal("default page retained the removed title or lost its main actions or program settings")
			}
			captureNativeTest(t, tt, "rebuilt-main-"+language)
		})
	}
}
