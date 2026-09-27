//go:build windows

package ipc

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

// MarkReady publishes that WinTray is up, as a named event that exists for as
// long as the returned release has not been called or the process lives.
// Helper processes started by program logon tasks wait for it, so no program
// WinTray starts at sign-in comes up before WinTray itself.
func MarkReady(name string) (release func(), err error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateEvent(nil, 1, 1, namePtr)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(h) }, nil
}

// WaitReady waits until MarkReady has been called for name, polling because
// the event does not exist before then. It reports false after timeout.
func WaitReady(name string, timeout, poll time.Duration) bool {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		if h, err := windows.OpenEvent(windows.SYNCHRONIZE, false, namePtr); err == nil {
			_ = windows.CloseHandle(h)
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(poll)
	}
}
