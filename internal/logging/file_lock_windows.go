//go:build windows

package logging

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/windows"
)

type logFileLock struct {
	handle windows.Handle
}

func newLogFileLock(path string) (*logFileLock, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absolute))))
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`Local\WinTray_Log_%x`, digest))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	return &logFileLock{handle: handle}, nil
}

func (l *logFileLock) withLock(fn func() error) error {
	// Windows mutexes are owned by an OS thread, not a Go goroutine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	status, err := windows.WaitForSingleObject(l.handle, 5000)
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED {
		return fmt.Errorf("waiting for log mutex: status %d", status)
	}
	defer windows.ReleaseMutex(l.handle)
	return fn()
}

func (l *logFileLock) close() error {
	return windows.CloseHandle(l.handle)
}
