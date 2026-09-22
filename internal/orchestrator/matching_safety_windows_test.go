//go:build windows

package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wintray/internal/config"
	"wintray/internal/stringutil"
)

func TestLaunchedHiddenConsoleRemainsRecoverableDuringCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	oldLookup := consoleWindowLookup
	t.Cleanup(func() { consoleWindowLookup = oldLookup })
	consoleWindowLookup = func(lookupCtx context.Context, pid uint32, timeout time.Duration) uintptr {
		if lookupCtx.Err() != nil {
			t.Fatal("shutdown must not cancel recovery of an already hidden window")
		}
		if pid != 42 || timeout != consoleWindowWait {
			t.Fatalf("lookup pid=%d timeout=%s", pid, timeout)
		}
		return 1234
	}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
	result := svc.hostLaunchedConsole(ctx, config.ManagedAppEntry{Name: "app"}, 42)
	if result.Hidden == nil || result.Hidden.Handle != 1234 || result.Hidden.ProcessID != 42 {
		t.Fatalf("missing recovery identity: %+v", result)
	}
}

func TestWindowActionsRequireProcessIdentity(t *testing.T) {
	const expectedPath = `C:\Tools\app.exe`
	pid := uint32(42)
	for _, tc := range []struct {
		name   string
		window ManagedWindowInfo
		want   bool
	}{
		{
			name: "same name from another directory",
			window: ManagedWindowInfo{ProcessID: 77, ProcessName: "app", ProcessPath: `D:\Other\app.exe`,
				Title: "App", ClassName: "AppWindow"},
		},
		{
			name: "unknown path with matching name and title",
			window: ManagedWindowInfo{ProcessID: 77, ProcessName: "app",
				Title: "App", ClassName: "AppWindow"},
		},
		{
			name: "unrelated terminal mentioning app",
			window: ManagedWindowInfo{ProcessID: 77, ProcessName: "WindowsTerminal", ProcessPath: `C:\Terminal\WindowsTerminal.exe`,
				Title: "App", ClassName: "CASCADIA_HOSTING_WINDOW_CLASS"},
		},
		{
			name: "configured executable path",
			window: ManagedWindowInfo{ProcessID: 77, ProcessName: "app", ProcessPath: `c:\tools\APP.EXE`,
				Title: "App", ClassName: "AppWindow"},
			want: true,
		},
		{
			name: "launched process with unavailable path",
			window: ManagedWindowInfo{ProcessID: pid, ProcessName: "app",
				Title: "App", ClassName: "AppWindow"},
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Fake HWNDs are never passed to an actual window action. The manager
			// records requests, and the absent window makes verification finish.
			tc.window.Handle = 0x101
			mgr := &testManager{}
			svc := NewService(&testEnumerator{windows: []ManagedWindowInfo{tc.window}}, mgr, &testLogger{})
			_, ok := svc.manageFirstMatchingWindow(context.Background(), func(ManagedWindowInfo) bool { return true },
				normalizePath(expectedPath), "app", &pid, map[uintptr]struct{}{}, 0, "close")
			if ok != tc.want || (len(mgr.closeCalls)+len(mgr.hideCalls) > 0) != tc.want {
				t.Fatalf("managed=%t close=%v hide=%v; want window acted on=%t", ok, mgr.closeCalls, mgr.hideCalls, tc.want)
			}
		})
	}
}

func TestMatchesExecutableDoesNotIgnoreConflictingPath(t *testing.T) {
	window := ManagedWindowInfo{ProcessName: "app", ProcessPath: `D:\Other\app.exe`}
	if matchesExecutable(window, normalizePath(`C:\Tools\app.exe`), "app") {
		t.Fatal("a different executable with the same name must not suppress launch")
	}
	window.ProcessPath = ""
	if !matchesExecutable(window, normalizePath(`C:\Tools\app.exe`), "app") {
		t.Fatal("name fallback should remain available when the process path is unavailable")
	}
}

func TestProcessIdentityMatchesIgnoresPathCase(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(exe)
	if !processIdentityMatches(uint32(os.Getpid()), name, strings.ToUpper(normalizePath(exe)), normalizeIdentity(stringutil.TrimExt(name))) {
		t.Fatal("running process should match its executable path regardless of casing")
	}
}
