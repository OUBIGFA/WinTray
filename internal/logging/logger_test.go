package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerRotatesOversizedFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger.Info(strings.Repeat("x", maxLogSize))
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	oldInfo, err := os.Stat(filepath.Join(dir, "wintray.log.old"))
	if err != nil {
		t.Fatalf("rotated log missing: %v", err)
	}
	if oldInfo.Size() <= maxLogSize {
		t.Fatalf("rotated log size = %d, want more than %d", oldInfo.Size(), maxLogSize)
	}
	currentInfo, err := os.Stat(filepath.Join(dir, "wintray.log"))
	if err != nil {
		t.Fatalf("new log missing: %v", err)
	}
	if currentInfo.Size() >= oldInfo.Size() {
		t.Fatalf("new log size = %d, want smaller than rotated size %d", currentInfo.Size(), oldInfo.Size())
	}
}
