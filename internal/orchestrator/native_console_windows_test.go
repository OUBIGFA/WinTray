//go:build windows

package orchestrator

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"wintray/internal/config"
)

type nativeConsoleReport struct {
	InputConsole, OutputConsole, ErrorConsole bool
	Text, Directory, Environment              string
}

// The reporter runs inside the real child, so tests never attach WinTray's
// process to a console or redirect the target's streams to inspect output.
func runNativeConsoleProbe(t *testing.T, output string) {
	fmt.Fprintln(os.Stdout, "native stdout before show")
	fmt.Fprintln(os.Stderr, "native stderr before show")
	report := func(suffix string) {
		t.Helper()
		var value nativeConsoleReport
		for _, item := range []struct {
			id  uint32
			dst *bool
		}{
			{windows.STD_INPUT_HANDLE, &value.InputConsole},
			{windows.STD_OUTPUT_HANDLE, &value.OutputConsole},
			{windows.STD_ERROR_HANDLE, &value.ErrorConsole},
		} {
			handle, err := windows.GetStdHandle(item.id)
			var mode uint32
			*item.dst = err == nil && windows.GetConsoleMode(handle, &mode) == nil
		}
		value.Directory, _ = os.Getwd()
		value.Environment = os.Getenv("WINTRAY_NATIVE_PROBE")
		value.Text = nativeConsoleText(t)
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output+suffix, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	report(".ready")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "native input: "+strings.TrimSpace(line))
	report(".input")
	for until := time.Now().Add(30 * time.Second); time.Now().Before(until); time.Sleep(25 * time.Millisecond) {
		if _, err := os.Stat(output + ".hidden"); err == nil {
			fmt.Fprintln(os.Stdout, "native stdout after hide")
			report(".done")
			break
		}
	}
	for until := time.Now().Add(30 * time.Second); time.Now().Before(until); time.Sleep(25 * time.Millisecond) {
		if _, err := os.Stat(output); err == nil {
			return
		}
	}
	t.Fatal("native console probe was not released")
}

func nativeConsoleText(t *testing.T) string {
	t.Helper()
	handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if err != nil {
		t.Fatal(err)
	}
	text := make([]uint16, 8192)
	var read uint32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleOutputCharacterW")
	if ok, _, err := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), 0, uintptr(unsafe.Pointer(&read))); ok == 0 {
		t.Fatalf("read native console: %v", err)
	}
	return windows.UTF16ToString(text[:read])
}

func waitNativeReport(t *testing.T, path string) nativeConsoleReport {
	t.Helper()
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); time.Sleep(25 * time.Millisecond) {
		data, err := os.ReadFile(path)
		var report nativeConsoleReport
		if err == nil && json.Unmarshal(data, &report) == nil {
			return report
		}
	}
	t.Fatalf("native console report not received: %s", path)
	return nativeConsoleReport{}
}

