//go:build windows

package shellapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallationDirectoryUsesProgramFile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "程序 files & data")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, extension := range []string{".exe", ".cmd", ".ps1"} {
		path := filepath.Join(directory, "example"+extension)
		if err := os.WriteFile(path, []byte("must not run"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := InstallationDirectory(path)
		if err != nil || got != directory {
			t.Fatalf("program location = %q, %v; want %q", got, err, directory)
		}
	}
}

func TestInstallationDirectoryRejectsInvalidTarget(t *testing.T) {
	for _, path := range []string{"", "example.exe", "shell:AppsFolder\\Missing.Package_123456789abcd!App", filepath.Join(t.TempDir(), "absent", "example.exe")} {
		if directory, err := InstallationDirectory(path); err == nil || directory != "" {
			t.Fatalf("invalid path opened another directory: %q -> %q, %v", path, directory, err)
		}
	}
}

func TestInstallationDirectoryResolvesStorePackage(t *testing.T) {
	if os.Getenv("WINTRAY_TEST_INSTALLED_APPS") != "1" {
		t.Skip("requires the installed Windows application catalog")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	apps, err := Enumerate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range apps {
		if !IsPath(app.Path) {
			continue
		}
		directory, err := InstallationDirectory(app.Path)
		if err != nil || !filepath.IsAbs(directory) || strings.Contains(directory, "shell:") {
			t.Fatalf("store app has no installation directory: %s: %q, %v", app.Name, directory, err)
		}
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			t.Fatalf("store installation folder is unavailable: %q, %v", directory, err)
		}
		t.Logf("resolved installation folder for %s", app.Name)
		return
	}
	t.Fatal("no Store app available to verify its installation folder")
}
