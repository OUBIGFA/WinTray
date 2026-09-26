//go:build windows

package startup

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`

type logonTask interface {
	Sync(exePath, args string) error
	Remove() error
}

// Registrar keeps WinTray's own run-at-logon registration in one place: a
// logon task when Task Scheduler accepts it, the Run value otherwise. Only
// one of them is left behind so WinTray is never started twice at logon.
type Registrar struct {
	appName string
	runPath string
	task    logonTask
	taskErr error

	mu      sync.Mutex
	applied string
}

func NewRegistrar(appName string) *Registrar {
	task, err := NewLogonTask()
	if err != nil {
		return &Registrar{appName: appName, runPath: runKeyPath, taskErr: err}
	}
	return &Registrar{appName: appName, runPath: runKeyPath, task: task}
}

// Apply registers exePath/args to start at logon, or removes every
// registration when enabled is false. taskErr is set when the logon task
// could not be used and the Run value was written instead. Repeating the
// last successful request does nothing.
func (r *Registrar) Apply(exePath, args string, enabled bool) (taskErr, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request := fmt.Sprintf("%t\x00%s\x00%s", enabled, exePath, args)
	if request == r.applied {
		return nil, nil
	}
	if enabled {
		taskErr, err = r.enable(exePath, args)
	} else {
		err = r.disable()
	}
	if err == nil && taskErr == nil {
		r.applied = request
	}
	return taskErr, err
}

// Remove deletes every run-at-logon registration, whatever was applied
// before, so a later Apply registers again instead of trusting its cache.
func (r *Registrar) Remove() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied = ""
	return r.disable()
}

func (r *Registrar) enable(exePath, args string) (taskErr, err error) {
	taskErr = r.taskErr
	if r.task != nil {
		taskErr = r.task.Sync(exePath, args)
	}
	if taskErr == nil {
		return nil, r.deleteRunValue()
	}
	return taskErr, r.setRunValue(fmt.Sprintf("\"%s\" %s", exePath, args))
}

func (r *Registrar) disable() error {
	var taskErr error
	if r.task != nil {
		taskErr = r.task.Remove()
	}
	return errors.Join(taskErr, r.deleteRunValue())
}

func (r *Registrar) setRunValue(command string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, r.runPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(r.appName, command)
}

func (r *Registrar) deleteRunValue() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, r.runPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	if err = key.DeleteValue(r.appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
