//go:build windows

package traybox

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const (
	clientAdd     = win.WM_APP + 100
	clientModify  = win.WM_APP + 101
	clientVersion = win.WM_APP + 102
	clientDelete  = win.WM_APP + 103
	clientRect    = win.WM_APP + 104
	clientStats   = win.WM_APP + 105
	clientTaskbar = win.WM_APP + 106
	clientHide    = win.WM_APP + 107
	clientProbe   = win.WM_APP + 108
	clientLayout  = win.WM_APP + 109
)

type nativeTestClient struct {
	path string
	hwnd win.HWND
	pid  uint32
	cmd  *exec.Cmd
	done chan error
}

func nativeClientPath(t *testing.T, arch string) string {
	t.Helper()
	dir := os.Getenv("WINTRAY_NATIVE_CLIENT_DIR")
	if dir == "" {
		dir = "../../_temp/native-tests"
	}
	path, err := filepath.Abs(filepath.Join(dir, arch, "WinTray-TrayClient.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("build native fixtures with build/native.ps1 -Test: %v", err)
	}
	return path
}

func startNativeClient(t *testing.T, path string, args ...string) *nativeTestClient {
	t.Helper()
	c := &nativeTestClient{path: path, cmd: exec.Command(path, args...), done: make(chan error, 1)}
	c.cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	output, err := c.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c.cmd.Stderr = os.Stderr
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { c.done <- c.cmd.Wait() }()
	t.Cleanup(func() {
		win.PostMessage(c.hwnd, win.WM_CANCELMODE, 0, 0)
		win.PostMessage(c.hwnd, win.WM_CLOSE, 0, 0)
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			_ = c.cmd.Process.Kill()
			<-c.done
		}
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		var hwnd uint64
		if _, err := fmt.Sscan(line, &hwnd, &c.pid); err != nil || hwnd == 0 {
			t.Fatalf("native fixture startup: %q %v", line, err)
		}
		c.hwnd = win.HWND(hwnd)
	case <-time.After(5 * time.Second):
		t.Fatal("native fixture did not start")
	}
	return c
}

func nativeObserver(t *testing.T) *nativeTestClient {
	t.Helper()
	path := nativeClientPath(t, "x64")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same filename, different full path: selection must not inject a
	// similarly named observer or any other unselected process.
	copyPath := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.WriteFile(copyPath, image, 0o600); err != nil {
		t.Fatal(err)
	}
	return startNativeClient(t, copyPath)
}

func (c *nativeTestClient) request(t *testing.T, msg uint32, wp, lp uintptr) uintptr {
	t.Helper()
	var result uintptr
	var pinned runtime.Pinner
	pinned.Pin(&result)
	defer pinned.Unpin()
	ok, _, err := procSendMessageTimeoutW.Call(uintptr(c.hwnd), uintptr(msg), wp, lp, smtoAbortIfHung, 3000,
		uintptr(unsafe.Pointer(&result)))
	if ok == 0 {
		t.Fatalf("native fixture request %d did not complete: %v", msg, err)
	}
	return result
}

func (c *nativeTestClient) visibleIcon(t *testing.T, owner win.HWND, guid bool) bool {
	// Once the receiving-end bridge is active, every Shell32 rectangle
	// request (including the observer's) gets the collected click anchor.
	// Probe Explorer through the broker's return leg, not that anchor.
	taskbar := explorerTray(0)
	property, _, _ := user32.NewProc("GetPropW").Call(uintptr(taskbar), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("WinTray.ShellBridge.Route.v3"))))
	if property != 0 {
		l := &listener{hwnd: win.HWND(property), cfg: listenerConfig{native: true, target: explorerTray}}
		request := trayRequest{hwnd: uint32(owner), uid: 71}
		if guid {
			var pid uint32
			win.GetWindowThreadProcessId(owner, &pid)
			request.flags = nifGUID
			request.guid = [16]byte{byte(pid), byte(pid >> 8), byte(pid >> 16), byte(pid >> 24), 0x21, 0x79, 0xa0, 0x45, 0x9c, 0x37, 0x30, 0x11, 0x01, 0x09, 0x17, 0x24}
		}
		return l.shownByExplorer(request.bytes())
	}
	uid := uintptr(71)
	if guid {
		uid |= 0x80000000
	}
	return c.request(t, clientProbe, uintptr(owner), uid) != 0
}

