//go:build windows

package traybox

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const settingsKeyPath = `Control Panel\NotifyIconSettings`

// ErrUnsupported means the notification-area settings Windows 11 keeps per
// icon are not available, as on Windows 10.
var ErrUnsupported = errors.New("notification icon settings are not available; Windows 11 is required")

// ReadIcons lists the icons Explorer has recorded, including ones whose
// program is not running.
func ReadIcons() ([]Icon, error) {
	root, err := registry.OpenKey(registry.CURRENT_USER, settingsKeyPath, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", settingsKeyPath, err)
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", settingsKeyPath, err)
	}
	icons := make([]Icon, 0, len(names))
	for _, name := range names {
		icon, ok := readIcon(root, name)
		if ok {
			icons = append(icons, icon)
		}
	}
	return icons, nil
}

func readIcon(root registry.Key, name string) (Icon, bool) {
	k, err := registry.OpenKey(root, name, registry.QUERY_VALUE)
	if err != nil {
		return Icon{}, false
	}
	defer k.Close()
	path, _, err := k.GetStringValue("ExecutablePath")
	// Paths below a known folder are stored as "{GUID}\rest"; they belong to
	// packaged or system programs that cannot be matched to a process path.
	if err != nil || path == "" || strings.HasPrefix(path, "{") {
		return Icon{}, false
	}
	uid, _, _ := k.GetIntegerValue("UID")
	tooltip, _, _ := k.GetStringValue("InitialTooltip")
	snapshot, _, _ := k.GetBinaryValue("IconSnapshot")
	promoted, _, _ := k.GetIntegerValue("IsPromoted")
	return Icon{
		ExePath:  path,
		UID:      uint32(uid),
		Tooltip:  tooltip,
		Snapshot: snapshot,
		Promoted: promoted == 1,
	}, true
}

// SetPromoted shows every icon of the program on the taskbar or moves it to
// the hidden-icons flyout. Explorer applies the change immediately. Values
// already set are left alone so Explorer is not notified needlessly. If any
// update fails, the values changed so far are restored.
func SetPromoted(exePath string, promoted bool) error {
	return updatePromoted(exePath, promoted, nil)
}

// updatePromoted keeps the previous values until commit succeeds. A failed
// update or commit restores each changed icon, including absent values.
func updatePromoted(exePath string, promoted bool, commit func() error) error {
	return updatePromotedPaths([]string{exePath}, promoted, commit)
}

func updatePromotedPaths(paths []string, promoted bool, commit func() error) error {
	root, err := registry.OpenKey(registry.CURRENT_USER, settingsKeyPath, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return ErrUnsupported
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", settingsKeyPath, err)
	}
	defer root.Close()
	return updatePromotedPathsAt(root, paths, promoted, commit)
}

func updatePromotedAt(root registry.Key, exePath string, promoted bool, commit func() error) error {
	return updatePromotedPathsAt(root, []string{exePath}, promoted, commit)
}

func updatePromotedPathsAt(root registry.Key, paths []string, promoted bool, commit func() error) error {
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return fmt.Errorf("list notification icon settings: %w", err)
	}
	want := uint64(0)
	if promoted {
		want = 1
	}
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		wanted[strings.ToLower(path)] = true
	}
	var changes []promotedChange
	defer func() {
		for _, c := range changes {
			c.key.Close()
		}
	}()
	for _, name := range names {
		change, err := setPromoted(root, name, wanted, want)
		if err != nil {
			return errors.Join(fmt.Errorf("%s: %w", name, err), restorePromoted(changes))
		}
		if change != nil {
			changes = append(changes, *change)
		}
	}
	if commit != nil {
		if err := commit(); err != nil {
			return errors.Join(fmt.Errorf("save tray box: %w", err), restorePromoted(changes))
		}
	}
	return nil
}

// typ is zero when IsPromoted was absent, otherwise DWORD or QWORD. Retaining
// the key handle ensures rollback addresses the entry we actually changed.
type promotedChange struct {
	key   registry.Key
	name  string
	value uint64
	typ   uint32
}

func restorePromoted(changes []promotedChange) error {
	var errs []error
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		var err error
		switch c.typ {
		case registry.DWORD:
			err = c.key.SetDWordValue("IsPromoted", uint32(c.value))
		case registry.QWORD:
			err = c.key.SetQWordValue("IsPromoted", c.value)
		default:
			err = c.key.DeleteValue("IsPromoted")
			if errors.Is(err, registry.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restore %s IsPromoted: %w", c.name, err))
		}
	}
	return errors.Join(errs...)
}

func setPromoted(root registry.Key, name string, wanted map[string]bool, want uint64) (*promotedChange, error) {
	k, err := registry.OpenKey(root, name, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()
	path, _, err := k.GetStringValue("ExecutablePath")
	if errors.Is(err, registry.ErrNotExist) || errors.Is(err, registry.ErrUnexpectedType) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !wanted[strings.ToLower(path)] {
		return nil, nil
	}
	current, typ, err := k.GetIntegerValue("IsPromoted")
	if errors.Is(err, registry.ErrNotExist) {
		typ = 0
	} else if err != nil {
		// Never overwrite a value we cannot snapshot and restore faithfully.
		return nil, err
	} else if current == want {
		return nil, nil
	}
	writable, err := registry.OpenKey(k, "", registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	if err := writable.SetDWordValue("IsPromoted", uint32(want)); err != nil {
		writable.Close()
		return nil, err
	}
	return &promotedChange{key: writable, name: name, value: current, typ: typ}, nil
}
