//go:build windows

package ipc

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

type SingleInstance struct {
	handle windows.Handle
}

func Acquire(name string) (*SingleInstance, bool, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}
	h, err := windows.CreateMutex(nil, false, namePtr)
	alreadyRunning := errors.Is(err, syscall.Errno(windows.ERROR_ALREADY_EXISTS))
	if err != nil && !alreadyRunning {
		return nil, false, err
	}
	return &SingleInstance{handle: h}, alreadyRunning, nil
}

func (s *SingleInstance) Close() {
	if s == nil || s.handle == 0 {
		return
	}
	_ = windows.CloseHandle(s.handle)
}
