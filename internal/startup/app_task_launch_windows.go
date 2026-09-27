//go:build windows

package startup

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"

	"wintray/internal/config"
)

// LaunchAppTaskNow tests the same registered action/principal used at logon.
// It deliberately does not fall back to starting the configured exe: that
// would drop an app's original startup flags and administrator token again.
func LaunchAppTaskNow(entry config.ManagedAppEntry) error {
	tasks, err := NewAppTasks()
	if err != nil {
		return err
	}
	return tasks.launchNow(entry)
}

func (t *AppTasks) launchNow(entry config.ManagedAppEntry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	inventory, err := readTaskInventory(t.run)
	if err != nil {
		return err
	}
	name, err := findNativeLogonTask(inventory, entry.ExePath, t.userSID, t.namePrefix)
	if err != nil {
		return err
	}
	if name == "" {
		wanted := t.namePrefix + entry.ID
		for _, task := range inventory {
			if strings.TrimPrefix(task.Registration.URI, `\`) != wanted || !task.logonFor(t.userSID) {
				continue
			}
			if len(task.Actions.Exec) != 1 || !strings.Contains(task.Registration.Description, "wintray-app-") {
				return errors.New("registered WinTray task was edited outside WinTray; save settings again before testing")
			}
			action := task.Actions.Exec[0]
			matches := sameExecutablePath(action.Command, entry.ExePath)
			if sameExecutablePath(action.Command, t.selfExe) {
				args, parseErr := windows.DecomposeCommandLine(action.Arguments)
				matches = parseErr == nil && len(args) == 3 &&
					(args[0] == AppTaskHelperRun || args[0] == AppTaskHelperShortcut || args[0] == AppTaskHelperConfigured) && sameExecutablePath(args[2], entry.ExePath)
			}
			if !matches {
				return errors.New("registered task targets another executable; save settings again before testing")
			}
			name = wanted
			break
		}
	}
	if name == "" {
		return errors.New("no enabled logon task is registered; enable sign-in startup and wait for settings to finish saving before testing")
	}
	if _, err := t.run("/Run", "/TN", name); err != nil {
		return fmt.Errorf("run original startup task %s: %w", name, err)
	}
	return nil
}
