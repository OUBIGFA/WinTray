//go:build windows

package traybox

import (
	"sync"
	"testing"

	"github.com/lxn/win"
)

// A reentrant registration can arrive while Explorer is restoring a removed
// icon. Additions must not be hidden until that restoration has committed.
func TestSelectionAdditionDoesNotHideBeforeRemovedIconsRestore(t *testing.T) {
	f := startFakeShell(t)
	oldPath, newPath := `c:\old.exe`, `c:\new.exe`
	owners := &sync.Map{}
	owners.Store(uint32(f.program), hostedIconOwner{path: oldPath})
	owners.Store(uint32(f.target), hostedIconOwner{path: newPath})
	l, err := startListener(listenerConfig{className: "WinTraySelectionTransactionTest", hostedOwners: owners,
		target: func(win.HWND) win.HWND { return f.target }}, "", []string{oldPath}, quietLogger{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { f.setReply(nil); l.stop() }()
	old := trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 1}
	send(l, old)
	f.clear()
	f.setReply(func(d trayData) uintptr {
		if d.HWnd == uint32(f.program) && isShown(d) {
			return 0
		}
		return 1
	})
	f.setRequestHook(func(d trayData) {
		if d.HWnd == uint32(f.program) && isShown(d) {
			f.setRequestHook(nil)
			send(l, trayRequest{message: nimAdd, hwnd: uint32(f.target), uid: 2})
		}
	})
	if err := l.updatePaths([]string{newPath}); err == nil {
		t.Fatal("failed removal was accepted as a completed selection")
	}
	requests, _, _ := f.snapshot()
	found := false
	for _, request := range requests {
		if request.HWnd == uint32(f.target) {
			found = true
			if isHidden(request) {
				t.Fatal("new program's icon was hidden before the selection could commit")
			}
		}
	}
	if !found || !l.selected(oldPath) || l.selected(newPath) {
		t.Fatal("failed transaction did not preserve the old selection and native addition")
	}
}
