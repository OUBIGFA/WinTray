//go:build windows

package ui

import (
	"fmt"
	native "github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func nativeProgramsFixture(count int) *MainWindow {
	settings := config.DefaultSettings()
	for i := 0; i < count; i++ {
		settings.ManagedApps = append(settings.ManagedApps, config.ManagedAppEntry{ID: fmt.Sprint(i), Name: fmt.Sprintf("Program %03d", i), ExePath: fmt.Sprintf(`C:\WinTray-test\app%d.exe`, i), RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}, Schedule: config.Schedule{AutoExitMinutes: 30, FrequencyDays: 1, FrequencyRuns: 1}})
	}
	return &MainWindow{settings: settings}
}

// Scrolling may move program rows, never the toolbar, editor or footer.
func TestNativeListScrollKeepsPageFixed(t *testing.T) {
	w := nativeProgramsFixture(100)
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	m := i18n.For("zh-CN")
	labels := []string{m.AddProgram, m.OpenSettings, m.ManagedLaunchNow, m.RemoveSelected, m.ExitApp}
	before := make(map[string]native.Rect)
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing %s", label)
		}
		before[label] = r
	}
	row, _ := tt.Find("Program 005")
	captureNativeTest(t, tt, "programs")
	tt.Move(row.X+5, row.Y+5)
	tt.Scroll(row.X+5, row.Y+5, 0, 1200)
	captureNativeTest(t, tt, "programs-scrolled")
	if first, last := w.desktop.list.Visible(); first == 0 {
		t.Fatalf("list did not scroll: row=%+v visible=%d..%d buttons=%+v", row, first, last, before)
	}
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok || r != before[label] {
			t.Errorf("scroll moved %s: %+v -> %+v", label, before[label], r)
		}
	}
}

