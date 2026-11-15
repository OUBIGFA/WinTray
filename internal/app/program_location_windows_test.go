//go:build windows

package app

import (
	"errors"
	"path/filepath"
	"testing"

	"wintray/internal/config"
)

func TestOpenProgramLocationUsesInstallFolderNotWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	entry := config.ManagedAppEntry{ExePath: filepath.Join(directory, "example.exe"), WorkingDirectory: t.TempDir(), Args: "--different-folder"}
	calls := 0
	if err := openProgramLocation(entry, func(path string) error {
		calls++
		if path != directory {
			t.Fatalf("opened %q, want program directory %q", path, directory)
		}
		return nil
	}); err != nil || calls != 1 {
		t.Fatalf("folder open calls=%d err=%v", calls, err)
	}
}

func TestOpenProgramLocationPreservesFailures(t *testing.T) {
	shellError := errors.New("file manager unavailable")
	entry := config.ManagedAppEntry{ExePath: filepath.Join(t.TempDir(), "example.exe")}
	if err := openProgramLocation(entry, func(string) error { return shellError }); !errors.Is(err, shellError) {
		t.Fatalf("file manager error was hidden: %v", err)
	}
	entry.ExePath = ""
	if err := openProgramLocation(entry, func(string) error {
		t.Fatal("invalid program path opened an unrelated folder")
		return nil
	}); err == nil {
		t.Fatal("invalid program path did not return an error")
	}
}
