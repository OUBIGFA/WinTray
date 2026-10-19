//go:build windows

package ui

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
)

// Check pixels presented by the native window, not CapturePage: CapturePage
// can build a frame in memory even when the actual client area stays blank.
func TestMainWindowPaintsOnFirstOpenAndReopen(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	if runWindowTestInChild(t) {
		return
	}
	runTestMainThread(func() {
		w, err := NewMainWindow(config.DefaultSettings(), Callbacks{})
		if err != nil {
			t.Error(err)
			return
		}
		result := make(chan error, 1)
		w.ShowMainWindow()
		w.OnStarting(func() {
			go func() {
				defer w.RequestExplicitClose()
				if err := waitForNativeWindowPixels(w); err != nil {
					result <- fmt.Errorf("first open: %w", err)
					return
				}
				w.Synchronize(func() {
					w.HideMainWindow()
					w.settings.ManagedApps = nativeProgramsFixture(3).settings.ManagedApps
					w.ShowMainWindow()
				})
				if err := waitForNativeWindowPixels(w); err != nil {
					result <- fmt.Errorf("reopen: %w", err)
					return
				}
				result <- nil
			}()
		})
		if code := w.Run(); code != 0 {
			t.Errorf("desktop exit %d", code)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Error(err)
			}
		default:
			t.Error("window exited before the pixel checks completed")
		}
	})
}

func waitForNativeWindowPixels(w *MainWindow) error {
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		checked := make(chan error, 1)
		w.Synchronize(func() {
			if w.desktop.window == nil || !w.desktop.window.IsVisible() {
				checked <- errors.New("main window is not visible")
				return
			}
			checked <- checkNativeClientPixels(win.HWND(w.desktop.window.NativeHandle()))
		})
		select {
		case last = <-checked:
			if last == nil {
				return nil
			}
		case <-time.After(time.Until(deadline)):
			return errors.New("UI thread did not answer the pixel check")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return last
}

func checkNativeClientPixels(hwnd win.HWND) error {
	var rect win.RECT
	if !win.GetClientRect(hwnd, &rect) || rect.Right <= 0 || rect.Bottom <= 0 {
		return errors.New("main window has no client area")
	}
	dc := win.GetDC(hwnd)
	if dc == 0 {
		return errors.New("GetDC failed")
	}
	defer win.ReleaseDC(hwnd, dc)
	memory := win.CreateCompatibleDC(dc)
	if memory == 0 {
		return errors.New("CreateCompatibleDC failed")
	}
	defer win.DeleteDC(memory)
	info := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: rect.Right, BiHeight: -rect.Bottom, BiPlanes: 1, BiBitCount: 32}
	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(dc, &info, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		return errors.New("CreateDIBSection failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	old := win.SelectObject(memory, win.HGDIOBJ(bitmap))
	defer win.SelectObject(memory, old)
	// PW_CLIENTONLY excludes the title bar: its text must not make a blank
	// application count as painted. PW_RENDERFULLCONTENT includes children.
	if ok, _, err := windows.NewLazySystemDLL("user32.dll").NewProc("PrintWindow").Call(uintptr(hwnd), uintptr(memory), 3); ok == 0 {
		return fmt.Errorf("PrintWindow: %w", err)
	}
	return checkClientPixelCoverage(unsafe.Slice((*uint32)(bits), int(rect.Right)*int(rect.Bottom)), int(rect.Right))
}

func checkClientPixelCoverage(pixels []uint32, width int) error {
	if width < 2 || len(pixels) == 0 || len(pixels)%width != 0 {
		return errors.New("invalid native client pixel dimensions")
	}
	// Both halves must contain painted UI. A text strip at the left edge can
	// exceed eight colors while the rest of the window is still blank.
	const minColors = 8
	colors := [2]map[uint32]bool{make(map[uint32]bool), make(map[uint32]bool)}
	for i, pixel := range pixels {
		side := 0
		if i%width >= width/2 {
			side = 1
		}
		if len(colors[side]) < minColors {
			colors[side][pixel&0xFFFFFF] = true
		}
		if len(colors[0]) >= minColors && len(colors[1]) >= minColors {
			return nil
		}
	}
	return fmt.Errorf("native client area is not painted across its width (left=%d right=%d colors)", len(colors[0]), len(colors[1]))
}
