//go:build windows

package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/ipc"
	"wintray/internal/orchestrator"
)

// Drive the real Launch Now callback through UI Automation. The session helper
// uses temporary settings and no-op startup registration; only our own short-
// lived console fixture can be launched, never one of the user's programs.
func TestManualLaunchOnlyShowsFailures(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for index, tc := range []struct {
		name    string
		success bool
		pending bool
	}{
		{name: "success", success: true},
		{name: "failure"},
		{name: "pending startup/success", success: true, pending: true},
		{name: "pending startup/failure", pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			success := tc.success
			dir := t.TempDir()
			sessionExe := filepath.Join(dir, "manual-session.exe")
			programExe := filepath.Join(dir, "manual-program.exe")
			release := filepath.Join(dir, "release")
			if err := os.WriteFile(sessionExe, image, 0600); err != nil {
				t.Fatal(err)
			}
			if success {
				if err := os.WriteFile(programExe, image, 0600); err != nil {
					t.Fatal(err)
				}
			}
			settings := config.DefaultSettings()
			settings.RunAtLogon = false
			settings.ExitAfterManagedAppsCompleted = false
			settings.ManagedApps = []config.ManagedAppEntry{{
				ID: "manual", Name: "Manual launch fixture", ExePath: programExe,
				Args: "-test.run=^TestSessionConsoleProcess$ -- " + syscall.EscapeArg(release),
			}}
			if tc.pending {
				scheduledExe := filepath.Join(dir, "scheduled-program.exe")
				if err := os.WriteFile(scheduledExe, image, 0600); err != nil {
					t.Fatal(err)
				}
				// Task mode keeps the scheduled startup pending regardless of
				// the desktop's sign-in age. The fixture registers no task, and
				// the test only launches the separate manual entry.
				settings.ManagedApps = append(settings.ManagedApps, config.ManagedAppEntry{
					ID: "scheduled", Name: "Scheduled launch fixture", ExePath: scheduledExe,
					RunOnStartup: true, LaunchViaLogonTask: true,
					Schedule: config.Schedule{Enabled: true, StartDelayMinutes: 10},
				})
			}
			if err := config.NewStore(filepath.Join(dir, "settings.json")).Save(settings); err != nil {
				t.Fatal(err)
			}
			manifest, err := filepath.Abs("../../build/WinTray.exe.manifest")
			if err != nil {
				t.Fatal(err)
			}
			eventName := fmt.Sprintf("WinTray_ManualLaunchTest_%d_%d_%d", os.Getpid(), index, time.Now().UnixNano())
			cmd := exec.Command(sessionExe, "-test.run=^TestMainSessionProcess$", "--", dir, manifest, eventName)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var waitErr error
			go func() { waitErr = cmd.Wait(); close(done) }()
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				<-done
				if t.Failed() {
					log, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
					t.Logf("session output:\n%s\nlog:\n%s", output.String(), log)
				}
			})
			waitSessionCondition(t, "session activation", func() bool { return ipc.TrySignalActivation(eventName) })
			var hwnd win.HWND
			waitSessionCondition(t, "settings window", func() bool { hwnd = visibleSessionWindow(uint32(cmd.Process.Pid)); return hwnd != 0 })
			if tc.pending {
				waitSessionCondition(t, "scheduled startup waiting", func() bool {
					log, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
					return strings.Contains(string(log), "external startup owns launch: Scheduled launch fixture")
				})
				if err := sessionControlAction(hwnd, settings.ManagedApps[1].Name, 10010); err != nil {
					t.Fatal(err)
				}
				waitSessionCondition(t, "scheduled program launch action", func() bool { return sessionHasButton(hwnd, "立即启动") })
			}
			if err := sessionControlAction(hwnd, settings.ManagedApps[0].Name, 10010); err != nil { // SelectionItem.Select
				t.Fatal(err)
			}
			waitSessionCondition(t, "enabled launch action", func() bool { return sessionHasButton(hwnd, "立即启动") })
			clickSessionButton(t, hwnd, "立即启动")
			if success {
				var pid uint32
				waitSessionCondition(t, "launched program", func() bool {
					data, err := os.ReadFile(release + ".pid")
					value, parseErr := strconv.ParseUint(string(data), 10, 32)
					pid = uint32(value)
					return err == nil && parseErr == nil && pid != 0
				})
				handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = windows.TerminateProcess(handle, 0)
					_, _ = windows.WaitForSingleObject(handle, 3000)
					_ = windows.CloseHandle(handle)
				})
				for attempt := 0; attempt < 2; attempt++ {
					if attempt == 1 {
						// Already-running is also a successful no-op, not an error.
						clickSessionButton(t, hwnd, "立即启动")
					}
					waitSessionCondition(t, "launch action restored", func() bool { return sessionHasButton(hwnd, "立即启动") })
					for until := time.Now().Add(600 * time.Millisecond); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
						if !win.IsWindowEnabled(hwnd) || manualLaunchDialog(uint32(cmd.Process.Pid), hwnd) != 0 {
							t.Fatal("successful launch displayed a modal dialog")
						}
					}
				}
			} else {
				var dialog win.HWND
				waitSessionCondition(t, "launch failure dialog", func() bool {
					dialog = manualLaunchDialog(uint32(cmd.Process.Pid), hwnd)
					return dialog != 0 && !win.IsWindowEnabled(hwnd)
				})
				log, err := os.ReadFile(filepath.Join(dir, "wintray.log"))
				if err != nil || !strings.Contains(string(log), "launch now failed:") {
					t.Fatalf("failure was not logged: %v\n%s", err, log)
				}
				win.PostMessage(dialog, win.WM_CLOSE, 0, 0)
				waitSessionCondition(t, "failure dialog dismissed", func() bool { return win.IsWindowEnabled(hwnd) })
				waitSessionCondition(t, "launch action restored after failure", func() bool { return sessionHasButton(hwnd, "立即启动") })
			}
			if tc.pending {
				log, err := os.ReadFile(filepath.Join(dir, "wintray.log"))
				if err != nil || strings.Contains(string(log), "managed summary:") {
					t.Fatalf("scheduled startup ended before the manual launch: %v\n%s", err, log)
				}
			}
			clickSessionButton(t, hwnd, "退出 WinTray")
			select {
			case <-done:
				if waitErr != nil {
					t.Fatalf("session exit: %v", waitErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("manual-launch session did not exit")
			}
		})
	}
}

func manualLaunchDialog(pid uint32, parent win.HWND) win.HWND {
	for _, window := range orchestrator.NewWin32WindowEnumerator().EnumerateTopLevelWindows() {
		if window.ProcessID == pid && window.IsVisible && window.OwnerHandle == uintptr(parent) {
			return win.HWND(window.Handle)
		}
	}
	return 0
}
