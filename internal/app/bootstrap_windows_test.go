//go:build windows

package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wintray/internal/config"
	"wintray/internal/logging"
	"wintray/internal/startup"
)

// Only the elevated helper's own command lines may start a task operation;
// every other argument list belongs to a normal launch.
func TestRunAppTaskHelperRecognizesOnlyItsOwnFlags(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--autorun"},
		{"--background", "--autorun"},
		{startup.AppTaskHelperRegister},
		{startup.AppTaskHelperRegister, "name"},
		{startup.AppTaskHelperRegister, "name", "path", "extra"},
		{startup.AppTaskHelperDelete},
		{startup.AppTaskHelperDelete, "name", "extra"},
		{startup.AppTaskHelperRun},
		{startup.AppTaskHelperRun, "name"},
		{startup.AppTaskHelperRun, "name", "exe", "extra"},
		{startup.AppTaskHelperShortcut},
		{startup.AppTaskHelperShortcut, "link"},
		{startup.AppTaskHelperShortcut, "link", "exe", "extra"},
		{startup.AppTaskHelperConfigured, "args"},
		{startup.AppTaskHelperConfigured, "args", "exe", "extra"},
	} {
		if code := runAppTaskHelper(args); code != -1 {
			t.Errorf("runAppTaskHelper(%q) = %d, want -1 for a non-helper launch", args, code)
		}
	}
}

func TestStartupHelpersConsumeFailuresWithoutOpeningAnotherUI(t *testing.T) {
	data := t.TempDir()
	t.Setenv("LOCALAPPDATA", data)
	// No WinTray publishes its ready state here: the helpers give up waiting
	// for it, note that, and still try their launch.
	restore := readyWait
	readyWait = 50 * time.Millisecond
	t.Cleanup(func() { readyWait = restore })
	for _, args := range [][]string{
		{startup.AppTaskHelperRun, "WinTrayTest-Missing-Original-Entry", `C:\Missing\app.exe`},
		{startup.AppTaskHelperShortcut, filepath.Join(t.TempDir(), "missing.lnk"), `C:\Missing\app.exe`},
		{startup.AppTaskHelperConfigured, "--flag", `relative\app.exe`},
	} {
		if got := runAppTaskHelper(args); got != 1 {
			t.Fatalf("helper failure = %d, want a consumed failure (no normal launch)", got)
		}
	}
	logs, err := os.ReadFile(filepath.Join(data, "WinTray", "wintray.log"))
	if err != nil || strings.Count(string(logs), "WinTray was not running") != 3 {
		t.Fatalf("the wait for WinTray must end in a logged timeout: %v\n%s", err, logs)
	}
}

func TestProgramTaskSyncErrorsReachTheRequestingUI(t *testing.T) {
	logger, err := logging.New(t.TempDir())
	if err != nil { t.Fatal(err) }
	defer logger.Close()
	var reported error
	ensureAppTasks(nil, appTaskSyncRequest{onError: func(err error) { reported = err }}, logger)
	if reported == nil { t.Fatal("unavailable task service was silently accepted") }
}

func TestTaskSyncRequestSnapshotsTheEditableProgramList(t *testing.T) {
	request := appTaskSyncRequest{settings: config.Settings{ManagedApps: []config.ManagedAppEntry{
		{ID: "1", Name: "Original", ExePath: `C:\Apps\app.exe`, LaunchViaLogonTask: true},
	}}}
	queued := request.snapshot()
	request.settings.ManagedApps[0].Name = "Edited during sync"
	request.settings.ManagedApps[0].LaunchViaLogonTask = false
	if queued.settings.ManagedApps[0].Name != "Original" || !queued.settings.ManagedApps[0].LaunchViaLogonTask {
		t.Fatal("UI edits changed an already queued task migration")
	}
}

func TestCleanupKeepsRecoveryDataUntilOriginalStartupRestored(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "WinTray")
	if err := os.Mkdir(dir, 0o700); err != nil { t.Fatal(err) }
	path := filepath.Join(dir, "startup-migrations.json")
	if err := os.WriteFile(path, []byte("recovery data"), 0o600); err != nil { t.Fatal(err) }
	restoreErr := errors.New("task deletion denied")
	if err := cleanupAppDataAfterRestore(dir, func() error { return restoreErr }); !errors.Is(err, restoreErr) {
		t.Fatalf("restore failure was hidden: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "recovery data" {
		t.Fatalf("failed restoration erased the backup: %q %v", data, err)
	}
	if err := cleanupAppDataAfterRestore(dir, func() error {
		if _, err := os.Stat(path); err != nil { t.Fatal("cleanup ran before restoration") }
		return nil
	}); err != nil { t.Fatal(err) }
	if _, err := os.Stat(dir); !os.IsNotExist(err) { t.Fatalf("restored data directory was not cleaned: %v", err) }
}
