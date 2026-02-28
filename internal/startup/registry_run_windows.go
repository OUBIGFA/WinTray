//go:build windows

package startup

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`

type Registrar struct{}

func NewRegistrar() *Registrar { return &Registrar{} }

func (r *Registrar) SetEnabled(appName, command string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if enabled {
		return key.SetStringValue(appName, command)
	}
	if err = key.DeleteValue(appName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
