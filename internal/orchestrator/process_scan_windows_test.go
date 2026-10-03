//go:build windows

package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wintray/internal/config"
)

func TestRunningProcessStartsByPathPreservesUnicodeCaseMatching(t *testing.T) {
	entry := startupTestEntry(t)
	image, err := os.ReadFile(entry.ExePath)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Σ")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "case.exe")
	if err := os.WriteFile(exe, image, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, err := startProcess(exe, entry.Args, launchNoWindow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	// EqualFold treats sigma and final sigma as the same case-insensitive
	// directory name, although their lower-case strings differ.
	requested := filepath.Join(filepath.Dir(dir), "ς", "case.exe")
	created, ok := earliestRunningProcessStart(requested, "case")
	if !ok {
		t.Fatal("single-executable lookup did not recognize the fixture")
	}
	if got := runningProcessStartsByPath([]string{requested}); !got[strings.ToLower(requested)].Equal(created) {
		t.Fatalf("batch lookup changed case-insensitive path matching: %v", got)
	}
}

// Real isolated helpers cover same-name executables in different directories,
// multiple instances and fresh observations after an instance exits.
func TestRunningProcessStartsByPathTracksOldestInstancePerExecutable(t *testing.T) {
	first, other := startupTestEntry(t), startupTestEntry(t)
	start := func(entry config.ManagedAppEntry) (*exec.Cmd, time.Time) {
		t.Helper()
		cmd, err := startProcess(entry.ExePath, entry.Args, launchNoWindow)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		created, ok := processStartTime(uint32(cmd.Process.Pid))
		if !ok {
			t.Fatal("helper creation time unavailable")
		}
		return cmd, created
	}
	oldest, oldTime := start(first)
	_, otherTime := start(other)
	_, newTime := start(first)
	paths := []string{strings.ToUpper(first.ExePath), other.ExePath, filepath.Join(t.TempDir(), "qq.exe"), ""}
	got := runningProcessStartsByPath(paths)
	if len(got) != 2 || !got[strings.ToLower(first.ExePath)].Equal(oldTime) || !got[strings.ToLower(other.ExePath)].Equal(otherTime) {
		t.Fatalf("creation times mixed across paths or instances: %v", got)
	}
	if err := oldest.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = oldest.Wait()
	got = runningProcessStartsByPath(paths)
	if len(got) != 2 || !got[strings.ToLower(first.ExePath)].Equal(newTime) {
		t.Fatalf("exited instance retained in next scan: %v", got)
	}
}
