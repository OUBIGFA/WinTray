//go:build windows

package traybox

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

// Live tests use the same layout as the package; no production environment
// override or alternate interception path exists in the application.
func nativeTestSelfPath(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("WINTRAY_NATIVE_DIR")
	if dir == "" {
		dir = "../../dist/native"
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(dir), "WinTray.exe")
}

func attachNativeTestBridge(t *testing.T, l *listener) {
	t.Helper()
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
	t.Cleanup(func() {
		l.stop()
		if err := lease.cancel(); err != nil {
			t.Error(err)
		}
	})
	l.mu.Lock()
	var paths []string
	for path := range l.paths {
		paths = append(paths, path)
	}
	l.mu.Unlock()
	if err := lease.selectPaths(paths); err != nil {
		t.Fatal(err)
	}
	if err := lease.activate(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeBridgeArgumentLayout(t *testing.T) {
	var args bridgeArgs
	if unsafe.Sizeof(args) != 560 || unsafe.Offsetof(args.HostCreated) != 16 || unsafe.Offsetof(args.TargetCreated) != 24 ||
		unsafe.Offsetof(args.Broker) != 32 || unsafe.Offsetof(args.Taskbar) != 40 || unsafe.Offsetof(args.StopEvent) != 48 || unsafe.Offsetof(args.ReadyEvent) != 304 {
		t.Fatal("Go bridge argument ABI differs from the x86/x64 native bridge")
	}
}

func TestMissingNativeBridgeDoesNotStartCollection(t *testing.T) {
	if bridge, err := newNativeBridge(filepath.Join(t.TempDir(), "WinTray.exe"), 0); err == nil || bridge != nil {
		t.Fatal("accepted a package without native components")
	}
}