func nativeBridgeLoaded(t *testing.T, pid uint32) bool {
	t.Helper()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snap)
	entry := windows.ModuleEntry32{Size: uint32(unsafe.Sizeof(windows.ModuleEntry32{}))}
	for err := windows.Module32First(snap, &entry); err == nil; err = windows.Module32Next(snap, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.Module[:]), bridgeDLL) {
			return true
		}
	}
	return false
}

func hasNativePopup(pid uint32) bool {
	for _, hwnd := range allWindows() {
		var owner uint32
		win.GetWindowThreadProcessId(hwnd, &owner)
		if owner != pid || !win.IsWindowVisible(hwnd) {
			continue
		}
		var class [64]uint16
		n, _ := win.GetClassName(hwnd, &class[0], len(class))
		if n != 0 && windows.UTF16ToString(class[:]) == "#32768" {
			return true
		}
	}
	return false
}

func TestNativeBridgeLiveCrossBitnessCollection(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	for _, tc := range []struct {
		arch       string
		ansi, guid bool
	}{
		{"x64", false, false}, {"x64", true, true},
		{"x86", false, true}, {"x86", true, false},
	} {
		t.Run(fmt.Sprintf("%s/ansi=%t/guid=%t", tc.arch, tc.ansi, tc.guid), func(t *testing.T) {
			args := []string{"--icon"}
			if tc.ansi {
				args = append(args, "--ansi")
			}
			if tc.guid {
				args = append(args, "--guid")
			}
			client := startNativeClient(t, nativeClientPath(t, tc.arch), args...)
			observer := nativeObserver(t)
			before := currentTaskbar(t)
			waitFor(t, "original native icon", func() bool { return observer.visibleIcon(t, client.hwnd, tc.guid) })
			b := NewBox(nativeTestSelfPath(t), testLogger{t})
			defer func() {
				if err := b.Close(); err != nil {
					t.Error(err)
				}
			}()
			for cycle := 0; cycle < 2; cycle++ {
				if err := b.SetPaths([]string{client.path}); err != nil {
					t.Fatal(err)
				}
				// The broker records before forwarding (to handle reentrancy),
				// so list membership alone cannot prove Explorer has hidden it.
				win.KillTimer(b.listener.hwnd, timerHeal)
				t.Cleanup(func() {
					if t.Failed() {
						t.Logf("fixture still registered=%d refreshes=%d", client.request(t, clientStats, 3, 0), client.request(t, clientStats, 2, 0))
					}
				})
				waitFor(t, "existing native icon recollected and hidden", func() bool {
					return len(b.Icons()) == 1 && !observer.visibleIcon(t, client.hwnd, tc.guid)
				})
				if nativeBridgeLoaded(t, observer.pid) || nativeBridgeLoaded(t, client.pid) {
					t.Fatal("receiving-end bridge was loaded into an application")
				}
				for _, process := range []*nativeTestClient{client, observer} {
					if win.HWND(process.request(t, clientTaskbar, 0, 0)) != before.hwnd ||
						win.HWND(process.request(t, clientLayout, 0, 0)) != before.composition {
						t.Fatal("a selected/unselected process found a different taskbar layout")
					}
				}
				if got := currentTaskbar(t); got != before {
					t.Fatalf("native taskbar geometry/parent changed: %+v -> %+v", before, got)
				}
				icons := b.Icons()
				if icons[0].Image == nil || !icons[0].Clickable() {
					t.Fatal("native image/callback was lost")
				}
				pixels := bytes.Clone(icons[0].Image.Pix)
				if client.request(t, clientModify, 1, 0) == 0 {
					t.Fatal("native update was rejected")
				}
				icons = b.Icons()
				if len(icons) != 1 || icons[0].Tooltip != "native update 1" || bytes.Equal(pixels, icons[0].Image.Pix) {
					t.Fatal("native icon/tooltip did not update in collection")
				}
				for _, version := range []uintptr{0, 3, 4} {
					if client.request(t, clientVersion, version, 0) == 0 || client.request(t, clientRect, 0, 0) == 0 {
						t.Fatal("native version/rectangle query failed")
					}
					if err := Activate(b.Icons()[0], ActionClick); err != nil {
						t.Fatal(err)
					}
					waitFor(t, "native callback actually opens window", func() bool { return win.IsWindowVisible(client.hwnd) })
					client.request(t, clientHide, 0, 0)
				}
				if err := Activate(b.Icons()[0], ActionRightClick); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "program's real native tray menu", func() bool { return hasNativePopup(client.pid) })
				win.PostMessage(client.hwnd, win.WM_CANCELMODE, 0, 0)
				waitFor(t, "native menu closed", func() bool { return client.request(t, clientStats, 4, 0) == 0 })
				if err := b.SetPaths(nil); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "deselected icon restored", func() bool { return observer.visibleIcon(t, client.hwnd, tc.guid) })
				if client.request(t, clientModify, uintptr(2+cycle), 0) == 0 || len(b.Icons()) != 0 {
					t.Fatal("dormant bridge did not pass native updates back to Explorer")
				}
			}
		})
	}
}

