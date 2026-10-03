//go:build windows

package orchestrator

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"wintray/internal/config"
)

// The ordinary resident session has no automatic-exit work. Its periodic
// check must not normalize paths or allocate per-program bookkeeping.
func TestEndOverdueProgramsWithoutLimitsDoesNotAllocate(t *testing.T) {
	entries := make([]config.ManagedAppEntry, 50)
	for i := range entries {
		entries[i].ExePath = fmt.Sprintf(`C:\Apps\app-%d.exe`, i)
	}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	now := time.Now()
	allocs := testing.AllocsPerRun(2, func() {
		if check := svc.EndOverduePrograms(entries, now); check.Running != 0 || !check.Next.IsZero() {
			t.Fatalf("programs without limits kept pending: %+v", check)
		}
	})
	if allocs != 0 {
		t.Fatalf("idle run-limit check allocated %.0f objects; want no work without limits", allocs)
	}
}

// Absent executables keep the benchmark read-only while exercising the same
// process discovery used for programs waiting to start outside WinTray.
func BenchmarkEndOverduePrograms(b *testing.B) {
	for _, count := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("apps_%d", count), func(b *testing.B) {
			entries := make([]config.ManagedAppEntry, count)
			for i := range entries {
				entries[i] = config.ManagedAppEntry{
					ExePath:  filepath.Join(b.TempDir(), fmt.Sprintf("absent-%d.exe", i)),
					Schedule: config.Schedule{Enabled: true, AutoExitMinutes: 30},
				}
			}
			svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
			now := time.Now()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if check := svc.EndOverduePrograms(entries, now); check.Running != 0 {
					b.Fatalf("absent programs reported running: %+v", check)
				}
			}
		})
	}
}
