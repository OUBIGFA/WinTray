package logging

import (
	"errors"
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
	lock    *logFileLock
	path    string
	lastErr error
}

func New(appDir string) (*Logger, error) {
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(appDir, "wintray.log")
	lock, err := newLogFileLock(path)
	if err != nil {
		return nil, err
	}
	if err := lock.withLock(func() error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		return f.Close()
	}); err != nil {
		_ = lock.close()
		return nil, err
	}
	return &Logger{lock: lock, path: path}, nil
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lock == nil {
		return l.lastErr
	}
	err := l.lock.close()
	l.lock = nil
	return errors.Join(l.lastErr, err)
}

func (l *Logger) Info(msg string)  { l.write("INFO", msg) }
func (l *Logger) Warn(msg string)  { l.write("WARN", msg) }
func (l *Logger) Error(msg string) { l.write("ERROR", msg) }

func (l *Logger) write(level, msg string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lock == nil {
		return
	}
	if err := l.lock.withLock(func() error { return l.appendAndRotate(level, msg) }); err != nil {
		// Keep the last failure observable through Close without recursive
		// logging or an unbounded collection of errors on an unavailable disk.
		l.lastErr = err
		_, _ = fmt.Fprintf(os.Stderr, "WinTray log write failed: %v\n", err)
	}
}

// No process holds the file open between writes: detached hosts must not block
// another logger's rotation, nor deletion of the app data directory on reset.
// The named lock covers opening, writing AND rotation across all processes.
func (l *Logger) appendAndRotate(level, msg string) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintf(f, "%s [%s] %s\n", time.Now().Format(time.RFC3339), level, msg)
	info, statErr := f.Stat()
	if err := errors.Join(writeErr, statErr, f.Close()); err != nil {
		return err
	}
	if info.Size() < maxLogSize {
		return nil
	}
	oldPath := l.path + ".old"
	if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(l.path, oldPath); err != nil {
		return err
	}
	f, err = os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}
