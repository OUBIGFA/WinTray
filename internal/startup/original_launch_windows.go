//go:build windows

package startup

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const AppTaskHelperRun = "--launch-startup-run"

// Read the original value at launch, so changes made by the program (including
// its next update) are not replaced with stale, copied arguments. REG_EXPAND_SZ
// is expanded at logon, while literal percent signs in REG_SZ stay literal.
func LaunchStartupRun(name, expectedExe string) error {
	return launchStartupRunFrom(runKeyPath, name, expectedExe, shellLaunchOriginal)
}

func launchStartupRunFrom(keyPath, name, expectedExe string, launch func(string, string, string, int32) error) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer key.Close()
	command, kind, err := key.GetStringValue(name)
	if err != nil {
		return err
	}
	if kind == registry.EXPAND_SZ {
		command, err = registry.ExpandString(command)
		if err != nil {
			return err
		}
	}
	path, args, err := splitStartupCommand(command)
	if err != nil {
		return err
	}
	if !sameExecutablePath(path, expectedExe) {
		return errors.New("startup Run target changed; save its WinTray settings again")
	}
	if err := launch(path, args, "", 1); err != nil {
		return fmt.Errorf("launch original Run entry %s: %w", name, err)
	}
	return nil
}