func TestNativeBridgeLiveDiscoversExternalProcessRestart(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	path := nativeClientPath(t, "x64")
	observer := nativeObserver(t)
	b := NewBox(nativeTestSelfPath(t), quietLogger{})
	defer b.Close()
	if err := b.SetPaths([]string{path}); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 2; cycle++ {
		client := startNativeClient(t, path, "--icon")
		waitFor(t, "externally started process collected and hidden", func() bool {
			return len(b.Icons()) == 1 && !observer.visibleIcon(t, client.hwnd, false)
		})
		client.request(t, clientDelete, 0, 0)
		waitFor(t, "deleted icon removed from collection", func() bool { return len(b.Icons()) == 0 })
		if client.request(t, clientAdd, 0, 0) == 0 {
			t.Fatal("native re-add failed")
		}
		waitFor(t, "native re-add collected", func() bool { return len(b.Icons()) == 1 })
		win.PostMessage(client.hwnd, win.WM_CLOSE, 0, 0)
		waitFor(t, "exited process removed from collection", func() bool { return !isWindow(client.hwnd) && len(b.Icons()) == 0 })
	}
}

// A successful LoadLibrary/Attach is only preparation. Until the controller
// confirms the helper's result and commits the selection, packets must still
// go straight to Explorer, including when the selected program was restarted.
func TestNativeBridgeLiveDefersPacketsUntilLeaseActivated(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	for _, arch := range []string{"x64", "x86"} {
		t.Run(arch, func(t *testing.T) {
			client := startNativeClient(t, nativeClientPath(t, arch))
			observer := nativeObserver(t)
			l, err := startListener(listenerConfig{className: brokerWindowClass, target: explorerTray, native: true}, "", []string{client.path}, quietLogger{})
			if err != nil {
				t.Fatal(err)
			}
			defer l.stop()
			// Observe the registration itself, not the periodic visibility
			// repair. Explorer can publish its new layout after replying.
			win.KillTimer(l.hwnd, timerHeal)
			native, err := newNativeBridge(nativeTestSelfPath(t), l.hwnd)
			if err != nil {
				t.Fatal(err)
			}
			target, err := currentShellEndpoint()
			if err != nil {
				t.Fatal(err)
			}
			lease, err := native.attach(target)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := l.stop(); err != nil {
					t.Error(err)
				}
				if lease != nil {
					if err := lease.cancel(); err != nil {
						t.Error(err)
					}
				}
			}()
			if client.request(t, clientAdd, 0, 0) == 0 {
				t.Fatal("native icon registration failed")
			}
			if len(l.snapshot()) != 0 || !observer.visibleIcon(t, client.hwnd, false) {
				t.Fatal("unconfirmed attachment hid a native icon before its lease was activated")
			}
			// Model a timed-out/killed helper: the DLL is loaded, but the host
			// cancels without acknowledging. Later owner updates stay native.
			err = lease.cancel()
			lease = nil
			if err != nil {
				t.Fatal(err)
			}
			if client.request(t, clientModify, 1, 0) == 0 || len(l.snapshot()) != 0 || !observer.visibleIcon(t, client.hwnd, false) {
				t.Fatal("cancelled preparation intercepted an owner's later update")
			}
			lease, err = native.attach(target)
			if err != nil {
				t.Fatal(err)
			}
			if client.request(t, clientModify, 2, 0) == 0 || len(l.snapshot()) != 0 || !observer.visibleIcon(t, client.hwnd, false) {
				t.Fatal("reattaching implicitly activated the new lease")
			}
			if err := lease.selectPaths([]string{client.path}); err != nil {
				t.Fatal(err)
			}
			if err := lease.activate(); err != nil {
				t.Fatal(err)
			}
			result := client.request(t, clientAdd, 0, 0)
			count := len(l.snapshot())
			if result == 0 || count != 1 {
				t.Fatalf("acknowledged lease: native result=%d collected=%d", result, count)
			}
			started := time.Now()
			waitFor(t, "acknowledged native icon hidden without healing", func() bool {
				return !observer.visibleIcon(t, client.hwnd, false)
			})
			if elapsed := time.Since(started); elapsed > 10*time.Millisecond {
				t.Logf("Explorer published hidden icon state after %s", elapsed)
			}
		})
	}
}

