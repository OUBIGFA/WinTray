package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func TryMigrateFromWinTray(targetPath string) error {
	legacyDir, err := legacyAppDir()
	if err != nil {
		return nil
	}
	legacy := filepath.Join(legacyDir, "settings.json")
	if _, err := os.Stat(legacy); err != nil {
		return nil
	}
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	}

	data, err := os.ReadFile(legacy)
	if err != nil {
		return err
	}
	var settings Settings
	if err = json.Unmarshal(data, &settings); err != nil {
		return errors.New("legacy settings format invalid")
	}
	settings = migrate(settings)
	if err = os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	if err = os.WriteFile(targetPath+".bak.from.wintray", data, 0o644); err != nil {
		return err
	}
	newData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, newData, 0o644)
}

// legacyAppDir uses the same resolution logic as appDataBaseDir to find
// the legacy "WinTray" directory.
func legacyAppDir() (string, error) {
	base, err := appDataBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "WinTray"), nil
}
