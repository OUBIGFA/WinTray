//go:build windows

package orchestrator

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"wintray/internal/config"
)

type runLimitTestLogger struct {
	warnings []string
}

func (l *runLimitTestLogger) Info(string)         {}
func (l *runLimitTestLogger) Warn(message string) { l.warnings = append(l.warnings, message) }
func (l *runLimitTestLogger) Error(string)        {}

func TestEndOverdueProgramsSkipsAmbiguousDuplicateExecutable(t *testing.T) {
	first := startupTestEntry(t)
	first.Schedule = config.Schedule{Enabled: true, AutoExitMinutes: 30}
	cmd, err := startProcess(first.ExePath, first.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	duplicate := first
	duplicate.Name = "second"
	duplicate.Args = "--second"
	logger := &runLimitTestLogger{}
	svc := NewService(&testEnumerator{}, &testManager{}, logger)
	check := svc.EndOverduePrograms([]config.ManagedAppEntry{first, duplicate}, time.Now())
	if check.Running != 1 || check.Next.IsZero() || time.Until(check.Next) < 20*time.Second {
		t.Fatalf("ambiguous duplicate run limit = %+v, want pending check", check)
	}
	if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "ambiguous duplicate") {
		t.Fatalf("warnings = %q, want one ambiguity warning", logger.warnings)
	}
}

func TestEndOverdueProgramsIgnoresDuplicatesWithoutRunLimits(t *testing.T) {
	for _, schedule := range []config.Schedule{
		{Enabled: false, AutoExitMinutes: 30},
		{Enabled: true, AutoExitMinutes: 0},
	} {
		t.Run(fmt.Sprintf("schedule_enabled_%t", schedule.Enabled), func(t *testing.T) {
			first := config.ManagedAppEntry{ExePath: `C:\Apps\tool.exe`, Args: "--first", Schedule: schedule}
			second := first
			second.Args = "--second"
			logger := &runLimitTestLogger{}
			svc := NewService(&testEnumerator{}, &testManager{}, logger)
			check := svc.EndOverduePrograms([]config.ManagedAppEntry{first, second}, time.Now())
			if check.Running != 0 || !check.Next.IsZero() || len(logger.warnings) != 0 {
				t.Fatalf("unlimited duplicates keep WinTray alive: check=%+v warnings=%q", check, logger.warnings)
			}
		})
	}
}

// A partial termination must not be reported as completion: residency uses
// Running to decide whether WinTray may exit and stop enforcing run limits.
func TestEndOverdueProgramsKeepsSurvivingInstancesPending(t *testing.T) {
	for _, killCount := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("terminate_%d_of_2", killCount), func(t *testing.T) {
			entry := startupTestEntry(t)
			entry.Schedule = config.Schedule{Enabled: true, AutoExitMinutes: 1}
			commands := make(map[uint32]*exec.Cmd)
			for range 2 {
				cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
				if err != nil {
					t.Fatal(err)
				}
				commands[uint32(cmd.Process.Pid)] = cmd
				t.Cleanup(func() {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				})
			}
			svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
			svc.terminateProcesses = func(pids []uint32) int {
				if len(pids) != 2 {
					t.Fatalf("termination roots = %v, want both isolated helper instances", pids)
				}
				// Simulate one inaccessible instance without changing Windows ACLs.
				ended := terminateProcessTrees(pids[:killCount])
				for _, pid := range pids[:killCount] {
					_ = commands[pid].Wait()
				}
				if ended < killCount {
					t.Fatalf("terminated %d processes, want at least %d helper roots", ended, killCount)
				}
				return ended
			}
			now := time.Now().Add(2 * time.Minute)
			check := svc.EndOverduePrograms([]config.ManagedAppEntry{entry}, now)
			if got := len(startupTestPIDs(entry.ExePath)); got != 2-killCount {
				t.Fatalf("survivors = %d, want %d", got, 2-killCount)
			}
			if killCount < 2 {
				if check.Running != 1 || !check.Next.Equal(now) {
					t.Fatalf("surviving program lost its retry: %+v", check)
				}
			} else if check.Running != 0 || !check.Next.IsZero() {
				t.Fatalf("fully terminated program still pending: %+v", check)
			}
		})
	}
}

func TestUniqueRunLimitEntriesSkipDifferentLimits(t *testing.T) {
	first := config.ManagedAppEntry{Name: "long", ExePath: `C:\Apps\tool.exe`, Args: "--same", Schedule: config.Schedule{Enabled: true, AutoExitMinutes: 30}}
	second := first
	second.Name = "short"
	second.Schedule.AutoExitMinutes = 5
	if got := uniqueRunLimitEntries([]config.ManagedAppEntry{first, second}); len(got) != 0 {
		t.Fatalf("unique run limits = %+v, want ambiguous executable skipped", got)
	}
}

func TestUniqueRunLimitEntriesSkipNeverExitDuplicate(t *testing.T) {
	first := config.ManagedAppEntry{ExePath: `C:\Apps\tool.exe`, Schedule: config.Schedule{Enabled: true, AutoExitMinutes: 30}}
	second := first
	second.Schedule.AutoExitMinutes = 0
	if got := uniqueRunLimitEntries([]config.ManagedAppEntry{first, second}); len(got) != 0 {
		t.Fatalf("run limits = %+v, want no termination when another entry never exits", got)
	}
}

func TestUniqueRunLimitEntriesDeduplicateExactDuplicate(t *testing.T) {
	first := config.ManagedAppEntry{Name: "first", ExePath: `C:\Apps\tool.exe`, Args: "--same", Schedule: config.Schedule{Enabled: true, AutoExitMinutes: 5}}
	duplicate := first
	duplicate.Name = "second"
	got := uniqueRunLimitEntries([]config.ManagedAppEntry{first, duplicate})
	if len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("unique run limits = %+v, want first exact duplicate", got)
	}
}
