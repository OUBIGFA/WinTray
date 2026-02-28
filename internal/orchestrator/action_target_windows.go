//go:build windows

package orchestrator

func resolveOwnerChain(window ManagedWindowInfo) uintptr {
	target := window.Handle
	owner := window.OwnerHandle
	for depth := 0; depth < 8; depth++ {
		if owner == 0 || owner == target || !isWindow(owner) {
			break
		}
		target = owner
		nextOwner, _, _ := procGetWindow.Call(owner, gwOwner)
		owner = nextOwner
	}
	return target
}
