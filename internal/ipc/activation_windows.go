//go:build windows

package ipc

import (
	"sync/atomic"

	"golang.org/x/sys/windows"
)

type ActivationListener struct {
	event   windows.Handle
	stop    chan struct{}
	done    chan struct{}
	started uint32
}

func NewActivationListener(name string) (*ActivationListener, error) {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateEvent(nil, 0, 0, namePtr)
	if err != nil {
		return nil, err
	}
	return &ActivationListener{
		event: h,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}, nil
}

func (l *ActivationListener) Start(onActivated func()) {
	if l == nil || onActivated == nil {
		return
	}
	if !atomic.CompareAndSwapUint32(&l.started, 0, 1) {
		return
	}
	go l.listen(onActivated)
}

func (l *ActivationListener) Close() {
	if l == nil {
		return
	}
	if atomic.LoadUint32(&l.started) == 1 {
		close(l.stop)
		<-l.done
	}
	if l.event != 0 {
		_ = windows.CloseHandle(l.event)
		l.event = 0
	}
}

func (l *ActivationListener) listen(onActivated func()) {
	defer close(l.done)
	for {
		select {
		case <-l.stop:
			return
		default:
		}

		wait, err := windows.WaitForSingleObject(l.event, 250)
		if err != nil {
			continue
		}

		switch wait {
		case windows.WAIT_OBJECT_0:
			onActivated()
		case uint32(windows.WAIT_TIMEOUT):
			continue
		default:
			continue
		}
	}
}

func TrySignalActivation(name string) bool {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, namePtr)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	return windows.SetEvent(h) == nil
}
