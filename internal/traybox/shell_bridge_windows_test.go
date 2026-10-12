//go:build windows

package traybox

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

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
			started = time.Now()
			if result := client.request(t, win.WM_APP+110, 0, 0); result != 0 {
				t.Fatal("stalled selected request claimed success")
			}
			if time.Since(started) > time.Second || lease.alive() {
				t.Fatal("selected stall did not promptly cancel route")
			}
			if result := client.request(t, win.WM_APP+110, 0, 0); result != 73 {
				t.Fatal("cancelled route did not pass native requests")
			}
			close(stall.release)
			waitFor(t, "subclass removed after timeout", func() bool {
				prop, _, _ := user32.NewProc("GetPropW").Call(uintptr(shell.hwnd), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("WinTray.ShellBridge.Route.v3"))))
				return prop == 0
			})
			if result := l.sendRequest(0, withHidden(trayRequest{hwnd: uint32(client.hwnd), uid: 900, message: nimAdd}.bytes())); result != 0 {
				t.Fatal("late hide reached shell after route expiry")
			}
		})
	}
}
