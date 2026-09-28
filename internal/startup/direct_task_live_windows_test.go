//go:build windows

package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"wintray/internal/config"
)

func TestConfiguredStartupLiveRoundTrip(t *testing.T) {
	if os.Getenv("WINTRAY_STARTUP_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_STARTUP_LIVE_TEST=1 for real Task Scheduler round trips")
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
	dir := t.TempDir()
	exe := filepath.Join(dir, "便携程序 & portable.exe")
	if err := os.WriteFile(exe, data, 0700); err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("WinTrayLive-Configured-%d-%d-", os.Getpid(), time.Now().UnixNano())
	tasks := &AppTasks{userSID: user.User.Sid.String(), selfExe: self, namePrefix: name, statePath: filepath.Join(dir, "migration.json"), run: runSchtasks,
		findEntries: func(string) ([]startupEntry, error) { return nil, nil }}
	if helper := os.Getenv("WINTRAY_STARTUP_HELPER_EXE"); helper != "" {
		if !filepath.IsAbs(helper) {
			t.Fatal("WINTRAY_STARTUP_HELPER_EXE must be an absolute path")
		}
		tasks.selfExe = helper
	}
	t.Cleanup(func() {
		if _, err := runSchtasks("/Delete", "/TN", name+"1", "/F"); err != nil {
			t.Error(err)
		}
	})
	output := filepath.Join(dir, "result.json")
	want := []string{"--background", "a b", `C:\ends-with-slash\`, `--literal=%SystemRoot%`, `a"b`}
	settings := taskSettings(exe)
	settings.ManagedApps[0].Args = windows.ComposeCommandLine(append([]string{"--wintray-startup-test-report", output}, want...))
	// Task Scheduler expands environment variables in action arguments.
	// Compare against the original direct task, not shell-free argv rules;
	// introducing the ready-wait helper must preserve that native behavior.
	if err := tasks.syncOne(tasks.spec(config.LogonTaskApps(settings)[0]), false); err != nil {
		t.Fatal(err)
	}
	if err := tasks.launchNow(settings.ManagedApps[0]); err != nil {
		t.Fatal(err)
	}
	direct := waitForLiveLaunch(t, output)
	output = filepath.Join(dir, "helper-result.json")
	settings.ManagedApps[0].Args = windows.ComposeCommandLine(append([]string{"--wintray-startup-test-report", output}, want...))
	want = direct.Args
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if err := tasks.launchNow(settings.ManagedApps[0]); err != nil {
		t.Fatal(err)
	}
	got := waitForLiveLaunch(t, output)
	if !slices.Equal(got.Args, want) || !sameExecutablePath(got.Cwd, dir) || got.Elevated != IsProcessElevated() {
		t.Fatalf("configured task changed launch attributes: %+v", got)
	}
	t.Logf("real configured task preserved args, working directory and token: %+v", got)
}
