//go:build windows

package orchestrator

// resolveActionTargetHandle preserves the actual window selected for closing.
// GW_OWNER is not a parent/control relationship: legacy applications often
// own their forms with an invisible application window that also services
// their tray icon. Sending SC_CLOSE to that owner can remove the icon or end
// the program instead of closing the form into its native tray.
func resolveActionTargetHandle(window ManagedWindowInfo) uintptr {
	return window.Handle
}
