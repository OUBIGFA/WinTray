//go:build windows

package ui

import native "github.com/egoist/mygo/ui"

// Reuse the existing mygo/system palette unchanged. Panels and selection
// differ only through neutral surfaces already used by the original UI.
func desktopPanel(t *native.Theme) native.Color { return t.Surface }

func desktopSelection(t *native.Theme) native.Color { return t.SurfacePressed }

func nativePrimaryAction(c *native.Context, label string, icon *native.SVG) native.Element {
	return native.PrimaryButton(c, "").Label(label).Padding(8, 14).Children(func() {
		if icon != nil {
			native.Icon(c, icon).Size(16, 16)
		}
		native.Text(c, label).SingleLine().FontWeight(500)
	})
}

func nativeQuietAction(c *native.Context, label string, icon *native.SVG) native.Element {
	// Toolbar applies the toolkit's flat button treatment and arrow-key group.
	var action native.Element
	native.Toolbar(c, func() { action = nativeAction(c, label, icon) }).Padding(0)
	return action
}

func nativeSectionTitle(c *native.Context, title string, icon *native.SVG) {
	native.Row(c).Gap(8).Children(func() {
		native.Icon(c, icon).Size(18, 18).TextColor(c.Theme().TextMuted)
		native.Text(c, title).FontSize(17).FontWeight(500)
	})
}
