//go:build windows

package ui

import "github.com/lxn/walk"

var (
	textColor      = walk.RGB(35, 48, 65)
	secondaryColor = walk.RGB(83, 98, 116)
	disabledColor  = walk.RGB(128, 138, 150)
	dividerColor   = walk.RGB(205, 212, 221)
	noticeColor    = walk.RGB(252, 243, 219)
)

// uiFontFamily draws Chinese and Latin text in one face, so mixed text such
// as "开机时自动运行 WinTray" keeps one size instead of falling back per glyph.
const uiFontFamily = "Microsoft YaHei UI"

// Type scale shared by every page: page title, the selected program's name,
// section title, and body text (the window font).
const (
	pageTitleSize    = 16
	programTitleSize = 14
	sectionTitleSize = 12
	bodyTextSize     = 10
)

// Spacing shared by every page: between the parts of one setting (its name,
// control and explanation), and between settings.
const (
	fieldSpacing = 6
	groupSpacing = 20
)

// Keep native controls, keyboard navigation and DPI scaling, with a shared
// spacing and type scale instead of fixed widths for translated button text.
// Rows and columns pack their children to the left and top: walk centers
// anything that does not fill its slot, which left check boxes floating.
func newRow(parent walk.Container, spacing int) (*walk.Composite, error) {
	row, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	layout := walk.NewHBoxLayout()
	layout.SetMargins(walk.Margins{})
	layout.SetSpacing(spacing)
	if err := layout.SetAlignment(walk.AlignHNearVCenter); err != nil {
		return nil, err
	}
	if err := row.SetLayout(layout); err != nil {
		return nil, err
	}
	return row, nil
}

func newColumn(parent walk.Container, spacing int) (*walk.Composite, error) {
	return newIndentedColumn(parent, 0, spacing)
}

// newIndentedColumn stacks its children with a left indent, for options that
// only apply together with the setting above them.
func newIndentedColumn(parent walk.Container, indent, spacing int) (*walk.Composite, error) {
	column, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{HNear: indent})
	layout.SetSpacing(spacing)
	if err := layout.SetAlignment(walk.AlignHNearVNear); err != nil {
		return nil, err
	}
	if err := column.SetLayout(layout); err != nil {
		return nil, err
	}
	return column, nil
}

// Walk's Separator in the pinned version reports reversed layout flags. A
// fixed-size composite gives us a true divider at every DPI.
func newDivider(parent walk.Container) (*walk.Composite, error) {
	divider, err := newRow(parent, 0)
	if err != nil {
		return nil, err
	}
	if _, err = walk.NewHSpacer(divider); err != nil {
		return nil, err
	}
	if err = divider.SetMinMaxSize(walk.Size{Height: 1}, walk.Size{Height: 1}); err != nil {
		return nil, err
	}
	return divider, fillBackground(divider, dividerColor)
}

func newVerticalDivider(parent walk.Container) (*walk.Composite, error) {
	divider, err := newColumn(parent, 0)
	if err != nil {
		return nil, err
	}
	if _, err = walk.NewVSpacer(divider); err != nil {
		return nil, err
	}
	if err = divider.SetMinMaxSize(walk.Size{Width: 1}, walk.Size{Width: 1}); err != nil {
		return nil, err
	}
	return divider, fillBackground(divider, dividerColor)
}

func fillBackground(widget walk.Widget, color walk.Color) error {
	brush, err := walk.NewSolidColorBrush(color)
	if err != nil {
		return err
	}
	widget.SetBackground(brush)
	return nil
}

func newTitleLabel(parent walk.Container, size int) (*walk.Label, error) {
	label, err := walk.NewLabel(parent)
	if err != nil {
		return nil, err
	}
	label.SetTextColor(textColor)
	if font, err := walk.NewFont(uiFontFamily, size, 0); err == nil {
		label.SetFont(font)
	}
	return label, nil
}

func newSectionTitle(parent walk.Container) (*walk.Label, error) {
	return newTitleLabel(parent, sectionTitleSize)
}

// newFieldLabel names a setting; it sits above or beside its control rather
// than in a fixed-width column, so translated labels never need measuring.
func newFieldLabel(parent walk.Container) (*walk.Label, error) {
	label, err := walk.NewLabel(parent)
	if err != nil {
		return nil, err
	}
	label.SetTextColor(textColor)
	return label, nil
}

// newHint is a single line of secondary text, cut off with an ellipsis when
// the window is too narrow.
func newHint(parent walk.Container) (*walk.Label, error) {
	label, err := walk.NewLabel(parent)
	if err != nil {
		return nil, err
	}
	label.SetTextColor(secondaryColor)
	return label, label.SetEllipsisMode(walk.EllipsisEnd)
}

