//go:build windows

package orchestrator

import (
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
	first := config.ManagedAppEntry{Name: "first", ExePath: `C:\Apps\tool.exe`, Args: "--first", Schedule: config.Schedule{Enabled: true, AutoExitMinutes: 30}}
	duplicate := first
	duplicate.Name = "second"
	duplicate.Args = "--second"
	logger := &runLimitTestLogger{}
	svc := NewService(&testEnumerator{}, &testManager{}, logger)
	original := runningProcessStartLookup
	runningProcessStartLookup = func(string, string) (time.Time, bool) { return time.Now().Add(-time.Hour), true }
	defer func() { runningProcessStartLookup = original }()

	check := svc.EndOverduePrograms([]config.ManagedAppEntry{first, duplicate}, time.Now())
	if check.Running != 1 || check.Next.IsZero() || time.Until(check.Next) < 20*time.Second {
		t.Fatalf("ambiguous duplicate run limit = %+v, want pending check", check)
	}
	if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "ambiguous duplicate") {
		t.Fatalf("warnings = %q, want one ambiguity warning", logger.warnings)
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