func TestNativeConsolePreservesOutputInputAndSameWindow(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WINTRAY_NATIVE_PROBE", "inherited environment")
	for _, kind := range []string{"exe", "cmd", "bat", "ps1", "py"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "py" {
				if _, err := exec.LookPath("python.exe"); err != nil {
					t.Skip("python.exe is not installed")
				}
			}
			dir := filepath.Join(t.TempDir(), "native scripts with spaces")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "probe")
			// Never match the test runner itself when the service scans by path.
			helper := filepath.Join(dir, "native-console.exe")
			if err := os.WriteFile(helper, image, 0600); err != nil {
				t.Fatal(err)
			}
			path, args := helper, runnerHelperArgs("console", output)
			if kind != "exe" {
				path = filepath.Join(dir, "native probe."+kind)
				body := fmt.Sprintf("@echo off\r\n\"%s\" %s\r\n", helper, args)
				args = ""
				if kind == "ps1" {
					body = fmt.Sprintf("& '%s' '-test.run=^TestRunnerHelperProcess$' '--' 'wintray-runner-helper' 'console' '%s'\r\n", strings.ReplaceAll(helper, "'", "''"), strings.ReplaceAll(output, "'", "''"))
				}
				if kind == "py" {
					argv, err := json.Marshal([]string{helper, "-test.run=^TestRunnerHelperProcess$", "--", "wintray-runner-helper", "console", output})
					if err != nil {
						t.Fatal(err)
					}
					body = "import subprocess\nraise SystemExit(subprocess.call(" + string(argv) + "))\n"
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			entry := config.ManagedAppEntry{ID: kind, Name: kind, ExePath: path, Args: args, RunOnStartup: true, LaunchHiddenInBackground: true}
			svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
			result := svc.StartNow(context.Background(), entry, 0)
			if result.Hidden == nil || !result.Managed {
				t.Fatalf("background console launch: %+v", result)
			}
			pid := result.Hidden.ProcessID
			process, err := os.FindProcess(int(pid))
			if err != nil {
				t.Fatal(err)
			}
			finished := false
			t.Cleanup(func() {
				if !finished {
					terminateProcessTrees([]uint32{pid})
					_, _ = process.Wait()
				}
			})
			hwnd := result.Hidden.Handle
			if hwnd == 0 || isWindowVisible(hwnd) {
				t.Fatalf("native console did not start hidden: 0x%X", hwnd)
			}
			before := waitNativeReport(t, output+".ready")
			if !before.InputConsole || !before.OutputConsole || !before.ErrorConsole || !strings.Contains(before.Text, "native stdout before show") || !strings.Contains(before.Text, "native stderr before show") {
				t.Fatalf("native streams/output lost: %+v", before)
			}
			if before.Environment != "inherited environment" || !strings.EqualFold(before.Directory, filepath.Dir(path)) {
				t.Fatalf("native launch changed directory/environment: %+v", before)
			}
			// A fresh service represents reopening WinTray: scripts must resolve
			// by interpreter + path, without relying on a launch-time PID cache.
			svc = NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
			shown, err := svc.SetProgramWindowVisible(context.Background(), entry, true)
			if err != nil && !errors.Is(err, ErrWindowActivation) {
				t.Fatal(err)
			}
			if shown.Handle != hwnd || shown.ProcessID != pid || !shown.IsVisible() {
				t.Fatalf("show replaced native process/window: %+v", shown)
			}
			for _, char := range "native input works\r" {
				if ok, _, err := procPostMessageW.Call(hwnd, 0x0102, uintptr(char), 0); ok == 0 {
					t.Fatalf("type into native console: %v", err)
				}
			}
			input := waitNativeReport(t, output+".input")
			if !strings.Contains(input.Text, "native input: native input works") {
				t.Fatalf("native input not received: %q", input.Text)
			}
			if _, err := svc.SetProgramWindowVisible(context.Background(), entry, false); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(output+".hidden", nil, 0600); err != nil {
				t.Fatal(err)
			}
			after := waitNativeReport(t, output+".done")
			if isWindowVisible(hwnd) || !strings.Contains(after.Text, "native stdout before show") || !strings.Contains(after.Text, "native stdout after hide") {
				t.Fatalf("rehide lost output: %+v", after)
			}
			// Launch Now must not create another instance after hiding a script.
			adopted := svc.StartNow(context.Background(), entry, 0)
			if adopted.Code != ResultAlreadyRunningSkipped || adopted.Hidden == nil || adopted.Hidden.ProcessID != pid {
				t.Fatalf("background script was duplicated: %+v", adopted)
			}
			// Let each interpreter reap its child before deleting the fixture
			// executable; TerminateProcess alone is asynchronous on Windows.
			if err := os.WriteFile(output, nil, 0600); err != nil {
				t.Fatal(err)
			}
			state, err := process.Wait()
			finished = true
			if err != nil || !state.Success() {
				t.Fatalf("native console fixture exit: %v %v", state, err)
			}
		})
	}
}
