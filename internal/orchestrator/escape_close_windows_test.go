//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
	"wintray/internal/config"
	"wintray/internal/traybox"
)

func TestNativeCloseHonorsWindowCloseCapability(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, mode := range []string{"enabled", "disabled", "removed", "no menu", "disabled window"} {
		t.Run(mode, func(t *testing.T) {
			style := uint32(win.WS_OVERLAPPEDWINDOW)
			if mode == "no menu" {
				style = win.WS_POPUP
			}
			hwnd := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("STATIC"), nil, style, -32000, -32000, 120, 80, 0, 0, 0, nil)
			if hwnd == 0 {
				t.Fatal("create capability fixture")
			}
			defer win.DestroyWindow(hwnd)
			menu := win.GetSystemMenu(hwnd, false)
			switch mode {
			case "disabled":
				win.EnableMenuItem(menu, scClose, win.MF_BYCOMMAND|win.MF_GRAYED)
			case "removed":
				win.DeleteMenu(menu, scClose, win.MF_BYCOMMAND)
			case "disabled window":
				win.EnableWindow(hwnd, false)
			}
			want := mode == "enabled"
			ok, err := NewWin32WindowManager().CloseWindow(uintptr(hwnd))
			if ok != want || (!want && !errors.Is(err, errNativeCloseUnavailable)) {
				t.Fatalf("CloseWindow(%s) = %t, %v; want enabled=%t", mode, ok, err, want)
			}
			// Inspect the actual queue, without dispatching a Close that could
			// destroy the fixture. Unsupported commands must not be posted.
			var msg win.MSG
			posted := win.PeekMessage(&msg, hwnd, wmSysCommand, wmSysCommand, win.PM_REMOVE)
			if posted != want {
				t.Fatalf("SC_CLOSE posted=%t, want %t", posted, want)
			}
			if win.PeekMessage(&msg, hwnd, wmClose, wmClose, win.PM_REMOVE) {
				t.Fatal("WM_CLOSE bypassed the window's disabled command")
			}
		})
	}
}

func TestEscapeTargetIsScopedToWindowNotThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := syscall.StringToUTF16Ptr("STATIC")
	root := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 1, 1, 0, 0, 0, nil)
	sibling := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 1, 1, 0, 0, 0, nil)
	owned := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 1, 1, root, 0, 0, nil)
	child := win.CreateWindowEx(0, class, nil, win.WS_CHILD, 0, 0, 1, 1, root, 0, 0, nil)
	for _, h := range []win.HWND{root, sibling, owned, child} {
		if h == 0 {
			t.Fatal("create focus fixture")
		}
	}
	defer win.DestroyWindow(root)
	defer win.DestroyWindow(sibling)
	defer win.DestroyWindow(owned)
	for _, tc := range []struct {
		target win.HWND
		want   bool
	}{{root, true}, {child, true}, {sibling, false}, {owned, false}, {0, false}} {
		if got := escapeTargetBelongsTo(uintptr(root), uintptr(tc.target)); got != tc.want {
			t.Fatalf("focus %x accepted=%t, want %t", tc.target, got, tc.want)
		}
	}
}

func TestEscapeDismissalPreservesProcessAndNativeRestore(t *testing.T) {
	entry, file := startupFixtureEntry(t, "escape-hide")
	entry.TrayBehavior.CloseDelaySeconds = 0
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	state := awaitStartupFixture(t, file)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &liveEscapeLogger{t})
	for cycle := 0; cycle < 2; cycle++ {
		for _, hwnd := range state.Forms {
			focusEscapeFixture(t, state, hwnd)
			result, ok := svc.tryManageAndVerify(context.Background(), windowInfo(hwnd, 0, nil), closeAllowedScoreThreshold, "close")
			if !ok || result.Hidden {
				t.Fatalf("cycle %d: native Esc dismissal = %+v, %t", cycle, result, ok)
			}
		}
		if !processAlive(uint32(cmd.Process.Pid)) || !isWindow(state.Owner) {
			t.Fatal("dismissal ended the process or destroyed its native tray owner")
		}
		for _, hwnd := range state.Forms {
			if isWindowVisible(hwnd) || visibilityOwner(hwnd) != 0 {
				t.Fatal("window not natively hidden or shield left attached")
			}
		}
		if count := win.SendMessage(win.HWND(state.Owner), win.WM_APP+23, 0, 0); count < 2 {
			t.Fatalf("only %d child controls received Esc", count)
		}
		// Use the program's own restore handler, not ShowWindow on our side.
		win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
		for _, hwnd := range state.Forms {
			if !waitVisible(hwnd, true) {
				t.Fatal("native restore no longer works")
			}
		}
	}
}

func TestUnresponsiveEscapeNeverEscalatesToDisabledClose(t *testing.T) {
	entry, file := startupFixtureEntry(t, "escape-ignore")
	entry.TrayBehavior.CloseDelaySeconds = 0
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	result := svc.StartAndManage(context.Background(), entry, 2)
	state := awaitStartupFixture(t, file)
	if result.Managed || result.Hidden != nil {
		t.Fatalf("unhandled Esc reported success: %+v", result)
	}
	if !svc.IsProgramRunning(entry) || !isWindow(state.Owner) {
		t.Fatal("ignored Esc escalated to process exit")
	}
	for _, hwnd := range state.Forms {
		if !isWindowVisible(hwnd) || visibilityOwner(hwnd) != 0 {
			t.Fatal("failed dismissal stranded the program's UI")
		}
	}
}

