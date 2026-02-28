//go:build !windows

package orchestrator

func resolveOwnerChain(window ManagedWindowInfo) uintptr {
	return window.Handle
}
