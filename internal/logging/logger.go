package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	mu   sync.Mutex
	file *os.File
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
	return &Logger{file: f}, nil
}

func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *Logger) Info(msg string) { l.write("INFO", msg) }
func (l *Logger) Warn(msg string) { l.write("WARN", msg) }
func (l *Logger) Error(msg string) { l.write("ERROR", msg) }

func (l *Logger) write(level, msg string) {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.file, "%s [%s] %s\n", time.Now().Format(time.RFC3339), level, msg)
}