func captureNativeTest(t *testing.T, tt *native.Tester, name string) {
	t.Helper()
	dir := os.Getenv("WINTRAY_UI_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestNativePausedProgramKeepsIndependentAutoExit(t *testing.T) {
	w := nativeProgramsFixture(1)
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For("zh-CN")
	for _, label := range []string{"Program 000", m.ManagedEnabled, m.ManagedSchedule, m.ManagedLaunchHidden} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	entry := w.settings.ManagedApps[0]
	if entry.RunOnStartup || !entry.Schedule.Enabled || entry.Schedule.AutoExitMinutes != 30 || !entry.TrayBehavior.AutoMinimizeAndHideOnLaunch {
		t.Fatalf("paused settings changed incorrectly: %+v", entry)
	}
}

// Turning off sign-in startup must not prevent preparing any schedule field.
func TestNativePausedProgramCanEditFullSchedule(t *testing.T) {
	w := nativeProgramsFixture(1)
	store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
	w.callbacks.OnSave = func(s config.Settings) {
		if err := store.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For("zh-CN")
	for _, label := range []string{"Program 000", m.ManagedEnabled, m.ManagedSchedule, m.ManagedFrequency} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if !w.settings.ManagedApps[0].Schedule.FrequencyEnabled {
		t.Fatal("frequency cannot be enabled with sign-in startup off")
	}
	for _, field := range []struct {
		label, value string
		fieldFirst   bool // Find returns the input when it appears before its text label.
	}{
		{m.ManagedScheduleStart, "17", false},
		{m.ManagedScheduleExit, "43", false},
		{m.ManagedFrequencyDays, "7", true},
		{m.ManagedFrequencyRuns, "3", true},
	} {
		r, ok := tt.Find(field.label)
		if !ok {
			t.Fatalf("missing %s", field.label)
		}
		x := r.X + r.W + 24
		if field.fieldFirst {
			x = r.X + r.W/2
		}
		tt.ClickAt(x, r.Y+r.H/2)
		tt.Key(native.Ctrl, native.KeyA)
		tt.Type(field.value)
		tt.Key(0, native.KeyEnter)
	}
	w.commitNativeEdits()
	saved, err := store.LoadWithError()
	if err != nil {
		t.Fatal(err)
	}
	entry := saved.ManagedApps[0]
	if entry.RunOnStartup || !entry.Schedule.Enabled || !entry.Schedule.FrequencyEnabled ||
		entry.Schedule.StartDelayMinutes != 17 || entry.Schedule.AutoExitMinutes != 43 ||
		entry.Schedule.FrequencyDays != 7 || entry.Schedule.FrequencyRuns != 3 {
		t.Fatalf("independent timing did not persist: %+v", entry)
	}
}

func TestNativeProgramEditsPersist(t *testing.T) {
	w := nativeProgramsFixture(3)
	saved := config.Settings{}
	w.callbacks.OnSave = func(s config.Settings) {
		saved = s
		saved.ManagedApps = append([]config.ManagedAppEntry(nil), s.ManagedApps...)
	}
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For("zh-CN")
	for _, label := range []string{"Program 000", m.ManagedLaunchHidden, m.ManagedSchedule, m.ManagedFrequency} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	entry := saved.ManagedApps[0]
	if !entry.LaunchHiddenInBackground || entry.TrayBehavior.AutoMinimizeAndHideOnLaunch || !entry.Schedule.FrequencyEnabled {
		t.Fatalf("edits were not persisted: %+v", entry)
	}
}

func TestNativeBlankListClickClearsSelection(t *testing.T) {
	w := nativeProgramsFixture(2)
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	tt.ClickAt(180, 650)
	if w.desktop.selected != -1 {
		t.Fatal("blank list click kept the editor selected")
	}
}

// The empty editor must neither retain a real program's values nor save or
// launch the illustrative entry, including after the last program is removed.
func TestNativeUnselectedEditorShowsDisabledDefaults(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		for _, scenario := range []string{"empty", "unselected", "deselected", "removed"} {
			t.Run(lang+"/"+scenario, func(t *testing.T) {
				count := 1
				if scenario == "empty" {
					count = 0
				}
				w := nativeProgramsFixture(count)
				w.settings.Language = lang
				if count > 0 {
					w.settings.ManagedApps[0].Args = "--real-program"
					w.settings.ManagedApps[0].TrayBehavior.CloseDelaySeconds = 45
				}
				saves, launches := 0, 0
				w.callbacks.OnSave = func(config.Settings) { saves++ }
				w.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { launches++ }
				tt := native.NewTester(w.nativeView, 1200, 850)
				m := i18n.For(lang)
				if scenario == "deselected" || scenario == "removed" {
					if err := tt.Click("Program 000"); err != nil {
						t.Fatal(err)
					}
					if scenario == "removed" {
						if err := tt.Click(m.RemoveSelected); err != nil {
							t.Fatal(err)
						}
					} else {
						tt.ClickAt(180, 650)
					}
				}
				before := slices.Clone(w.settings.ManagedApps)
				saves = 0
				captureNativeTest(t, tt, lang+"-editor-"+scenario)
				for _, label := range []string{m.ManagedExampleName, `C:\Apps\Example\Example.exe`, m.ManagedLaunchNow, m.BrowseProgram, m.ManagedEnabled, m.ManagedModeLabel, m.ManagedAppArgs, m.ManagedCloseDelay, m.ManagedSchedule, m.ManagedScheduleStart, m.ManagedScheduleExit, m.ManagedFrequency, m.ManagedFrequencyDays, m.ManagedFrequencyRuns, m.RemoveSelected} {
					if !tt.HasText(label) {
						t.Fatalf("unselected editor missing %q", label)
					}
				}
				if tt.HasText(`C:\WinTray-test\app0.exe`) {
					t.Fatal("unselected editor retained the real program path")
				}
				for _, label := range []string{m.ManagedLaunchNow, m.ManagedEnabled, m.ManagedLaunchHidden, m.ManagedSchedule, m.ManagedFrequency, m.RemoveSelected} {
					if err := tt.Click(label); err != nil {
						t.Fatal(err)
					}
				}
				r, _ := tt.Find(m.ManagedCloseDelay)
				tt.ClickAt(r.X+r.W+24, r.Y+r.H/2)
				if tt.Focused(m.ManagedCloseDelay) {
					t.Fatal("disabled numeric field accepted focus")
				}
				tt.Key(native.Ctrl, native.KeyA)
				tt.Type("99")
				tt.Key(0, native.KeyUp)
				tt.Key(0, native.KeyEnter)
				w.commitNativeEdits()
				if saves != 0 || launches != 0 || !slices.Equal(before, w.settings.ManagedApps) {
					t.Fatalf("disabled editor accepted input: saves=%d launches=%d", saves, launches)
				}
				if len(before) > 0 {
					if err := tt.Click("Program 000"); err != nil {
						t.Fatal(err)
					}
					if !tt.HasText(before[0].ExePath) || tt.HasText(m.ManagedExampleName) {
						t.Fatal("selecting a program did not replace the example")
					}
					if err := tt.Click(m.ManagedEnabled); err != nil {
						t.Fatal(err)
					}
					if w.settings.ManagedApps[0].RunOnStartup || saves != 1 {
						t.Fatal("selecting a program did not re-enable editing")
					}
				}
			})
		}
	}
}

func TestNativeOrderDropdownSavesFinalPosition(t *testing.T) {
	w := nativeProgramsFixture(3)
	saved := 0
	w.callbacks.OnSave = func(config.Settings) { saved++ }
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("序号 Program 000"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyEnd)
	tt.Key(0, native.KeyEnter)
	if w.settings.ManagedApps[2].ID != "0" || w.desktop.selected != 2 || saved != 1 {
		t.Fatalf("order selection: selected=%d saves=%d apps=%+v", w.desktop.selected, saved, w.settings.ManagedApps)
	}
	if err := tt.Click("序号 Program 000"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, native.KeyHome)
	tt.Key(0, native.KeyEnter)
	if w.settings.ManagedApps[0].ID != "0" || saved != 2 {
		t.Fatal("choosing first position did not move and save")
	}
}

func TestNativeDraggingDoesNotReorder(t *testing.T) {
	w := nativeProgramsFixture(3)
	saved := 0
	w.callbacks.OnSave = func(config.Settings) { saved++ }
	tt := native.NewTester(w.nativeView, 1200, 850)
	first, _ := tt.Find("Program 000")
	last, _ := tt.Find("Program 002")
	x := first.X + 160
	tt.Press(x, first.Y+first.H/2)
	tt.Move(x, last.Y+last.H-2)
	tt.Release(x, last.Y+last.H-2)
	if w.settings.ManagedApps[0].ID != "0" || saved != 0 {
		t.Fatal("removed drag gesture still changed the order")
	}
}

func TestNativeClosingCommitsEditedNumber(t *testing.T) {
	w := nativeProgramsFixture(1)
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	label := i18n.For("zh-CN").ManagedCloseDelay
	// The label and field share accessible text; the field follows the label.
	r, ok := tt.Find(label)
	if !ok {
		t.Fatal("close-delay label missing")
	}
	tt.ClickAt(r.X+r.W+24, r.Y+r.H/2)
	tt.Key(native.Ctrl, native.KeyA)
	tt.Type("45")
	w.commitNativeEdits()
	if got := w.settings.ManagedApps[0].TrayBehavior.CloseDelaySeconds; got != 45 {
		t.Fatalf("%s lost on close: %d", label, got)
	}
}

func TestNativeFullPagesFitWithoutPageScroll(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run(lang, func(t *testing.T) {
			w := nativeProgramsFixture(10)
			w.settings.Language = lang
			w.settings.RunAtLogon = false
			w.settings.ManagedApps[0].Schedule.Enabled = true
			w.settings.ManagedApps[0].Schedule.FrequencyEnabled = true
			tt := native.NewTester(w.nativeView, 1200, 850)
			if err := tt.Click("Program 000"); err != nil {
				t.Fatal(err)
			}
			m := i18n.For(lang)
			check := func(labels ...string) {
				for _, label := range labels {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 1201 || r.Y+r.H > 851 {
						t.Errorf("%q clipped: %+v found=%t", label, r, ok)
					}
				}
			}
			captureNativeTest(t, tt, lang+"-full-programs")
			check(m.OpenSettings, m.ManagedFrequencyHint, m.RemoveSelected, m.ExitApp, m.RunSilently)
			tt.SetSize(1184, 811)
			remove, ok := tt.Find(m.RemoveSelected)
			footer, _ := tt.Find(m.ExitApp)
			if !ok || remove.W <= 0 || remove.Y+remove.H > footer.Y {
				t.Errorf("editor overlaps footer with window chrome: remove=%+v footer=%+v", remove, footer)
			}
			if err := tt.Click(m.OpenSettings); err != nil {
				t.Fatal(err)
			}
			captureNativeTest(t, tt, lang+"-settings")
			check(m.RunAtLogon, m.CleanupRestore, m.ExitApp, m.BackToPrograms)
			tt.SetSize(1040, 680)
			for _, label := range []string{m.CleanupRestore, m.ExitApp} {
				r, ok := tt.Find(label)
				if !ok || r.Y+r.H > 680 {
					t.Errorf("small settings clips %s: %+v", label, r)
				}
			}
			if err := tt.Click(m.BackToPrograms); err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{m.ManagedFrequencyHint, m.RemoveSelected, m.ExitApp} {
				r, ok := tt.Find(label)
				if !ok || r.Y+r.H > 680 {
					t.Errorf("small programs clips %s: %+v", label, r)
				}
			}
		})
	}
}

// A framework migration must still expose both pages and the actions on them.
func TestNativePagesKeepExistingActions(t *testing.T) {
	settings := config.DefaultSettings()
	w := &MainWindow{settings: settings}
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For("zh-CN")
	for _, label := range []string{m.AddProgram, m.OpenSettings, m.ExitApp, m.RunSilently} {
		if !tt.HasText(label) {
			t.Errorf("missing action %q", label)
		}
	}
	if err := tt.Click(m.OpenSettings); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{m.RunAtLogon, m.StartHidden, m.ExitOnDone, m.OpenLogs, m.RemoveLogonTask, m.CleanupRestore, m.BackToPrograms} {
		if !tt.HasText(label) {
			t.Errorf("missing setting %q", label)
		}
	}
	if err := tt.Click(m.BackToPrograms); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(m.AddProgram) {
		t.Fatal("back did not restore programs page")
	}
}
