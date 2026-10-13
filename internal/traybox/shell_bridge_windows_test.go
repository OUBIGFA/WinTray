//go:build windows

package traybox

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lxn/win"
)

// A disposable private shell proves the actual cross-process hook, dormant
// handshake, return-leg unwrapping and duplicate rejection before live use.
func TestShellBridgeIsolatedReceivingEndpoint(t *testing.T) {
	if os.Getenv("WINTRAY_SHELL_BRIDGE_TEST") != "1" {
		t.Skip("build/native.ps1 -Test and WINTRAY_SHELL_BRIDGE_TEST=1")
	}
	for _, arch := range []string{"x64", "x86"} {
		t.Run(arch, func(t *testing.T) {
			shell := startNativeClient(t, nativeClientPath(t, arch), "--shell")
			client := startNativeClient(t, nativeClientPath(t, arch), "--target", strconv.FormatUint(uint64(shell.hwnd), 10))
			l, err := startListener(listenerConfig{className: brokerWindowClass, target: func(win.HWND) win.HWND { return shell.hwnd }, native: true}, "", nil, quietLogger{})
			if err != nil {
				t.Fatal(err)
			}
			defer l.stop()
			created, err := processCreation(shell.pid)
			if err != nil {
				t.Fatal(err)
			}
			hostCreated, err := processCreation(uint32(os.Getpid()))
			if err != nil {
				t.Fatal(err)
			}
			native := &nativeBridge{broker: l.hwnd, host: bridgeTarget{pid: uint32(os.Getpid()), created: hostCreated}, dir: filepath.Dir(filepath.Dir(nativeClientPath(t, arch))), cached: map[string]string{}}
			target := shellEndpoint{bridgeTarget{shell.pid, created, shell.path}, shell.hwnd}
			lease, err := native.attach(target)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { l.stop(); lease.cancel() }()
			if result := client.request(t, win.WM_APP+110, 0, 0); result != 73 {
				t.Fatalf("dormant result %d", result)
			}
			if err := lease.activate(); err != nil {
				t.Fatal(err)
			}
			if result := client.request(t, win.WM_APP+110, 0, 0); result != 73 {
				t.Fatalf("roundtrip result %d", result)
			}
			duplicate, err := native.attach(target)
			if duplicate != nil {
				duplicate.cancel()
			}
			if err == nil {
				t.Fatal("duplicate accepted")
			}
			if err := lease.selectPaths([]string{client.path}); err != nil {
				t.Fatal(err)
			}
			if err := l.updatePaths([]string{client.path}); err != nil {
				t.Fatal(err)
			}
			if result := client.request(t, win.WM_APP+110, 0, 0); result != 73 || len(l.snapshot()) != 1 {
				t.Fatalf("collection result=%d count=%d", result, len(l.snapshot()))
			}
			if nativeBridgeLoaded(t, client.pid) {
				t.Fatal("injected client")
			}
			// Stall the broker after attachment. Neither unrelated tray packets
			// nor ordinary taskbar messages may wait on it (issue #4 boundary).
			image, err := os.ReadFile(client.path)
			if err != nil {
				t.Fatal(err)
			}
			otherPath := filepath.Join(t.TempDir(), "unselected.exe")
			if err := os.WriteFile(otherPath, image, 0600); err != nil {
				t.Fatal(err)
			}
			other := startNativeClient(t, otherPath, "--target", strconv.FormatUint(uint64(shell.hwnd), 10))
			stall := &stalledBrokerLogger{entered: make(chan struct{}), release: make(chan struct{})}
			l.logger = stall
			defer func() {
				select {
				case <-stall.release:
				default:
					close(stall.release)
				}
			}()
			go func() { send(l, trayRequest{message: nimAdd, hwnd: uint32(client.hwnd), uid: 900}) }()
			select {
			case <-stall.entered:
			case <-time.After(time.Second):
				t.Fatal("broker did not stall")
			}
			started := time.Now()
			if result := other.request(t, win.WM_APP+110, 0, 0); result != 73 {
				t.Fatal("unselected packet changed")
			}
			shell.request(t, clientStats, 0, 0)
			if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
				t.Fatalf("unrelated taskbar traffic waited for broker: %s", elapsed)
			}
			count := shell.request(t, clientStats, 5, 0)
			started = time.Now()
			if result := client.request(t, win.WM_APP+110, 11, 0); result != 73 {
				t.Fatalf("unhandled slow request did not reach native shell: %d", result)
			}
			if time.Since(started) > time.Second || !lease.alive() {
				t.Fatal("single slow request disabled collection or blocked taskbar")
			}
			if result := client.request(t, win.WM_APP+110, 22, 0); result != 73 {
				t.Fatal("paused native update failed")
			}
			close(stall.release)
			waitFor(t, "broker responsive again", func() bool { return win.SendMessage(l.hwnd, win.WM_NULL, 0, 0) == 0 })
			time.Sleep(350 * time.Millisecond)
			if got := shell.request(t, clientStats, 5, 0); got != count+2 {
				t.Fatalf("slow packet replayed: count=%d want=%d", got, count+2)
			}
			if uid := shell.request(t, clientStats, 6, 0); uid != 22 {
				t.Fatalf("late request overwrote newer native update: uid=%d", uid)
			}
			if result := client.request(t, win.WM_APP+110, 33, 0); result != 73 {
				t.Fatal("collection did not resume after transient delay")
			}
			if !lease.alive() || len(l.snapshot()) == 0 {
				t.Fatal("working route was lost")
			}
			found := false
			for _, icon := range l.snapshot() {
				if icon.uid == 33 {
					found = true
				}
			}
			if !found {
				t.Fatal("resumed request bypassed collection")
			}
			// The original shell can also be slow after Go has forwarded. It
			// must not receive a second registration when the outer call times out.
			count = shell.request(t, clientStats, 5, 0)
			shell.request(t, win.WM_APP+111, 700, 0)
			if result := client.request(t, win.WM_APP+110, 44, 0); result != 73 {
				t.Fatalf("slow shell result=%d", result)
			}
			time.Sleep(350 * time.Millisecond)
			if got := shell.request(t, clientStats, 5, 0); got != count+1 {
				t.Fatalf("slow shell duplicate count=%d", got)
			}
			if !lease.alive() {
				t.Fatal("slow native receiver destroyed lease")
			}
		})
	}
}
