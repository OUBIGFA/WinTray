//go:build windows

package tray

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func TestBackgroundHostingHasNoIndividualIconAndReportsRemoval(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	path, err := filepath.Abs("../../build/WinTray.exe.manifest")
	if err != nil {
		t.Fatal(err)
	}
	source, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := win.CreateActCtx(&win.ACTCTX{Source: source})
	if ctx == win.HANDLE(windows.InvalidHandle) {
		t.Fatal("create test activation context")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	defer kernel.NewProc("ReleaseActCtx").Call(uintptr(ctx))
	cookie, ok := win.ActivateActCtx(ctx)
	if !ok {
		t.Fatal("activate test manifest")
	}
	defer kernel.NewProc("DeactivateActCtx").Call(0, cookie)
	host := NewHost("zh-CN", testLogger{}, nil)
	pid := uint32(os.Getpid())
	item, err := host.newItem(HostedWindow{Name: "background test", ProcessID: pid, Background: true})
	if err != nil {
		t.Fatal(err)
	}
	if item.icon != nil || item.show != nil || item.unregisterIcon != nil || item.form == nil {
		t.Fatal("background mode created an individual tray icon/menu")
	}
	host.items[pid] = item
	// Another individually hosted program remains after the background one
	// exits. This must still refresh the main icon's residency decision.
	host.items[pid+1] = &hostedItem{info: HostedWindow{ProcessID: pid + 1}, stop: make(chan struct{})}
	changed, emptied := 0, 0
	host.SetOnChange(func() { changed++ })
	host.SetOnEmpty(func() { emptied++ })
	host.SetLanguage("en-US")
	if !host.HasBackgroundWindows() || host.Count() != 2 {
		t.Fatal("background program lost its recovery owner")
	}
	host.remove(item, true)
	if host.HasBackgroundWindows() || host.Count() != 1 || changed != 1 || emptied != 0 {
		t.Fatalf("background removal did not refresh residency: count=%d changes=%d empty=%d", host.Count(), changed, emptied)
	}
}
