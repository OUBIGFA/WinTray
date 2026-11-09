// Package kit is the shared design system for MyGo's native UI: the theme,
// text roles, layout of settings pages and the few compositions every page
// shares. Pages use these instead of their own colors and sizes, so one
// change here restyles the whole window.
//
// The surface uses neutral grays, Windows' system font, MyGo's own controls,
// a fixed type ramp, tight rows inside groups and generous space between
// groups. Semantic colors appear only where they carry meaning.
package kit

import "github.com/egoist/mygo/ui"

// Palette holds the colors of the theme that ui.Theme has no slot for.
type Palette struct {
	// Panel is the face of grouped settings, one step from the canvas.
	Panel ui.Color
	// Sidebar is the navigation layer beside the content.
	Sidebar ui.Color
	// Selected fills the chosen navigation item and list row.
	Selected ui.Color
	// Hairline separates rows within a group and regions of the shell.
	Hairline ui.Color
	// DangerSoft and DangerLine tint an error notice; InfoSoft tints a
	// neutral one.
	DangerSoft, DangerLine, InfoSoft ui.Color
	// SuccessSoft and WarningSoft tint status badges.
	SuccessSoft, WarningSoft ui.Color
	// Skeleton fills placeholders while content loads.
	Skeleton ui.Color
}

var (
	lightTheme = ui.Theme{
		Background:     ui.Hex("#fcfcfc"),
		Surface:        ui.Hex("#f3f3f3"),
		SurfaceHover:   ui.Hex("#ebebeb"),
		SurfacePressed: ui.Hex("#e2e2e2"),
		Border:         ui.Hex("#d6d6d6"),
		Text:           ui.Hex("#262626"),
		TextMuted:      ui.Hex("#5e5e5e"),
		Accent:         ui.Hex("#2e2e2e"),
		AccentHover:    ui.Hex("#454545"),
		AccentPressed:  ui.Hex("#1c1c1c"),
		AccentText:     ui.Hex("#ffffff"),
		Danger:         ui.Hex("#c42b1c"),
		Warning:        ui.Hex("#9d5d00"),
		Success:        ui.Hex("#0f7b0f"),
		Selection:      ui.RGBA(46, 46, 46, 0.18),
		Focus:          ui.RGBA(46, 46, 46, 0.6),
		Inverse:        ui.Hex("#2b2b2b"),
		InverseText:    ui.Hex("#fafafa"),
		Scrollbar:      ui.RGBA(0, 0, 0, 0.3),
		Radius:         6,
		Spacing:        4,
		FontSize:       14,
		Font:           systemFont,
	}
	lightPalette = Palette{
		Panel:       ui.Hex("#ffffff"),
		Sidebar:     ui.Hex("#f3f3f3"),
		Selected:    ui.Hex("#e6e6e6"),
		Hairline:    ui.RGBA(0, 0, 0, 0.08),
		DangerSoft:  ui.RGBA(196, 43, 28, 0.07),
		DangerLine:  ui.RGBA(196, 43, 28, 0.32),
		InfoSoft:    ui.Hex("#f3f3f3"),
		SuccessSoft: ui.RGBA(15, 123, 15, 0.1),
		WarningSoft: ui.RGBA(157, 93, 0, 0.1),
		Skeleton:    ui.Hex("#ececec"),
	}
	darkTheme = ui.Theme{
		Dark:           true,
		Background:     ui.Hex("#191919"),
		Surface:        ui.Hex("#2b2b2b"),
		SurfaceHover:   ui.Hex("#333333"),
		SurfacePressed: ui.Hex("#3b3b3b"),
		Border:         ui.Hex("#3d3d3d"),
		Text:           ui.Hex("#e9e9e9"),
		TextMuted:      ui.Hex("#adadad"),
		Accent:         ui.Hex("#e6e6e6"),
		AccentHover:    ui.Hex("#ffffff"),
		AccentPressed:  ui.Hex("#cccccc"),
		AccentText:     ui.Hex("#1a1a1a"),
		Danger:         ui.Hex("#ff99a4"),
		Warning:        ui.Hex("#fce100"),
		Success:        ui.Hex("#6ccb5f"),
		Selection:      ui.RGBA(233, 233, 233, 0.22),
		Focus:          ui.RGBA(233, 233, 233, 0.65),
		Inverse:        ui.Hex("#e9e9e9"),
		InverseText:    ui.Hex("#1a1a1a"),
		Scrollbar:      ui.RGBA(255, 255, 255, 0.3),
		Radius:         6,
		Spacing:        4,
		FontSize:       14,
		Font:           systemFont,
	}
	darkPalette = Palette{
		Panel:       ui.Hex("#222222"),
		Sidebar:     ui.Hex("#202020"),
		Selected:    ui.Hex("#323232"),
		Hairline:    ui.RGBA(255, 255, 255, 0.08),
		DangerSoft:  ui.RGBA(255, 153, 164, 0.08),
		DangerLine:  ui.RGBA(255, 153, 164, 0.35),
		InfoSoft:    ui.Hex("#262626"),
		SuccessSoft: ui.RGBA(108, 203, 95, 0.12),
		WarningSoft: ui.RGBA(252, 225, 0, 0.1),
		Skeleton:    ui.Hex("#2a2a2a"),
	}
)

// systemFont is Windows' interface face, with the Chinese one DirectWrite
// would pick anyway named so both languages keep one weight scale.
const systemFont = "Segoe UI Variable Text, Segoe UI, Microsoft YaHei UI"

// MonoFont is the face of code, paths and identifiers that are compared
// character by character.
const MonoFont = "Cascadia Mono, Consolas, Microsoft YaHei UI"

// Theme returns the light or dark theme for the desktop's preferences:
// Windows' text size scales every role of the type ramp, and high
// contrast darkens borders and secondary text as MyGo's own theme does.
// The result is shared; do not change it.
func Theme(p ui.Preferences, dark bool) *ui.Theme {
	key := themeKey{dark: dark, scale: p.TextScale, contrast: p.HighContrast}
	if t, ok := themes[key]; ok {
		return t
	}
	t := lightTheme
	if dark {
		t = darkTheme
	}
	if p.TextScale > 0 {
		t.FontSize = 14 * p.TextScale
	}
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus = t.Accent
	}
	themes[key] = &t
	return &t
}

type themeKey struct {
	dark, contrast bool
	scale          float32
}

// themes is touched on the UI thread only.
var themes = map[themeKey]*ui.Theme{}

// Colors returns the palette matching the context's theme.
func Colors(c *ui.Context) Palette {
	if c.Theme().Dark {
		return darkPalette
	}
	return lightPalette
}
