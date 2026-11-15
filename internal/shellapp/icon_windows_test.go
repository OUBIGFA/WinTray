//go:build windows

package shellapp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestApplicationIconLoadsWindowsExecutable(t *testing.T) {
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	icon, err := Icon(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	if icon == nil || icon.Bounds().Dx() < 32 || icon.Bounds().Dy() < 32 {
		t.Fatalf("application icon is missing or too small: %v", icon)
	}
	visible := false
	for offset := 3; offset < len(icon.Pix); offset += 4 {
		visible = visible || icon.Pix[offset] != 0
	}
	if !visible {
		t.Fatal("application icon has no visible pixels")
	}
}

func TestApplicationIconPreservesTransparentPixels(t *testing.T) {
	path := os.Getenv("WINTRAY_TEST_TRANSPARENT_ICON_APP")
	if path == "" {
		t.Skip("requires an installed app with a transparent source icon")
	}
	icon, err := Icon(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	transparent, visible := 0, 0
	for offset := 3; offset < len(icon.Pix); offset += 4 {
		if icon.Pix[offset] == 0 {
			transparent++
		} else {
			visible++
		}
	}
	if transparent < 16 || visible < 16 {
		t.Fatalf("original icon transparency was lost: transparent=%d visible=%d", transparent, visible)
	}
}

func TestApplicationIconRejectsMissingApplication(t *testing.T) {
	for _, path := range []string{
		filepath.Join(t.TempDir(), "missing.exe"),
		"shell:AppsFolder\\WinTray.MissingTestApp_123456789abcd!App",
	} {
		if icon, err := Icon(path, 64); err == nil || icon != nil {
			t.Fatalf("missing application produced a successful icon: %q, %v", path, err)
		}
	}
}

func TestApplicationIconReleasesGraphicsResources(t *testing.T) {
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := Icon(path, 64); err != nil {
		t.Fatal(err)
	}
	resources := windows.NewLazySystemDLL("user32.dll").NewProc("GetGuiResources")
	before, _, _ := resources.Call(uintptr(windows.CurrentProcess()), 0)
	for range 16 {
		if _, err := Icon(path, 64); err != nil {
			t.Fatal(err)
		}
	}
	after, _, _ := resources.Call(uintptr(windows.CurrentProcess()), 0)
	if after > before+2 {
		t.Fatalf("repeated icon loading leaked graphics resources: before=%d after=%d", before, after)
	}
}

func TestInstalledApplicationsIncludeTheirIcons(t *testing.T) {
	if os.Getenv("WINTRAY_TEST_INSTALLED_APPS") != "1" {
		t.Skip("requires the installed Windows application catalog")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	applications, err := Enumerate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	packaged, desktop := 0, 0
	for _, app := range applications {
		if app.Icon == nil {
			t.Errorf("installed application is missing its icon: %s (%s)", app.Name, app.Path)
			continue
		}
		if IsPath(app.Path) {
			packaged++
			if packaged == 1 {
				icon, err := Icon(app.Path, 64)
				if err != nil || icon == nil || icon.Bounds().Empty() {
					t.Fatalf("saved store app cannot reload its icon: %s: %v", app.Name, err)
				}
			}
		} else {
			desktop++
		}
	}
	if len(applications) == 0 {
		t.Fatal("installed application catalog is empty")
	}
	t.Logf("real application icons: desktop=%d store=%d", desktop, packaged)
}
