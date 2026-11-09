package kit

import "github.com/egoist/mygo/ui"

// SplitState keeps a Split's divider between frames: Percent is the first
// pane's share of the split's width, from 0 to 100. Keep it in the page's
// state; set Percent before the first frame for the initial share.
type SplitState struct {
	Percent float32
	// area binds the split's row, whose laid-out width turns the
	// divider's movement in DIPs into a share.
	area ui.Handle
}

// SplitOptions describe a Split.
type SplitOptions struct {
	// Label names the divider for assistive technology and its tooltip,
	// as "调整列表与设置宽度".
	Label string
	// MinFirst and MinSecond are the least shares, in percent, the
	// divider leaves each pane.
	MinFirst, MinSecond float32
}

// splitStep is how far an arrow key moves the divider, in DIPs.
const splitStep = 16

// Split lays out first and second side by side with a divider between
// them, which the user drags, or moves with the Left and Right keys once
// it has the keyboard focus. Unlike MyGo's ui.Split, the divider carries
// the page's own name and keeps each pane a share of the width, so both
// panes grow with the window. Give the split a height; each pane fills it
// and scrolls on its own.
func Split(c *ui.Context, s *SplitState, o SplitOptions, first, second func()) ui.Element {
	t, p := c.Theme(), Colors(c)
	clamp := func(v float32) float32 { return min(max(v, o.MinFirst), 100-o.MinSecond) }
	s.Percent = clamp(s.Percent)
	width := s.area.Bounds(c).W
	row := ui.Row(c).FillWidth().AlignItems(ui.Stretch).Bind(&s.area)
	return row.Children(func() {
		ui.Column(c).BasisPercent(s.Percent).Shrink(0).MinWidth(0).ClipX().Children(first)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).ClipX().Children(second)
		// The divider is built last so that it stays above both panes:
		// a hit area of a few DIPs centered on the panes' common edge.
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).LeftPercent(s.Percent).Width(9).Margin(0, 0, 0, -4).
			Cursor(ui.CursorResizeEW).Focusable().Role(ui.RoleSplitter).Label(o.Label).Tooltip(o.Label)
		line := p.Hairline
		if handle.Hovered() || handle.Pressed() {
			line = t.Border
		}
		focusVisible := handle.FocusVisible()
		handle.Draw(func(painter *ui.Painter, r ui.Rect) {
			painter.Fill(ui.Rect{X: r.X + 4, Y: r.Y, W: 1, H: r.H}, line, 0)
			if focusVisible {
				painter.FocusRing(r, [4]float32{})
			}
		})
		move := func(dx float32) {
			if width > 0 {
				s.Percent = clamp(s.Percent + dx*100/width)
			}
		}
		if dx, _, ok := handle.Dragged(); ok && dx != 0 {
			move(dx)
		}
		if handle.Shortcut(0, ui.KeyLeft) {
			move(-splitStep)
		}
		if handle.Shortcut(0, ui.KeyRight) {
			move(splitStep)
		}
	})
}
