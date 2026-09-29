//go:build windows

package startup

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"wintray/internal/config"
)

// Native tasks keep their actions, conditions, other triggers and privileges.
// Only their logon triggers are suppressed; /Run still launches the same task.
type nativeTaskBackup struct {
	Name    string `json:"name"`
	Before  string `json:"before"`
	Applied string `json:"applied"`
}

// removeLogonTriggers edits byte ranges rather than marshaling taskDefinition,
// whose discovery-only schema intentionally does not model every task setting.
func removeLogonTriggers(definition string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(definition))
	decoder.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var out strings.Builder
	depth, start, last := 0, -1, 0
	for {
		offset := int(decoder.InputOffset())
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch node := token.(type) {
		case xml.StartElement:
			if start >= 0 {
				depth++
			} else if node.Name.Local == "LogonTrigger" {
				start = offset
				depth = 1
			}
		case xml.EndElement:
			if start >= 0 {
				depth--
				if depth == 0 {
					out.WriteString(definition[last:start])
					last = int(decoder.InputOffset())
					start = -1
				}
			}
		}
	}
	out.WriteString(definition[last:])
	return out.String(), nil
}

// Ignore export formatting, not meaningful XML content, when checking ownership.
func canonicalTaskXML(definition string) (string, error) {
	decoder := xml.NewDecoder(strings.NewReader(definition))
	decoder.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var out strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch node := token.(type) {
		case xml.StartElement:
			fmt.Fprintf(&out, "<%q:%q", node.Name.Space, node.Name.Local)
			attrs := append([]xml.Attr(nil), node.Attr...)
			sort.Slice(attrs, func(i, j int) bool {
				return attrs[i].Name.Space+attrs[i].Name.Local < attrs[j].Name.Space+attrs[j].Name.Local
			})
			for _, attr := range attrs {
				if attr.Name.Local != "xmlns" && attr.Name.Space != "xmlns" {
					fmt.Fprintf(&out, " %q:%q=%q", attr.Name.Space, attr.Name.Local, attr.Value)
				}
			}
			out.WriteByte('>')
		case xml.EndElement:
			fmt.Fprintf(&out, "</%q:%q>", node.Name.Space, node.Name.Local)
		case xml.CharData:
			if strings.TrimSpace(string(node)) != "" {
				fmt.Fprintf(&out, "%q", string(node))
			}
		}
	}
	return out.String(), nil
}

func sameTaskXML(a, b string) bool {
	left, err := canonicalTaskXML(a)
	if err != nil {
		return false
	}
	right, err := canonicalTaskXML(b)
	return err == nil && left == right
}

func (t *AppTasks) verifyNativeTask(backup *nativeTaskBackup) error {
	out, err := t.run("/Query", "/TN", backup.Name, "/XML")
	if err != nil {
		return err
	}
	current := taskXMLText(out)
	if !sameTaskXML(current, backup.Applied) && !sameTaskXML(current, backup.Before) {
		return fmt.Errorf("original task %s changed outside WinTray; leaving it and its backup untouched", backup.Name)
	}
	return nil
}

func (t *AppTasks) writeNativeTask(name, definition string, allowElevated bool) error {
	var task taskDefinition
	if err := decodeTaskXML([]byte(definition), &task); err != nil {
		return err
	}
	highest := false
	for _, principal := range task.Principals {
		highest = highest || principal.RunLevel == "HighestAvailable"
	}
	path, cleanup, err := writeTaskDefinition(definition)
	if err != nil {
		return err
	}
	defer cleanup()
	if highest && !IsProcessElevated() {
		if !allowElevated || t.elevateSelf == nil {
			return errElevationNeeded
		}
		return t.elevateSelf(AppTaskHelperRegister, name, path)
	}
	_, err = t.run("/Create", "/TN", name, "/XML", path, "/F")
	return err
}

func (t *AppTasks) syncLimitedNative(spec appTaskSpec, name string, previous appMigration, state *migrationState, allowElevated bool) error {
	out, err := t.run("/Query", "/TN", name, "/XML")
	if err != nil {
		return err
	}
	current := taskXMLText(out)
	backup := previous.NativeTask
	if backup == nil {
		applied, err := removeLogonTriggers(current)
		if err != nil {
			return err
		}
		backup = &nativeTaskBackup{Name: name, Before: current, Applied: applied}
	} else if err := t.verifyNativeTask(backup); err != nil {
		return err
	}
	previous.ExePath = spec.exePath
	previous.Frequency = true
	previous.NativeTask = backup
	state.Apps[spec.name] = previous
	// Persist the exact original before the first system change, also on retries.
	if err := t.saveMigrationState(state); err != nil {
		return err
	}
	if sameTaskXML(current, backup.Applied) {
		return nil
	}
	if err := t.writeNativeTask(name, backup.Applied, allowElevated); err != nil {
		return err
	}
	exported, err := t.run("/Query", "/TN", name, "/XML")
	if err != nil {
		return err
	}
	backup.Applied = taskXMLText(exported)
	return t.saveMigrationState(state)
}

func (t *AppTasks) restoreNativeTask(backup *nativeTaskBackup, allowElevated bool) error {
	if backup == nil {
		return nil
	}
	if err := t.verifyNativeTask(backup); err != nil {
		return err
	}
	out, err := t.run("/Query", "/TN", backup.Name, "/XML")
	if err != nil {
		return err
	}
	if sameTaskXML(taskXMLText(out), backup.Before) {
		return nil
	}
	return t.writeNativeTask(backup.Name, backup.Before, allowElevated)
}

// frequencyTaskName refuses to launch before takeover has completed, or after
// an external edit has restored an unchecked logon trigger.
func (t *AppTasks) frequencyTaskName(entry config.ManagedAppEntry) (string, error) {
	state, err := loadMigrationState(t.statePath)
	if err != nil {
		return "", err
	}
	name := t.namePrefix + entry.ID
	migration, ok := state.Apps[name]
	if !ok || !migration.Frequency || !sameExecutablePath(migration.ExePath, entry.ExePath) {
		return "", errors.New("startup frequency takeover is not ready; save settings again")
	}
	if migration.NativeTask != nil {
		name = migration.NativeTask.Name
		out, err := t.run("/Query", "/TN", name, "/XML")
		if err != nil {
			return "", err
		}
		if !sameTaskXML(taskXMLText(out), migration.NativeTask.Applied) {
			return "", errors.New("original task no longer has WinTray's frequency control")
		}
	}
	out, err := t.run("/Query", "/TN", name, "/XML")
	if err != nil {
		return "", err
	}
	var task taskDefinition
	if err := decodeTaskXML(out, &task); err != nil {
		return "", err
	}
	if !task.interactiveFor(t.userSID) || len(task.Triggers) != 0 {
		return "", errors.New("frequency task is disabled or has unchecked logon triggers")
	}
	if migration.NativeTask == nil {
		if len(task.Actions.Exec) != 1 || !sameExecutablePath(task.Actions.Exec[0].Command, t.selfExe) {
			return "", errors.New("frequency task action changed")
		}
		// Validate the action and expected executable using the same helper parser
		// as normal task launches below.
		if !matchesStartupHelper(task.Actions.Exec[0].Arguments, entry.ExePath) {
			return "", errors.New("frequency task targets another program")
		}
	}
	entries, err := t.findEntries(entry.ExePath)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.enabled {
			return "", errors.New("original startup was re-enabled outside WinTray; save settings again")
		}
	}
	return name, nil
}
