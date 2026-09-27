package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"wintray/internal/config"
)

// logonTaskWaitMargin pads the wait for a task-launched program beyond its
// scheduled delay, covering slow process creation. When the program is due
// is the task's decision, not WinTray's. A variable so tests can shorten it.
var logonTaskWaitMargin = 120 * time.Second

// queuedStart carries one entry's launch decision made ahead of its turn.
// A scheduled entry waits for its start delay outside the queue, so it holds
// no turn from the programs behind it.
type queuedStart struct {
	entry        config.ManagedAppEntry
	externalWait time.Duration
	startDelay   time.Duration
}

// startupSequence owns the launch clock for one autorun batch. Only one task
// may decide or perform a launch at a time; window handling continues in the
// background after that task releases its turn.
type startupSequence struct {
	interval   time.Duration
	nextLaunch time.Time
	gate       chan struct{}
}

type startupTurn struct {
	sequence *startupSequence
	done     chan struct{}
	once     sync.Once
	acquired bool
}

func (t *startupTurn) wait(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if t == nil {
		return true
	}
	select {
	case t.sequence.gate <- struct{}{}:
		t.acquired = true
	case <-ctx.Done():
		return false
	}
	return waitWithContext(ctx, time.Until(t.sequence.nextLaunch))
}

// finish releases the next task, not the next window action. Failed/skipped
// launches do not consume an interval. Updating the clock before closing done
// makes the clock visible to the next task without a lock or polling.
func (t *startupTurn) finish(started bool) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		if started && t.acquired {
			t.sequence.nextLaunch = time.Now().Add(t.sequence.interval)
		}
		if t.acquired {
			<-t.sequence.gate
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
	entries := make([]queuedStart, 0, len(settings.ManagedApps))
	taskIndex := 0
	for _, entry := range settings.ManagedApps {
		if !config.ShouldLaunchViaWinTray(entry) {
			continue
		}
		queued := queuedStart{entry: entry}
		if entry.LaunchViaLogonTask {
			// Task Scheduler starts it at the delay the task was registered
			// with (config.LogonTaskApps); the wait only needs to cover that
			// delay plus a margin.
			delay := max(config.LogonTaskDelaySeconds(taskIndex, settings.StartupIntervalSeconds), config.ScheduledStartDelaySeconds(entry))
			taskIndex++
			queued.externalWait = time.Duration(delay)*time.Second + logonTaskWaitMargin
		} else {
			queued.startDelay = time.Duration(config.ScheduledStartDelaySeconds(entry)) * time.Second
		}
		entries = append(entries, queued)
	}
	sequence := &startupSequence{
		interval: time.Duration(config.ClampStartupIntervalSeconds(settings.StartupIntervalSeconds)) * time.Second,
		gate:     make(chan struct{}, 1),
	}
	s.logger.Info("managed startup queue: interval=" + sequence.interval.String())
	results := make([]Result, len(entries))
	var wg sync.WaitGroup
	begin := time.Now()
	for _, queued := range entries {
		if queued.startDelay > 0 {
			if logon, err := logonTimeLookup(); err == nil {
				begin = logon
			} else {
				s.logger.Warn(fmt.Sprintf("sign-in time unavailable, using WinTray start for schedules: %v", err))
			}
			break
		}
	}
	for i, queued := range entries {
		if queued.startDelay > 0 {
			turn := &startupTurn{sequence: sequence, done: make(chan struct{})}
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.logger.Info(fmt.Sprintf("scheduled start: %s in %s", queued.entry.Name, queued.startDelay))
				result := cancelledResult(queued.entry)
				if waitWithContext(ctx, time.Until(begin.Add(queued.startDelay))) {
					opts := managedStartOptions(queued.entry)
					opts.turn = turn
					result = s.start(ctx, queued.entry, settings.CloseWindowRetrySeconds, opts)
				}
				results[i] = result
				if onResult != nil {
					onResult(queued.entry, result)
				}
			}()
			continue
		}
		turn := &startupTurn{sequence: sequence, done: make(chan struct{})}
		wg.Add(1)
		go func() {
			defer wg.Done()
			opts := managedStartOptions(queued.entry)
			opts.turn = turn
			opts.externalWait = queued.externalWait
			result := s.start(ctx, queued.entry, settings.CloseWindowRetrySeconds, opts)
			results[i] = result
			if onResult != nil {
				onResult(queued.entry, result)
			}
		}()
		<-turn.done
	}
	wg.Wait()
	return results
}
