package app

import (
	"os"
	"time"
)

const exitGracePeriod = 2 * time.Second

// An explicit exit must outlive neither a hung Shell call nor a blocked
// startup worker. Arm before any cleanup, and stop only after Run's outer
// defers have completed. Process exit also releases the recovery pipe, so the
// separate tray recovery process can finish without holding WinTray open.
type exitDeadline struct {
	timer *time.Timer
}

func (d *exitDeadline) start() {
	if d.timer == nil {
		d.timer = time.AfterFunc(exitGracePeriod, func() { os.Exit(0) })
	}
}

func (d *exitDeadline) stop() {
	if d.timer != nil {
		d.timer.Stop()
	}
}
