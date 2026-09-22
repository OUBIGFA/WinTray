//go:build windows

package app

import (
	"context"
	"sync"
	"testing"

	"wintray/internal/tray"
)

func TestPendingHostedRecoveryKeepsOnlyUnacceptedWindows(t *testing.T) {
	var pending pendingHostedWindows
	accepted := tray.HostedWindow{ProcessID: 1, Handle: 11}
	unaccepted := tray.HostedWindow{ProcessID: 2, Handle: 22}
	pending.remember(accepted)
	pending.remember(unaccepted)
	pending.forget(accepted)
	var restored []tray.HostedWindow
	pending.restoreAll(func(w tray.HostedWindow) { restored = append(restored, w) })
	if len(restored) != 1 || restored[0] != unaccepted {
		t.Fatalf("restored = %+v, want only %+v", restored, unaccepted)
	}
	pending.restoreAll(func(w tray.HostedWindow) { t.Fatalf("restored twice: %+v", w) })
}

func TestPendingHostedRecoveryAfterLaunchCompletesDuringShutdown(t *testing.T) {
	var pending pendingHostedWindows
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Add(1)
	window := tray.HostedWindow{ProcessID: 42, Handle: 1234}
	go func() {
		defer workers.Done()
		<-ctx.Done()
		// The hidden result arrives after the UI loop has stopped: no queued
		// host.Add callback will run to accept it.
		pending.remember(window)
	}()
	cancel()
	workers.Wait()
	var restored []tray.HostedWindow
	pending.restoreAll(func(w tray.HostedWindow) { restored = append(restored, w) })
	if len(restored) != 1 || restored[0] != window {
		t.Fatalf("late hidden result was not recovered: %+v", restored)
	}
}
