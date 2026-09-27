//go:build windows

package startup

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Task discovery is based on the action's executable and the interactive user,
// never a product/task name. Keep the native definition intact: even actions
// before/after the executable, conditions and privileges belong to the app.
type taskDefinition struct {
	XMLName      xml.Name `xml:"Task"`
	Registration struct {
		URI         string `xml:"URI"`
		Description string `xml:"Description"`
	} `xml:"RegistrationInfo"`
	Principals []struct {
		ID        string `xml:"id,attr"`
		UserID    string `xml:"UserId"`
		GroupID   string `xml:"GroupId"`
		LogonType string `xml:"LogonType"`
		RunLevel  string `xml:"RunLevel"`
	} `xml:"Principals>Principal"`
	Triggers []struct {
		Enabled       *bool  `xml:"Enabled"`
		UserID        string `xml:"UserId"`
		Delay         string `xml:"Delay"`
		StartBoundary string `xml:"StartBoundary"`
		EndBoundary   string `xml:"EndBoundary"`
	} `xml:"Triggers>LogonTrigger"`
	Settings struct {
		Enabled *bool `xml:"Enabled"`
	} `xml:"Settings"`
	Actions struct {
		Context string `xml:"Context,attr"`
		Exec    []struct {
			Command          string `xml:"Command"`
			Arguments        string `xml:"Arguments"`
			WorkingDirectory string `xml:"WorkingDirectory"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

func enabledByDefault(value *bool) bool { return value == nil || *value }

func taskXMLText(data []byte) string {
	// schtasks exports UTF-16 on some versions and console-encoded XML on
	// others. Decode before XML parsing (including non-ASCII paths/names).
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || (len(data) > 3 && data[1] == 0 && data[3] == 0) {
		data = bytes.TrimPrefix(data, []byte{0xff, 0xfe})
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
		}
		return string(utf16.Decode(units))
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(data) {
		return string(data)
	}
	return oemText(data)
}

func decodeTaskXML(data []byte, value any) error {
	decoder := xml.NewDecoder(strings.NewReader(taskXMLText(data)))
	decoder.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		// The bytes have already been converted to UTF-8 above.
		if strings.EqualFold(charset, "UTF-16") || strings.EqualFold(charset, "UTF-8") {
			return input, nil
		}
		return nil, fmt.Errorf("unsupported task XML encoding %q", charset)
	}
	return decoder.Decode(value)
}

func readTaskInventory(run func(...string) ([]byte, error)) ([]taskDefinition, error) {
	// ONE is important: without it schtasks embeds an XML declaration before
	// every task, producing something that is not a valid XML document.
	out, err := run("/Query", "/XML", "ONE")
	if err != nil {
		return nil, fmt.Errorf("read scheduled tasks: %w", err)
	}
	var inventory struct {
		XMLName xml.Name         `xml:"Tasks"`
		Tasks   []taskDefinition `xml:"Task"`
	}
	if err := decodeTaskXML(out, &inventory); err != nil {
		return nil, fmt.Errorf("parse scheduled tasks: %w", err)
	}
	return inventory.Tasks, nil
}

func sameExecutablePath(a, b string) bool {
	a = strings.Trim(strings.TrimSpace(a), `"`)
	b = strings.Trim(strings.TrimSpace(b), `"`)
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	// Junctions, redirected profile folders, short paths and hard links can
	// name the same executable. Compare Windows file identity, not contents,
	// before concluding that no original startup exists.
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

func expandedPath(path string) (string, error) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	return registry.ExpandString(path)
}

func sameWindowsUser(userID, sid string) bool {
	if strings.EqualFold(userID, sid) {
		return true
	}
	if userID == "" || strings.HasPrefix(strings.ToUpper(userID), "S-1-") {
		return false
	}
	resolved, _, _, err := windows.LookupSID("", userID)
	return err == nil && strings.EqualFold(resolved.String(), sid)
}

func (task taskDefinition) launches(exePath string) bool {
	for _, action := range task.Actions.Exec {
		path, err := expandedPath(action.Command)
		if err == nil && sameExecutablePath(path, exePath) {
			return true
		}
	}
	return false
}

func (task taskDefinition) logonFor(sid string) bool {
	if !enabledByDefault(task.Settings.Enabled) {
		return false
	}
	interactive := false
	for _, principal := range task.Principals {
		if task.Actions.Context != "" && principal.ID != task.Actions.Context {
			continue
		}
		if principal.GroupID == "" && sameWindowsUser(principal.UserID, sid) && principal.LogonType == "InteractiveToken" {
			interactive = true
		}
	}
	if !interactive {
		return false
	}
	for _, trigger := range task.Triggers {
		if !enabledByDefault(trigger.Enabled) || (trigger.UserID != "" && !sameWindowsUser(trigger.UserID, sid)) {
			continue
		}
		if !logonBoundaryActive(trigger.StartBoundary, trigger.EndBoundary, time.Now()) {
			continue
		}
		return true
	}
	return false
}

func logonBoundaryActive(start, end string, now time.Time) bool {
	parse := func(value string) (time.Time, error) {
		if value, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return value, nil
		}
		return time.ParseInLocation("2006-01-02T15:04:05", value, time.Local)
	}
	if start != "" {
		value, err := parse(start)
		if err != nil || now.Before(value) {
			return false
		}
	}
	if end != "" {
		value, err := parse(end)
		if err != nil || !now.Before(value) {
			return false
		}
	}
	return true
}

func findNativeLogonTask(tasks []taskDefinition, exePath, sid, ownPrefix string) (string, error) {
	var matches, unavailable []string
	for _, task := range tasks {
		name := strings.TrimPrefix(task.Registration.URI, `\`)
		if strings.HasPrefix(name, ownPrefix) || strings.HasPrefix(name, "WinTrayApp-") {
			continue
		}
		if !task.launches(exePath) {
			if task.logonFor(sid) {
				for _, action := range task.Actions.Exec {
					if strings.Contains(strings.ToLower(action.Arguments), strings.ToLower(exePath)) {
						return "", fmt.Errorf("task %s uses an indirect startup command; select its original launcher instead", name)
					}
				}
			}
			continue
		}
		if name == "" {
			return "", errors.New("matching scheduled task has no registered path")
		}
		if task.logonFor(sid) {
			matches = append(matches, name)
		} else if len(task.Triggers) != 0 {
			unavailable = append(unavailable, name)
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple original logon tasks match; refusing to choose or create another: %s", strings.Join(matches, ", "))
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(unavailable) != 0 {
		return "", fmt.Errorf("original logon task is disabled, outside its time boundaries, or not interactive for this user: %s", strings.Join(unavailable, ", "))
	}
	return "", nil
}

// A description fingerprint alone is not proof: Task Scheduler lets users edit
// actions/privileges without changing Description. Validate the actual fields.
func appTaskUpToDate(exported []byte, sid string, spec appTaskSpec, fingerprint string) bool {
	var task taskDefinition
	if decodeTaskXML(exported, &task) != nil || !strings.Contains(task.Registration.Description, fingerprint) || !task.logonFor(sid) {
		return false
	}
	if len(task.Actions.Exec) != 1 || len(task.Principals) != 1 || len(task.Triggers) != 1 {
		return false
	}
	action, principal, trigger := task.Actions.Exec[0], task.Principals[0], task.Triggers[0]
	level := "LeastPrivilege"
	if spec.highest {
		level = "HighestAvailable"
	}
	actualLevel := principal.RunLevel
	if actualLevel == "" {
		actualLevel = "LeastPrivilege" // Task Scheduler omits the schema default
	}
	// An undelayed trigger is exported without a Delay element.
	actualDelay, err := time.Duration(0), error(nil)
	if trigger.Delay != "" {
		actualDelay, err = time.ParseDuration(strings.ToLower(strings.TrimPrefix(trigger.Delay, "PT")))
	}
	return err == nil && sameExecutablePath(action.Command, spec.exePath) && action.Arguments == spec.args &&
		action.WorkingDirectory == spec.workingDir && actualLevel == level &&
		actualDelay == time.Duration(spec.delay)*time.Second
}
