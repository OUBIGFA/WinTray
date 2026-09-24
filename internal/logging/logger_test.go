package logging

import (
	"fmt"
	"os"
	"os/exec"
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

func TestLoggersShareRotationWithoutHoldingFilesOpen(t *testing.T) {
	dir := t.TempDir()
	first, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	first.Info(strings.Repeat("x", maxLogSize))
	second.Info("written after another logger rotated")
	first.Info("first logger also follows the current file")
	current, err := os.ReadFile(filepath.Join(dir, "wintray.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(current) >= maxLogSize || !strings.Contains(string(current), "written after") || !strings.Contains(string(current), "first logger") {
		t.Fatalf("loggers did not follow rotation: current size %d", len(current))
	}
	old, err := os.Stat(filepath.Join(dir, "wintray.log.old"))
	if err != nil || old.Size() < maxLogSize {
		t.Fatalf("shared log did not rotate: info=%v err=%v", old, err)
	}
	// A live detached host's logger must not hold the data directory hostage.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("live loggers blocked cleanup: %v", err)
	}
}

func TestLoggerConcurrentProcesses(t *testing.T) {
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		cmd := exec.Command(self, "-test.run=^TestLoggerProcessHelper$")
		cmd.Env = append(os.Environ(), "WINTRAY_LOG_HELPER_DIR="+dir, fmt.Sprintf("WINTRAY_LOG_HELPER_ID=%d", i))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		commands[i] = cmd
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("log helper failed: %v", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "wintray.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 200 {
		t.Fatalf("concurrent log lines = %d, want 200", len(lines))
	}
	for writer := range commands {
		for line := 0; line < 100; line++ {
			marker := fmt.Sprintf("[INFO] writer-%d-line-%d\n", writer, line)
			if strings.Count(string(data), marker) != 1 {
				t.Fatalf("missing/duplicated concurrent log line %q", marker)
			}
		}
	}
}

func TestLoggerProcessHelper(t *testing.T) {
	dir := os.Getenv("WINTRAY_LOG_HELPER_DIR")
	if dir == "" {
		return
	}
	logger, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		logger.Info(fmt.Sprintf("writer-%s-line-%d", os.Getenv("WINTRAY_LOG_HELPER_ID"), i))
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
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
