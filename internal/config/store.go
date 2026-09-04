package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() Settings {
	settings, _ := s.LoadWithError()
	return settings
}

// LoadWithError distinguishes a missing settings file from an unreadable or
// malformed one. Callers can then avoid replacing a user's file silently.
func (s *Store) LoadWithError() (Settings, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), err
	}
	var settings Settings
	if err = json.Unmarshal(data, &settings); err != nil {
		_ = backupInvalidSettingsFile(s.path, data)
		return DefaultSettings(), fmt.Errorf("invalid settings: %w", err)
	}
	return migrate(settings), nil
}

func backupInvalidSettingsFile(path string, data []byte) error {
	stamp := time.Now().Format("20060102-150405.000000000")
	backupPath := fmt.Sprintf("%s.invalid-%s.bak", path, stamp)
	return os.WriteFile(backupPath, data, 0o644)
}

func (s *Store) Save(settings Settings) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func migrate(settings Settings) Settings {
	if settings.SchemaVersion <= 0 {
		settings.SchemaVersion = 1
	}
	// v2 enforces RunOnStartup during autorun; preserve legacy startup behavior.
	if settings.SchemaVersion < 2 {
		for i := range settings.ManagedApps {
			settings.ManagedApps[i].RunOnStartup = true
		}
		settings.SchemaVersion = 2
	}
	// v3 restores legacy default behavior for EXE entries: when an entry is meant
	// to launch on startup and not as hidden background task, default to closing
	// its foreground window unless the user explicitly configured otherwise in
	// newer schema versions.
	if settings.SchemaVersion < 3 {
		for i := range settings.ManagedApps {
			if settings.ManagedApps[i].LaunchHiddenInBackground {
				continue
			}
			if !settings.ManagedApps[i].RunOnStartup {
				continue
			}
			if settings.ManagedApps[i].TrayBehavior.AutoMinimizeAndHideOnLaunch {
				continue
			}
			if strings.EqualFold(filepath.Ext(settings.ManagedApps[i].ExePath), ".exe") {
				settings.ManagedApps[i].TrayBehavior.AutoMinimizeAndHideOnLaunch = true
			}
		}
		settings.SchemaVersion = 3
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
		if settings.ManagedApps[i].LaunchHiddenInBackground {
			settings.ManagedApps[i].TrayBehavior.AutoMinimizeAndHideOnLaunch = false
		}
	}
	return settings
}
