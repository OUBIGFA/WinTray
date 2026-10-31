//go:build windows

package ui

import (
	"errors"
	"testing"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestNativeDeleteMenuKeepsProgramIdentityAcrossReorder(t *testing.T) {
	w := nativeProgramsFixture(3)
	// Names need not be unique; menu state must follow the row's ID.
	w.settings.ManagedApps[0].Name = "Same name"
	w.settings.ManagedApps[1].Name = "Same name"
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For(w.settings.Language)
	if err := tt.Click(m.ManagedColumnDelete + " Same name"); err != nil {
		t.Fatal(err)
	}
	w.nativeSetOrder(0, 2)
	tt.Frame()
	if err := tt.ChooseMenuItem(m.RemoveConfirmLabel); err != nil {
		t.Fatal(err)
	}
	if len(w.settings.ManagedApps) != 2 || w.settings.ManagedApps[0].ID != "1" || w.settings.ManagedApps[1].ID != "2" {
		t.Fatalf("confirmation deleted a different row after reordering: %+v", w.settings.ManagedApps)
	}
}

func TestNativeDeleteKeepsAnotherProgramsSelection(t *testing.T) {
	w := nativeProgramsFixture(3)
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For(w.settings.Language)
	if err := tt.Click("Program 002"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(m.ManagedColumnDelete + " Program 000"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(m.RemoveConfirmLabel); err != nil {
		t.Fatal(err)
	}
	selected, ok := w.nativeSelected()
	if !ok || selected.ID != "2" {
		t.Fatalf("deleting another row lost the selected program: %+v", selected)
	}
}

func TestNativeDeleteRestoresCollectedIconBeforeRemovingEntry(t *testing.T) {
	for _, restoreFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "restore failed"}[restoreFails], func(t *testing.T) {
			w := nativeProgramsFixture(2)
			w.settings.ManagedApps[0].CollectTrayIcon = true
			restored, saves := 0, 0
			w.callbacks.OnToggleTrayBox = func(id string, on bool) error {
				if id != "0" || on || len(w.settings.ManagedApps) != 2 {
					t.Fatalf("icon restoration ran after removal or for the wrong program: id=%s on=%t", id, on)
				}
				restored++
				if restoreFails {
					return errors.New("test restore failure")
				}
				return nil
			}
			w.callbacks.OnSave = func(config.Settings) { saves++ }
			tt := native.NewTester(w.nativeView, 1200, 850)
			m := i18n.For(w.settings.Language)
			if err := tt.Click(m.ManagedColumnDelete + " Program 000"); err != nil {
				t.Fatal(err)
			}
			if restored != 0 {
				t.Fatal("opening confirmation already restored the icon")
			}
			if err := tt.ChooseMenuItem(m.RemoveConfirmLabel); err != nil {
				t.Fatal(err)
			}
			if restored != 1 {
				t.Fatalf("restored the icon %d times", restored)
			}
			if restoreFails {
				if len(w.settings.ManagedApps) != 2 || !w.settings.ManagedApps[0].CollectTrayIcon || saves != 0 {
					t.Fatal("failed restoration removed the entry or changed its saved state")
				}
			} else if len(w.settings.ManagedApps) != 1 || w.settings.ManagedApps[0].ID != "1" || saves != 1 {
				t.Fatal("restored entry was not removed and saved once")
			}
		})
	}
}

func TestNativeDeleteLastProgramShowsEmptyEditor(t *testing.T) {
	w := nativeProgramsFixture(1)
	w.callbacks.OnLaunchNow = func(config.ManagedAppEntry) { t.Error("empty editor launched a program") }
	tt := native.NewTester(w.nativeView, 1200, 850)
	m := i18n.For(w.settings.Language)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(m.ManagedColumnDelete + " Program 000"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(m.RemoveConfirmLabel); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(m.ManagedSelectPrompt) || !tt.HasText(m.ManagedListEmpty) || w.desktop.selected != -1 {
		t.Fatal("last deletion left a live editor or stale selection")
	}
	if _, found := tt.Find(m.ManagedLaunchNow); found {
		t.Fatal("empty editor still offers a launch action")
	}
}
