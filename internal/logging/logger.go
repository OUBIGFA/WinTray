package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxLogSize is the maximum log file size before rotation (5 MB).
const maxLogSize = 5 * 1024 * 1024

type Logger struct {
	mu      sync.Mutex
	file    *os.File
	appDir  string
	written int64
}

func New(appDir string) (*Logger, error) {
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(appDir, "wintray.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat()
	var size int64
	if info != nil {
		size = info.Size()
	}
	return &Logger{file: f, appDir: appDir, written: size}, nil
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *Logger) Info(msg string)  { l.write("INFO", msg) }
func (l *Logger) Warn(msg string)  { l.write("WARN", msg) }
func (l *Logger) Error(msg string) { l.write("ERROR", msg) }

func (l *Logger) write(level, msg string) {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n, _ := fmt.Fprintf(l.file, "%s [%s] %s\n", time.Now().Format(time.RFC3339), level, msg)
	l.written += int64(n)
	if l.written >= maxLogSize {
		l.rotateUnlocked()
	}
}

// rotateUnlocked renames the current log to wintray.log.old and opens a new file.
// Must be called with l.mu held.
func (l *Logger) rotateUnlocked() {
	_ = l.file.Close()
	logPath := filepath.Join(l.appDir, "wintray.log")
	oldPath := filepath.Join(l.appDir, "wintray.log.old")
	_ = os.Remove(oldPath)
	_ = os.Rename(logPath, oldPath)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		l.file = nil
		return
	}
	l.file = f
	l.written = 0
}
