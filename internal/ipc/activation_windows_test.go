//go:build windows

package ipc

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestActivationListenerAcceptsExistingEvent(t *testing.T) {
	name := fmt.Sprintf("WinTray_TestActivation_%d_%d", os.Getpid(), time.Now().UnixNano())
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := windows.CreateEvent(nil, 0, 0, ptr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(previous)
	listener, err := NewActivationListener(name)
	if err != nil {
		t.Fatalf("listen on still-open event: %v", err)
	}
	defer listener.Close()
	activated := make(chan struct{}, 1)
	listener.Start(func() { activated <- struct{}{} })
	if !TrySignalActivation(name) {
		t.Fatal("could not signal listener")
	}
	select {
	case <-activated:
	case <-time.After(2 * time.Second):
		t.Fatal("existing event did not activate new listener")
	}
	listener.Close()
	listener.Close()
}
