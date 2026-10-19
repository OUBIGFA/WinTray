//go:build windows

package ui

import (
	"fmt"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestNativeToolbarMovesMoreFeaturesLeftAndKeepsLanguageRight(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, size := range [][2]int{{1124, 720}, {1200, 850}} {
			for _, count := range []int{0, 4} {
				t.Run(fmt.Sprintf("%s/%dx%d/%d-programs", language, size[0], size[1], count), func(t *testing.T) {
					w := nativeProgramsFixture(count)
					w.settings.Language = language
					tt := native.NewTester(w.nativeView, size[0], size[1])
					m := i18n.For(language)
					settings, hasSettings := tt.Find(m.OpenSettings)
					languageButton, hasLanguage := tt.Find(m.LanguageLabel)
					if !hasSettings || !hasLanguage || settings.X > 32 || languageButton.X < settings.X+settings.W {
						t.Fatalf("toolbar did not move more features to the left: settings=%+v language=%+v", settings, languageButton)
					}
					if absFloat(settings.Y+settings.H/2-languageButton.Y-languageButton.H/2) > 1 || size[0]-int(languageButton.X+languageButton.W) > 32 {
						t.Fatalf("language switch is not aligned at the right: settings=%+v language=%+v", settings, languageButton)
					}
					tt.Move(-1, -1)
					assertLanguageGlyphCentered(t, tt, languageButton)
					captureNativeTest(t, tt, fmt.Sprintf("toolbar-%s-%d-%d", w.settings.Language, size[0], count))
				})
			}
		}
	}
}

// Inspect the actual icon pixels as well as its target; a text button with an
// empty child can have correct outer bounds and still render an off-center icon.
func assertLanguageGlyphCentered(t *testing.T, tt *native.Tester, button native.Rect) {
	t.Helper()
	img := tt.Image()
	minX, minY, maxX, maxY := int(button.X+button.W), int(button.Y+button.H), -1, -1
	for y := int(button.Y + 4); y < int(button.Y+button.H-4); y++ {
		for x := int(button.X + 4); x < int(button.X+button.W-4); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 100 && g>>8 < 100 && b>>8 < 100 {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX-minX < 12 || maxY-minY < 12 ||
		absFloat(float32(minX+maxX+1)/2-button.X-button.W/2) > 1.5 ||
		absFloat(float32(minY+maxY+1)/2-button.Y-button.H/2) > 1.5 {
		t.Fatalf("language glyph is too small or off-center: ink=(%d,%d)-(%d,%d), button=%+v", minX, minY, maxX, maxY, button)
	}
}

func absFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func TestNativeToolbarSettingsAndLanguageActions(t *testing.T) {
	w := nativeProgramsFixture(1)
	var savedLanguage string
	w.callbacks.OnSave = func(s config.Settings) { savedLanguage = s.Language }
	tt := native.NewTester(w.nativeView, 1200, 850)
	for _, want := range []string{"en-US", "zh-CN"} {
		m := i18n.For(w.settings.Language)
		if err := tt.Click(m.LanguageLabel); err != nil {
			t.Fatal(err)
		}
		if savedLanguage != want || w.settings.Language != want {
			t.Fatalf("language switch saved %q, want %q", savedLanguage, want)
		}
	}
	m := i18n.For("zh-CN")
	if err := tt.Click(m.OpenSettings); err != nil {
		t.Fatal(err)
	}
	if !w.onSettingsPage {
		t.Fatal("settings button did not open settings")
	}
	if err := tt.Click(m.BackToPrograms); err != nil {
		t.Fatal(err)
	}
	if _, hasTitle := tt.Find(m.WindowTitle); hasTitle || !tt.HasText(m.OpenSettings) || w.onSettingsPage {
		t.Fatal("returning from settings retained the removed toolbar title or lost the more-features action")
	}
}
