package kit

import (
	"time"

	"github.com/egoist/mygo/ui"

	"wintray/internal/ui/kit/icons"
)

// Button is the ordinary action: MyGo's button with an optional icon
// before its label. busy shows a turning loader in place of the icon and
// disables the button, as the React window's Button did.
func Button(c *ui.Context, label string, icon *ui.SVG, busy bool) ui.Element {
	return decorate(c, ui.Button(c, ""), label, icon, busy)
}

// PrimaryButton is the one main action of a view.
func PrimaryButton(c *ui.Context, label string, icon *ui.SVG, busy bool) ui.Element {
	return decorate(c, ui.PrimaryButton(c, ""), label, icon, busy)
}

// DangerButton confirms an action that removes or ends something.
func DangerButton(c *ui.Context, label string, icon *ui.SVG, busy bool) ui.Element {
	t := c.Theme()
	b := decorate(c, ui.ButtonBase(c), label, icon, busy)
	fill := t.Danger
	if b.Pressed() {
		fill = t.Danger.Mix(t.Text, 0.15)
	} else if b.Hovered() {
		fill = t.Danger.Mix(t.Background, 0.12)
	}
	text := ui.Hex("#ffffff")
	if t.Dark {
		text = ui.Hex("#1a1a1a")
	}
	return b.Padding(t.Space(1.5), t.Space(3.5)).Gap(t.Space(1.5)).Radius(t.Radius).Background(fill).TextColor(text)
}

// GhostButton has no face until hovered, for secondary actions inside
// rows and toolbars.
func GhostButton(c *ui.Context, label string, icon *ui.SVG, busy bool) ui.Element {
	t := c.Theme()
	b := decorate(c, ui.ButtonBase(c), label, icon, busy).Padding(t.Space(1.5), t.Space(2.5)).
		Gap(t.Space(1.5)).Radius(t.Radius)
	if b.Pressed() {
		b.Background(t.SurfacePressed)
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	return b
}

// IconButton is a square button showing only icon. label names it for
// assistive technology and shows as its tooltip.
func IconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(ControlHeight, ControlHeight).Radius(t.Radius).Justify(ui.Center).
		AlignItems(ui.Center).Label(label).Tooltip(label)
	if b.Pressed() {
		b.Background(t.SurfacePressed)
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() { ui.Icon(c, icon).FontSize(16) })
	return b
}

// OutlineIconButton is IconButton with a visible face, for actions that
// float over content, as the save rail's.
func OutlineIconButton(c *ui.Context, icon *ui.SVG, label string, primary bool) ui.Element {
	t := c.Theme()
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c, "")
	} else {
		b = ui.Button(c, "")
	}
	return b.Size(ControlHeight+4, ControlHeight+4).Padding(0).Justify(ui.Center).AlignItems(ui.Center).
		Label(label).Tooltip(label).Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, shadowAlpha(t))).
		Children(func() { ui.Icon(c, icon).FontSize(16) })
}

func shadowAlpha(t *ui.Theme) float32 {
	if t.Dark {
		return 0.35
	}
	return 0.08
}

func decorate(c *ui.Context, b ui.Element, label string, icon *ui.SVG, busy bool) ui.Element {
	if busy {
		b.Disabled(true)
	}
	return b.Children(func() {
		switch {
		case busy:
			Loader(c)
		case icon != nil:
			ui.Icon(c, icon).FontSize(16)
		}
		if label != "" {
			ui.Text(c, label).SingleLine()
		}
	})
}

// Loader is the small turning circle of work in progress.
func Loader(c *ui.Context) ui.Element {
	spin := ui.Icon(c, icons.LoaderCircle).FontSize(16)
	return spin.Rotate(spin.Loop("spin", time.Second, ui.Linear) * 360)
}
