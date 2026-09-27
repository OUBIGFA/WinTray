package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"wintray/internal/config"
)

// traySelectionChange returns the settings with one program's tray icon
// collected or released, without changing the settings passed in.
func traySelectionChange(current config.Settings, id string, on bool) (config.Settings, error) {
	next := current
	next.ManagedApps = slices.Clone(current.ManagedApps)
	for i := range next.ManagedApps {
		app := &next.ManagedApps[i]
		if app.ID != id {
			continue
		}
		if on && !strings.EqualFold(filepath.Ext(app.ExePath), ".exe") {
			return current, fmt.Errorf("tray icon collection requires an .exe program")
		}
		app.CollectTrayIcon = on
		return next, nil
	}
	return current, fmt.Errorf("program %q is no longer in the list", id)
}
