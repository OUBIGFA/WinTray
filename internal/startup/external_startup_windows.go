//go:build windows

package startup

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// NewExternalStartupLookup observes original startup owners without modifying
// them. One autorun batch shares a task snapshot so programs without a Run or
// Startup-folder entry do not each invoke Task Scheduler during sign-in.
func NewExternalStartupLookup() func(string) (string, error) {
	var once sync.Once
	var tasks []taskDefinition
	var sid string
	var taskErr error
	readTasks := func() ([]taskDefinition, string, error) {
		once.Do(func() {
			user, err := windows.GetCurrentProcessToken().GetTokenUser()
			if err != nil {
				taskErr = err
				return
			}
			sid = user.User.Sid.String()
			tasks, taskErr = readTaskInventory(runSchtasks)
		})
		return tasks, sid, taskErr
	}
	return func(exePath string) (string, error) {
		return findEnabledStartupEntry(exePath, findStartupEntries, readTasks)
	}
}

func findEnabledStartupEntry(exePath string, readEntries func(string) ([]startupEntry, error), readTasks func() ([]taskDefinition, string, error)) (string, error) {
	entries, err := readEntries(exePath)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.enabled {
			return entry.label, nil
		}
	}
	tasks, sid, err := readTasks()
	if err != nil {
		return "", err
	}
	for _, task := range tasks {
		name := strings.TrimPrefix(task.Registration.URI, "\\")
		if strings.HasPrefix(name, "WinTrayApp-") || !task.logonFor(sid) {
			continue
		}
		if task.launches(exePath) {
			return "Task Scheduler\\" + name, nil
		}
		// A wrapper mentioning the target is not safe evidence that WinTray
		// should launch another instance. Reuse the migration check's rule.
		for _, action := range task.Actions.Exec {
			if strings.Contains(strings.ToLower(action.Arguments), strings.ToLower(exePath)) {
				return "", fmt.Errorf("task %s uses an indirect startup command", name)
			}
		}
	}
	return "", nil
}
