//go:build windows

package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"testing"
	"time"
	"unsafe"

	native "github.com/egoist/mygo/ui"
	"github.com/lxn/win"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

// Every settings control must be visible without a wheel gesture. Checking
// both edges also catches a scroll container that merely hides its scrollbar.
func TestSettingsAndEditorStayFullyVisible(t *testing.T) {
	for _, lang := range []string{"zh-CN", "en-US"} {
		for _, logon := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/logon=%t", lang, logon), func(t *testing.T) {
				w := nativeProgramsFixture(20)
				w.settings.Language = lang
				w.settings.RunAtLogon = logon
				entry := &w.settings.ManagedApps[0]
				entry.ExePath = "C:/Programs/Weixin.exe"
				entry.Schedule.Enabled = true
				entry.Schedule.FrequencyEnabled = true
				m := i18n.For(lang)
				tt := native.NewTester(w.nativeView, mainWindowMinWidth, mainWindowMinHeight)
				if err := tt.Click(entry.Name); err != nil {
					t.Fatal(err)
				}
				check := func(labels []string, x, y float32) {
					t.Helper()
					footer, _ := tt.Find(m.ExitApp)
					before := make(map[string]native.Rect)
					for _, label := range labels {
						r, ok := tt.Find(label)
						if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.X+r.W > mainWindowMinWidth || r.Y < 0 || r.Y+r.H > footer.Y {
							t.Errorf("control outside page: %s = %+v, footer=%+v", label, r, footer)
						}
						before[label] = r
					}
					tt.Scroll(x, y, 0, 1200)
					for _, label := range labels {
						if r, ok := tt.Find(label); !ok || r != before[label] {
							t.Errorf("settings scrolled: %s: %+v -> %+v", label, before[label], r)
						}
					}
				}
				labels := []string{m.ManagedLaunchNow, m.ManagedWeChatAutoLogin, m.ManagedCloseDelay, m.ManagedScheduleStart, m.ManagedScheduleExit, m.FrequencyDaysLabel, m.FrequencyRunsLabel}
				check(labels, 1000, 650)
				// The divider must not squeeze the fixed editor into overflow.
				divider, _ := tt.Find(m.ResizePanels)
				tt.Press(divider.X+divider.W/2, divider.Y+40)
				tt.Move(1100, divider.Y+40)
				tt.Release(1100, divider.Y+40)
				check(labels, 1000, 650)
				if err := tt.Click(m.FrequencyRunsLabel); err != nil {
					t.Fatal(err)
				}
				if !tt.Focused(m.FrequencyRunsLabel) {
					t.Fatal("last schedule input is not reachable")
				}
				captureNativeTest(t, tt, lang+"-full-editor")
				if err := tt.Click(m.SettingsTab); err != nil {
					t.Fatal(err)
				}
				check([]string{m.RunAtLogon, m.StartHidden, m.ExitOnDone, m.StartupInterval, m.RetrySeconds, m.OpenLogs, m.RemoveLogonTask, m.CleanupRestore}, 1000, 650)
				captureNativeTest(t, tt, lang+"-full-settings")
			})
		}
	}
}

// Real application lifetimes share an isolated user-data directory: reopening
// after hide or explicit exit must restore the persisted normal window size.
func TestMainWindowRemembersSizeAcrossRestart(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	phase := os.Getenv("WINTRAY_WINDOW_SIZE_PHASE")
	if phase == "" {
		data := t.TempDir()
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		for _, phase := range []string{"save", "restore", "after-exit"} {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			cmd := exec.CommandContext(ctx, exe, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1")
			cmd.Env = append(os.Environ(), "LOCALAPPDATA="+data, "WINTRAY_WINDOW_SIZE_PHASE="+phase)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			output, err := cmd.CombinedOutput()
			cancel()
			if err != nil {
				t.Fatalf("%s: %v\n%s", phase, err, output)
			}
		}
		return
	}
	runTestMainThread(func() {
		w, err := NewMainWindow(config.DefaultSettings(), Callbacks{OnSave: func(config.Settings) { t.Error("resizing saved program settings") }})
		if err != nil {
			t.Error(err)
			return
		}
		w.ShowMainWindow()
		w.OnStarting(func() {
			go func() {
				defer w.RequestExplicitClose()
				step := func(f func()) {
					done := make(chan struct{})
					w.Synchronize(func() { defer close(done); f() })
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("window did not dispatch")
					}
				}
				time.Sleep(100 * time.Millisecond)
				step(func() {
					window := w.desktop.window
					var limits win.MINMAXINFO
					win.SendMessage(win.HWND(window.NativeHandle()), win.WM_GETMINMAXINFO, 0, uintptr(unsafe.Pointer(&limits)))
					var outer, client win.RECT
					win.GetWindowRect(win.HWND(window.NativeHandle()), &outer)
					win.GetClientRect(win.HWND(window.NativeHandle()), &client)
					scale := float64(client.Right) / float64(window.ContentBounds().Width)
					minWidth := float64(limits.PtMinTrackSize.X-(outer.Right-outer.Left-client.Right)) / scale
					minHeight := float64(limits.PtMinTrackSize.Y-(outer.Bottom-outer.Top-client.Bottom)) / scale
					if minWidth < mainWindowMinWidth-1 || minHeight < mainWindowMinHeight-1 {
						t.Errorf("minimum client size=%gx%g", minWidth, minHeight)
					}
					if phase == "save" {
						w.desktop.window.SetContentSize(1300, 900)
					} else {
						width, height := w.desktop.window.ContentSize()
						wantWidth, wantHeight := 1300, 900
						if phase == "after-exit" {
							wantWidth, wantHeight = 1360, 940
						}
						if width != wantWidth || height != wantHeight {
							t.Errorf("restart size=%dx%d, want %dx%d", width, height, wantWidth, wantHeight)
						}
					}
				})
				if phase == "after-exit" {
					return
				}
				time.Sleep(100 * time.Millisecond)
				step(func() {
					w.HideMainWindow()
					if _, err := os.Stat(filepath.Join(os.Getenv("LOCALAPPDATA"), "WinTray", "window-state.json")); err != nil {
						t.Errorf("window state not saved: %v", err)
					}
					w.ShowMainWindow()
				})
				time.Sleep(100 * time.Millisecond)
				step(func() {
					width, height := w.desktop.window.ContentSize()
					if width != 1300 || height != 900 {
						t.Errorf("reopen size=%dx%d, want 1300x900", width, height)
					}
					w.desktop.window.Maximize()
				})
				time.Sleep(100 * time.Millisecond)
				step(func() { w.HideMainWindow(); w.ShowMainWindow() })
				time.Sleep(100 * time.Millisecond)
				step(func() {
					if !w.desktop.window.IsMaximized() {
						t.Error("reopen lost maximized state")
					}
					w.desktop.window.Unmaximize()
				})
				time.Sleep(100 * time.Millisecond)
				step(func() {
					width, height := w.desktop.window.ContentSize()
					if width != 1300 || height != 900 {
						t.Errorf("maximize changed normal size: %dx%d", width, height)
					}
					if phase == "restore" {
						w.desktop.window.SetContentSize(1360, 940)
					}
				})
			}()
		})
		if code := w.Run(); code != 0 {
			t.Errorf("desktop exit=%d", code)
		}
	})
}
