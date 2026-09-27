//go:build windows

package startup

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wintray/internal/config"
)

var errApprovalChanged = errors.New("startup enable state changed outside WinTray; not overwriting it")

type approvalBackup struct {
	Path     string        `json:"path"`
	Name     string        `json:"name"`
	Before   approvalValue `json:"before"`
	Disabled []byte        `json:"disabled"`
}

func (b approvalBackup) id() string { return strings.ToLower(b.Path + `\` + b.Name) }
func (b approvalBackup) owns(value approvalValue) bool {
	return value.Exists && bytes.Equal(value.Data, b.Disabled)
}

type appMigration struct {
	ExePath   string           `json:"exePath"`
	Approvals []approvalBackup `json:"approvals"`
}

type migrationState struct {
	Version int                     `json:"version"`
	Apps    map[string]appMigration `json:"apps"`
}

func loadMigrationState(path string) (*migrationState, error) {
	state := &migrationState{Version: 1, Apps: make(map[string]appMigration)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read original startup backups: %w", err)
	}
	var decoded migrationState
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Version != 1 || decoded.Apps == nil {
		return nil, fmt.Errorf("invalid original startup backup %s (left untouched): version=%d err=%v", path, decoded.Version, err)
	}
	for name, app := range decoded.Apps {
		if name == "" || !filepath.IsAbs(app.ExePath) {
			return nil, fmt.Errorf("invalid startup backup entry %q (left untouched)", name)
		}
		for _, backup := range app.Approvals {
			enabled, err := approvalEnabled(backup.Before)
			if backup.Path == "" || backup.Name == "" || len(backup.Disabled) != 12 || backup.Disabled[0] != 3 || err != nil || !enabled || (!backup.Before.Exists && len(backup.Before.Data) != 0) {
				return nil, fmt.Errorf("invalid startup approval backup for %s (left untouched)", name)
			}
		}
	}
	return &decoded, nil
}

func (t *AppTasks) saveMigrationState(state *migrationState) error {
	if t.statePath == "" {
		return errors.New("original startup backup path is unavailable")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(t.statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".startup-migrations-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), t.statePath)
}

func disabledApproval() []byte {
	data := make([]byte, 12)
	data[0] = 3
	// The normal Windows disabled timestamp also serves as an ownership token.
	// Never restore over an enable/disable operation performed by somebody else.
	binary.LittleEndian.PutUint64(data[4:], uint64(time.Now().UnixNano()/100)+116444736000000000)
	return data
}

func writeUserApproval(path, name string, expected, value approvalValue) error {
	current, err := readApproval(registry.CURRENT_USER, path, name)
	if err != nil {
		return err
	}
	if !sameApproval(current, expected) {
		return errApprovalChanged
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer key.Close()
	if value.Exists {
		return key.SetBinaryValue(name, value.Data)
	}
	err = key.DeleteValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

// restoreApprovals changes only the exact values WinTray wrote. A removed or
// externally edited value belongs to that other writer, not to our backup.
func (t *AppTasks) restoreApprovals(backups []approvalBackup) ([]approvalBackup, error) {
	var remaining []approvalBackup
	var errs []error
	for _, backup := range backups {
		current, err := readApproval(registry.CURRENT_USER, backup.Path, backup.Name)
		if err == nil && backup.owns(current) {
			err = t.writeApproval(backup.Path, backup.Name, current, backup.Before)
		}
		if errors.Is(err, errApprovalChanged) {
			err = nil // a concurrent user change wins
		}
		if err != nil {
			remaining = append(remaining, backup)
			errs = append(errs, fmt.Errorf("restore %s: %w", backup.Name, err))
		}
	}
	return remaining, errors.Join(errs...)
}

// Delete our replacement before restoring the original trigger. If deletion
// fails, retain the backup and suppression; never enable two launch owners.
func (t *AppTasks) releaseMigration(name string, state *migrationState, allowElevated bool) error {
	if err := t.remove(name, allowElevated); err != nil {
		return err
	}
	migration, ok := state.Apps[name]
	if !ok {
		return nil
	}
	remaining, restoreErr := t.restoreApprovals(migration.Approvals)
	if len(remaining) == 0 {
		delete(state.Apps, name)
	} else {
		migration.Approvals = remaining
		state.Apps[name] = migration
	}
	return errors.Join(restoreErr, t.saveMigrationState(state))
}

func usableStartupEntries(entries []startupEntry, previous appMigration) ([]startupEntry, error) {
	owned := make(map[string]approvalBackup, len(previous.Approvals))
	for _, backup := range previous.Approvals {
		owned[backup.id()] = backup
	}
	var usable []startupEntry
	for _, entry := range entries {
		backup, wasOwned := owned[entry.approvalID()]
		if wasOwned && !backup.owns(entry.approval) && !sameApproval(entry.approval, backup.Before) {
			return nil, fmt.Errorf("%s: %w", entry.label, errApprovalChanged)
		}
		if !entry.enabled && !(wasOwned && backup.owns(entry.approval)) {
			continue
		}
		if entry.machine {
			return nil, fmt.Errorf("%s is a shared machine startup entry; a user-level migration cannot safely disable it for other users", entry.label)
		}
		usable = append(usable, entry)
	}
	if len(usable) == 0 && len(entries) > 0 {
		return nil, errors.New("original startup entry is disabled; refusing to bypass its disabled state")
	}
	if len(usable) == 0 && len(previous.Approvals) > 0 {
		return nil, errors.New("original startup entry was removed or changed executable; refusing a bare-executable fallback")
	}
	if len(usable) == 0 {
		return nil, errors.New("no supported original startup entry found; enable the program's own startup first, or use WinTray's normal launch mode")
	}
	return usable, nil
}

func (t *AppTasks) syncPreservingStartup(app config.LogonTaskApp, inventory []taskDefinition, state *migrationState, allowElevated bool) error {
	spec := t.spec(app)
	previous := state.Apps[spec.name]
	if previous.ExePath != "" && !sameExecutablePath(previous.ExePath, app.Entry.ExePath) {
		if err := t.releaseMigration(spec.name, state, allowElevated); err != nil {
			return err
		}
		previous = appMigration{}
	}
	fail := func(cause error) error {
		// This also heals old, parameterless replacement tasks on ambiguous
		// discovery. Keeping such a task enabled would keep the bug alive.
		return errors.Join(cause, t.releaseMigration(spec.name, state, allowElevated))
	}
	if !filepath.IsAbs(app.Entry.ExePath) || !strings.EqualFold(filepath.Ext(app.Entry.ExePath), ".exe") {
		return fail(errors.New("logon startup requires an absolute .exe path"))
	}
	native, err := findNativeLogonTask(inventory, app.Entry.ExePath, t.userSID, t.namePrefix)
	if err != nil {
		return fail(err)
	}
	entries, err := t.findEntries(app.Entry.ExePath)
	if err != nil {
		return fail(err)
	}
	// A native task is already a valid startup owner. Leave other disabled
	// entries disabled; only active entries (or our own suppression) need work.
	if native != "" {
		var active []startupEntry
		for _, entry := range entries {
			owned := false
			for _, backup := range previous.Approvals {
				owned = owned || (backup.id() == entry.approvalID() && backup.owns(entry.approval))
			}
			if entry.enabled || owned {
				active = append(active, entry)
			}
		}
		entries = active
		if len(entries) == 0 {
			if err := t.releaseMigration(spec.name, state, allowElevated); err != nil {
				return err
			}
			t.report("reuse original logon task: " + app.Entry.Name + " -> " + native)
			return nil
		}
	}
	// With no original trigger there is nothing to migrate or preserve: the
	// user explicitly chose system startup for this configured command.
	// Do not mistake an unreadable, disabled, or removed migrated source for
	// this case (those retain the fail-closed paths below).
	if native == "" && len(entries) == 0 && len(previous.Approvals) == 0 {
		if err := t.syncOne(spec, allowElevated); err != nil {
			return err
		}
		t.report("registered configured logon startup: " + app.Entry.Name)
		return nil
	}
	usable, err := usableStartupEntries(entries, previous)
	if err != nil {
		return fail(err)
	}
	for i := 1; i < len(usable); i++ {
		if !usable[0].sameLaunch(usable[i]) {
			return fail(errors.New("multiple original startup commands differ; refusing to choose or merge their arguments"))
		}
	}
	if native != "" {
		for _, task := range inventory {
			if strings.TrimPrefix(task.Registration.URI, `\`) != native {
				continue
			}
			for _, action := range task.Actions.Exec {
				path, _ := expandedPath(action.Command)
				if sameExecutablePath(path, app.Entry.ExePath) && (action.Arguments != usable[0].args ||
					(usable[0].workingDir != "" && !sameExecutablePath(action.WorkingDirectory, usable[0].workingDir))) {
					return fail(errors.New("native task and ordinary startup entry use different launch settings; refusing to choose between them"))
				}
			}
		}
	}
	if native == "" {
		entry := usable[0]
		spec.highest = entry.highest || requiresElevationForLaunch(entry.path)
		spec.workingDir = entry.workingDir
		// Keep the original Run/shortcut as the source of truth, including
		// future program-controlled changes and literal argument quoting.
		spec.exePath = t.selfExe
		if entry.shortcut != "" {
			spec.args = windows.ComposeCommandLine([]string{AppTaskHelperShortcut, entry.shortcut, entry.path})
		} else {
			spec.args = windows.ComposeCommandLine([]string{AppTaskHelperRun, entry.approvalName, entry.path})
		}
	}
	if native != "" {
		if err := t.remove(spec.name, allowElevated); err != nil {
			return err
		}
	}

	backups := make([]approvalBackup, 0, len(usable))
	byID := make(map[string]approvalBackup, len(previous.Approvals))
	for _, backup := range previous.Approvals {
		byID[backup.id()] = backup
	}
	for _, entry := range usable {
		backup, ok := byID[entry.approvalID()]
		if !ok {
			backup = approvalBackup{Path: entry.approvalPath, Name: entry.approvalName, Before: entry.approval, Disabled: disabledApproval()}
		}
		backups = append(backups, backup)
		delete(byID, entry.approvalID())
	}
	var obsolete []approvalBackup
	for _, backup := range byID {
		obsolete = append(obsolete, backup)
	}
	if _, err := t.restoreApprovals(obsolete); err != nil {
		return fail(err)
	}
	if len(backups) > 0 || len(previous.Approvals) > 0 {
		state.Apps[spec.name] = appMigration{ExePath: app.Entry.ExePath, Approvals: backups}
		// Durable backup BEFORE changing Windows. A crash can be reconciled
		// on the next run without deleting/reconstructing the program's data.
		if err := t.saveMigrationState(state); err != nil {
			return err
		}
	}
	for i, backup := range backups {
		if backup.owns(usable[i].approval) {
			continue
		}
		value := approvalValue{Exists: true, Data: backup.Disabled}
		if err := t.writeApproval(backup.Path, backup.Name, usable[i].approval, value); err != nil {
			return fail(fmt.Errorf("disable original trigger %s: %w", usable[i].label, err))
		}
	}
	if native == "" {
		if err := t.syncOne(spec, allowElevated); err != nil {
			return fail(err)
		}
		t.report("migrated original startup trigger: " + app.Entry.Name + " <- " + usable[0].label)
	} else {
		t.report("reuse original logon task: " + app.Entry.Name + " -> " + native)
	}
	return nil
}
