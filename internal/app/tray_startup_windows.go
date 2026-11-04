//go:build windows

package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wintray/internal/ipc"
	"wintray/internal/traybox"
)

// trayStartup couples collection with the logon-task marker. Both the initial
// attempt and later selection changes must publish readiness after success;
// otherwise a recovered collector leaves the original program tasks blocked.
// All mutations, including retry callbacks, belong to the UI thread.
type trayStartup struct {
	setPaths  func([]string) error
	readyName string
	ready     chan struct{}
	release   func()
}

func (s *trayStartup) collect(paths []string) error {
	if err := s.setPaths(paths); err != nil {
		return err
	}
	if s.release != nil {
		return nil
	}
	release, err := ipc.MarkReady(s.readyName)
	if err != nil {
		return fmt.Errorf("publish tray startup readiness: %w", err)
	}
	s.release = release
	close(s.ready)
	return nil
}

func (s *trayStartup) close() {
	if s.release != nil {
		s.release()
		s.release = nil
	}
}

// The desktop may not exist when the logon task starts. Retry only that
// temporary condition, not missing components or a rejected collection.
// The caller reads current settings on the UI thread; never replay the boot
// selection after the user has changed it. Cancellation also guards callbacks
// already queued when the user chooses Exit.
func retryTrayCollection(ctx context.Context, synchronize func(func()), collect func() error, report func(error)) {
	for waitContext(ctx, time.Second) {
		result := make(chan error, 1)
		synchronize(func() {
			if err := ctx.Err(); err != nil {
				result <- err
				return
			}
			result <- collect()
		})
		select {
		case err := <-result:
			if ctx.Err() != nil || err == nil {
				return
			}
			if !errors.Is(err, traybox.ErrTaskbarUnavailable) {
				report(err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
