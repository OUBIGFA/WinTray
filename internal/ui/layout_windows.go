//go:build windows

package ui

import "github.com/lxn/walk"

var (
	textColor      = walk.RGB(35, 48, 65)
	secondaryColor = walk.RGB(83, 98, 116)
)

// Keep native controls, keyboard navigation and DPI scaling, with a shared
// spacing and type scale instead of fixed widths for translated button text.
func newRow(parent walk.Container, spacing int) (*walk.Composite, error) {
	row, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	layout := walk.NewHBoxLayout()
	layout.SetMargins(walk.Margins{})
	layout.SetSpacing(spacing)
	if err := row.SetLayout(layout); err != nil {
		return nil, err
	}
	return row, nil
}

func newColumn(parent walk.Container, spacing int) (*walk.Composite, error) {
	column, err := walk.NewComposite(parent)
	if err != nil {
		return nil, err
	}
	layout := walk.NewVBoxLayout()
	layout.SetMargins(walk.Margins{})
	layout.SetSpacing(spacing)
	if err := column.SetLayout(layout); err != nil {
		return nil, err
	}
	return column, nil
}

// Walk's Separator in the pinned version reports reversed layout flags. A
// fixed-height composite gives us a true horizontal divider at every DPI.
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
	brush, err := walk.NewSolidColorBrush(walk.RGB(205, 212, 221))
	if err != nil {
		return nil, err
	}
	divider.SetBackground(brush)
	return divider, nil
}

func newSectionTitle(parent walk.Container) (*walk.Label, error) {
	label, err := walk.NewLabel(parent)
	if err != nil {
		return nil, err
	}
	label.SetTextColor(textColor)
	if font, err := walk.NewFont("Segoe UI", 12, 0); err == nil {
		label.SetFont(font)
	}
	return label, nil
}

func newActionButton(parent walk.Container, onClick func()) (*walk.PushButton, error) {
	button, err := walk.NewPushButton(parent)
	if err != nil {
		return nil, err
	}
	button.SetMinMaxSize(walk.Size{Width: 88, Height: 30}, walk.Size{Height: 30})
	button.Clicked().Attach(onClick)
	return button, nil
}

func (w *MainWindow) buildHeader() error {
	row, err := newRow(w.mw, 12)
	if err != nil {
		return err
	}
	titles, err := newColumn(row, 2)
	if err != nil {
		return err
	}
	title, err := walk.NewLabel(titles)
	if err != nil {
		return err
	}
	title.SetText("WinTray")
	title.SetTextColor(textColor)
	if font, err := walk.NewFont("Segoe UI", 20, 0); err == nil {
		title.SetFont(font)
	}
	w.subtitle, err = walk.NewLabel(titles)
	if err != nil {
		return err
	}
	w.subtitle.SetTextColor(secondaryColor)
	if _, err = walk.NewHSpacer(row); err != nil {
		return err
	}
	w.languageLabel, err = walk.NewLabel(row)
	if err != nil {
		return err
	}
	w.languageLabel.SetTextColor(secondaryColor)
	w.languageCombo, err = walk.NewComboBox(row)
	if err != nil {
		return err
	}
	w.languageCombo.SetMinMaxSize(walk.Size{Width: 128}, walk.Size{Width: 128})
	w.languageCombo.CurrentIndexChanged().Attach(func() {
		if w.applyingLocale {
			return
		}
		language := "zh-CN"
		if w.languageCombo.CurrentIndex() == 1 {
			language = "en-US"
		}
		w.applyLanguage(language)
		w.refreshManagedList()
		w.save()
	})
	return nil
}
