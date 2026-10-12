// Package traybox collects other programs' notification-area icons into
// WinTray's own tray menu.
//
// A thread-specific bridge on Explorer's tray receiver forwards selected
// owners' native Shell_NotifyIcon packets to a private broker. No Shell_TrayWnd
// is created or application injected; unrelated packets remain in Explorer.
// Selected requests retain native data except for the icons' hidden state.
// The box keeps each icon's current image, tooltip and callback for its menu.
//
// Icons are restored before a route is cancelled. The native bridge becomes
// dormant on cancellation or host exit; an independent recovery process also
// restores icons when WinTray is forcibly terminated.
package traybox

import (
	"image"
	"path/filepath"
	"strings"
)

// Icon is the current state of one collected notification-area icon.
type Icon struct {
	ExePath string
	Tooltip string
	// Image is the icon as the program last set it, or nil when the program
	// has not supplied one yet.
	Image *image.NRGBA

	// owner and uid address the program's callback message.
	key      iconID
	owner    uint32
	uid      uint32
	callback uint32
	version  uint32
}

// iconID identifies an icon as Shell_NotifyIcon does: by its GUID when the
// program registered one, otherwise by owner window and uID.
type iconID struct {
	hwnd uint32
	uid  uint32
	guid [16]byte
}

func idOf(d trayData) iconID {
	if d.usesGUID() {
		return iconID{guid: d.GUID}
	}
	return iconID{hwnd: d.HWnd, uid: d.UID}
}

// Action is what a click from the menu does to an icon.
type Action int

const (
	// ActionClick is a left click, which usually opens the program.
	ActionClick Action = iota
	// ActionRightClick opens the program's own tray menu.
	ActionRightClick
	// ActionDoubleClick delivers the native double-click gesture. It is
	// also used by OpenIcon when a single click has not shown a window.
	ActionDoubleClick
)

// SameIcon compares native registration identity across live snapshots. An
// open menu must not target the next icon when one before it is removed.
func (i Icon) SameIcon(other Icon) bool {
	return i.key == other.key && strings.EqualFold(i.ExePath, other.ExePath)
}

// DisplayName prefers the icon's first tooltip line, falling back to the
// program's file name.
func (i Icon) DisplayName() string {
	if line, _, _ := strings.Cut(i.Tooltip, "\n"); strings.TrimSpace(line) != "" {
		return strings.TrimSpace(line)
	}
	base := filepath.Base(i.ExePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// Clickable reports whether the program asked to be told about clicks.
func (i Icon) Clickable() bool { return i.callback != 0 }

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
