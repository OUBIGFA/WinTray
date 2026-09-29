//go:build windows

package startup

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
	"time"
	"wintray/internal/config"
)

// Only isolated tasks and a harmless reporting executable are used. This proves
// Windows accepts demand-only definitions and restores exported native XML.
func TestFrequencyLiveRoundTrip(t *testing.T) {
	if os.Getenv("WINTRAY_STARTUP_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_STARTUP_LIVE_TEST=1 for isolated Task Scheduler checks")
	}
	requireSchtasks(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native_%t", native), func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "frequency-helper.exe")
			if err := os.WriteFile(target, data, 0o700); err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("WinTrayFrequencyTest-%d-%d", os.Getpid(), time.Now().UnixNano())
			tasks := &AppTasks{userSID: user.User.Sid.String(), selfExe: self, namePrefix: name + "-", statePath: filepath.Join(dir, "migration.json"), run: runSchtasks, findEntries: func(string) ([]startupEntry, error) { return nil, nil }}
			settings := frequencySettings(t, target)
			entry := &settings.ManagedApps[0]
			reportPath := filepath.Join(dir, "report.json")
			entry.Args = windows.ComposeCommandLine([]string{"--wintray-startup-test-report", reportPath, "--original", "a b"})
			cleanupName := tasks.namePrefix + entry.ID
			var original string
			if native {
				cleanupName = name + "-native"
				spec := tasks.spec(config.LogonTaskApp{Entry: *entry})
				spec.name = cleanupName
				spec.demandOnly = false
				if err := tasks.syncOne(spec, false); err != nil {
					t.Fatal(err)
				}
				out, err := runSchtasks("/Query", "/TN", cleanupName, "/XML")
				if err != nil {
					t.Fatal(err)
				}
				original = taskXMLText(out)
				// Native task names must not share the managed prefix.
				tasks.namePrefix = name + "-managed-"
			}
			t.Cleanup(func() {
				if _, err := runSchtasks("/Delete", "/TN", cleanupName, "/F"); err != nil {
					t.Error(err)
				}
			})
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatal(err)
			}
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatal(err)
			}
			history := config.NewStartupHistory(filepath.Join(dir, "history.json"))
			launch := func() error { return tasks.launchNow(*entry) }
			if ok, err := history.Launch(*entry, time.Now(), launch); err != nil || !ok {
				t.Fatalf("first launch: %t %v", ok, err)
			}
			report := waitForLiveLaunch(t, reportPath)
			if len(report.Args) != 2 || report.Args[0] != "--original" || report.Args[1] != "a b" || report.Elevated != IsProcessElevated() {
				t.Fatalf("launch settings changed: %+v", report)
			}
			if ok, err := history.Launch(*entry, time.Now(), func() error { t.Error("limit allowed a second task launch"); return launch() }); err != nil || ok {
				t.Fatalf("limit: %t %v", ok, err)
			}
			if native {
				if err := tasks.Sync(config.Settings{}, false); err != nil {
					t.Fatal(err)
				}
				out, err := runSchtasks("/Query", "/TN", cleanupName, "/XML")
				if err != nil || !sameTaskXML(taskXMLText(out), original) {
					t.Fatalf("native task did not restore: %v", err)
				}
			}
		})
	}
}
