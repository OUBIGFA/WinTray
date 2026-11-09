//go:build windows

package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wintray/internal/config"
)

func TestConfiguredProcessWorkingDirectory(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []launchMode{launchNoWindow, launchVisible, launchHiddenConsole} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("mode=%d/custom=%t", mode, custom), func(t *testing.T) {
				output := filepath.Join(t.TempDir(), "directory.txt")
				entry := config.ManagedAppEntry{ExePath: self, Args: runnerHelperArgs("directory", output)}
				want := filepath.Dir(self)
				if custom {
					want = filepath.Join(t.TempDir(), "工作 data")
					if err := os.Mkdir(want, 0700); err != nil {
						t.Fatal(err)
					}
					entry.WorkingDirectory = want
				}
				process, err := startConfiguredProcess(entry, mode)
				if err != nil {
					t.Fatal(err)
				}
				waitRunnerCommand(t, process)
				got, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.EqualFold(string(got), want) {
					t.Fatalf("child directory = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestConfiguredScriptReadsRelativeFiles(t *testing.T) {
	for _, ext := range []string{".cmd", ".bat", ".ps1"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "工作 data")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "input.txt"), []byte("from custom directory"), 0600); err != nil {
				t.Fatal(err)
			}
			body := "@echo off\r\ntype input.txt > result.txt\r\n"
			if ext == ".ps1" {
				body = "[IO.File]::WriteAllText((Join-Path (Get-Location) 'result.txt'), (Get-Content -Raw 'input.txt'))"
			}
			script := filepath.Join(root, "read relative"+ext)
			if err := os.WriteFile(script, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			process, err := startConfiguredProcess(config.ManagedAppEntry{ExePath: script, WorkingDirectory: directory}, launchNoWindow)
			if err != nil {
				t.Fatal(err)
			}
			waitRunnerCommand(t, process)
			got, err := os.ReadFile(filepath.Join(directory, "result.txt"))
			if err != nil || string(got) != "from custom directory" {
				t.Fatalf("relative file read = %q, err=%v", got, err)
			}
		})
	}
}

func TestConfiguredProcessRejectsInvalidDirectory(t *testing.T) {
	self, _ := os.Executable()
	root := t.TempDir()
	file := filepath.Join(root, "not-a-folder")
	if err := os.WriteFile(file, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{filepath.Join(root, "missing"), file} {
		process, err := startConfiguredProcess(config.ManagedAppEntry{ExePath: self, WorkingDirectory: directory}, launchNoWindow)
		if process != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
			t.Fatal("invalid directory launched a process")
		}
		if err == nil {
			t.Fatalf("invalid directory accepted: %s", directory)
		}
	}
}
