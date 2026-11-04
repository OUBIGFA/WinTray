//go:build windows

package ui

import (
	"time"

	"wintray/internal/config"
)

type programStatus struct {
	path      string
	running   bool
	pending   bool
	checkedAt time.Time
}

// Program discovery can read other windows, including our own. Never wait for
// it inside a render callback: WM_GETTEXT can need the very same UI thread.
// Reuse each result for a second instead of scanning on every pointer/frame.
// Hidden settings do not render and therefore do not keep polling processes.
func (w *MainWindow) programRunning(entry config.ManagedAppEntry) bool {
	query := w.callbacks.OnProgramRunning
	if query == nil || w.closing.Load() {
		return false
	}
	if w.programStatuses == nil {
		w.programStatuses = make(map[string]*programStatus)
	}
	status := w.programStatuses[entry.ID]
	if status == nil || status.path != entry.ExePath {
		status = &programStatus{path: entry.ExePath}
		w.programStatuses[entry.ID] = status
	}
	if !status.pending && time.Since(status.checkedAt) >= time.Second {
		status.pending = true
		go func() {
			running := query(entry)
			w.Synchronize(func() {
				// A replaced/removed entry or completed action invalidates a
				// pending observation. It must not overwrite the newer state.
				if w.programStatuses[entry.ID] == status {
					status.running, status.pending = running, false
					status.checkedAt = time.Now()
				}
			})
		}()
	}
	return status.running
}
