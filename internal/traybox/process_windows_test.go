//go:build windows

package traybox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lxn/win"
)

func TestTaskbarRefreshBatchTargetsSelectedProcessesOnce(t *testing.T) {
	f := startFakeShell(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := canonicalIconPath(exe)
	absent := filepath.Join(t.TempDir(), "absent.exe")
	// Both paths belong to this test. No user's tray icon receives a message.
	counts := postTaskbarCreated([]string{path, absent, path}, fakeTaskbar)
	if counts[path] < 2 || counts[absent] != 0 {
		t.Fatalf("refresh counts = %v; want both fixture windows only", counts)
	}
	// This marker is queued after every refresh, so the assertion also catches
	// duplicate messages still waiting in the target window's queue.
	if win.PostMessage(f.program, testCallback, 0, 0) == 0 {
		t.Fatal("could not queue refresh completion marker")
	}
	waitFor(t, "batched refresh", func() bool { _, events, _ := f.snapshot(); return len(events) == 1 })
	_, _, n := f.snapshot()
	if n != 1 {
		t.Fatalf("duplicate path refreshed the program %d times", n)
	}
}

func TestProcessesByPathFindsOnlySelectedExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := canonicalIconPath(exe)
	got := processesByPath(map[string]bool{path: true})
	if got[uint32(os.Getpid())] != path {
		t.Fatalf("current process missing: %v", got)
	}
	for pid, found := range got {
		if found != path {
			t.Fatalf("unselected process %d returned: %q", pid, found)
		}
	}
}

func TestProcessesByPathEmptySelection(t *testing.T) {
	allocs := testing.AllocsPerRun(2, func() {
		if got := processesByPath(nil); len(got) != 0 {
			t.Fatalf("empty selection matched processes: %v", got)
		}
	})
	if allocs != 0 {
		t.Fatalf("empty selection allocated %.0f objects; want no scan without a selection", allocs)
	}
}

// Use the test process so path matching and canonicalization are included,
// without posting messages to any real tray icon.
func BenchmarkProcessesByPath(b *testing.B) {
	exe, err := os.Executable()
	if err != nil {
		b.Fatal(err)
	}
	path := canonicalIconPath(exe)
	wanted := map[string]bool{path: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := processesByPath(wanted); got[uint32(os.Getpid())] != path {
			b.Fatal("current process missing")
		}
	}
}
