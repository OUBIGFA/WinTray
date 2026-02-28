package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() Settings {
	if _, err := os.Stat(s.path); err != nil {
		return DefaultSettings()
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return DefaultSettings()
	}
	var settings Settings
	if err = json.Unmarshal(data, &settings); err != nil {
		_ = backupInvalidSettingsFile(s.path, data)
		return DefaultSettings()
	}
	return migrate(settings)
}

func backupInvalidSettingsFile(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	stamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.invalid-%s.bak", path, stamp)
	return os.WriteFile(backupPath, data, 0o644)
}

func (s *Store) Save(settings Settings) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func migrate(settings Settings) Settings {
	if settings.SchemaVersion <= 0 {
		settings.SchemaVersion = 1
	}
	if settings.CloseWindowRetrySeconds < 0 {
		settings.CloseWindowRetrySeconds = 0
	}
	if settings.CloseWindowRetrySeconds > 120 {
		settings.CloseWindowRetrySeconds = 120
	}
	if settings.Language != "zh-CN" && settings.Language != "en-US" {
		settings.Language = "zh-CN"
	}
	if settings.ManagedApps == nil {
		settings.ManagedApps = make([]ManagedAppEntry, 0)
	}
	for i := range settings.ManagedApps {
		if settings.ManagedApps[i].Name == "" {
			settings.ManagedApps[i].Name = "New App"
		}
		if settings.ManagedApps[i].WindowMatch.Strategy == "" {
			settings.ManagedApps[i].WindowMatch.Strategy = MatchProcessNameThenTitle
		}
		if settings.ManagedApps[i].LaunchHiddenInBackground {
			settings.ManagedApps[i].TrayBehavior.AutoMinimizeAndHideOnLaunch = false
		}
	}
	return settings
}
