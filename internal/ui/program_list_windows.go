//go:build windows

package ui

import (
	"fmt"
	"strconv"
	"strings"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/i18n"
)

// Keep the established table and editor proportions. Only the name column
// gives up space to the wider start action and the new trailing delete column.
func (w *MainWindow) nativeProgramTable(c *native.Context, m i18n.Messages) {
	columns := []native.TableColumn{
		{ID: "name", Title: m.ManagedColumnName, MinWidth: 96, Fixed: true},
		{ID: "tray", Title: m.TrayBoxHomeTitle, Width: 96, Fixed: true, Align: native.Center},
		{ID: "mode", Title: m.ManagedColumnRule, Width: 180, Fixed: true, Align: native.Center},
		{ID: "order", Title: m.ManagedColumnOrder, Width: 88, Fixed: true, Align: native.Center},
		{ID: "delete", Title: m.ManagedColumnDelete, Width: 64, Fixed: true, Align: native.Center},
	}
	moveFrom, moveTo := -1, -1
	removeID := ""
	theme := c.Theme()
	listTheme := *theme
	listTheme.Accent, listTheme.AccentText = theme.SurfacePressed, theme.Text
	c.SetTheme(&listTheme)
	table := native.Table(c, &w.desktop.list, columns, len(w.settings.ManagedApps), func(row, column int) {
		c.SetTheme(theme)
		defer c.SetTheme(&listTheme)
		entry := &w.settings.ManagedApps[row]
		switch column {
		case 0:
			native.Row(c).Gap(6).Children(func() {
				if native.Checkbox(c, &entry.RunOnStartup, "").Label(m.ManagedEnabled + " " + entry.Name).Changed() {
					w.save()
				}
				if icon := w.desktop.icons[entry.ExePath]; icon != nil {
					native.Image(c, icon).Size(18, 18)
				} else {
					native.Icon(c, programIconSVG).Size(18, 18)
				}
				name := native.Text(c, entry.Name).SingleLine().Grow(1).Tooltip(entry.Name)
				if !entry.RunOnStartup {
					name.TextColor(theme.TextMuted)
				}
			})
		case 1:
			collected := entry.CollectTrayIcon
			native.Row(c).Center().Disabled(shouldDefaultLaunchHidden(entry.ExePath)).Children(func() {
				if native.Checkbox(c, &collected, "").Label(m.TrayBoxHomeTitle + " " + entry.Name).Changed() {
					w.nativeCollect(row, collected)
				}
			})
		case 2:
			action := i18n.FormatManagedParam(w.settings.Language, *entry)
			native.Text(c, action).SingleLine().Tooltip(action)
		case 3:
			order := strconv.Itoa(row + 1)
			if nativeOrderSelect(c, &order, w.desktop.orderOptions, m.ManagedColumnOrder+" "+entry.Name).Changed() {
				if position, err := strconv.Atoi(order); err == nil {
					moveFrom, moveTo = row, position-1
				}
			}
		case 4:
			nativeDeleteButton(c, m.ManagedColumnDelete+" "+entry.Name).Menu(func(menu *native.Menu) {
				menu.Item(fmt.Sprintf(m.RemoveConfirmPrompt, strings.ReplaceAll(entry.Name, "&", "&&"))).Disabled(true)
				menu.Separator()
				if menu.Item(m.RemoveConfirmLabel).Chosen() {
					removeID = entry.ID
				}
			})
		}
	}).Grow(1).MinHeight(0).Label(m.ManagedListTitle)
	c.SetTheme(theme)
	if table.Changed() {
		w.commitNativeEdits()
	}
	if table.Clicked() {
		w.commitNativeEdits()
		w.desktop.selected = -1
	}
	// Keyed rows must finish before their identities move or disappear.
	if moveFrom >= 0 {
		w.nativeSetOrder(moveFrom, moveTo)
	}
	if removeID != "" {
		w.nativeRemove(removeID)
	}
}

// SelectBase retains the toolkit's selection, focus and keyboard behavior.
// Equal arrow-sized spaces on both sides center the value without replacing
// the select with a menu button or positioning a tiny arrow over its text.
func nativeOrderSelect(c *native.Context, value *string, options []string, label string) native.Element {
	theme := c.Theme()
	selectBox := native.SelectBase(c, value)
	trigger := selectBox.Trigger.FillWidth().Padding(6, 4).Gap(0).Radius(theme.Radius).Border(1, theme.Border).Label(label)
	surface := theme.Surface
	if trigger.Pressed() || selectBox.Open() {
		surface = theme.SurfacePressed
	} else if trigger.Hovered() {
		surface = theme.SurfaceHover
	}
	trigger.Background(surface).Children(func() {
		native.Box(c).Size(12, 12).Shrink(0)
		native.Text(c, *value).SingleLine().Grow(1).TextAlign(native.Center)
		native.Icon(c, chevronIcon).Size(12, 12).Shrink(0).TextColor(theme.TextMuted)
	})
	visibleOption := native.Local(trigger, "visible-order", func() string { return "" })
	if !selectBox.Open() {
		*visibleOption = ""
	}
	selectBox.Popup(func(panel native.Element) {
		panel.Margin(4, 0, 0, 0).Padding(4).Radius(theme.Radius+2).Background(theme.Background).Border(1, theme.Border)
		native.Scroll(c).MaxHeight(288).Children(func() {
			for _, option := range options {
				item := selectBox.Item(option).Padding(6, 10).Radius(theme.Radius)
				if item.Highlighted() {
					item.Background(theme.Accent).TextColor(theme.AccentText)
					if *visibleOption != option {
						item.ScrollIntoView()
						*visibleOption = option
					}
				} else if option == *value {
					item.Background(theme.Surface)
				}
				item.Children(func() { native.Text(c, option).FillWidth().SingleLine().TextAlign(native.Center) })
			}
		})
	})
	return trigger
}

// A row's secondary action stays flat until hovered. It remains a full-size
// keyboard-accessible button, without adding another boxed control per row.
func nativeDeleteButton(c *native.Context, label string) native.Element {
	theme := c.Theme()
	button := native.ButtonBase(c).Size(32, 32).Radius(theme.Radius).Label(label).Tooltip(label)
	if button.Pressed() {
		button.Background(theme.SurfacePressed)
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() { native.Icon(c, deleteIcon).Size(16, 16).TextColor(theme.TextMuted) })
	return button
}
