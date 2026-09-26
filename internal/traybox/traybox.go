// Package traybox moves other programs' notification-area icons off the
// taskbar and reaches them again from WinTray's own tray menu.
//
// Windows 11 no longer exposes the notification area as a toolbar, and
// intercepting Shell_NotifyIcon would require injecting code into Explorer.
// The box relies on documented behaviour instead:
//   - HKCU\Control Panel\NotifyIconSettings lists every icon Explorer has
//     seen, with its program path, its uID and a PNG snapshot. Its IsPromoted
//     value is the "show in taskbar" switch of the Settings app; clearing it
//     moves the icon into the system's hidden-icons flyout.
//   - Shell_NotifyIconGetRect finds the window that owns an icon and where
//     the icon is on screen, so a click can be delivered to it.
//
// Icons stay in the system flyout while WinTray is not running, so nothing is
// lost when it exits.
package traybox

import (
	"path/filepath"
	"strings"
)

// Icon is one entry of the notification-area settings.
type Icon struct {
	ExePath  string
	UID      uint32
	Tooltip  string
	Snapshot []byte
	// Promoted reports whether the icon is shown on the taskbar rather than
	// in the hidden-icons flyout.
	Promoted bool
}

// Action is what a click from the box does to an icon.
type Action int

const (
	ActionClick Action = iota
	ActionDoubleClick
	ActionRightClick
)

// DisplayName prefers the icon's first tooltip line, falling back to the
// program's file name.
func (i Icon) DisplayName() string {
	if line, _, _ := strings.Cut(i.Tooltip, "\n"); strings.TrimSpace(line) != "" {
		return strings.TrimSpace(line)
	}
	base := filepath.Base(i.ExePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Contains reports whether paths lists exePath. Windows paths compare
// case-insensitively.
func Contains(paths []string, exePath string) bool {
	for _, p := range paths {
		if strings.EqualFold(p, exePath) {
			return true
		}
	}
	return false
}

// Toggle adds exePath to paths or removes it, returning the new list and
// whether the path is now included.
func Toggle(paths []string, exePath string) ([]string, bool) {
	out := make([]string, 0, len(paths)+1)
	for _, p := range paths {
		if !strings.EqualFold(p, exePath) {
			out = append(out, p)
		}
	}
	if len(out) < len(paths) {
		return out, false
	}
	return append(out, exePath), true
}
