//go:build windows

package tray

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/traybox"
)

// Capture a real native menu with two separate program rows.
// The disposable images stand in for program-provided icon frames; no user
// tray registrations or saved collection settings are touched.
func TestNativeTrayMenuAppearance(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	for _, language := range []string{"zh-CN", "en-US"} {
		t.Run(language, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			hwnd, menu := newBoxMenuLoopFixture(t, language)
			for i, ink := range []color.NRGBA{{R: 37, G: 99, B: 235, A: 255}, {R: 36, G: 138, B: 84, A: 255}} {
				img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
				for y := 2; y < 14; y++ {
					for x := 2; x < 14; x++ {
						img.SetNRGBA(x, y, ink)
					}
				}
				bitmap, err := traybox.MenuBitmap(img, int(win.GetSystemMetrics(win.SM_CXSMICON)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { win.DeleteObject(win.HGDIOBJ(bitmap)) })
				label := []string{"Example one", "Example two"}[i]
				if language == "zh-CN" {
					label = []string{"收纳程序一", "收纳程序二"}[i]
				}
				if i == 0 {
					info := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_STRING | win.MIIM_BITMAP, DwTypeData: syscall.StringToUTF16Ptr(label), HbmpItem: bitmap}
					if !win.SetMenuItemInfo(menu, trayBoxedBase, false, &info) {
						t.Fatal("set first program's icon and label")
					}
				} else if !appendTrayMenuItem(menu, menuItem{id: trayBoxedBase + 1, text: label, bitmap: bitmap}) {
					t.Fatal("append second program row")
				}
			}
			var cursor win.POINT
			win.GetCursorPos(&cursor)
			defer win.SetCursorPos(cursor.X, cursor.Y)
			win.SetCursorPos(0, 0)
			win.SetForegroundWindow(hwnd)
			captured := false
			var captureErr error
			_, err := trackBoxRightClicks(hwnd, menu, func() {
				popup := win.FindWindow(syscall.StringToUTF16Ptr("#32768"), nil)
				if popup != 0 && win.GetWindowThreadProcessId(popup, nil) == win.GetCurrentThreadId() {
					captureErr = captureNativeMenu(popup, "tray-menu-"+language)
					captured = true
				}
				procEndMenu.Call()
			}, func() { win.TrackPopupMenuEx(menu, trayMenuFlags, 100, 100, hwnd, nil) })
			if err != nil || !captured {
				t.Fatalf("native menu was not displayed: %v", err)
			}
			if captureErr != nil {
				t.Fatal(captureErr)
			}
		})
	}
}

func captureNativeMenu(hwnd win.HWND, name string) error {
	dir := os.Getenv("WINTRAY_UI_SCREENSHOTS")
	if dir == "" {
		return nil
	}
	var rect win.RECT
	if !win.GetWindowRect(hwnd, &rect) || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return fmt.Errorf("get native menu bounds")
	}
	width, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	dc := win.CreateCompatibleDC(0)
	if dc == 0 {
		return fmt.Errorf("create menu capture DC")
	}
	defer win.DeleteDC(dc)
	info := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(width), BiHeight: -int32(height), BiPlanes: 1, BiBitCount: 32}
	var pixels unsafe.Pointer
	bitmap := win.CreateDIBSection(0, &info, win.DIB_RGB_COLORS, &pixels, 0, 0)
	if bitmap == 0 || pixels == nil {
		return fmt.Errorf("create menu capture bitmap")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	previous := win.SelectObject(dc, win.HGDIOBJ(bitmap))
	defer win.SelectObject(dc, previous)
	if ok, _, err := menuUser32.NewProc("PrintWindow").Call(uintptr(hwnd), uintptr(dc), 2); ok == 0 {
		return fmt.Errorf("capture native menu: %w", err)
	}
	win.GdiFlush()
	raw := unsafe.Slice((*byte)(pixels), width*height*4)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(raw); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = raw[i+2], raw[i+1], raw[i], 255
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		return err
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
