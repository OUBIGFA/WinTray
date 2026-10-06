//go:build windows

package ui

import (
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"path/filepath"
	"testing"
)

// Tray-only tests still exercise actual Shell_NotifyIcon and common controls.
func discardStaleMessages() {
	var msg win.MSG
	for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) && msg.Message != win.WM_QUIT && msg.Message != win.WM_PAINT {
	}
}
func activateTestManifest(t *testing.T) func() {
	t.Helper()
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
		t.Fatal("create common-controls context")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	cookie, ok := win.ActivateActCtx(ctx)
	if !ok {
		kernel.NewProc("ReleaseActCtx").Call(uintptr(ctx))
		t.Fatal("activate common-controls manifest")
	}
	return func() {
		kernel.NewProc("DeactivateActCtx").Call(0, cookie)
		kernel.NewProc("ReleaseActCtx").Call(uintptr(ctx))
	}
}
