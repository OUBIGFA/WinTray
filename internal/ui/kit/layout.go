package kit

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Spacing of settings pages, in DIPs. Rows inside a group sit tight; groups
// stand apart, so a page reads as a few named tasks.
const (
	// PageWidth bounds a settings page's column for comfortable scanning.
	PageWidth = 880
	// WidePageWidth bounds work areas such as lists beside details.
	WidePageWidth = 1200
	// GroupGap separates groups on a page.
	GroupGap = 32
	// HeadingGap separates a group's heading from its rows.
	HeadingGap = 10
	// RowMinHeight is the height of a one-line setting row.
	RowMinHeight = 48
	// ControlHeight is the height of buttons, inputs and selects.
	ControlHeight = 32
	// PanelRadius rounds groups, popovers and notices.
	PanelRadius = 8
)

// Page lays out the column of a settings page; the shell supplies the
// scrolling and the margins.
func Page(c *ui.Context, fn func()) ui.Element {
	return ui.Column(c).FillWidth().MaxWidth(PageWidth).Margin(0, ui.Auto).Gap(GroupGap).Children(fn)
}

// WidePage is Page for work areas that use more width.
func WidePage(c *ui.Context, fn func()) ui.Element {
	return ui.Column(c).FillWidth().MaxWidth(WidePageWidth).Margin(0, ui.Auto).Gap(GroupGap).Children(fn)
}

// Section is a group heading over any content: an optional icon in the
// text's muted color, the title, and an optional sentence of purpose.
func Section(c *ui.Context, icon *ui.SVG, title, description string, fn func()) ui.Element {
	return ui.Column(c).FillWidth().Gap(HeadingGap).Children(func() {
		SectionHeading(c, icon, title, description)
		fn()
	})
}

// SectionHeading is a Section's heading alone, for groups laid out by hand.
func SectionHeading(c *ui.Context, icon *ui.SVG, title, description string) ui.Element {
	t := c.Theme()
	return ui.Column(c).Gap(2).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if icon != nil {
				ui.Icon(c, icon).FontSize(t.Rem(headingSize)).TextColor(t.TextMuted)
			}
			Heading(c, title).Role(ui.RoleHeading).Level(2)
		})
		if description != "" {
			Help(c, description)
		}
	})
}

// Group is a Section whose content is setting rows on a panel, one
// hairline between each two.
func Group(c *ui.Context, icon *ui.SVG, title, description string, rows func()) ui.Element {
	return Section(c, icon, title, description, func() { Panel(c, rows) })
}

// Panel is the face grouped rows sit on.
func Panel(c *ui.Context, rows func()) ui.Element {
	p := Colors(c)
	return ui.Column(c).FillWidth().Background(p.Panel).Border(1, p.Hairline).Radius(PanelRadius).
		Gap(1).Dividers(1, p.Hairline).Children(rows)
}

// Row is a setting: its label and optional help on the left, its control
// on the right. In a narrow window the control moves below the text.
func Row(c *ui.Context, label, help string, control func()) ui.Element {
	return ui.Row(c).FillWidth().MinHeight(RowMinHeight).Padding(10, 16).GapX(16).GapY(8).
		AlignItems(ui.Center).Wrap().Children(func() {
		ui.Column(c).Basis(220).Grow(1).Shrink(1).Gap(2).Children(func() {
			Label(c, label)
			if help != "" {
				Help(c, help)
			}
		})
		if control != nil {
			ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center).Children(control)
		}
	})
}

// Stack is a setting whose control needs the row's width: a path, a long
// text, a list. The label and help sit above it.
func Stack(c *ui.Context, label, help string, control func()) ui.Element {
	return ui.Column(c).FillWidth().Padding(12, 16).Gap(8).Children(func() {
		if label != "" || help != "" {
			ui.Column(c).Gap(2).Children(func() {
				if label != "" {
					Label(c, label)
				}
				if help != "" {
					Help(c, help)
				}
			})
		}
		control()
	})
}

// Indented holds the settings that depend on the row above them, as the
// options of a switch that is on.
func Indented(c *ui.Context, fn func()) ui.Element {
	return ui.Column(c).FillWidth().PaddingStart(28).Children(fn)
}

