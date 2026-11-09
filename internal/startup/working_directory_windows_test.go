//go:build windows

package startup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Launch the same helper functions used by a task, on the test's desktop.
// No system task or real startup entry is registered.
func TestConfiguredStartupInheritsTaskWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "report.exe")
	buildLaunchReporter(t, target)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "hidden"} {
		t.Run(mode, func(t *testing.T) {
			directory := filepath.Join(root, "工作 data "+mode)
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, mode+".json")
			args := windows.ComposeCommandLine([]string{"--wintray-startup-test-report", output})
			cmd := exec.Command(self, "-test.run=^TestConfiguredDirectoryHelper$", "--", mode, target, args)
			cmd.Dir = directory
			if data, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("helper: %v\n%s", err, data)
			}
			report := waitForLiveLaunch(t, output)
			if !strings.EqualFold(report.Cwd, directory) {
				t.Fatalf("task child directory = %q, want %q", report.Cwd, directory)
			}
		})
	}
}

func TestConfiguredDirectoryHelper(t *testing.T) {
	if len(os.Args) != 6 || os.Args[2] != "--" {
		return
	}
	var err error
	if os.Args[3] == "hidden" {
		err = LaunchHiddenStartup(AppTaskHelperConfigured, os.Args[5], os.Args[4])
	} else {
		err = LaunchConfigured(os.Args[5], os.Args[4])
	}
	if err != nil {
		t.Fatal(err)
	}
}
