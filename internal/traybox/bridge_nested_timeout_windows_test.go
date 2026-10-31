//go:build windows

package traybox

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

// Exercise the real receiving DLL and nested SendMessageTimeout stacks, not
// a fake pause state. Two owners enter the same shell thread; the inner call
// times out first, then the older outer call may time out while unwinding.
func TestShellBridgeNestedTimeoutRestoresIcons(t *testing.T) {
	if os.Getenv("WINTRAY_SHELL_BRIDGE_TEST") != "1" {
		t.Skip("build/native.ps1 -Test and WINTRAY_SHELL_BRIDGE_TEST=1")
	}
	for _, arch := range []string{"x64", "x86"} {
		for _, outerTimeout := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/outer-timeout=%t", arch, outerTimeout), func(t *testing.T) {
				shell := startNativeClient(t, nativeClientPath(t, arch), "--shell")
				targetArg := strconv.FormatUint(uint64(shell.hwnd), 10)
				first := startNativeClient(t, nativeClientPath(t, arch), "--target", targetArg)
				second := startNativeClient(t, nativeClientPath(t, arch), "--target", targetArg)
				l, err := startListener(listenerConfig{className: brokerWindowClass, native: true,
					target: func(win.HWND) win.HWND { return shell.hwnd }}, "", nil, quietLogger{})
				if err != nil {
					t.Fatal(err)
				}
				defer l.close()
				created, err := processCreation(shell.pid)
				if err != nil {
					t.Fatal(err)
				}
				hostCreated, err := processCreation(uint32(os.Getpid()))
				if err != nil {
					t.Fatal(err)
				}
				n := &nativeBridge{broker: l.hwnd, host: bridgeTarget{pid: uint32(os.Getpid()), created: hostCreated},
					dir: filepath.Dir(filepath.Dir(nativeClientPath(t, arch))), cached: map[string]string{}}
				lease, err := n.attach(shellEndpoint{bridgeTarget{shell.pid, created, shell.path}, shell.hwnd})
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					// Check restoration while the route is still active. Cancelling
					// it first would mask the stuck-pause bug by bypassing the DLL.
					if err := l.close(); err != nil {
						t.Errorf("restore icons after nested timeout: %v", err)
					}
					if err := lease.cancel(); err != nil {
						t.Error(err)
					}
				}()
				if err := lease.selectPaths([]string{first.path}); err != nil {
					t.Fatal(err)
				}
				if err := l.updatePaths([]string{first.path}); err != nil {
					t.Fatal(err)
				}
				if err := lease.activate(); err != nil {
					t.Fatal(err)
				}
				log := &nestedTimeoutLogger{t: t, outerTimeout: outerTimeout, resumed: make(chan struct{}),
					outer: "uid=101 message=0", inner: "uid=102 message=0"}
				log.reenter = func() {
					if got := second.request(t, win.WM_APP+110, 102, 0); got != 73 {
						t.Errorf("inner native result=%d, want 73", got)
					}
					// Token 2 is paused. A request for token 1 must not claim
					// that there is no pause (reply 2), even before stack unwind.
					if reply := nativeResumeProbe(t, l, shell.hwnd, 1); reply != 0 {
						t.Errorf("different active pause reported as resumed: reply=%d", reply)
					}
				}
				l.logger = log
				if got := first.request(t, win.WM_APP+110, 101, 0); got != 73 {
					t.Errorf("outer native result=%d, want 73", got)
				}
				select {
				case <-log.resumed:
				case <-time.After(3 * time.Second):
					t.Fatal("nested timeout did not resume the original route")
				}
				if got := shell.request(t, clientStats, 5, 0); got != 2 {
					t.Errorf("native requests replayed or lost: %d, want 2", got)
				}
				if got := first.request(t, win.WM_APP+110, 103, 0); got != 73 {
					t.Errorf("post-resume native result=%d, want 73", got)
				}
				found := false
				for _, icon := range l.snapshot() {
					found = found || icon.uid == 103
				}
				if !found {
					t.Error("route reported recovery but still bypasses the collector")
				}
				if !lease.alive() {
					t.Error("nested timeout replaced the receiving lease")
				}
			})
		}
	}
}

func nativeResumeProbe(t *testing.T, l *listener, shell win.HWND, token uint64) uintptr {
	t.Helper()
	var wire [8]byte
	binary.LittleEndian.PutUint64(wire[:], token)
	cds := copyDataStruct{bridgeResumeData, uint32(len(wire)), uintptr(unsafe.Pointer(&wire[0]))}
	var reply uintptr
	var pin runtime.Pinner
	pin.Pin(&wire[0])
	pin.Pin(&cds)
	pin.Pin(&reply)
	defer pin.Unpin()
	ok, _, err := procSendMessageTimeoutW.Call(uintptr(shell), win.WM_COPYDATA, uintptr(l.hwnd), uintptr(unsafe.Pointer(&cds)),
		smtoAbortIfHung, 500, uintptr(unsafe.Pointer(&reply)))
	if ok == 0 {
		t.Errorf("probe pause acknowledgement: %v", err)
	}
	return reply
}

type nestedTimeoutLogger struct {
	t            *testing.T
	reenter      func()
	outerTimeout bool
	outer, inner string
	entered      atomic.Bool
	delayed      atomic.Bool
	notified     atomic.Bool
	resumed      chan struct{}
}

func (l *nestedTimeoutLogger) Info(s string) {
	l.t.Log(s)
	if strings.Contains(s, l.outer) && l.entered.CompareAndSwap(false, true) {
		l.reenter()
		if l.outerTimeout {
			time.Sleep(800 * time.Millisecond)
		}
	}
	if strings.Contains(s, l.inner) && l.delayed.CompareAndSwap(false, true) {
		time.Sleep(800 * time.Millisecond)
	}
}

func (l *nestedTimeoutLogger) Warn(s string) {
	l.t.Log(s)
	if strings.Contains(s, "delayed request recovered") && l.notified.CompareAndSwap(false, true) {
		close(l.resumed)
	}
}
