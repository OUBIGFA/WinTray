package kit

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"wintray/internal/ui/kit/icons"
)

// SelectOption is one choice of Select or Segmented: the stored value and
// what the user reads.
type SelectOption struct {
	Value string
	Label string
	Icon  *ui.SVG
}

// Select is a drop-down choosing a value by its label: MyGo's select base
// with the shared field look. It returns whether the choice changed.
func Select(c *ui.Context, selected *string, options []SelectOption) (changed bool, trigger ui.Element) {
	t, p := c.Theme(), Colors(c)
	before := *selected
	sel := ui.SelectBase(c, selected)
	current := SelectOption{Value: *selected, Label: *selected}
	for _, option := range options {
		if option.Value == *selected {
			current = option
		}
	}
	b := sel.Trigger.MinHeight(ControlHeight).Padding(5, 10).Gap(8).Radius(t.Radius).Background(t.Surface).
		Border(1, t.Border).Justify(ui.SpaceBetween).AlignItems(ui.Center).MinWidth(160)
	if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
			if current.Icon != nil {
				ui.Icon(c, current.Icon).FontSize(16).TextColor(t.TextMuted).Shrink(0)
			}
			ui.Text(c, current.Label).SingleLine().MinWidth(0)
		})
		ui.Icon(c, icons.ChevronDown).FontSize(14).TextColor(t.TextMuted).Shrink(0)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(PanelRadius).Background(p.Panel).Border(1, t.Border).
			Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 2*shadowAlpha(t))).MinWidth(180)
		for _, option := range options {
			item := sel.Item(option.Value).Padding(6, 10).Gap(8).Radius(t.Radius).AlignItems(ui.Center)
			chosen := option.Value == *selected
			if item.Highlighted() {
				item.Background(t.SurfaceHover)
			}
			item.Children(func() {
				if option.Icon != nil {
					ui.Icon(c, option.Icon).FontSize(16).TextColor(t.TextMuted).Shrink(0)
				}
				label := ui.Text(c, option.Label).SingleLine().Grow(1).MinWidth(0)
				if chosen {
					label.FontWeight(600)
					ui.Icon(c, icons.Check).FontSize(14).Shrink(0)
				}
			})
		}
	})
	return *selected != before, b
}

// Segmented chooses one of a few short options shown at once, as a theme
// or a language. It returns whether the choice changed.
func Segmented(c *ui.Context, selected *string, options []SelectOption, label string) bool {
	t, p := c.Theme(), Colors(c)
	index := 0
	for i, option := range options {
		if option.Value == *selected {
			index = i
		}
	}
	before := index
	seg := ui.SegmentedBase(c, &index, len(options))
	seg.Track.Padding(3).Gap(2).Radius(t.Radius+2).Background(t.Surface).Border(1, p.Hairline).Label(label).Wrap().
		Children(func() {
			for i, option := range options {
				item := seg.Segment(i).Padding(5, 12).Gap(6).Radius(t.Radius).AlignItems(ui.Center).Label(option.Label)
				if i == index {
					item.Background(p.Panel).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, shadowAlpha(t)+0.04))
				} else if item.Hovered() {
					item.Background(t.SurfaceHover)
				}
				item.Children(func() {
					if option.Icon != nil {
						ui.Icon(c, option.Icon).FontSize(15).Shrink(0)
					}
					text := ui.Text(c, option.Label).SingleLine()
					if i == index {
						text.FontWeight(600)
					} else {
						text.TextColor(t.TextMuted)
					}
				})
			}
		})
	if index != before && index < len(options) {
		*selected = options[index].Value
		return true
	}
	return false
}

// PathField shows a chosen path, shortened in the middle when it does not
// fit, with the action that chooses another; the full path is its tooltip.
func PathField(c *ui.Context, path, empty, action string, disabled bool) (choose ui.Element) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		shown := path
		if shown == "" {
			shown = empty
		}
		text := ui.Text(c, shown).Font(MonoFont).FontSize(HelpSize(c)).SingleLine().Ellipsis("…").MinWidth(0).
			Shrink(1).Basis(200).Grow(1).Selectable()
		if path == "" {
			text.TextColor(t.TextMuted)
		} else {
			text.Tooltip(path)
		}
		choose = Button(c, action, icons.FolderOpen, false).Disabled(disabled)
	})
	return choose
}

// Itoa formats a count for text.
func Itoa(n int) string { return strconv.Itoa(n) }
