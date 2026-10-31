//go:build windows

package traybox

import (
	"fmt"
	"os"
	"testing"
)

// The isolated regression checks exact native replies and packet counts;
// this opt-in companion verifies Explorer's actual icon visibility as well.
// Only disposable fixture programs are collected, never user applications.
func TestShellBridgeLiveNestedTimeoutRestoresIcons(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("interactive live opt-in with WinTray closed")
	}
	for _, arch := range []string{"x64", "x86"} {
		t.Run(arch, func(t *testing.T) {
			first := startNativeClient(t, nativeClientPath(t, arch), "--icon")
			second := startNativeClient(t, nativeClientPath(t, arch), "--icon")
			observer := nativeObserver(t)
			log := &nestedTimeoutLogger{t: t, resumed: make(chan struct{})}
			b := NewBox(nativeTestSelfPath(t), log)
			defer func() {
				if err := b.Close(); err != nil {
					t.Error(err)
				}
			}()
			// Keep instrumentation disarmed while collecting the initial icons.
			log.entered.Store(true)
			log.delayed.Store(true)
			if err := b.SetPaths([]string{first.path}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "both icons collected", func() bool {
				return len(b.Icons()) == 2 && !observer.visibleIcon(t, first.hwnd, false) && !observer.visibleIcon(t, second.hwnd, false)
			})
			first.request(t, clientDelete, 0, 0)
			second.request(t, clientDelete, 0, 0)
			log.outer = fmt.Sprintf("hwnd=0x%X uid=71 message=0", first.hwnd)
			log.inner = fmt.Sprintf("hwnd=0x%X uid=71 message=0", second.hwnd)
			log.outerTimeout = true
			log.reenter = func() {
				if second.request(t, clientAdd, 0, 0) == 0 {
					t.Error("nested owner lost native registration")
				}
			}
			log.entered.Store(false)
			log.delayed.Store(false)
			if first.request(t, clientAdd, 0, 0) == 0 {
				t.Fatal("outer owner lost native registration")
			}
			if !log.entered.Load() || !log.delayed.Load() {
				t.Fatal("both nested requests must reach the delayed collector")
			}
			// SendMessageTimeout can keep pumping a nested call without timing
			// out. Check native state, not the presence of a recovery log line.
			// Token zero only probes; it cannot resume an active generation.
			waitFor(t, "unpaused route with both native icons collected", func() bool {
				return nativeResumeProbe(t, b.listener, explorerTray(0), 0) == 2 && len(b.Icons()) == 2 &&
					!observer.visibleIcon(t, first.hwnd, false) && !observer.visibleIcon(t, second.hwnd, false)
			})
			if first.request(t, clientModify, 1, 0) == 0 || second.request(t, clientModify, 2, 0) == 0 {
				t.Fatal("native icon updates failed after nested timeout")
			}
			waitFor(t, "both new icon updates reach the collector", func() bool {
				var firstUpdated, secondUpdated bool
				for _, icon := range b.Icons() {
					firstUpdated = firstUpdated || icon.owner == uint32(first.hwnd) && icon.Tooltip == "native update 1"
					secondUpdated = secondUpdated || icon.owner == uint32(second.hwnd) && icon.Tooltip == "native update 2"
				}
				return firstUpdated && secondUpdated
			})
			if err := b.Close(); err != nil {
				t.Fatalf("restore after nested timeout: %v", err)
			}
			waitFor(t, "both original icons visible after close", func() bool {
				return observer.visibleIcon(t, first.hwnd, false) && observer.visibleIcon(t, second.hwnd, false)
			})
		})
	}
}
