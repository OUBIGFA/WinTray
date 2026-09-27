//go:build windows

package startup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wintray/internal/config"
)

type liveLaunchReport struct {
	Args     []string `json:"args"`
	Cwd      string   `json:"cwd"`
	Elevated bool     `json:"elevated"`
	Show     uint16   `json:"show"`
	Flags    uint32   `json:"flags"`
}

// Test children execute the same startup helper functions as WinTray, without
// bringing up the UI or depending on an installed release. This is a fixture
// process, not a second test run, and is also the harmless target application.
func TestMain(m *testing.M) {
	args := os.Args[1:]
	if len(args) == 3 && args[0] == AppTaskHelperRun {
		if err := LaunchStartupRun(args[1], args[2]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(args) == 3 && args[0] == AppTaskHelperConfigured {
		if err := LaunchConfigured(args[1], args[2]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(args) == 3 && args[0] == AppTaskHelperShortcut {
		if err := LaunchStartupShortcut(args[1], args[2]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(args) >= 2 && args[0] == "--wintray-startup-test-report" {
		cwd, _ := os.Getwd()
		var info windows.StartupInfo
		windows.GetStartupInfo(&info)
		report := liveLaunchReport{Args: args[2:], Cwd: cwd, Elevated: IsProcessElevated(), Show: info.ShowWindow, Flags: info.Flags}
		data, err := json.Marshal(report)
		if err != nil || os.WriteFile(args[1], data, 0o600) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func waitForLiveLaunch(t *testing.T, path string) liveLaunchReport {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var report liveLaunchReport
		data, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(data, &report) == nil {
			return report
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("startup fixture produced no report: %s", path)
	return liveLaunchReport{}
}

// Opt-in because this test registers temporary tasks and, for the Run case, a
// uniquely named real HKCU Run value. It never changes an existing app's entry.
// Every task/value/file is removed on exit, including failure paths.
func TestOriginalStartupLiveRoundTrip(t *testing.T) {
	if os.Getenv("WINTRAY_STARTUP_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_STARTUP_LIVE_TEST=1 for real Task Scheduler / ShellExecute round trips")
	}
	requireSchtasks(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := os.Getenv("WINTRAY_STARTUP_HELPER_EXE")
	if helper == "" {
		helper = self
	} else if !filepath.IsAbs(helper) {
		t.Fatal("WINTRAY_STARTUP_HELPER_EXE must be an absolute path")
	} else if _, err := os.Stat(helper); err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"Run", "Shortcut"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "原程序 & original.exe")
			if err := os.WriteFile(target, image, 0o700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "启动 report.json")
			wantArgs := []string{"--startup", "参数 with spaces", `C:\ends-with-slash\`, `--literal=%SystemRoot%`, `a"b`}
			args := windows.ComposeCommandLine(append([]string{"--wintray-startup-test-report", output}, wantArgs...))
			name := fmt.Sprintf("WinTrayLive-%s-%d-%d", kind, os.Getpid(), time.Now().UnixNano())
			tasks := &AppTasks{
				userSID: user.User.Sid.String(), selfExe: helper, namePrefix: name + "-",
				statePath: filepath.Join(dir, "startup-migrations.json"), run: runSchtasks, writeApproval: writeUserApproval,
				Log: func(message string) { t.Log(message) },
			}
			var originalCommand, linkPath string
			var run registry.Key
			var approvalPath string
			wantCwd := systemDir()
			wantShow := uint16(1)
			if kind == "Run" {
				run, _, err = registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE|registry.WOW64_64KEY)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { run.Close() })
				if _, _, err := run.GetStringValue(name); err != registry.ErrNotExist {
					t.Fatalf("test value already exists or cannot be read: %v", err)
				}
				originalCommand = windows.ComposeCommandLine([]string{target}) + " " + args
				if err := run.SetStringValue(name, originalCommand); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = run.DeleteValue(name) })
				approvalPath = startupApprovedPath + `\Run`
				tasks.findEntries = func(path string) ([]startupEntry, error) {
					return findRunStartupEntries(path, []runSource{{registry.CURRENT_USER, runKeyPath, registry.WOW64_64KEY, approvalPath, "Live HKCU Run"}})
				}
			} else {
				linkPath = filepath.Join(dir, name+".lnk")
				wantCwd = filepath.Join(dir, "Original profile")
				if err := os.Mkdir(wantCwd, 0o700); err != nil {
					t.Fatal(err)
				}
				wantShow = 7
				createTestStartupShortcut(t, linkPath, shortcutLaunch{path: target, arguments: args, directory: wantCwd, show: 7})
				approvalPath = startupApprovedPath + `\StartupFolder`
				tasks.findEntries = func(path string) ([]startupEntry, error) { return findFolderStartupEntries(path, dir, false) }
			}
			approvalName := name
			if kind == "Shortcut" {
				approvalName += ".lnk"
			}
			before, err := readApproval(registry.CURRENT_USER, approvalPath, approvalName)
			if err != nil || before.Exists {
				t.Fatalf("test approval already exists: %+v %v", before, err)
			}
			t.Cleanup(func() {
				if err := tasks.Sync(config.Settings{}, false); err != nil {
					t.Errorf("cleanup original startup: %v", err)
				}
				_, _ = runSchtasks("/Delete", "/TN", tasks.namePrefix+"1", "/F")
				// This name was verified absent above and belongs only to this test.
				if key, err := registry.OpenKey(registry.CURRENT_USER, approvalPath, registry.SET_VALUE|registry.WOW64_64KEY); err == nil {
					_ = key.DeleteValue(approvalName)
					key.Close()
				}
			})
			if kind == "Shortcut" {
				// The Windows Shell itself expands environment variables in
				// shortcut arguments. Compare against opening the original .lnk,
				// not against an assumption that .lnk behaves like a REG_SZ value.
				if err := shellLaunchOriginal(linkPath, "", "", 7); err != nil {
					t.Fatal(err)
				}
				original := waitForLiveLaunch(t, output)
				if original.Show != wantShow || !sameExecutablePath(original.Cwd, wantCwd) {
					t.Fatalf("invalid original shortcut fixture: %+v", original)
				}
				wantArgs = original.Args
				if err := os.Remove(output); err != nil {
					t.Fatal(err)
				}
			}
			settings := taskSettings(target)
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatal(err)
			}
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatalf("idempotent sync: %v", err)
			}
			if _, err := runSchtasks("/Run", "/TN", tasks.namePrefix+"1"); err != nil {
				t.Fatal(err)
			}
			report := waitForLiveLaunch(t, output)
			if strings.Join(report.Args, "\x00") != strings.Join(wantArgs, "\x00") || !sameExecutablePath(report.Cwd, wantCwd) || report.Show != wantShow || (!IsProcessElevated() && report.Elevated) {
				t.Fatalf("real task launch changed attributes: report=%+v wantArgs=%q wantCwd=%s wantShow=%d", report, wantArgs, wantCwd, wantShow)
			}
			t.Logf("actual child: args=%q cwd=%s elevated=%t show=%d flags=0x%x", report.Args, report.Cwd, report.Elevated, report.Show, report.Flags)
			if kind == "Run" {
				got, typ, err := run.GetStringValue(name)
				if err != nil || got != originalCommand || typ != registry.SZ {
					t.Fatalf("original Run data changed: %s %d %v", got, typ, err)
				}
			} else if _, err := readStartupShortcut(linkPath); err != nil {
				t.Fatal(err)
			}
			if err := tasks.Sync(config.Settings{}, false); err != nil {
				t.Fatal(err)
			}
			after, err := readApproval(registry.CURRENT_USER, approvalPath, approvalName)
			if err != nil || !sameApproval(before, after) {
				t.Fatalf("original approval not restored: %+v %v", after, err)
			}
		})
	}
}
