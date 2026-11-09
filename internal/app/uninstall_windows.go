//go:build windows

package app

import (
	"fmt"
	"wintray/internal/config"
	"wintray/internal/ipc"
	"wintray/internal/startup"
)

// prepareUninstall restores the startup entries owned by WinTray while keeping
// settings and run history for a later installation or portable launch.
func prepareUninstall(restorePrograms, removeLogon func() error) error {
	if err := restorePrograms(); err != nil {
		return fmt.Errorf("restore program startup: %w", err)
	}
	if err := removeLogon(); err != nil {
		return fmt.Errorf("remove WinTray startup: %w", err)
	}
	return nil
}

func runPrepareUninstall() error {
	instance, running, err := ipc.Acquire(singleInstanceName)
	if err != nil {
		return err
	}
	defer instance.Close()
	if running {
		return fmt.Errorf("exit WinTray before uninstalling")
	}
	tasks, err := startup.NewAppTasks()
	if err != nil {
		return err
	}
	return prepareUninstall(func() error { return tasks.Sync(config.Settings{}, true) }, startup.NewRegistrar(appName).Remove)
}
