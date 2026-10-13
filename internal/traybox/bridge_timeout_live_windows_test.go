//go:build windows

package traybox

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lxn/win"
)

// Pause only the broker, never Explorer. A real Shell32 owner must retain a
// working registration, then be collected again automatically on this lease.
func TestShellBridgeLiveRecoversTransientBrokerStall(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("interactive live opt-in with WinTray closed")
	}
	for _, arch := range []string{"x64", "x86"} {
		t.Run(arch, func(t *testing.T) {
			client := startNativeClient(t, nativeClientPath(t, arch), "--icon")
			observer := nativeObserver(t)
			before := currentTaskbar(t)
			log := &pausableBridgeLogger{t: t}
			b := NewBox(nativeTestSelfPath(t), log)
			defer func() {
				if err := b.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := b.SetPaths([]string{client.path}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "initial collection", func() bool { return len(b.Icons()) == 1 && !observer.visibleIcon(t, client.hwnd, false) })
			for cycle := 0; cycle < 3; cycle++ {
				refreshes := client.request(t, clientStats, 2, 0)
				client.request(t, clientDelete, 0, 0)
				entered, release := log.arm()
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				completed := make(chan uintptr, 1)
				// Delay the real native ADD, not a synthetic icon record. The
				// owner must receive native success even while Go is paused.
				go func() { completed <- client.request(t, clientAdd, 0, 0) }()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("broker did not pause")
				}
				started := time.Now()
				if got := <-completed; got == 0 {
					t.Fatal("timeout lost native registration")
				}
				if got := client.request(t, clientModify, uintptr(10+cycle), 0); got == 0 {
					t.Fatal("timeout lost native update")
				}
				t.Logf("native registration/update completed after %s", time.Since(started))
				if got := currentTaskbar(t); got != before {
					t.Fatal("taskbar changed during pause")
				}
				close(release)
				t.Cleanup(func() {
					if t.Failed() {
						t.Logf("refreshes=%d baseline=%d installed=%d", client.request(t, clientStats, 2, 0), refreshes, client.request(t, clientStats, 3, 0))
					}
				})
				waitFor(t, "automatic recollection after delay", func() bool {
					icons := b.Icons()
					return len(icons) >= 1 && client.request(t, clientStats, 2, 0) > refreshes && !observer.visibleIcon(t, client.hwnd, false) && client.request(t, clientStats, 3, 0) != 0
				})
				if client.request(t, clientModify, uintptr(20+cycle), 0) == 0 {
					t.Fatal("update after recovery failed")
				}
				waitFor(t, "current native data after recovery", func() bool {
					for _, icon := range b.Icons() {
						if icon.uid == 71 {
							return icon.Tooltip == fmt.Sprintf("native update %d", 20+cycle)
						}
					}
					return false
				})
				var icon Icon
				for _, item := range b.Icons() {
					if item.uid == 71 {
						icon = item
					}
				}
				if err := Activate(icon, ActionClick); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "recovered click opens actual window", func() bool { return win.IsWindowVisible(client.hwnd) })
				client.request(t, clientHide, 0, 0)
			}
			// Owner state changes while the broker is paused must reach the
			// restoration snapshot before an immediate Close. Otherwise the
			// collector would resurrect an owner-hidden/deleted native icon.
			refreshes := client.request(t, clientStats, 2, 0)
			client.request(t, clientDelete, 0, 0)
			entered, release := log.arm()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			done := make(chan uintptr, 1)
			go func() { done <- client.request(t, clientAdd, 0, 0) }()
			<-entered
			if <-done == 0 {
				t.Fatal("native registration failed")
			}
			if client.request(t, win.WM_APP+112, 1, 0) == 0 {
				t.Fatal("native hidden state rejected")
			}
			client.request(t, clientDelete, 0, 0)
			close(release)
			waitFor(t, "deleted record reconciled after pause", func() bool {
				return client.request(t, clientStats, 2, 0) > refreshes && len(b.Icons()) == 0
			})
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if observer.visibleIcon(t, client.hwnd, false) {
				t.Fatal("shutdown resurrected owner-deleted icon")
			}
			if client.request(t, clientAdd, 0, 0) == 0 {
				t.Fatal("owner could not register natively after recovery")
			}
			waitFor(t, "original icon restored", func() bool { return observer.visibleIcon(t, client.hwnd, false) })
			if currentTaskbar(t) != before {
				t.Fatal("taskbar changed after recovery")
			}
		})
	}
}

type pausableBridgeLogger struct {
	t                *testing.T
	mu               sync.Mutex
	entered, release chan struct{}
}

func (l *pausableBridgeLogger) arm() (chan struct{}, chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entered = make(chan struct{})
	l.release = make(chan struct{})
	return l.entered, l.release
}
func (l *pausableBridgeLogger) Info(s string) {
	l.t.Log(s)
	l.mu.Lock()
	entered, release := l.entered, l.release
	l.entered = nil
	l.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
}
func (l *pausableBridgeLogger) Warn(s string) { l.t.Log(s) }
