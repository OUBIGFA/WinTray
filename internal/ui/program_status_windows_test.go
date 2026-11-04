//go:build windows

package ui

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

func waitNativeProgramStatus(t *testing.T, w *MainWindow, tt *native.Tester, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.drain()
		tt.Frame()
		if status := w.programStatuses[id]; status != nil && !status.pending {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("program status did not arrive")
}

// A slow system query must neither stall a frame nor multiply with redraws.
func TestNativeProgramStatusDoesNotBlockRendering(t *testing.T) {
	w := nativeProgramsFixture(2)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	watchdog := time.AfterFunc(2*time.Second, release)
	defer watchdog.Stop()
	var queries atomic.Int32
	w.callbacks.OnProgramRunning = func(config.ManagedAppEntry) bool {
		queries.Add(1)
		<-gate
		return true
	}
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		tt.Frame()
	}
	select {
	case <-gate:
		t.Fatal("render waited for the blocked process query")
	default:
	}
	release()
	waitNativeProgramStatus(t, w, tt, "0")
	m := i18n.For(w.settings.Language)
	if !tt.HasText(m.ManagedStopProgram) || queries.Load() != 1 {
		t.Fatalf("query not coalesced/published: queries=%d texts=%v", queries.Load(), tt.Texts())
	}
	for i := 0; i < 10; i++ {
		tt.Frame()
	}
	if queries.Load() != 1 {
		t.Fatal("fresh status was queried again on redraw")
	}
	// A new observation must still notice external process exits.
	w.callbacks.OnProgramRunning = func(config.ManagedAppEntry) bool { return false }
	w.programStatuses["0"].checkedAt = time.Time{}
	tt.Frame()
	waitNativeProgramStatus(t, w, tt, "0")
	if !tt.HasText(m.ManagedLaunchNow) {
		t.Fatal("stale running result hid the launch action")
	}
}

func TestNativeProgramStatusDiscardsReplacedPath(t *testing.T) {
	w := nativeProgramsFixture(1)
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	defer release()
	w.callbacks.OnProgramRunning = func(config.ManagedAppEntry) bool { <-gate; return true }
	tt := native.NewTester(w.nativeView, 1200, 850)
	if err := tt.Click("Program 000"); err != nil {
		t.Fatal(err)
	}
	old := w.programStatuses["0"]
	w.settings.ManagedApps[0].ExePath = `C:\Different\program.exe`
	w.callbacks.OnProgramRunning = func(config.ManagedAppEntry) bool { return false }
	tt.Frame()
	waitNativeProgramStatus(t, w, tt, "0")
	release()
	// The old worker must have posted before checking its discarded result.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.queueMu.Lock()
		queued := len(w.queue) != 0
		w.queueMu.Unlock()
		if queued {
			w.drain()
			if w.programStatuses["0"] == old || w.programStatuses["0"].running {
				t.Fatal("old path's result replaced the new program's status")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("old worker did not finish")
}
