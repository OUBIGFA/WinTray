//go:build windows

package app

import (
	"wintray/internal/config"
	"wintray/internal/shellapp"
)

func openProgramLocation(entry config.ManagedAppEntry, openFolder func(string) error) error {
	directory, err := shellapp.InstallationDirectory(entry.ExePath)
	if err != nil {
		return err
	}
	return openFolder(directory)
}
