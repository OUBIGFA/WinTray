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
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/i18n"
	"wintray/internal/ipc"
	"wintray/internal/orchestrator"
)

// Exercise the actual UI action, taskbar and shutdown recovery using only an
// isolated session with no startup registrar and a disposable console program.
func TestMainSessionNativeWindowActions(t *testing.T) {
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
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			dir := t.TempDir()
			sessionExe, programExe := filepath.Join(dir, "native-window-session.exe"), filepath.Join(dir, "native-window-program.exe")
			for _, path := range []string{sessionExe, programExe} {
				if err := os.WriteFile(path, image, 0600); err != nil {
					t.Fatal(err)
				}
			}
			release := filepath.Join(dir, "release")
			settings := config.DefaultSettings()
			settings.RunAtLogon, settings.ExitAfterManagedAppsCompleted = false, false
			settings.Language = language
			name := "控制台示例"
			if language == "en-US" {
				name = "Console example"
			}
			settings.ManagedApps = []config.ManagedAppEntry{{ID: "native", Name: name, ExePath: programExe, RunOnStartup: true, LaunchHiddenInBackground: true, Args: "-test.run=^TestWindowActionConsoleProcess$ -- " + syscall.EscapeArg(release)}}
			settingsPath := filepath.Join(dir, "settings.json")
			if err := config.NewStore(settingsPath).Save(settings); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := filepath.Abs("../../build/WinTray.exe.manifest")
			if err != nil {
				t.Fatal(err)
			}
			event := fmt.Sprintf("WinTray_NativeWindow_%d_%d", os.Getpid(), time.Now().UnixNano())
			cmd := exec.Command(sessionExe, "-test.run=^TestMainSessionProcess$", "--", dir, manifest, event)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			finished := false
			t.Cleanup(func() {
				if !finished {
					_ = cmd.Process.Kill()
					<-done
				}
				if t.Failed() {
					logs, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
					t.Logf("session:\n%s\n%s", output.String(), logs)
				}
			})
			waitSessionCondition(t, "native session activation", func() bool { return ipc.TrySignalActivation(event) })
			var hwnd win.HWND
			waitSessionCondition(t, "native session window", func() bool { hwnd = visibleSessionWindow(uint32(cmd.Process.Pid)); return hwnd != 0 })
			if err := sessionControlAction(hwnd, name, 10010); err != nil {
				t.Fatal(err)
			}
			m := i18n.For(language)
			// This fixture starts with the session. Its live action becomes
			// Stop Program, so a second Launch Now is neither needed nor stable.
			var pid uint32
			waitSessionCondition(t, "native console process", func() bool {
				data, err := os.ReadFile(release + ".pid")
				value, parseErr := strconv.ParseUint(string(data), 10, 32)
				pid = uint32(value)
				return err == nil && parseErr == nil && pid != 0
			})
			process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = windows.TerminateProcess(process, 0)
				_, _ = windows.WaitForSingleObject(process, 3000)
				_ = windows.CloseHandle(process)
			})
			var console win.HWND
			waitSessionCondition(t, "original hidden console", func() bool {
				console = win.HWND(orchestrator.FindConsoleWindow(pid))
				return console != 0 && !win.IsWindowVisible(console)
			})
			waitSessionCondition(t, "show window action", func() bool {
				return sessionHasButton(hwnd, m.HostedShowWindow) && sessionHasButton(hwnd, m.ManagedStopProgram)
			})
			assertNativeTaskbar(t, false)
			captureWindowActionSession(t, uint32(cmd.Process.Pid), "native-window-"+language)
			win.SetForegroundWindow(hwnd)
			clickSessionButton(t, hwnd, m.HostedShowWindow)
			waitSessionCondition(t, "native console foreground", func() bool {
				return win.IsWindowVisible(console) && !win.IsIconic(console) && win.GetForegroundWindow() == console
			})
			waitSessionCondition(t, "hide window action", func() bool { return sessionHasButton(hwnd, m.HostedHideWindow) })
			if manualLaunchDialog(uint32(cmd.Process.Pid), hwnd) != 0 {
				t.Fatal("successful native show displayed a dialog")
			}
			if language == "zh-CN" {
				captureWindowActionSession(t, pid, "native-console-output")
			}
			assertNativeTaskbar(t, true)
			clickSessionButton(t, hwnd, m.HostedHideWindow)
			waitSessionCondition(t, "native console hidden again", func() bool { return !win.IsWindowVisible(console) && sessionHasButton(hwnd, m.HostedShowWindow) })
			assertNativeTaskbar(t, false)
			after, err := os.ReadFile(settingsPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("window action changed saved startup settings: %v", err)
			}
			if language == "en-US" {
				// Removing the only per-program control must first restore the
				// background window, without ending its process.
				if err := sessionControlAction(hwnd, m.ManagedColumnDelete+" "+name, 10005); err != nil { // ExpandCollapse.Expand
					t.Fatal(err)
				}
				var menu win.HWND
				waitSessionCondition(t, "delete confirmation menu", func() bool {
					menu = sessionPopupMenu(uint32(cmd.Process.Pid))
					return menu != 0
				})
				if manualLaunchDialog(uint32(cmd.Process.Pid), hwnd) != 0 {
					t.Fatal("delete opened a modal confirmation instead of a menu")
				}
				if unchanged, err := os.ReadFile(settingsPath); err != nil || !bytes.Equal(before, unchanged) {
					t.Fatalf("opening delete menu changed settings: %v", err)
				}
				clickSessionButton(t, menu, m.RemoveConfirmLabel)
				waitSessionCondition(t, "removed program window restored", func() bool { return win.IsWindowVisible(console) })
			}
			clickSessionButton(t, hwnd, m.ExitApp)
			select {
			case err := <-done:
				finished = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("session did not exit")
			}
			waitSessionCondition(t, "native window restored on exit", func() bool {
				return win.IsWindowVisible(console) && orchestrator.FindConsoleWindow(pid) == uintptr(console)
			})
			if status, err := windows.WaitForSingleObject(process, 0); err != nil || status != uint32(windows.WAIT_TIMEOUT) {
				t.Fatalf("exiting WinTray ended the program: %d %v", status, err)
			}
			assertNativeTaskbar(t, true)
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func sessionPopupMenu(pid uint32) win.HWND {
	for _, w := range orchestrator.NewWin32WindowEnumerator().EnumerateTopLevelWindows() {
		if w.ProcessID == pid && w.IsVisible && w.ClassName == "#32768" {
			return win.HWND(w.Handle)
		}
	}
	return 0
}

