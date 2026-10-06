//go:build windows

package traybox

import (
	"context"
	"time"

	"github.com/lxn/win"
)

// OpenIcon is the user-facing open gesture, distinct from raw native delivery.
// latest runs through the caller's UI dispatcher, so a removed or replaced
// registration can never receive the delayed double click. This call waits
// off the UI thread; only the native click delivery itself is immediate.
func OpenIcon(ctx context.Context, icon Icon, latest func() (Icon, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Activate(icon, ActionClick); err != nil {
		return err
	}
	var ownerPID uint32
	win.GetWindowThreadProcessId(win.HWND(icon.owner), &ownerPID)
	matching := map[uint32]bool{ownerPID: Contains([]string{icon.ExePath}, processPath(ownerPID))}
	deadline := time.NewTimer(650 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if iconHasVisibleWindow(icon, matching) {
				return nil
			}
		case <-deadline.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			if latest != nil {
				current, err := latest()
				if err != nil {
					return err
				}
				if !icon.SameIcon(current) {
					return ErrIconGone
				}
				icon = current
			}
			if iconHasVisibleWindow(icon, matching) {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return Activate(icon, ActionDoubleClick)
		}
	}
}

func iconHasVisibleWindow(icon Icon, matching map[uint32]bool) bool {
	for _, hwnd := range allWindows() {
		if !win.IsWindowVisible(hwnd) || win.IsIconic(hwnd) || win.GetWindowLong(hwnd, win.GWL_EXSTYLE)&win.WS_EX_TOOLWINDOW != 0 {
			continue
		}
		var rect win.RECT
		if !win.GetWindowRect(hwnd, &rect) || rect.Right-rect.Left < 32 || rect.Bottom-rect.Top < 32 {
			continue
		}
		var pid uint32
		win.GetWindowThreadProcessId(hwnd, &pid)
		match, known := matching[pid]
		if !known {
			match = Contains([]string{icon.ExePath}, processPath(pid))
			matching[pid] = match
		}
		if match {
			return true
		}
	}
	return false
}
