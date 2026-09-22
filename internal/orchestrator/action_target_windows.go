//go:build windows

package orchestrator

import "golang.org/x/sys/windows"

func resolveOwnerChain(window ManagedWindowInfo) uintptr {
	target := window.Handle
	owner := window.OwnerHandle
	for depth := 0; depth < 8; depth++ {
		if owner == 0 || owner == target || !isWindow(owner) {
			break
		}
		var ownerPID uint32
		if _, err := windows.GetWindowThreadProcessId(windows.HWND(owner), &ownerPID); err != nil || ownerPID != window.ProcessID {
			break
		}
		target = owner
		nextOwner, _, _ := procGetWindow.Call(owner, gwOwner)
		owner = nextOwner
	}
	return target
}
