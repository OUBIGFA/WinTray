//go:build windows

package ipc

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestWaitReadyFollowsMarker(t *testing.T) {
	name := fmt.Sprintf("WinTrayTestReady-%d-%d", os.Getpid(), time.Now().UnixNano())
	if WaitReady(name, 100*time.Millisecond, 10*time.Millisecond) {
		t.Fatal("ready before anything was marked")
	}
	done := make(chan bool, 1)
	go func() { done <- WaitReady(name, 5*time.Second, 10*time.Millisecond) }()
	time.Sleep(100 * time.Millisecond)
	release, err := MarkReady(name)
	if err != nil {
		t.Fatal(err)
	}
	if !<-done {
		t.Fatal("waiter missed the marker published while it waited")
	}
	release()
	if WaitReady(name, 100*time.Millisecond, 10*time.Millisecond) {
		t.Fatal("marker outlived its release")
	}
}
