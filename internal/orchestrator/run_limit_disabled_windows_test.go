//go:build windows

package orchestrator

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"wintray/internal/config"
)

// Use real, isolated processes with the same image name in different paths.
// Advancing the check time exercises expired limits without a 30-minute wait.
func TestEndOverdueProgramsCancelsChangedRunLimit(t *testing.T) {
	for _, runOnStartup := range []bool{true, false} {
		for _, change := range []string{"disable schedule", "never exit", "remove entry"} {
			t.Run(fmt.Sprintf("startup_%t/%s", runOnStartup, change), func(t *testing.T) {
				entry := startupTestEntry(t)
				entry.RunOnStartup = runOnStartup
				entry.Schedule = config.Schedule{
					Enabled: true, StartDelayMinutes: 10, AutoExitMinutes: 30,
					FrequencyEnabled: true, FrequencyDays: 1, FrequencyRuns: 1,
				}
				other := startupTestEntry(t)
				other.Schedule = entry.Schedule
				commands := make(map[uint32]*startedProcess)
				for _, app := range []config.ManagedAppEntry{entry, other} {
					cmd, err := startProcess(app.ExePath, app.Args, launchNoWindow)
					if err != nil {
						t.Fatal(err)
					}
					commands[uint32(cmd.Process.Pid)] = cmd
					t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
				}
				newService := func() *Service {
					svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
					svc.terminateProcesses = func(pids []uint32) int {
						for _, pid := range pids {
							if commands[pid] == nil {
								t.Fatalf("termination targeted a process outside the fixture: %d", pid)
							}
						}
						ended := terminateProcessTrees(pids)
						for _, pid := range pids {
							_ = commands[pid].Wait()
						}
						return ended
					}
					return svc
				}
				svc := newService()
				now := time.Now()
				settings := config.DefaultSettings()
				settings.ManagedApps = []config.ManagedAppEntry{entry, other}
				if check := svc.EndOverduePrograms(settings.ManagedApps, now); check.Running != 2 || check.Next.IsZero() {
					t.Fatalf("enabled limits were not pending: %+v", check)
				}
				switch change {
				case "disable schedule":
					// Retained numeric values and the frequency checkbox must not
					// override the schedule's master switch.
					settings.ManagedApps[0].Schedule.Enabled = false
				case "never exit":
					settings.ManagedApps[0].Schedule.AutoExitMinutes = 0
				case "remove entry":
					settings.ManagedApps = settings.ManagedApps[1:]
				}
				store := config.NewStore(filepath.Join(t.TempDir(), "settings.json"))
				if err := store.Save(settings); err != nil {
					t.Fatal(err)
				}
				reloaded, err := store.LoadWithError()
				if err != nil {
					t.Fatal(err)
				}
				for _, stage := range []struct {
					name     string
					service  *Service
					settings config.Settings
				}{
					{"same session", svc, settings},
					{"after restart", newService(), reloaded},
				} {
					check := stage.service.EndOverduePrograms(stage.settings.ManagedApps, now.Add(31*time.Minute))
					if check.Running != 0 || !check.Next.IsZero() {
						t.Fatalf("%s retained a cancelled limit: %+v", stage.name, check)
					}
					if pids := startupTestPIDs(entry.ExePath); len(pids) != 1 {
						t.Fatalf("%s ended the unlimited program: pids=%v", stage.name, pids)
					}
					if pids := startupTestPIDs(other.ExePath); len(pids) != 0 {
						t.Fatalf("%s stopped enforcing the other program's limit: pids=%v", stage.name, pids)
					}
				}
			})
		}
	}
}
