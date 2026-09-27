//go:build windows

package startup

import (
	"errors"
	"fmt"

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
	return findEnabledRunEntry(exePath, standardRunSources())
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

// runCommandMatches reports whether Windows starts exePath itself for command;
// a wrapper or an ambiguous unquoted command is not proof that it does.
func runCommandMatches(command, exePath string) bool {
	_, _, err := splitStartupCommandFor(command, exePath)
	return err == nil
}

func runEntryApproved(root registry.Key, path, name string) (bool, error) {
	value, err := readApproval(root, path, name)
	if err != nil {
		return false, err
	}
	return approvalEnabled(value)
}
