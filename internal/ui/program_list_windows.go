//go:build windows

package ui

import (
	native "github.com/egoist/mygo/ui"
	"strconv"
	"wintray/internal/i18n"
	"wintray/internal/ui/kit"
	"wintray/internal/ui/kit/icons"
)

// Column widths, icon size and row spacing are shared across the interface.
// MyGo's virtual list retains WinTray's keyboard navigation and drag reorder.
const collectWidth, replaceWidth, orderWidth, controlGap = 36, 56, 80, 16

func (w *MainWindow) nativeProgramTable(c *native.Context, m i18n.Messages) []string {
	moveFrom, moveTo := -1, -1
	replaceRow := -1
	var replacement []string
	listArea := native.Column(c).Fill().Padding(12, 12, 12, 0).Gap(2)
	dropped := listArea.DroppedFiles()
	if listArea.FileDragOver() {
		listArea.Background(kit.Colors(c).Selected)
	}
	listArea.Children(func() {
		native.Row(c).FillWidth().Padding(0, 8, 6, 8).Gap(8).Children(func() {
			native.Box(c).Width(32).Shrink(0)
			kit.Caption(c, m.ManagedColumnName).Grow(1).MinWidth(0).SingleLine()
			native.Row(c).Gap(controlGap).Shrink(0).Children(func() {
				for _, column := range []struct {
					label string
					width float32
				}{{m.TrayColumn, collectWidth}, {m.ReplaceColumn, replaceWidth}, {m.ManagedColumnOrder, orderWidth}} {
					kit.Caption(c, column.label).Width(column.width).SingleLine().TextAlign(native.Center)
				}
			})
		})
		if len(w.settings.ManagedApps) == 0 {
			kit.Help(c, m.ManagedListEmpty).TextAlign(native.Center).FillWidth().Padding(48, 16)
			return
		}
		theme := c.Theme()
		listTheme := *theme
		listTheme.Accent, listTheme.AccentText = kit.Colors(c).Selected, theme.Text
		c.SetTheme(&listTheme)
		list := native.List(c, &w.desktop.list, len(w.settings.ManagedApps), func(index int) {
			c.SetTheme(theme)
			defer c.SetTheme(&listTheme)
			app := &w.settings.ManagedApps[index]
			native.Row(c).FillWidth().Padding(8).Gap(8).AlignItems(native.Center).Children(func() {
				iconBox := native.Row(c).Size(32, 32).Shrink(0).Label(m.ManagedDragHandle + " " + app.Name).Tooltip(m.ManagedDragHint).Cursor(native.CursorGrab)
				iconBox.Children(func() {
					if icon := w.desktop.icons[app.ExePath]; icon != nil {
						native.Image(c, icon).Size(32, 32)
					} else {
						native.Icon(c, icons.AppWindow).Size(32, 32).TextColor(theme.TextMuted)
					}
				})
				native.Text(c, app.Name).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(48).Tooltip(app.Name)
				native.Row(c).Gap(controlGap).Shrink(0).AlignItems(native.Center).Children(func() {
					native.Row(c).Width(collectWidth).Justify(native.Center).Children(func() {
						on := app.CollectTrayIcon
						if native.Switch(c, &on).Label(m.TrayBoxHomeTitle + " " + app.Name).Tooltip(m.TrayBoxHomeTitle).Disabled(shouldDefaultLaunchHidden(app.ExePath)).Changed() {
							w.nativeCollect(index, on)
						}
					})
					native.Row(c).Width(replaceWidth).Justify(native.Center).Children(func() {
						button := kit.IconButton(c, icons.FolderOpen, m.BrowseProgram+" "+app.Name).Tooltip(m.BrowseProgramHint)
						if button.Clicked() {
							replaceRow = index
						}
						if paths := button.DroppedFiles(); len(paths) > 0 {
							replaceRow = index
							replacement = paths
						}
					})
					native.Box(c).Width(orderWidth).Shrink(0).Children(func() {
						order := strconv.Itoa(index + 1)
						if nativeOrderSelect(c, &order, w.desktop.orderOptions, m.ManagedColumnOrder+" "+app.Name).Changed() {
							if position, err := strconv.Atoi(order); err == nil {
								moveFrom, moveTo = index, position-1
							}
						}
					})
				})
			})
		}).Grow(1).MinHeight(0).Padding(0).Gap(2).Label(m.ProgramsTab)
		c.SetTheme(theme)
		if list.Changed() {
			w.commitNativeEdits()
		}
		if list.Clicked() {
			w.commitNativeEdits()
			w.desktop.selected = -1
		}
	})
	// Mutate identities only after the virtual list finished building.
	if moveFrom >= 0 {
		w.nativeSetOrder(moveFrom, moveTo)
	}
	if replaceRow >= 0 {
		w.commitNativeEdits()
		w.desktop.selected = replaceRow
		if len(replacement) > 0 {
			w.nativeReplacePaths(replacement)
		} else {
			w.nativeChooseProgram(true)
		}
	}
	return dropped
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
		native.Icon(c, icons.ChevronDown).Size(12, 12).Shrink(0).TextColor(theme.TextMuted)
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
