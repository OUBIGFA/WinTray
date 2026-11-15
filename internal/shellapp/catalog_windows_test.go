//go:build windows

package shellapp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestEnumerateHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Enumerate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled catalog lookup returned %v", err)
	}
}

func TestEnumerateInstalledWindowsApplications(t *testing.T) {
	if os.Getenv("WINTRAY_TEST_INSTALLED_APPS") != "1" {
		t.Skip("requires a Windows user with an installed application catalog")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	apps, err := Enumerate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) == 0 {
		t.Fatal("Windows application catalog was empty")
	}
	seen := make(map[string]bool)
	packaged, desktop := 0, 0
	for _, app := range apps {
		if app.Name == "" || app.Path == "" || seen[app.Key()] {
			t.Fatalf("empty or duplicate application: %+v", app)
		}
		seen[app.Key()] = true
		if IsPath(app.Path) {
			packaged++
		} else {
			desktop++
			if info, err := os.Stat(app.Path); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("desktop application has no launchable file: %q: %v", app.Path, err)
			}
		}
	}
	t.Logf("installed applications: desktop=%d packaged=%d", desktop, packaged)
}
