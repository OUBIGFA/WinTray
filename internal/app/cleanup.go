package app

import (
	"fmt"

	"wintray/internal/config"
)

// resetSettings must restore boxed icons before discarding their paths.
// A failed restore leaves the saved list available for another attempt.
func resetSettings(store *config.Store, release func() error) error {
	if err := release(); err != nil {
		return fmt.Errorf("restore boxed tray icons: %w", err)
	}
	return store.Save(config.DefaultSettings())
}
