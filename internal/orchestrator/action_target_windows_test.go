//go:build windows

package orchestrator

import (
	"runtime"
	"syscall"
	"testing"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Legacy GUI frameworks often give a visible form an invisible application
// owner that also receives the native tray callbacks. Closing that owner is
// not the same operation as clicking the form's close button.
func TestActionTargetsTheMatchedWindowNotItsApplicationOwner(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := syscall.StringToUTF16Ptr("STATIC")
	owner := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 1, 1, 0, 0, 0, nil)
	if owner == 0 {
		t.Fatal("create test application owner")
	}
	defer win.DestroyWindow(owner)
	form := win.CreateWindowEx(0, class, nil, win.WS_POPUP, 0, 0, 1, 1, owner, 0, 0, nil)
	if form == 0 {
		t.Fatal("create test owned form")
	}
	defer win.DestroyWindow(form)
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(form), &pid); err != nil {
		t.Fatal(err)
	}
	window := ManagedWindowInfo{Handle: uintptr(form), OwnerHandle: uintptr(owner), ProcessID: pid}
	if got := resolveActionTargetHandle(window); got != uintptr(form) {
		t.Fatalf("action target = 0x%X, want matched form 0x%X, not application owner 0x%X", got, form, owner)
	}
}
