//go:build windows

package startup

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRunHelperPreservesInheritedDirectoryAndOriginalArguments(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	// The harmless report fixture has no console or persistent process.
	pe := int(binary.LittleEndian.Uint32(image[0x3c:]))
	binary.LittleEndian.PutUint16(image[pe+24+68:], 2)
	dir := filepath.Join(t.TempDir(), "Legacy app with spaces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "legacy.exe")
	if err := os.WriteFile(target, image, 0700); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(dir, "report.json")
	wantArgs := []string{"/onboot", "--profile=original profile"}
	// Match the logon task's working directory, deliberately different from
	// the executable folder. Relative arguments must keep their meaning.
	t.Chdir(systemDir())
	command := windows.ComposeCommandLine(append([]string{target, "--wintray-startup-test-report", reportPath}, wantArgs...))
	// A private registry fixture is NOT a Windows startup key. Neither the
	// user's autorun values nor real scheduled tasks are changed.
	source, run, _ := testRunSource(t)
	if err := run.SetStringValue("Legacy", command); err != nil {
		t.Fatal(err)
	}
	if err := launchStartupRunFrom(source.path, "Legacy", target, shellLaunchOriginal); err != nil {
		t.Fatal(err)
	}
	report := waitForLiveLaunch(t, reportPath)
	if !sameExecutablePath(report.Cwd, systemDir()) || !slices.Equal(report.Args, wantArgs) || report.Elevated != IsProcessElevated() {
		t.Fatalf("legacy launch context=%+v, want cwd=%s args=%q and original token", report, systemDir(), wantArgs)
	}
	stored, _, err := run.GetStringValue("Legacy")
	if err != nil || stored != command {
		t.Fatal("original Run command was rewritten")
	}
}
