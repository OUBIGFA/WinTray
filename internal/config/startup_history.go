package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StartupHistory is separate from editable settings so UI saves cannot erase
// launch records. Only the single-instance main process uses this store.
type StartupHistory struct{ path string }

var historyLocks sync.Map

func NewStartupHistory(path string) *StartupHistory { return &StartupHistory{path: path} }

type startupHistoryData struct {
	Version int                    `json:"version"`
	Apps    map[string][]time.Time `json:"apps"`
}

// Launch reserves a slot durably before invoking launch. Definite launch
// failures refund it; a crash after reservation retains it to avoid exceeding
// the limit. Future timestamps count too, so clock rollback cannot reset it.
func (h *StartupHistory) Launch(entry ManagedAppEntry, now time.Time, launch func() error) (bool, error) {
	if !StartupFrequencyEnabled(entry) {
		return true, launch()
	}
	if h == nil || h.path == "" {
		return false, errors.New("startup history path is unavailable")
	}
	lock, _ := historyLocks.LoadOrStore(strings.ToLower(filepath.Clean(h.path)), &sync.Mutex{})
	mu := lock.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	state := startupHistoryData{Version: 1, Apps: map[string][]time.Time{}}
	data, err := os.ReadFile(h.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return false, fmt.Errorf("invalid startup history: %w", err)
		}
		if state.Version != 1 || state.Apps == nil {
			return false, errors.New("unsupported startup history")
		}
	}
	key := strings.ToLower(filepath.Clean(entry.ExePath))
	cutoff := now.Add(-time.Duration(ClampFrequencyDays(entry.Schedule.FrequencyDays)) * 24 * time.Hour)
	retained := make([]time.Time, 0, len(state.Apps[key])+1)
	count := 0
	for _, stamp := range state.Apps[key] {
		if stamp.IsZero() {
			return false, errors.New("invalid startup timestamp")
		}
		if stamp.After(now.Add(-MaxFrequencyDays * 24 * time.Hour)) {
			retained = append(retained, stamp)
		}
		if stamp.After(cutoff) {
			count++
		}
	}
	if count >= ClampFrequencyRuns(entry.Schedule.FrequencyRuns) {
		return false, nil
	}
	state.Apps[key] = append(retained, now)
	if err := h.save(state); err != nil {
		return false, err
	}
	if err := launch(); err != nil {
		state.Apps[key] = retained
		return false, errors.Join(err, h.save(state))
	}
	return true, nil
}

func (h *StartupHistory) save(state startupHistoryData) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".startup-history-*.tmp")
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
	return os.Rename(file.Name(), h.path)
}
