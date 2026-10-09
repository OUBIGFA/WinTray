//go:build windows

package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
)

func TestTaskbarShieldDoesNotExposeUtilityWindows(t *testing.T) {
	class := syscall.StringToUTF16Ptr("STATIC")
	for _, tc := range []struct {
		name           string
		style, exStyle uint32
		owned          bool
		want           bool
	}{
		{"ordinary", win.WS_OVERLAPPEDWINDOW, 0, false, true},
		{"owned", win.WS_OVERLAPPEDWINDOW, 0, true, false},
		{"owned app window", win.WS_OVERLAPPEDWINDOW, win.WS_EX_APPWINDOW, true, true},
		{"tool", win.WS_OVERLAPPEDWINDOW, win.WS_EX_TOOLWINDOW, false, false},
		{"no activate", win.WS_OVERLAPPEDWINDOW, win.WS_EX_NOACTIVATE, false, false},
		{"child", win.WS_CHILD, win.WS_EX_APPWINDOW, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			var owner win.HWND
			if tc.owned {
				owner = win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 0, 0, 0, 0, 0, nil)
				if owner == 0 {
					t.Fatal("create hidden owner")
				}
				defer win.DestroyWindow(owner)
			}
			hwnd := win.CreateWindowEx(tc.exStyle, class, nil, tc.style, -32000, -32000, 120, 80, owner, 0, 0, nil)
			if hwnd == 0 {
				t.Fatal("create taskbar eligibility fixture")
			}
			defer win.DestroyWindow(hwnd)
			if got := taskbarEligibleWindow(uintptr(hwnd)); got != tc.want {
				t.Fatalf("eligible=%t want %t", got, tc.want)
			}
		})
	}
}

// Opt-in because this deliberately creates temporary taskbar buttons and
// reads Explorer's UI Automation tree. The windows stay offscreen and only
// the isolated fixture process is launched/stopped; no user app is touched.
func TestStartupTaskbarLiveLifecycle(t *testing.T) {
	if os.Getenv("WINTRAY_STARTUP_TASKBAR_TEST") != "1" {
		t.Skip("set WINTRAY_STARTUP_TASKBAR_TEST=1 on an interactive Windows desktop")
	}
	for _, outcome := range []string{"closed", "cancelled", "refused"} {
		t.Run(outcome, func(t *testing.T) {
			mode := "taskbar"
			if outcome == "refused" {
				mode = "taskbar-refuse"
			}
			entry, file := startupFixtureEntry(t, mode)
			cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
			if err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Release()
			state := awaitStartupFixture(t, file)
			awaitFixtureTaskbar(t, filepath.Base(entry.ExePath), true)
			shield, err := beginStartupVisibility(entry.ExePath, &testLogger{})
			if err != nil {
				t.Fatal(err)
			}
			defer shield.close()
			if shield.taskbar == nil || len(shield.candidates()) != 2 {
				t.Fatal("taskbar shield did not start")
			}
			awaitFixtureTaskbar(t, filepath.Base(entry.ExePath), false)
			for _, form := range state.Forms {
				if !isWindowVisible(form) || win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE)&(exToolWindow|exNoActivate) != 0 {
					t.Fatal("taskbar suppression changed native visibility or window classification")
				}
				if outcome != "cancelled" {
					if ok, err := NewWin32WindowManager().CloseWindow(form); !ok {
						t.Fatal(err)
					}
					if !waitVisible(form, outcome == "refused") {
						t.Fatal("fixture native close had unexpected visibility")
					}
				}
			}
			if err := shield.close(); err != nil {
				t.Fatal(err)
			}
			awaitFixtureTaskbar(t, filepath.Base(entry.ExePath), outcome != "closed")
			for _, form := range state.Forms {
				if visibilityOwner(form) != 0 || win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE)&exLayered != 0 {
					t.Fatal("shield left temporary attributes behind")
				}
			}
			if outcome == "closed" {
				// Native tray restore must bring back the original taskbar button,
				// and closing it again must remove it without a residual icon.
				win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
				awaitFixtureTaskbar(t, filepath.Base(entry.ExePath), true)
				for _, form := range state.Forms {
					_, _ = NewWin32WindowManager().CloseWindow(form)
				}
				awaitFixtureTaskbar(t, filepath.Base(entry.ExePath), false)
			}
		})
	}
}

func awaitFixtureTaskbar(t *testing.T, processName string, visible bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "testdata/taskbar_buttons.ps1", "-ProcessName", processName).CombinedOutput()
		if err != nil {
			t.Fatalf("inspect taskbar: %v\n%s", err, output)
		}
		count, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil {
			t.Fatalf("taskbar response: %q", output)
		}
		if (count > 0) == visible {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("taskbar buttons=%d want visible=%t", count, visible)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
