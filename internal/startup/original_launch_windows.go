//go:build windows

package startup

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	AppTaskHelperRun = "--launch-startup-run"
	// AppTaskHelperConfigured starts a program that has no original startup
	// entry with the arguments configured in WinTray.
	AppTaskHelperConfigured = "--launch-configured"
)

// LaunchConfigured starts exePath with args from its own folder, as the task
// did when it launched the program directly.
func LaunchConfigured(args, exePath string) error {
	if !filepath.IsAbs(exePath) || !strings.EqualFold(filepath.Ext(exePath), ".exe") {
		return errors.New("configured startup requires an absolute .exe path")
	}
	return shellLaunchOriginal(exePath, args, filepath.Dir(exePath), 1)
}

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
	path, args, err := splitStartupCommandFor(command, expectedExe)
	if errors.Is(err, errNotStartupTarget) {
		return errors.New("startup Run target changed; save its WinTray settings again")
	}
	var ambiguous ambiguousStartupCommand
	if errors.As(err, &ambiguous) {
		return ambiguous.at(`HKCU\Run\` + name)
	}
	if err != nil {
		return err
	}
	// Run values do not specify a working directory. Preserve the inherited
	// launch context, as Explorer and the logon task do; forcing the program
	// folder can change the meaning of relative arguments.
	if err := launch(path, args, "", 1); err != nil {
		return fmt.Errorf("launch original Run entry %s: %w", name, err)
	}
	return nil
}
