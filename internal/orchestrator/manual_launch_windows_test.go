//go:build windows

package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wintray/internal/config"
)

func TestManualLaunchDoesNotWaitForScheduledStartup(t *testing.T) {
	scheduled := queueTestEntry(t, "scheduled")
	scheduled.Schedule = config.Schedule{Enabled: true, StartDelayMinutes: 1}
	other := queueTestEntry(t, "manual")
	settings := config.DefaultSettings()
	settings.ManagedApps = []config.ManagedAppEntry{scheduled}
	svc := queueTestService()

	// Keep the scheduler pending while manual launches finish. Advance the
	// observed sign-in age only after cancelling that batch, so this test does
	// not depend on process creation winning a short wall-clock deadline.
	ready := make(chan struct{})
	old := logonTimeLookup
	logonTimeLookup = func() (time.Time, error) {
		close(ready)
		return time.Now(), nil
	}
	defer func() { logonTimeLookup = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	results := make(chan []Result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		results <- svc.StartManagedApps(ctx, settings, nil)
	}()
	defer func() { cancel(); <-done }()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("scheduled startup did not begin")
	}
	for _, entry := range []config.ManagedAppEntry{other, scheduled} {
		result := svc.StartNow(ctx, entry, 0)
		if result.Code != ResultStartedHidden {
			t.Fatalf("manual launch of %s failed while scheduling: %+v", entry.Name, result)
		}
		queueProcessStart(t, entry)
	}
	select {
	case got := <-results:
		t.Fatalf("scheduled startup ended before its deadline: %+v", got)
	default:
	}
	cancel()
	<-done
	if got := <-results; len(got) != 1 || got[0].Code != ResultCancelled {
		t.Fatalf("pending schedule did not cancel: %+v", got)
	}

	logonTimeLookup = func() (time.Time, error) { return time.Now().Add(-time.Minute), nil }
	dueCtx, cancelDue := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDue()
	got := svc.StartManagedApps(dueCtx, settings, nil)
	if len(got) != 1 || got[0].Code != ResultAlreadyRunningSkipped {
		t.Fatalf("scheduled startup did not adopt the manual launch: %+v", got)
	}
	queueProcessStart(t, scheduled) // exactly one process when the schedule is due
}

// Opening manual actions during autorun permits two launch requests for the
// same executable. A task submission is not yet a process: the second request
// must wait for the first process, then recheck instead of submitting twice.
func TestManualAndAutomaticLaunchShareProcessCreation(t *testing.T) {
	for _, manualFirst := range []bool{true, false} {
		name := "automatic first"
		if manualFirst {
			name = "manual first"
		}
		t.Run(name, func(t *testing.T) {
			entry := queueTestEntry(t, "overlapping launch")
			entry.LaunchViaLogonTask = true
			entry.Schedule = config.Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}
			other := queueTestEntry(t, "independent launch")
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			var submissions atomic.Int32
			checked := make(chan struct{})
			var checkedOnce sync.Once
			svc := NewService(callbackEnumerator(func() []ManagedWindowInfo {
				if submissions.Load() == 1 {
					checkedOnce.Do(func() { close(checked) })
				}
				return nil
			}), &testManager{}, &testLogger{})
			svc.startupHistory = config.NewStartupHistory(filepath.Join(t.TempDir(), "history.json"))
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			duplicate := make(chan struct{}, 1)
			svc.logonTaskLaunch = func(app config.ManagedAppEntry) error {
				if submissions.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				} else {
					duplicate <- struct{}{}
				}
				cmd, err := startProcess(app.ExePath, app.Args, launchNoWindow)
				if err == nil {
					_ = cmd.Process.Release()
				}
				return err
			}
			first, second := svc.StartAndManage, svc.StartNow
			if manualFirst {
				first, second = second, first
			}
			var wg sync.WaitGroup
			t.Cleanup(func() {
				cancel()
				releaseOnce.Do(func() { close(release) })
				wg.Wait()
			})
			firstResult, secondResult := make(chan Result, 1), make(chan Result, 1)
			wg.Add(1)
			go func() { defer wg.Done(); firstResult <- first(ctx, entry, 0) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("first launch did not submit its task")
			}
			secondEntry := entry
			secondEntry.ExePath = strings.ToUpper(entry.ExePath)
			wg.Add(1)
			go func() { defer wg.Done(); secondResult <- second(ctx, secondEntry, 0) }()
			select {
			case <-checked:
			case <-ctx.Done():
				t.Fatal("second launch did not check the pending program")
			}
			select {
			case <-duplicate:
				t.Error("overlapping launch submitted a second task before the first process appeared")
			case <-time.After(200 * time.Millisecond):
			}
			// Cancelling another request must neither launch nor release the
			// first request's gate while its task is still pending.
			cancelledCtx, cancelRequest := context.WithTimeout(ctx, 50*time.Millisecond)
			got := svc.StartNow(cancelledCtx, entry, 0)
			cancelRequest()
			if got.Code != ResultCancelled {
				t.Fatalf("cancelled overlapping request = %+v", got)
			}
			// The guard must be per executable, not another global launch lock.
			if got := svc.StartNow(ctx, other, 0); got.Code != ResultStartedHidden {
				t.Fatalf("unrelated program blocked by pending launch: %+v", got)
			}
			releaseOnce.Do(func() { close(release) })
			if got := <-firstResult; got.Code != ResultStartedOnly {
				t.Fatalf("first launch result = %+v", got)
			}
			if got := <-secondResult; got.Code != ResultAlreadyRunningSkipped {
				t.Fatalf("second launch result = %+v, want existing process", got)
			}
			if got := submissions.Load(); got != 1 {
				t.Fatalf("task submitted %d times, want once", got)
			}
			queueProcessStart(t, entry)
		})
	}
}
