//go:build windows

package orchestrator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartProcess_PreservesWindowsArguments(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "arguments.json")
	want := []string{"", "plain", "two words", `C:\My Path\`, `say "hello"`, `a\"b`, "中文", ""}
	cmd, err := startProcess(self, runnerHelperArgs("args", output, want...), launchNoWindow)
	if err != nil {
		t.Fatalf("start helper: %v", err)
	}
	waitRunnerCommand(t, cmd)
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("child arguments = %q, want %q", got, want)
	}
}

func TestStartProcess_CmdScriptWithSpacesAndQuotedArguments(t *testing.T) {
	for _, ext := range []string{".cmd", ".bat"} {
		for _, tc := range []struct {
			name string
			args string
			want string
		}{
			{name: "no arguments", want: "[]\r\n[]\r\n[]\r\n"},
			{name: "plain arguments", args: "one two three", want: "[one]\r\n[two]\r\n[three]\r\n"},
			{name: "quoted and empty arguments", args: `"first argument" "" "last argument"`, want: "[first argument]\r\n[]\r\n[last argument]\r\n"},
		} {
			t.Run(ext+"/"+tc.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "scripts with spaces")
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				script := filepath.Join(dir, "echo arguments"+ext)
				body := "@echo off\r\n> \"%~dp0arguments.txt\" (\r\necho [%~1]\r\necho [%~2]\r\necho [%~3]\r\n)\r\nexit /b 0\r\n"
				if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				cmd, err := startProcess(script, tc.args, launchNoWindow)
				if err != nil {
					t.Fatalf("start script: %v", err)
				}
				waitRunnerCommand(t, cmd)
				got, err := os.ReadFile(filepath.Join(dir, "arguments.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.want {
					t.Fatalf("script output = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

func TestStartProcess_RejectsMissingAndInvalidExecutable(t *testing.T) {
	// Isolate any erroneous shell fallback: where.exe exits without launching
	// anything, even if the old runner invokes it as "cmd.exe /c start ...".
	// This makes the regression safe to run before the fallback is removed.
	dir := t.TempDir()
	image, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "where.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd.exe"), image, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	for _, name := range []string{"missing.exe", "invalid.exe"} {
		t.Run(name, func(t *testing.T) {
			exe := filepath.Join(dir, name)
			if name == "invalid.exe" {
				if err := os.WriteFile(exe, []byte("not a PE executable"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd, err := startProcess(exe, "", launchNoWindow)
			if cmd != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Error("failed executable launch returned a command (shell fallback)")
			}
			if err == nil {
				t.Fatal("failed executable launch reported success")
			}
		})
	}
}

// TestRunnerHelperProcess runs only in a child with the explicit helper marker.
func TestRunnerHelperProcess(t *testing.T) {
	if len(os.Args) < 6 || os.Args[2] != "--" || os.Args[3] != "wintray-runner-helper" {
		return
	}
	switch os.Args[4] {
	case "args":
		data, err := json.Marshal(os.Args[6:])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Args[5], data, 0o600); err != nil {
			t.Fatal(err)
		}
	case "wait":
		// The parent opens a process handle before releasing this short-lived
		// helper, so it can always Wait and clean up even on assertion failure.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Args[5]); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("parent did not release console helper")
	default:
		t.Fatalf("unknown helper mode %q", os.Args[4])
	}
}

func runnerHelperArgs(mode, output string, args ...string) string {
	all := append([]string{"-test.run=^TestRunnerHelperProcess$", "--", "wintray-runner-helper", mode, output}, args...)
	for i := range all {
		all[i] = syscall.EscapeArg(all[i])
	}
	return strings.Join(all, " ")
}

func waitRunnerCommand(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("child did not exit within 10 seconds")
	}
}