func assertNativeTaskbar(t *testing.T, present bool) {
	t.Helper()
	for until := time.Now().Add(8 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "../orchestrator/testdata/taskbar_buttons.ps1", "-ProcessName", "native-window-program.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("inspect native taskbar: %v\n%s", err, output)
		}
		count, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err == nil && (count > 0) == present {
			return
		}
	}
	t.Fatalf("native console taskbar presence did not become %t", present)
}

func captureWindowActionSession(t *testing.T, pid uint32, name string) {
	t.Helper()
	tool, dir := os.Getenv("WINTRAY_WINDOW_SHOT"), os.Getenv("WINTRAY_UI_SCREENSHOTS")
	if tool == "" || dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(tool, strconv.FormatUint(uint64(pid), 10), filepath.Join(dir, name+".png")).CombinedOutput()
	if err != nil {
		t.Fatalf("capture native window: %v\n%s", err, output)
	}
	t.Logf("%s screenshot: %s", name, output)
}

func TestWindowActionConsoleProcess(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	text := windows.StringToUTF16Ptr("WinTray Native Window Test")
	if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleTitleW").Call(uintptr(unsafe.Pointer(text))); ok == 0 {
		t.Fatalf("set console title: %v", err)
	}
	fmt.Fprintln(os.Stdout, "Native console output: started in the background")
	fmt.Fprintln(os.Stderr, "Native stderr: output remains in this console")
	if err := os.WriteFile(os.Args[3]+".pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(90 * time.Second); time.Now().Before(until); time.Sleep(25 * time.Millisecond) {
		if _, err := os.Stat(os.Args[3]); err == nil {
			return
		}
	}
	t.Fatal("native window fixture was not released")
}
