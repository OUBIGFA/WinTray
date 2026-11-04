//go:build windows

package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lxn/win"
)

// DC-backed renderers cannot use WS_EX_LAYERED. Checking only the final style
// misses the incompatible transition during initialization, so the real helper
// window records WM_STYLECHANGING throughout startup and native tray restore.
func TestStartupVisibilityPreservesDCBackedRendering(t *testing.T) {
	for _, mode := range []string{"own-dc", "class-dc"} {
		t.Run(mode, func(t *testing.T) {
			entry, file := startupFixtureEntry(t, mode)
			cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
			if err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Release()
			state := awaitStartupFixture(t, file)
			g, err := beginStartupVisibility(entry.ExePath, &testLogger{})
			if err != nil {
				t.Fatal(err)
			}
			defer g.close()
			if len(g.candidates()) != 2 {
				t.Fatal("DC-backed windows must still participate in startup management")
			}
			assertDCBackedRendering(t, state)
			for _, form := range state.Forms {
				if !isWindowVisible(form) || visibilityOwner(form) != g.token {
					t.Fatal("shield changed native visibility or lost window ownership")
				}
			}
			if err := g.close(); err != nil {
				t.Fatal(err)
			}
			assertDCBackedRendering(t, state)
			for _, form := range state.Forms {
				if !isWindowVisible(form) || visibilityOwner(form) != 0 {
					t.Fatal("shield release did not preserve a usable native window")
				}
			}
		})
	}
}

func TestDCBackedLaunchPreservesNativeTrayRestore(t *testing.T) {
	for _, mode := range []string{"own-dc", "class-dc"} {
		for _, manual := range []bool{false, true} {
			name := mode + "/automatic"
			if manual {
				name = mode + "/manual"
			}
			t.Run(name, func(t *testing.T) {
				entry, file := startupFixtureEntry(t, mode)
				entry.RunOnStartup = true
				entry.TrayBehavior.CloseDelaySeconds = 0
				svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
				launch := svc.StartAndManage
				if manual {
					launch = svc.StartNow
				}
				got := launch(context.Background(), entry, 3)
				if !got.Managed || got.Code != ResultManaged || got.Hidden != nil {
					t.Fatalf("native close-to-tray launch = %+v", got)
				}
				state := awaitStartupFixture(t, file)
				assertDCBackedRendering(t, state)
				for _, form := range state.Forms {
					if isWindowVisible(form) || visibilityOwner(form) != 0 {
						t.Fatal("native close left a visible or shielded window")
					}
				}
				// Use the program's own restoration callback, not SW_SHOW or a
				// replacement window, and verify both original windows survive.
				win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
				for _, form := range state.Forms {
					if !waitVisible(form, true) {
						t.Fatal("native tray restore did not show the original window")
					}
				}
				assertDCBackedRendering(t, state)
			})
		}
	}
}

// Opt-in: inspect Explorer's actual buttons, not just WS_VISIBLE. DC-backed
// windows keep the same taskbar suppression and native restoration lifecycle.
func TestDCBackedStartupTaskbarLiveLifecycle(t *testing.T) {
	if os.Getenv("WINTRAY_STARTUP_TASKBAR_TEST") != "1" {
		t.Skip("set WINTRAY_STARTUP_TASKBAR_TEST=1 on an interactive Windows desktop")
	}
	for _, mode := range []string{"taskbar-own-dc", "taskbar-class-dc"} {
		t.Run(mode, func(t *testing.T) {
			entry, file := startupFixtureEntry(t, mode)
			cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
			if err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Release()
			state := awaitStartupFixture(t, file)
			processName := filepath.Base(entry.ExePath)
			awaitFixtureTaskbar(t, processName, true)
			g, err := beginStartupVisibility(entry.ExePath, &testLogger{})
			if err != nil {
				t.Fatal(err)
			}
			defer g.close()
			if g.taskbar == nil || len(g.candidates()) != 2 {
				t.Fatal("DC-backed taskbar shield did not start")
			}
			awaitFixtureTaskbar(t, processName, false)
			assertDCBackedRendering(t, state)
			for _, form := range state.Forms {
				if ok, err := NewWin32WindowManager().CloseWindow(form); !ok {
					t.Fatal(err)
				}
				if !waitVisible(form, false) {
					t.Fatal("native close did not hide the original window")
				}
			}
			if err := g.close(); err != nil {
				t.Fatal(err)
			}
			awaitFixtureTaskbar(t, processName, false)
			win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
			awaitFixtureTaskbar(t, processName, true)
			assertDCBackedRendering(t, state)
			for _, form := range state.Forms {
				if ok, err := NewWin32WindowManager().CloseWindow(form); !ok {
					t.Fatal(err)
				}
			}
			awaitFixtureTaskbar(t, processName, false)
		})
	}
}

func assertDCBackedRendering(t *testing.T, state startupFixture) {
	t.Helper()
	if changes := win.SendMessage(win.HWND(state.Owner), win.WM_APP+25, 0, 0); changes != 0 {
		t.Fatalf("DC-backed renderer received %d incompatible layered transitions", changes)
	}
	for _, form := range state.Forms {
		if style := win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE); style&(exLayered|exToolWindow|exNoActivate) != 0 {
			t.Fatalf("DC-backed window acquired incompatible styles: 0x%X", style)
		}
	}
}
