package orchestrator

import (
	"context"
	"sync"
	"time"

	"wintray/internal/config"
)

// startupSequence owns the launch clock for one autorun batch. Only one task
// may decide or perform a launch at a time; window handling continues in the
// background after that task releases its turn.
type startupSequence struct {
	interval   time.Duration
	nextLaunch time.Time
}

type startupTurn struct {
	sequence *startupSequence
	done     chan struct{}
	once     sync.Once
}

func (t *startupTurn) wait(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	return t == nil || waitWithContext(ctx, time.Until(t.sequence.nextLaunch))
}

// finish releases the next task, not the next window action. Failed/skipped
// launches do not consume an interval. Updating the clock before closing done
// makes the clock visible to the next task without a lock or polling.
func (t *startupTurn) finish(started bool) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		if started {
			t.sequence.nextLaunch = time.Now().Add(t.sequence.interval)
		}
		close(t.done)
	})
}

// StartManagedApps starts enabled entries in list order, spacing actual process
// launches. Existing processes and externally owned startups release their turn
// before waiting for windows, so a login delay or missing Run process cannot
// stall the launch queue. Results keep list order (paused entries are omitted).
// onResult is called concurrently as tasks finish, before the batch returns;
// callers can hand hidden windows to a tray host immediately.
func (s *Service) StartManagedApps(ctx context.Context, settings config.Settings, onResult func(config.ManagedAppEntry, Result)) []Result {
	entries := make([]config.ManagedAppEntry, 0, len(settings.ManagedApps))
	for _, entry := range settings.ManagedApps {
		if config.ShouldLaunchViaWinTray(entry) {
			entries = append(entries, entry)
		}
	}
	sequence := &startupSequence{
		interval: time.Duration(config.ClampStartupIntervalSeconds(settings.StartupIntervalSeconds)) * time.Second,
	}
	s.logger.Info("managed startup queue: interval=" + sequence.interval.String())
	results := make([]Result, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		turn := &startupTurn{sequence: sequence, done: make(chan struct{})}
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts := managedStartOptions(entry)
			opts.turn = turn
			result := s.start(ctx, entry, settings.CloseWindowRetrySeconds, opts)
			results[i] = result
			if onResult != nil {
				onResult(entry, result)
			}
		}()
		<-turn.done
	}
	wg.Wait()
	return results
}
