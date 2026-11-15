//go:build windows

package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"testing"
	"time"

	native "github.com/egoist/mygo/ui"
	"wintray/internal/i18n"
	"wintray/internal/shellapp"
)

func TestInstalledAppsShowIconsBesideNames(t *testing.T) {
	for _, dark := range []bool{false, true} {
		window := installedAppsFixture()
		colors := []color.NRGBA{{R: 229, G: 37, B: 79, A: 255}, {R: 11, G: 193, B: 137, A: 255}}
		for index := range window.desktop.installedApps.apps {
			icon := image.NewNRGBA(image.Rect(0, 0, 64, 64))
			draw.Draw(icon, icon.Bounds(), image.NewUniform(colors[index]), image.Point{}, draw.Src)
			window.desktop.installedApps.apps[index].Icon = icon
		}
		tester := native.NewTester(window.nativeView, 1200, 850)
		tester.SetDark(dark)
		messages := i18n.For("zh-CN")
		if err := tester.Click(messages.AddInstalledApps); err != nil {
			t.Fatal(err)
		}
		for index, app := range window.desktop.installedApps.apps {
			row, found := tester.Find(app.Name)
			if !found || row.H > 48 {
				t.Fatalf("application is not in a single-line selectable row: %s: %+v", app.Name, row)
			}
			assertIconPixels(t, tester.Image(), row, colors[index])
			if tester.HasText(app.Path) {
				t.Fatalf("application path is shown instead of the icon-and-name row: %s", app.Path)
			}
			if err := tester.Click(app.Name); err != nil {
				t.Fatal(err)
			}
		}
		if err := tester.Click(messages.InstalledAppsAdd); err != nil {
			t.Fatal(err)
		}
		for index, app := range window.settings.ManagedApps {
			row, found := tester.Find(messages.ManagedDragHandle + " " + app.Name)
			if !found {
				t.Fatal("added application has no icon area")
			}
			assertIconPixels(t, tester.Image(), row, colors[index])
		}
	}
}

func assertIconPixels(t *testing.T, screenshot image.Image, rect native.Rect, want color.NRGBA) {
	t.Helper()
	pixels := 0
	for vertical := int(rect.Y); vertical < int(rect.Y+rect.H); vertical++ {
		for horizontal := int(rect.X); horizontal < int(rect.X+rect.W); horizontal++ {
			if color.NRGBAModel.Convert(screenshot.At(horizontal, vertical)) == want {
				pixels++
			}
		}
	}
	if pixels < 256 {
		t.Fatalf("application icon is not rendered beside its name: got %d icon pixels, want at least 256", pixels)
	}
}

func TestInstalledAppIconsUseWindowsCatalog(t *testing.T) {
	if os.Getenv("WINTRAY_TEST_INSTALLED_APPS") != "1" {
		t.Skip("requires the installed Windows application catalog")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	apps, err := shellapp.Enumerate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) == 0 {
		t.Fatal("Windows returned no installed applications")
	}
	window := nativeProgramsFixture(0)
	window.desktop.installedApps = &installedAppPicker{loaded: true, apps: apps, selected: make(map[string]bool)}
	tester := native.NewTester(window.nativeView, 1200, 850)
	messages := i18n.For("zh-CN")
	if err := tester.Click(messages.AddInstalledApps); err != nil {
		t.Fatal(err)
	}
	if len(window.desktop.installedApps.icons) != len(apps) {
		t.Fatalf("loaded %d icons for %d apps", len(window.desktop.installedApps.icons), len(apps))
	}
	for _, dark := range []bool{false, true} {
		tester.SetDark(dark)
		for _, filter := range []struct{ label, name string }{{messages.InstalledAppsDesktop, "desktop"}, {messages.InstalledAppsStore, "store"}} {
			if err := tester.Click(filter.label); err != nil {
				t.Fatal(err)
			}
			captureNativeTest(t, tester, fmt.Sprintf("app-icons-%s-dark-%t", filter.name, dark))
		}
	}
	t.Logf("Windows catalog shows %d real application icons", len(apps))
}
