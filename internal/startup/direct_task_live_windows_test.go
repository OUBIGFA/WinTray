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
	t.Cleanup(func() {
		if _, err := runSchtasks("/Delete", "/TN", name+"1", "/F"); err != nil {
			t.Error(err)
		}
	})
	output := filepath.Join(dir, "result.json")
	want := []string{"--background", "a b", `C:\ends-with-slash\`, `--literal=%SystemRoot%`, `a"b`}
	settings := taskSettings(exe)
	settings.ManagedApps[0].Args = windows.ComposeCommandLine(append([]string{"--wintray-startup-test-report", output}, want...))
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
