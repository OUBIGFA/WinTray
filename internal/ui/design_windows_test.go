//go:build windows

package ui

import (
	"fmt"
	"slices"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

// Use the production view and real text shaping: a string appearing in the
// accessibility tree alone does not prove that it fits on the screen.
func TestDesktopFullEditorAndSingleLineRowsFit(t *testing.T) {
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, dark := range []bool{false, true} {
			for _, size := range [][2]int{{1124, 680}, {1184, 811}, {1440, 950}} {
				t.Run(fmt.Sprintf("%s/dark=%t/%dx%d", language, dark, size[0], size[1]), func(t *testing.T) {
					w := nativeProgramsFixture(16)
					w.settings.Language, w.settings.RunAtLogon = language, false
					entry := &w.settings.ManagedApps[0]
					entry.Name = "A long program name that gives width to the startup action"
					entry.ExePath = `C:\Programs\Weixin.exe`
					entry.TrayBehavior.CloseDelaySeconds = 600
					entry.Schedule.Enabled, entry.Schedule.FrequencyEnabled = true, true
					m := i18n.For(language)
					action := i18n.FormatManagedParam(language, *entry)
					var actionWidth, actionHeight float32
					tt := native.NewTester(func(c *native.Context) {
						w.nativeView(c)
						actionWidth, actionHeight = c.MeasureText(0, native.Span{Text: action})
					}, size[0], size[1])
					tt.SetDark(dark)
					if err := tt.Click(entry.Name); err != nil {
						t.Fatal(err)
					}
					footer, _ := tt.Find(m.ExitApp)
					labels := []string{m.ManagedLaunchNow, m.HostedShowWindow, m.ManagedEnabled, m.ManagedWeChatAutoLogin, m.ManagedCloseDelay, m.ManagedAppArgs, m.ManagedScheduleStart, m.ManagedScheduleExit, m.ManagedFrequencyDays, m.ManagedFrequencyRuns}
					if size[1] >= 800 {
						labels = append(labels, m.ManagedFrequencyHint)
					}
					for _, label := range labels {
						r, ok := tt.Find(label)
						if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.X+r.W > float32(size[0])+1 || r.Y+r.H > footer.Y-10 {
							t.Errorf("editor control clipped: %q = %+v, footer=%+v", label, r, footer)
						}
					}
					r, found := tt.Find(action)
					if !found || r.W+0.5 < actionWidth || absFloat(r.H-actionHeight) > 1 {
						t.Errorf("longest action was shortened or wrapped: rect=%+v text=%vx%v", r, actionWidth, actionHeight)
					}
					name, _ := tt.Find(entry.Name)
					if name.H > 52 || r.Y < name.Y || r.Y+r.H > name.Y+name.H {
						t.Errorf("program no longer occupies one line: row=%+v action=%+v", name, r)
					}
					// Every row action must stay to the left of the inspector.
					editor, _ := tt.Find(m.ManagedEnabled)
					remove, _ := tt.Find(m.ManagedColumnDelete + " " + entry.Name)
					if remove.W < 32 || remove.X+remove.W >= editor.X {
						t.Errorf("action width pushed deletion into the inspector: %+v editor=%+v", remove, editor)
					}
					tt.Move(-1, -1)
					captureNativeTest(t, tt, fmt.Sprintf("redesign-%s-%t-%d", language, dark, size[0]))
				})
			}
		}
	}
}

func TestNativeGripReorderCancellationAndPersistence(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancel), func(t *testing.T) {
			w := nativeProgramsFixture(4)
			m := i18n.For(w.settings.Language)
			var saved config.Settings
			w.callbacks.OnSave = func(s config.Settings) {
				saved = s
				saved.ManagedApps = slices.Clone(s.ManagedApps)
			}
			tt := native.NewTester(w.nativeView, 1200, 850)
			if err := tt.Click("Program 003"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click(m.ManagedAppArgs); err != nil {
				t.Fatal(err)
			}
			tt.Type("--keep-with-program")
			grip, _ := tt.Find(m.ManagedDragHandle + " Program 003")
			first, _ := tt.Find("Program 000")
			tt.Press(grip.X+grip.W/2, grip.Y+grip.H/2)
			tt.Move(grip.X+grip.W/2, first.Y+2)
			if cancel {
				tt.Key(0, native.KeyEscape)
			}
			tt.Release(grip.X+grip.W/2, first.Y+2)
			wantIndex := 0
			if cancel {
				wantIndex = 3
			}
			if len(saved.ManagedApps) != 4 || saved.ManagedApps[wantIndex].ID != "3" || saved.ManagedApps[wantIndex].Args != "--keep-with-program" {
				t.Fatalf("drag lost order or pending edit: %+v", saved.ManagedApps)
			}
			if w.desktop.selected != wantIndex {
				t.Fatalf("selection did not follow the program: %d", w.desktop.selected)
			}
			for _, entry := range saved.ManagedApps {
				if !entry.RunOnStartup || entry.CollectTrayIcon {
					t.Fatal("drag changed a checkbox")
				}
			}
			// Reordering onto the same place must not cause another save.
			w.callbacks.OnSave = func(config.Settings) { t.Error("same-position drag saved settings") }
			grip, _ = tt.Find(m.ManagedDragHandle + " Program 003")
			tt.Press(grip.X+grip.W/2, grip.Y+grip.H/2)
			tt.Move(grip.X+grip.W/2+6, grip.Y+grip.H/2)
			tt.Release(grip.X+grip.W/2+6, grip.Y+grip.H/2)
		})
	}
}
