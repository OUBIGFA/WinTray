//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"wintray/internal/ui/kit"
)

func nativePrimaryAction(c *native.Context, label string, icon *native.SVG) native.Element {
	return kit.PrimaryButton(c, label, icon, false).Label(label)
}

func nativeQuietAction(c *native.Context, label string, icon *native.SVG) native.Element {
	return kit.GhostButton(c, label, icon, false).Label(label)
}