// A repeated or late helper must not replace a working route with a dormant
// one, even when it carries the same host PID and broker window identity.
func TestNativeBridgeLiveRejectsDuplicateLease(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	for _, arch := range []string{"x64", "x86"} {
		t.Run(arch, func(t *testing.T) {
			client := startNativeClient(t, nativeClientPath(t, arch))
			l, err := startListener(listenerConfig{className: brokerWindowClass, target: explorerTray, native: true}, "", []string{client.path}, quietLogger{})
			if err != nil {
				t.Fatal(err)
			}
			defer l.stop()
			native, err := newNativeBridge(nativeTestSelfPath(t), l.hwnd)
			if err != nil {
				t.Fatal(err)
			}
			target, err := currentShellEndpoint()
			if err != nil {
				t.Fatal(err)
			}
			lease, err := native.attach(target)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { l.stop(); lease.cancel() }()
			for _, active := range []bool{false, true} {
				if active {
					if err := lease.selectPaths([]string{client.path}); err != nil {
						t.Fatal(err)
					}
					if err := lease.activate(); err != nil {
						t.Fatal(err)
					}
					if client.request(t, clientAdd, 0, 0) == 0 {
						t.Fatal("original lease did not accept registration")
					}
				}
				duplicate, err := native.attach(target)
				if duplicate != nil {
					duplicate.cancel()
				}
				if err == nil || duplicate != nil {
					t.Fatalf("duplicate attachment replaced the original lease (active=%t)", active)
				}
			}
			if client.request(t, clientModify, 9, 0) == 0 {
				t.Fatal("rejected duplicate disrupted native updates")
			}
			icons := l.snapshot()
			if len(icons) != 1 || icons[0].Tooltip != "native update 9" {
				t.Fatal("rejected duplicate replaced the original route")
			}
		})
	}
}

func TestNativeBridgeLiveRejectsReusedProcessIdentity(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	for _, arch := range []string{"x64", "x86"} {
		client := startNativeClient(t, nativeClientPath(t, arch))
		l, err := startListener(listenerConfig{className: brokerWindowClass, target: explorerTray}, "", nil, quietLogger{})
		if err != nil {
			t.Fatal(err)
		}
		native, err := newNativeBridge(nativeTestSelfPath(t), l.hwnd)
		if err != nil {
			t.Fatal(err)
		}
		created, err := processCreation(client.pid)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := native.attach(shellEndpoint{bridgeTarget{client.pid, created + 1, client.path}, client.hwnd})
		if err == nil || lease != nil || nativeBridgeLoaded(t, client.pid) {
			t.Fatal("wrong process creation time was allowed to load the bridge")
		}
		if err := l.stop(); err != nil {
			t.Fatal(err)
		}
	}
}
