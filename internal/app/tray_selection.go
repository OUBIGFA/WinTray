package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"wintray/internal/config"
	"wintray/internal/traybox"
)

// traySelectionChange changes one managed program, but only changes the
// system icon when no other selected entry still refers to that executable.
func traySelectionChange(current config.Settings, id string, on bool) (config.Settings, string, bool, bool, error) {
	next := current
	next.ManagedApps = slices.Clone(current.ManagedApps)
	for i := range next.ManagedApps {
		app := &next.ManagedApps[i]
		if app.ID != id {
			continue
		}
		if on && !strings.EqualFold(filepath.Ext(app.ExePath), ".exe") {
			return current, "", false, false, fmt.Errorf("tray icon collection requires an .exe program")
		}
		path := app.ExePath
		wasSelected := traybox.Contains(config.CollectedTrayIconPaths(current), path)
		app.CollectTrayIcon = on
		isSelected := traybox.Contains(config.CollectedTrayIconPaths(next), path)
		return next, path, wasSelected, isSelected, nil
	}
	return current, "", false, false, fmt.Errorf("program %q is no longer in the list", id)
}
