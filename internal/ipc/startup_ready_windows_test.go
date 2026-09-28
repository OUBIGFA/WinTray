//go:build windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Window 2 deliberately precedes Explorer's taskbar, just as a collector's
// same-class window can. Only the desktop (1) and its own taskbar (3) may count.
func testDesktopShell() desktopShellProbe {
	return desktopShellProbe{
		shellWindow: func() uintptr { return 1 },
		processID: func(h uintptr) uint32 {
			if h == 2 {
				return 200
			}
			return 100
		},
		nextTaskbar: func(after uintptr) uintptr {
			switch after {
			case 0:
				return 2
			case 2:
				return 3
			default:
				return 0
			}
		},
		responsive: func(context.Context, uintptr) bool { return true },
	}
}

func TestDesktopShellReadinessRejectsIncompleteOrImpostorShell(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*desktopShellProbe)
		want string
	}{
		{"no desktop", func(p *desktopShellProbe) { p.shellWindow = func() uintptr { return 0 } }, "desktop window is absent"},
		{"unknown desktop PID", func(p *desktopShellProbe) { p.processID = func(uintptr) uint32 { return 0 } }, "desktop process is unavailable"},
		{"only fake taskbar", func(p *desktopShellProbe) {
			p.nextTaskbar = func(after uintptr) uintptr {
				if after == 0 {
					return 2
				}
				return 0
			}
		}, "no Shell_TrayWnd owned by desktop pid=100"},
		{"unresponsive desktop", func(p *desktopShellProbe) {
			p.responsive = func(_ context.Context, h uintptr) bool { return h != 1 }
		}, "did not respond to WM_NULL"},
		{"unresponsive taskbar", func(p *desktopShellProbe) {
			p.responsive = func(_ context.Context, h uintptr) bool { return h != 3 }
		}, "did not respond to WM_NULL"},
		{"shell restarted during probe", func(p *desktopShellProbe) {
			calls := 0
			p.shellWindow = func() uintptr {
				calls++
				if calls == 1 {
					return 1
				}
				return 4
			}
		}, "changed during readiness probe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testDesktopShell()
			tc.edit(&p)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := p.check(ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readiness = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestStartupReadyMatchingShellPassesWithoutDelay(t *testing.T) {
	p := testDesktopShell()
	var messaged []uintptr
	p.responsive = func(_ context.Context, h uintptr) bool {
		messaged = append(messaged, h)
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// A mandatory sleep would exceed the context; successful probes must
	// pass immediately, independent of the retry polling interval.
	if err := waitStartupReady(ctx, time.Hour, func() error { return nil }, p.check); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(messaged, []uintptr{1, 3}) {
		t.Fatalf("messaged windows = %v, want desktop and its own taskbar only", messaged)
	}
}

func TestStartupReadyNeverProbesShellBeforeWinTray(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := waitStartupReady(ctx, time.Millisecond, func() error {
		return errors.New("not published")
	}, func(context.Context) error {
		t.Fatal("Shell was checked before WinTray was ready")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "WinTray ready: not published") {
		t.Fatalf("readiness = %v", err)
	}
}

func TestStartupReadyFollowsPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	markerCalls := 0
	err := waitStartupReady(ctx, time.Millisecond, func() error {
		markerCalls++
		if markerCalls == 1 {
			return errors.New("not published yet")
		}
		return nil
	}, testDesktopShell().check)
	if err != nil || markerCalls < 3 { // pending, published, still published
		t.Fatalf("readiness = %v, marker checks = %d", err, markerCalls)
	}
}

func TestStartupReadyRechecksWinTrayAfterShellWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	released := false
	err := waitStartupReady(ctx, time.Millisecond, func() error {
		if released {
			return errors.New("WinTray exited")
		}
		return nil
	}, func(context.Context) error {
		released = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "WinTray exited") {
		t.Fatalf("a stale ready marker allowed startup: %v", err)
	}
}

func TestStartupReadyFakeTaskbarTimesOutWithShellStage(t *testing.T) {
	p := testDesktopShell()
	p.nextTaskbar = func(after uintptr) uintptr {
		if after == 0 {
			return 2
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := waitStartupReady(ctx, time.Millisecond, func() error { return nil }, p.check)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "desktop Shell: no Shell_TrayWnd owned by desktop pid=100") {
		t.Fatalf("fake taskbar allowed startup or lost wait stage: %v", err)
	}
}

func TestStartupReadyCancellationCannotReleaseHelper(t *testing.T) {
	for _, duringProbe := range []bool{false, true} {
		t.Run(fmt.Sprint(duringProbe), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !duringProbe {
				cancel()
			}
			err := waitStartupReady(ctx, time.Hour, func() error {
				if !duringProbe {
					t.Fatal("probed despite prior cancellation")
				}
				return nil
			}, func(context.Context) error {
				cancel()
				return nil // even success cannot override cancellation
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled wait allowed startup: %v", err)
			}
		})
	}
}

func TestWaitStartupReadyRequiresSignaledMarkerNotJustExistence(t *testing.T) {
	name := fmt.Sprintf("WinTrayTestStartupReady-%d-%d", os.Getpid(), time.Now().UnixNano())
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateEvent(nil, 1, 0, ptr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = WaitStartupReady(ctx, name, time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "WinTray ready: marker is not signaled") {
		t.Fatalf("unsignaled marker allowed startup: %v", err)
	}
}

// Opt-in and read-only: sends WM_NULL to the existing Shell; never creates
// windows, modifies startup tasks/registry, or launches a user program.
func TestWaitDesktopShellCurrentSession(t *testing.T) {
	if os.Getenv("WINTRAY_SHELL_READINESS_TEST") != "1" {
		t.Skip("set WINTRAY_SHELL_READINESS_TEST=1 for a read-only desktop Shell check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitDesktopShell(ctx, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
