//go:build windows

package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Reproduce the settings workers querying the inventory while WinTray's own
// logon task is removed/recreated. schtasks can return partial XML plus a
// file-not-found error when deletion overlaps enumeration.
func TestTaskInventoryDuringLogonToggle(t *testing.T) {
	if os.Getenv("WINTRAY_STARTUP_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_STARTUP_LIVE_TEST=1 for concurrent Task Scheduler operations")
	}
	requireSchtasks(t)
	task, err := NewLogonTask()
	if err != nil {
		t.Fatal(err)
	}
	task.name = fmt.Sprintf("WinTrayToggleTest-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		if err := task.Remove(); err != nil {
			t.Errorf("remove test task: %v", err)
		}
	})
	registrar, _ := testRegistrar(t, task)
	exe := filepath.Join(systemDir(), "cmd.exe")
	for i := 0; i < 20; i++ {
		if taskErr, err := registrar.Apply(exe, "/c exit", true); taskErr != nil || err != nil {
			t.Fatalf("enable: task=%v registration=%v", taskErr, err)
		}
		var wg sync.WaitGroup
		var inventoryErr, removeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, inventoryErr = readTaskInventory(runSchtasks)
		}()
		go func() {
			defer wg.Done()
			_, removeErr = registrar.Apply(exe, "/c exit", false)
		}()
		wg.Wait()
		if inventoryErr != nil || removeErr != nil {
			t.Fatalf("toggle %d: inventory=%v removal=%v", i+1, inventoryErr, removeErr)
		}
	}
	// Enabling again must register a runnable task, not merely keep a true
	// setting or a stale successful-registration cache.
	dir := t.TempDir()
	target := filepath.Join(dir, "launch-report.exe")
	buildLaunchReporter(t, target)
	output := filepath.Join(dir, "launch.json")
	args := windows.ComposeCommandLine([]string{"--wintray-startup-test-report", output, "after-toggle"})
	if taskErr, err := registrar.Apply(target, args, true); taskErr != nil || err != nil {
		t.Fatalf("re-enable: task=%v registration=%v", taskErr, err)
	}
	if _, err := runSchtasks("/Run", "/TN", task.name); err != nil {
		t.Fatal(err)
	}
	report := waitForLiveLaunch(t, output)
	if len(report.Args) != 1 || report.Args[0] != "after-toggle" || !sameExecutablePath(report.Cwd, dir) {
		t.Fatalf("re-enabled task did not launch with the expected arguments and directory: %+v", report)
	}
}