// Toggle is a Row that is a switch as a whole: clicking its label toggles
// it as clicking the switch does, as an HTML label did for its control.
// It returns the switch, whose Changed reports a toggle and which Disabled
// dims with its label.
func Toggle(c *ui.Context, value *bool, label, help string) ui.Element {
	t := c.Theme()
	sw := ui.SwitchBase(c, value).FillWidth().Shrink(1).MinHeight(RowMinHeight).Padding(10, 16).GapX(16).GapY(8).
		AlignItems(ui.Center).Wrap().Label(label).Cursor(ui.CursorPointer)
	if help != "" {
		sw.Description(help)
	}
	on := float32(0)
	if *value {
		on = 1
	}
	pos := sw.Animate("knob", on, 140*time.Millisecond)
	hovered := sw.Hovered()
	sw.Children(func() {
		ui.Column(c).Basis(220).Grow(1).Shrink(1).Gap(2).Children(func() {
			Label(c, label)
			if help != "" {
				Help(c, help)
			}
		})
		// The track and knob of MyGo's Switch, drawn inside the row.
		off := t.Border.Mix(t.Text, 0.15)
		if hovered {
			off = t.Border.Mix(t.Text, 0.25)
		}
		ui.Box(c).Size(t.Space(9), t.Space(5)).Radius(t.Space(2.5)).Shrink(0).Background(off.Mix(t.Accent, pos)).
			Draw(func(p *ui.Painter, r ui.Rect) {
				in := t.Space(0.5)
				d := r.H - 2*in
				knob := ui.Rect{X: r.X + in + pos*(r.W-r.H), Y: r.Y + in, W: d, H: d}
				p.Shadow(knob, d/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.25))
				p.Fill(knob, ui.RGB(255, 255, 255), d/2)
			})
	})
	return sw
}

// CheckMark draws a check box as MyGo's Checkbox does, for a
// ui.CheckboxBase whose row holds more than a label: a picture and badges
// beside the name, all of which toggle it. hovered is the row's.
func CheckMark(c *ui.Context, on, hovered bool) ui.Element {
	t := c.Theme()
	box := ui.Box(c).Size(t.Space(4), t.Space(4)).Radius(t.Space(1)).Shrink(0)
	switch {
	case on:
		box.Background(t.Accent)
	case hovered:
		box.Background(t.Background).Border(1, t.Accent)
	default:
		box.Background(t.Background).Border(1, t.Border.Mix(t.Text, 0.25))
	}
	if on {
		box.Draw(func(p *ui.Painter, r ui.Rect) {
			var path ui.Path
			path.MoveTo(r.X+r.W*0.22, r.Y+r.H*0.52).LineTo(r.X+r.W*0.42, r.Y+r.H*0.71).LineTo(r.X+r.W*0.78, r.Y+r.H*0.31)
			p.StrokePath(&path, t.Space(0.5), t.AccentText)
		})
	}
	return box
}

// PartialMark is CheckMark for a check box over several, some of them
// checked: a dash in the accent's box.
func PartialMark(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Box(c).Size(t.Space(4), t.Space(4)).Radius(t.Space(1)).Shrink(0).Background(t.Accent).
		Draw(func(p *ui.Painter, r ui.Rect) {
			p.Line(r.X+r.W*0.25, r.Y+r.H/2, r.X+r.W*0.75, r.Y+r.H/2, t.Space(0.5), t.AccentText)
		})
}

// Option is one choice of Choices.
type Option struct {
	Label, Help string
	Disabled    bool
}

// Choices is a radio list on a panel: one row per option, its help below
// its label, for choices whose consequences need a sentence. It returns
// whether the user chose another option.
func Choices(c *ui.Context, selected *int, options []Option) bool {
	before := *selected
	t, p := c.Theme(), Colors(c)
	ui.Column(c).FillWidth().Background(p.Panel).Border(1, p.Hairline).Radius(PanelRadius).Children(func() {
		ui.RadioGroup(c, func() {
			for i, option := range options {
				radio := ui.RadioBase(c.Key(i), selected, i).FillWidth().Padding(10, 16).Gap(12).
					AlignItems(ui.Start).Disabled(option.Disabled).Label(option.Label)
				if option.Help != "" {
					radio.Description(option.Help)
				}
				on := *selected == i
				radio.Children(func() {
					dot := ui.Box(c).Size(16, 16).Radius(8).Shrink(0).Margin(2, 0, 0, 0)
					if on {
						dot.Background(t.Accent)
					} else {
						dot.Background(t.Background).Border(1, t.Border.Mix(t.Text, 0.25))
					}
					dot.Children(func() {
						if on {
							ui.Box(c).Size(6, 6).Radius(3).Background(t.AccentText).Margin(ui.Auto)
						}
					})
					ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
						Label(c, option.Label)
						if option.Help != "" {
							Help(c, option.Help)
						}
					})
				})
			}
		}).FillWidth().Gap(1).Dividers(1, p.Hairline)
	})
	return *selected != before
}

// Fact is one label and value of Facts.
type Fact struct{ Label, Value string }

// Facts lists labeled values in two aligned columns, as a package's
// version, publisher and file; values can be selected and copied.
func Facts(c *ui.Context, facts []Fact) ui.Element {
	t := c.Theme()
	return ui.Grid(c).FillWidth().ColumnTracks(ui.FitContent(), ui.Fr(1)).GapX(16).GapY(6).Children(func() {
		for i, fact := range facts {
			ui.Text(c.Key(i), fact.Label).FontSize(HelpSize(c)).TextColor(t.TextMuted).NoWrap()
			ui.Text(c.Key(-1-i), fact.Value).FontSize(HelpSize(c)).Selectable().MinWidth(0)
		}
	})
}
