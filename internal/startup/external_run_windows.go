//go:build windows

package startup

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const startupApprovedPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved`

type runSource struct {
	root         registry.Key
	path         string
	view         uint32
	approvalPath string
	label        string
}

// FindEnabledRunEntry finds a direct, enabled Windows Run command for exePath.
// Only the executable argument is compared: a name, title, or a path appearing
// inside another program's arguments is not proof that Windows will start it.
// This is read-only; WinTray never takes ownership of another app's Run entry.
func FindEnabledRunEntry(exePath string) (string, error) {
	return findEnabledRunEntry(exePath, []runSource{
		{registry.CURRENT_USER, runKeyPath, registry.WOW64_64KEY, startupApprovedPath + `\Run`, `HKCU\Run`},
		{registry.LOCAL_MACHINE, runKeyPath, registry.WOW64_64KEY, startupApprovedPath + `\Run`, `HKLM\Run`},
		{registry.LOCAL_MACHINE, runKeyPath, registry.WOW64_32KEY, startupApprovedPath + `\Run32`, `HKLM\Run32`},
	})
}

func findEnabledRunEntry(exePath string, sources []runSource) (string, error) {
	var errs []error
	for _, source := range sources {
		name, err := source.find(exePath)
		if name != "" {
			return source.label + `\` + name, nil
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", source.label, err))
		}
	}
	return "", errors.Join(errs...)
}

func (s runSource) find(exePath string) (string, error) {
	key, err := registry.OpenKey(s.root, s.path, registry.QUERY_VALUE|s.view)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer key.Close()
	names, err := key.ReadValueNames(0)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		command, kind, err := key.GetStringValue(name)
		if errors.Is(err, registry.ErrUnexpectedType) || errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if kind == registry.EXPAND_SZ {
			command, err = registry.ExpandString(command)
			if err != nil {
				return "", err
			}
		}
		if !runCommandMatches(command, exePath) {
			continue
		}
		enabled, err := runEntryApproved(s.root, s.approvalPath, name)
		if err != nil {
			return "", fmt.Errorf("%s approval: %w", name, err)
		}
		// Machine Run entries may also have a per-user disabled state.
		if enabled && s.root == registry.LOCAL_MACHINE {
			enabled, err = runEntryApproved(registry.CURRENT_USER, s.approvalPath, name)
			if err != nil {
				return "", fmt.Errorf("%s user approval: %w", name, err)
			}
		}
		if enabled {
			return name, nil
		}
	}
	return "", nil
}

func runCommandMatches(command, exePath string) bool {
	args, err := windows.DecomposeCommandLine(strings.TrimSpace(command))
	if err != nil || len(args) == 0 {
		return false
	}
	expected := strings.Trim(strings.TrimSpace(exePath), `"`)
	// Do not resolve a relative Run command against WinTray's working directory.
	return filepath.IsAbs(args[0]) && filepath.IsAbs(expected) &&
		strings.EqualFold(filepath.Clean(args[0]), filepath.Clean(expected))
}

func runEntryApproved(root registry.Key, path, name string) (bool, error) {
	// StartupApproved is in the native Explorer view even for Run32 entries.
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return true, nil // absent approval means enabled, not disabled
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	data, _, err := key.GetBinaryValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if len(data) >= 12 {
		switch data[0] {
		case 2, 6:
			return true, nil
		case 3, 7:
			return false, nil
		}
	}
	// An unreadable/unknown state is not evidence that no external launch is
	// pending. Surface it instead of risking a second process.
	return false, fmt.Errorf("unknown StartupApproved state %x", data)
}