// newWrappedHint is secondary text that wraps onto further lines. Without a
// minimum width walk sizes it for its whole text on one line, which would
// widen the window instead of wrapping.
func newWrappedHint(parent walk.Container) (*walk.TextLabel, error) {
	label, err := walk.NewTextLabel(parent)
	if err != nil {
		return nil, err
	}
	label.SetTextColor(secondaryColor)
	return label, label.SetMinMaxSize(walk.Size{Width: 160}, walk.Size{})
}

func newActionButton(parent walk.Container, onClick func()) (*walk.PushButton, error) {
	button, err := walk.NewPushButton(parent)
	if err != nil {
		return nil, err
	}
	button.SetMinMaxSize(walk.Size{Width: 88}, walk.Size{})
	button.Clicked().Attach(onClick)
	return button, nil
}

// newNumberEdit is a short field for a number of seconds followed by its unit.
func newNumberEdit(parent walk.Container) (*walk.LineEdit, *walk.Label, error) {
	edit, err := walk.NewLineEdit(parent)
	if err != nil {
		return nil, nil, err
	}
	edit.SetMinMaxSize(walk.Size{Width: 64, Height: 28}, walk.Size{Width: 64, Height: 28})
	unit, err := walk.NewLabel(parent)
	if err != nil {
		return nil, nil, err
	}
	unit.SetTextColor(textColor)
	return edit, unit, nil
}

// settingControlsWidth is room for the widest control of a setting row.
const settingControlsWidth = 200

// settingRow is one line of the settings page: what the setting is, with an
// explanation below it, on the left, and the control that changes it on the
// right, so every explanation sits with the setting it belongs to.
type settingRow struct {
	row      *walk.Composite
	text     *walk.Composite
	title    *walk.Label
	hint     *walk.TextLabel
	controls *walk.Composite
}

func newSettingRow(parent walk.Container, indent int) (*settingRow, error) {
	row, err := newRow(parent, 12)
	if err != nil {
		return nil, err
	}
	if err = row.Layout().SetMargins(walk.Margins{HNear: indent, VNear: 12, VFar: 12}); err != nil {
		return nil, err
	}
	s := &settingRow{row: row}
	if s.text, err = newColumn(row, 2); err != nil {
		return nil, err
	}
	// The trailing spacer lets the text take all the width the controls leave,
	// so the explanation wraps there instead of at its minimum width.
	titleRow, err := newRow(s.text, 0)
	if err != nil {
		return nil, err
	}
	if s.title, err = newFieldLabel(titleRow); err != nil {
		return nil, err
	}
	if _, err = walk.NewHSpacer(titleRow); err != nil {
		return nil, err
	}
	if s.hint, err = newWrappedHint(s.text); err != nil {
		return nil, err
	}
	s.hint.SetMinMaxSize(walk.Size{Width: 500}, walk.Size{})
	// Walk may leave some width unused after allocating the text and the
	// fixed control column. Absorb it here rather than let the layout add
	// gaps after both children, which moves controls away from the right edge.
	if _, err = walk.NewHSpacer(row); err != nil {
		return nil, err
	}
	// Reserve the same control width in every row.
	if s.controls, err = newRow(row, 8); err != nil {
		return nil, err
	}
	if _, err = walk.NewHSpacer(s.controls); err != nil {
		return nil, err
	}
	s.controls.SetMinMaxSize(walk.Size{Width: settingControlsWidth}, walk.Size{Width: settingControlsWidth})
	return s, nil
}

// setEnabled greys out the whole row, its text included, for a setting that
// has no effect in the current state.
func (s *settingRow) setEnabled(enabled bool) {
	s.row.SetEnabled(enabled)
	if enabled {
		s.title.SetTextColor(textColor)
		s.hint.SetTextColor(secondaryColor)
	} else {
		s.title.SetTextColor(disabledColor)
		s.hint.SetTextColor(disabledColor)
	}
}

// newToggle is an on/off check box for a setting row, labelled with its state
// on the left like a Windows switch; the row title names it for screen readers.
func newToggle(parent walk.Container, onChange func(bool)) (*walk.CheckBox, error) {
	toggle, err := walk.NewCheckBox(parent)
	if err != nil {
		return nil, err
	}
	if err = toggle.SetTextOnLeftSide(true); err != nil {
		return nil, err
	}
	toggle.CheckedChanged().Attach(func() { onChange(toggle.Checked()) })
	return toggle, nil
}
