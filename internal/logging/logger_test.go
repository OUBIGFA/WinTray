package logging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoggerCloseIsIdempotentAndDoesNotReopen(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("before close")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "wintray.log"))
	if err != nil {
		t.Fatal(err)
	}
	logger.Info(strings.Repeat("x", maxLogSize))
	if err := logger.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "wintray.log"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("logging after Close changed the file: %v", err)
	}
}

func TestLoggerConcurrentRotationAndClose(t *testing.T) {
	logger, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 8; j++ {
				logger.Info(strings.Repeat("x", maxLogSize/4))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if err := logger.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	close(start)
	wg.Wait()
}

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
