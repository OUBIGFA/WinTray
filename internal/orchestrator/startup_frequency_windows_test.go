//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"wintray/internal/config"
)

func TestFrequencyBlocksAutomaticButNotManualLaunch(t *testing.T) {
	entry := startupTestEntry(t)
	entry.ID = "daily"
	if err := json.Unmarshal([]byte(`{"enabled":true,"frequencyEnabled":true,"frequencyDays":1,"frequencyRuns":1}`), &entry.Schedule); err != nil {
		t.Fatal(err)
	}
	history := config.NewStartupHistory(filepath.Join(t.TempDir(), "history.json"))
	if ok, err := history.Launch(entry, time.Now(), func() error { return nil }); err != nil || !ok {
		t.Fatal(err)
	}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	svc.startupHistory = history
	svc.logonTaskLaunch = func(config.ManagedAppEntry) error { t.Error("exhausted limit launched a task"); return nil }
	got := svc.StartAndManage(context.Background(), entry, 0)
	if got.Code != ResultFrequencyLimit || len(startupTestPIDs(entry.ExePath)) != 0 {
		t.Fatalf("limit failed: %+v", got)
	}
	got = svc.StartNow(context.Background(), entry, 0)
	if got.Code != ResultStartedOnly || len(startupTestPIDs(entry.ExePath)) != 1 {
		t.Fatalf("manual launch blocked: %+v", got)
	}
}

func TestFrequencyCountsSuccessfulAutomaticTaskLaunch(t *testing.T) {
	entry := startupTestEntry(t)
	entry.Schedule = config.Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	svc.startupHistory = config.NewStartupHistory(filepath.Join(t.TempDir(), "history.json"))
	var command *exec.Cmd
	svc.logonTaskLaunch = func(e config.ManagedAppEntry) error {
		var err error
		command, err = startProcess(e.ExePath, e.Args, launchNoWindow)
		return err
	}
	got := svc.StartAndManage(context.Background(), entry, 0)
	if got.Code != ResultStartedOnly || command == nil {
		t.Fatalf("automatic launch: %+v", got)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	got = svc.StartAndManage(context.Background(), entry, 0)
	if got.Code != ResultFrequencyLimit || len(startupTestPIDs(entry.ExePath)) != 0 {
		t.Fatalf("completed run not counted: %+v", got)
	}
}

func TestFrequencyLaunchAtBootDoesNotWaitForStaggering(t *testing.T) {
	entry := startupTestEntry(t)
	entry.LaunchViaLogonTask = true
	entry.Schedule = config.Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	svc.startupHistory = config.NewStartupHistory(filepath.Join(t.TempDir(), "history.json"))
	_, err := svc.startupHistory.Launch(entry, time.Now(), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	turn := &startupTurn{sequence: &startupSequence{nextLaunch: time.Now().Add(time.Minute), gate: make(chan struct{}, 1)}, done: make(chan struct{})}
	opts := managedStartOptions(entry)
	opts.turn = turn
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := svc.start(ctx, entry, 0, opts)
	if got.Code != ResultFrequencyLimit {
		t.Fatalf("boot entry waited for the ordinary queue: %+v", got)
	}
}

func TestFrequencyHistoryErrorPreventsProcessCreation(t *testing.T) {
	entry := startupTestEntry(t)
	entry.Schedule = config.Schedule{Enabled: true, FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1}
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	svc.startupHistory = config.NewStartupHistory(path)
	svc.logonTaskLaunch = func(config.ManagedAppEntry) error { t.Error("corrupt history allowed a launch"); return nil }
	got := svc.StartAndManage(context.Background(), entry, 0)
	if got.Code != ResultFrequencyCheckFailed || len(startupTestPIDs(entry.ExePath)) != 0 {
		t.Fatalf("history failure was ignored: %+v", got)
	}
}
