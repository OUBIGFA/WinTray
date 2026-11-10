//go:build windows

package traybox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func junctionExecutable(t *testing.T) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return junctionPath(t, exe), exe
}

func junctionPath(t *testing.T, exe string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "linked programs")
	// Directory junctions require no elevation. Only this test's new link is
	// created; the running test image supplies a real process identity.
	if out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, filepath.Dir(exe)).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, out)
	}
	return filepath.Join(link, filepath.Base(exe))
}

func TestCanonicalIconPathMatchesExtendedDOSPath(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := canonicalIconPath(`\\?\`+exe), canonicalIconPath(exe); got != want {
		t.Fatalf("extended DOS path differs: got %q, want %q", got, want)
	}
}

func TestCanonicalIconPathResolvesDirectoryJunction(t *testing.T) {
	linked, exe := junctionExecutable(t)
	if got, want := canonicalIconPath(linked), canonicalIconPath(exe); got != want {
		t.Fatalf("junction and original identify different programs: got %q, want %q", got, want)
	}
}

func TestProcessesByPathFindsExecutableThroughJunction(t *testing.T) {
	linked, _ := junctionExecutable(t)
	selected := canonicalIconPath(linked)
	if got := processesByPath(map[string]bool{selected: true}); got[uint32(os.Getpid())] != selected {
		t.Fatalf("selected junction path did not find the already running process: %v", got)
	}
}

func TestCanonicalIconPathKeepsEmptyAndMissingPaths(t *testing.T) {
	if got := canonicalIconPath(""); got != "" {
		t.Fatalf("empty path became %q", got)
	}
	missing := filepath.Join(t.TempDir(), "missing.exe")
	if got := canonicalIconPath(missing); got != strings.ToLower(filepath.Clean(missing)) {
		t.Fatalf("missing path changed identity: %q", got)
	}
}