func TestProcessExitIsNotSuccessfulDismissal(t *testing.T) {
	entry, file := startupFixtureEntry(t, "escape-exits")
	entry.TrayBehavior.CloseDelaySeconds = 0
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	state := awaitStartupFixture(t, file)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &liveEscapeLogger{t})
	focusEscapeFixture(t, state, state.Forms[1])
	window := windowInfo(state.Forms[1], 0, nil)
	_, ok := svc.tryManageAndVerify(context.Background(), window, closeAllowedScoreThreshold, "close")
	if ok {
		t.Fatal("process exit was reported as a successful close-to-tray")
	}
	if processAlive(uint32(cmd.Process.Pid)) {
		t.Fatal("fixture did not exercise process exit")
	}
}

func focusEscapeFixture(t *testing.T, state startupFixture, hwnd uintptr) {
	t.Helper()
	win.PostMessage(win.HWND(state.Owner), win.WM_APP+24, hwnd, 0)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if target := escapeFocus(hwnd); target != 0 && target != hwnd {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fixture failed to set its own keyboard focus")
}

func TestCancelledEscapeDoesNotPostKeysOrClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := NewWin32WindowManager().SendEscape(ctx, 0); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Escape = %t, %v", ok, err)
	}
	manager := &testManager{}
	svc := NewService(&testEnumerator{}, manager, &testLogger{})
	if _, ok := svc.tryManageAndVerify(ctx, ManagedWindowInfo{Handle: 1}, closeAllowedScoreThreshold, "close"); ok {
		t.Fatal("cancelled operation succeeded")
	}
	if len(manager.escapeCalls)+len(manager.closeCalls)+len(manager.hideCalls) != 0 {
		t.Fatal("cancellation sent another window action")
	}
}

// Explicit opt-in only: use an installed Esc-only program on the test desktop.
// Never terminate it or change its configuration; the caller controls cold-start
// setup. The check uses the public service and observes the original process.
func TestEscapeOnlyProgramLive(t *testing.T) {
	path := os.Getenv("WINTRAY_ESCAPE_LIVE_EXE")
	if path == "" {
		t.Skip("set WINTRAY_ESCAPE_LIVE_EXE to an Esc-only program")
	}
	entry := config.ManagedAppEntry{Name: "Escape live check", ExePath: path, RunOnStartup: true,
		TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}}
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &liveEscapeLogger{t})
	before := runningProgramPIDs(normalizePath(path), "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var box *traybox.Box
	if host := os.Getenv("WINTRAY_ESCAPE_LIVE_HOST"); host != "" {
		box = traybox.NewBox(host, &liveEscapeLogger{t})
		if err := box.SetPaths([]string{path}); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := box.Close(); err != nil {
				t.Error(err)
			}
		}()
		if len(before) != 0 {
			openLiveEscapeIcon(t, ctx, box)
		}
	}
	result := svc.StartNow(ctx, entry, 15)
	if !result.Managed || result.Action != "close" || result.Hidden != nil {
		t.Fatalf("native dismissal = %+v", result)
	}
	after := runningProgramPIDs(normalizePath(path), "")
	if len(after) != 1 || (len(before) == 1 && before[0] != after[0]) {
		t.Fatalf("original process not preserved: before=%v after=%v", before, after)
	}
	time.Sleep(time.Second)
	if !processAlive(after[0]) {
		t.Fatal("process exited after dismissal")
	}
	for _, window := range NewWin32WindowEnumerator().EnumerateTopLevelWindows() {
		if window.ProcessID == after[0] && !window.IsToolWindow && !isUnmanageableWindow(window) {
			t.Fatalf("native UI still visible: %+v", window)
		}
	}
	t.Logf("native UI hidden; original process alive: pid=%d", after[0])
	if box != nil {
		for cycle := 0; cycle < 2; cycle++ {
			openLiveEscapeIcon(t, ctx, box)
			var window uintptr
			deadline := time.Now().Add(2 * time.Second)
			for window == 0 && time.Now().Before(deadline) {
				for _, w := range NewWin32WindowEnumerator().EnumerateTopLevelWindows() {
					if w.ProcessID == after[0] && !w.IsToolWindow && !isUnmanageableWindow(w) {
						window = w.Handle
					}
				}
				time.Sleep(25 * time.Millisecond)
			}
			if window == 0 {
				t.Fatal("native tray callback did not restore the interface")
			}
			result := svc.HideExisting(ctx, entry, 10)
			if !result.Managed || result.Hidden != nil || isWindowVisible(window) || !processAlive(after[0]) {
				t.Fatalf("native restore/dismiss cycle %d failed: %+v", cycle, result)
			}
			if len(box.Icons()) != 1 {
				t.Fatal("dismissal removed or duplicated the native tray icon")
			}
			t.Logf("native tray restore/dismiss cycle %d: same pid=%d, icon retained", cycle+1, after[0])
		}
	}
}

func openLiveEscapeIcon(t *testing.T, ctx context.Context, box *traybox.Box) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(box.Icons()) == 0 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	icons := box.Icons()
	if len(icons) != 1 {
		t.Fatalf("expected one native tray icon, got %d", len(icons))
	}
	if err := traybox.OpenIcon(ctx, icons[0], nil); err != nil {
		t.Fatal(err)
	}
}

type liveEscapeLogger struct{ t *testing.T }

func (l *liveEscapeLogger) Info(s string)  { l.t.Log(s) }
func (l *liveEscapeLogger) Warn(s string)  { l.t.Log(s) }
func (l *liveEscapeLogger) Error(s string) { l.t.Log(s) }
