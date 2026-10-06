//go:build windows

package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Reuse x/sys's signed fixture to exercise the actual Windows trust provider
// without installing certificates or including a binary in this repository.
// A caller may also supply an installed app to check its real certificate.
func TestSignedLauncherWindowIdentity(t *testing.T) {
	source := os.Getenv("WINTRAY_SIGNED_TEST_EXE")
	if source == "" {
		module, err := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go.exe"), "list", "-m", "-f", "{{.Dir}}", "golang.org/x/sys").Output()
		if err != nil {
			t.Fatal(err)
		}
		source = filepath.Join(strings.TrimSpace(string(module)), "windows", "testdata", "ev-signed-file.exe")
	}
	image, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	launcher := filepath.Join(dir, "Example", "Example.exe")
	target := filepath.Join(dir, "Example", "app", "Example.exe")
	for _, path := range []string{launcher, target} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, image, 0600); err != nil {
			t.Fatal(err)
		}
	}
	window := ManagedWindowInfo{Handle: 0x101, ProcessID: 1234, ProcessName: "Example", ProcessPath: target, ClassName: "Chrome_WidgetWin_1"}
	expected := normalizePath(launcher)
	if !matchesExecutable(window, expected, "Example") {
		t.Error("running main executable was not recognized through its launcher")
	}
	manager := &testManager{}
	svc := NewService(&testEnumerator{windows: []ManagedWindowInfo{window}}, manager, &testLogger{})
	_, ok := svc.manageFirstMatchingWindow(context.Background(), func(ManagedWindowInfo) bool { return true }, expected, "Example", nil, nil, 0, "close", 0)
	if !ok || len(manager.closeCalls) != 1 {
		t.Fatalf("launcher window was not closed: managed=%t calls=%v", ok, manager.closeCalls)
	}
	// Alter a signed section while preserving the certificate table: merely
	// extracting the same certificate must not authorize a tampered image.
	image[512] ^= 1
	if err := os.WriteFile(target, image, 0600); err != nil {
		t.Fatal(err)
	}
	if hasTrustedWindowIdentity(window, expected, nil) {
		t.Fatal("trusted a modified signed executable")
	}
	// Replacing a trusted child with an unsigned image must invalidate identity.
	if err := os.WriteFile(target, []byte("unsigned replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if hasTrustedWindowIdentity(window, expected, nil) {
		t.Fatal("trusted a replaced unsigned child executable")
	}
}

func TestLauncherMainPathBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, actual, expected string
		want                   bool
	}{
		{"app subdirectory", `C:\Example\app\Example.exe`, `C:\Example\Example.exe`, true},
		{"versioned subdirectory and case", `C:\Example\app-2.0\EXAMPLE.EXE`, `c:\example\Example.exe`, true},
		{"another installation", `D:\Example\app\Example.exe`, `C:\Example\Example.exe`, false},
		{"sibling prefix", `C:\Example-other\app\Example.exe`, `C:\Example\Example.exe`, false},
		{"different image", `C:\Example\app\Helper.exe`, `C:\Example\Example.exe`, false},
		{"parent is not a main image", `C:\Example\Example.exe`, `C:\Example\app\Example.exe`, false},
		{"drive root", `C:\Example\Example.exe`, `C:\Example.exe`, false},
		{"share root", `\\server\share\Example\Example.exe`, `\\server\share\Example.exe`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := launcherMainPath(tc.actual, tc.expected); got != tc.want {
				t.Fatalf("candidate=%t, want %t", got, tc.want)
			}
		})
	}
}

func TestUnsignedLauncherDoesNotAuthorizeChild(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "Example.exe")
	target := filepath.Join(dir, "app", "Example.exe")
	if err := os.Mkdir(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{launcher, target} {
		if err := os.WriteFile(path, []byte("unsigned"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if executablePathsMatch(target, launcher) {
		t.Fatal("same name and directory alone must not authorize a child")
	}
	if !executablePathsMatch(launcher, launcher) {
		t.Fatal("exact-path support must not require a signature")
	}
}
