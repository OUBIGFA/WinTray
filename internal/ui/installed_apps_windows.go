//go:build windows

package ui

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/shellapp"
	"wintray/internal/ui/kit"
	"wintray/internal/ui/kit/icons"
)

type installedAppPicker struct {
	open     bool
	loaded   bool
	loading  bool
	failed   bool
	query    string
	kind     string
	apps     []shellapp.Application
	icons    map[string]*native.Bitmap
	selected map[string]bool
	list     native.ListState
	cancel   context.CancelFunc
}

func (w *MainWindow) nativeOpenInstalledApps() {
	if w.desktop.installedApps == nil {
		w.desktop.installedApps = &installedAppPicker{selected: make(map[string]bool)}
	}
	picker := w.desktop.installedApps
	picker.open = true
	if picker.kind == "" {
		picker.kind = "all"
	}
	if !picker.loaded && !picker.loading {
		w.nativeLoadInstalledApps(picker)
	} else if picker.loaded && picker.icons == nil {
		picker.cacheIcons()
	}
}

func (picker *installedAppPicker) cacheIcons() {
	picker.icons = make(map[string]*native.Bitmap, len(picker.apps))
	for _, app := range picker.apps {
		if app.Icon != nil {
			picker.icons[app.Key()] = native.NewBitmap(app.Icon)
		}
	}
}

func (w *MainWindow) nativeLoadInstalledApps(picker *installedAppPicker) {
	if picker.cancel != nil {
		picker.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	picker.cancel = cancel
	picker.loading, picker.failed = true, false
	picker.selected = make(map[string]bool)
	go func() {
		applications, err := shellapp.Enumerate(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("enumerate installed applications: %v", err)
		}
		w.Synchronize(func() {
			defer cancel()
			if ctx.Err() != nil || w.desktop.installedApps != picker {
				return
			}
			picker.loading, picker.failed, picker.loaded = false, err != nil, err == nil
			picker.apps = applications
			picker.cacheIcons()
			picker.list = native.ListState{}
			picker.cancel = nil
		})
	}()
}

func (w *MainWindow) nativeInstalledApps(c *native.Context, m i18n.Messages) {
	picker := w.desktop.installedApps
	if picker == nil || !picker.open {
		return
	}
	width, height := c.Size()
	native.Modal(c, &picker.open, func() {
		native.Column(c).Width(min(720, width-80)).Height(min(580, height-100)).MinHeight(0).Gap(16).Children(func() {
			native.Row(c).FillWidth().Gap(12).AlignItems(native.Center).Children(func() {
				kit.Heading(c, m.InstalledAppsTitle).Grow(1)
				if kit.Button(c, m.InstalledAppsRefresh, nil, false).Disabled(picker.loading).Clicked() {
					w.nativeLoadInstalledApps(picker)
				}
			})
			if native.TextInput(c, &picker.query).FillWidth().Label(m.InstalledAppsSearch).Placeholder(m.InstalledAppsSearch).Changed() {
				picker.list = native.ListState{}
			}
			if kit.Segmented(c, &picker.kind, []kit.SelectOption{
				{Value: "all", Label: m.InstalledAppsAll},
				{Value: "desktop", Label: m.InstalledAppsDesktop},
				{Value: "store", Label: m.InstalledAppsStore},
			}, m.InstalledAppsTitle) {
				picker.list = native.ListState{}
			}
			var filtered []shellapp.Application
			query := strings.ToLower(strings.TrimSpace(picker.query))
			for _, app := range picker.apps {
				packaged := shellapp.IsPath(app.Path)
				if picker.kind == "store" && !packaged || picker.kind == "desktop" && packaged {
					continue
				}
				if query == "" || strings.Contains(strings.ToLower(app.Name+" "+app.Path+" "+app.Args), query) {
					filtered = append(filtered, app)
				}
			}
			native.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
				switch {
				case picker.loading:
					kit.Help(c, m.InstalledAppsLoading)
				case picker.failed:
					kit.Help(c, m.InstalledAppsFailed)
				case len(filtered) == 0:
					kit.Help(c, m.InstalledAppsEmpty)
				default:
					native.List(c, &picker.list, len(filtered), func(index int) {
						app := filtered[index]
						key := app.Key()
						native.Row(c).Key(key).FillWidth().Padding(6, 8).Gap(12).AlignItems(native.Center).Children(func() {
							selected := picker.selected[key]
							choice := native.Checkbox(c, &selected, "").Label(app.Name).Grow(1).Shrink(1).MinWidth(0).Tooltip(strings.TrimSpace(app.Path + " " + app.Args))
							choice.Children(func() {
								if icon := picker.icons[key]; icon != nil {
									native.Image(c, icon).Size(32, 32).Shrink(0)
								} else {
									native.Icon(c, icons.AppWindow).Size(32, 32).Shrink(0).TextColor(c.Theme().TextMuted)
								}
								native.Text(c, app.Name).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
							})
							if choice.Changed() {
								if selected {
									picker.selected[key] = true
								} else {
									delete(picker.selected, key)
								}
							}
							kind := m.InstalledAppsDesktop
							if shellapp.IsPath(app.Path) {
								kind = m.InstalledAppsStore
							}
							kit.Caption(c, kind).Shrink(0)
						})
					}).Fill().MinHeight(0).Label(m.InstalledAppsTitle)
				}
			})
			native.Row(c).FillWidth().Gap(12).AlignItems(native.Center).Children(func() {
				kit.Caption(c, fmt.Sprintf(m.InstalledAppsSelection, len(picker.selected))).Grow(1)
				if kit.Button(c, m.InstalledAppsCancel, nil, false).Clicked() {
					picker.open = false
				}
				if nativePrimaryAction(c, m.InstalledAppsAdd, icons.Plus).Disabled(picker.loading || picker.failed || len(picker.selected) == 0).Clicked() {
					var applications []shellapp.Application
					for _, app := range picker.apps {
						if picker.selected[app.Key()] {
							applications = append(applications, app)
						}
					}
					w.nativeAddApplications(applications)
					picker.selected = make(map[string]bool)
					picker.open = false
				}
			})
		})
	})
	if !picker.open && picker.cancel != nil {
		picker.cancel()
		picker.cancel = nil
		picker.loading = false
	}
}

func (w *MainWindow) nativeAddApplications(applications []shellapp.Application) {
	if len(applications) == 0 {
		return
	}
	w.commitNativeEdits()
	if w.desktop.icons == nil {
		w.desktop.icons = make(map[string]*native.Bitmap)
	}
	base := time.Now().UnixNano()
	for index, app := range applications {
		if app.Icon != nil {
			w.desktop.icons[app.Path] = native.NewBitmap(app.Icon)
		}
		w.settings.ManagedApps = append(w.settings.ManagedApps, config.ManagedAppEntry{
			ID: strconv.FormatInt(base+int64(index), 10), Name: app.Name, ExePath: app.Path,
			Args: app.Args, WorkingDirectory: app.WorkingDirectory,
			Schedule: config.Schedule{AutoExitMinutes: config.DefaultAutoExitMinutes, FrequencyDays: 1, FrequencyRuns: 1},
		})
	}
	w.desktop.selected = len(w.settings.ManagedApps) - 1
	w.desktop.list.ScrollIntoView(w.desktop.selected)
	w.save()
	if w.desktop.window != nil {
		w.loadNativeIcons()
	}
}
