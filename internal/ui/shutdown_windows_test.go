//go:build windows

package ui

import (
	"context"
	"os"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func TestExitButtonPrecedesPendingSave(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		w := nativeProgramsFixture(1)
		m := i18n.For(w.settings.Language)
		exiting, saves := false, 0
		w.callbacks.OnExit = func() {
			exiting = true // the app arms its deadline here
			w.RequestExplicitClose()
		}
		w.callbacks.OnSave = func(config.Settings) {
			saves++
			if !cancel && !exiting {
				t.Error("exit was preceded by an unbounded save")
			}
		}
		tt := native.NewTester(w.nativeView, 1200, 850)
		if err := tt.Click("Program 000"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click(m.ManagedAppArgs); err != nil {
			t.Fatal(err)
		}
		tt.Type("--pending-exit")
		button, _ := tt.Find(m.ExitApp)
		x, y := button.X+button.W/2, button.Y+button.H/2
		tt.Press(x, y)
		if saves != 0 || exiting {
			t.Fatalf("pointer-down saved or exited before the click: saves=%d exiting=%t", saves, exiting)
		}
		if cancel {
			tt.Move(-1, -1)
			tt.Release(-1, -1)
			if exiting || saves != 1 {
				t.Fatalf("cancelled click did not resume normal saving: saves=%d exiting=%t", saves, exiting)
			}
		} else {
			tt.Release(x, y)
			if !exiting {
				t.Fatal("exit callback did not run")
			}
			w.commitNativeEdits()
			if saves != 1 || w.settings.ManagedApps[0].Args != "--pending-exit" {
				t.Fatal("accepted exit lost the pending edit")
			}
		}
	}
}

func TestConfirmationAfterExitReturnsWithoutMessageLoop(t *testing.T) {
	w, err := NewMainWindow(config.DefaultSettings(), Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	w.RequestExplicitClose()
	done := make(chan bool, 1)
	go func() { done <- w.ConfirmContext(context.Background(), "exit", "must not wait") }()
	select {
	case yes := <-done:
		if yes {
			t.Fatal("confirmation after exit was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation waited on a closing message loop")
	}
}

// Use a real message loop: a modal message ahead of the queued close would
// otherwise block this child until the parent's timeout.
func TestExplicitCloseSuppressesQueuedDialogs(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	if runWindowTestInChild(t) {
		return
	}
	runTestMainThread(func() {
		w, err := NewMainWindow(config.DefaultSettings(), Callbacks{})
		if err != nil {
			t.Fatal(err)
		}
		w.OnStarting(func() {
			w.ShowError("exit regression", "queued before exit")
			w.RequestExplicitClose()
			w.ShowInfo("exit regression", "queued after exit")
			if w.Confirm("exit regression", "must not ask after exit") {
				t.Error("shutdown accepted a new confirmation")
			}
		})
		started := time.Now()
		if code := w.Run(); code != 0 {
			t.Errorf("exit code %d", code)
		}
		if time.Since(started) > 2*time.Second {
			t.Error("dialogs delayed exit")
		}
	})
}
