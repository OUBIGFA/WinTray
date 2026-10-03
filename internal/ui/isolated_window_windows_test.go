//go:build windows

package ui

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"testing"
	"time"
)

// runWindowTestInChild gives each first-open layout case the same fresh native
// lifetime as the application. Walk retains process/thread layout state across
// disposed forms; sequential long-copy cases can otherwise report zero-width
// controls even with the unchanged application. Assertions still run in full.
func runWindowTestInChild(t *testing.T) bool {
	t.Helper()
	const marker = "WINTRAY_ISOLATED_UI_TEST"
	if os.Getenv(marker) == t.Name() {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1")
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated window test: %v\n%s", err, output)
	}
	return true
}
