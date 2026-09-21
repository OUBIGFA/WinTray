package orchestrator

import (
	"debug/pe"
)

// imageSubsystemWindowsCUI is the PE optional-header subsystem value for
// console applications (IMAGE_SUBSYSTEM_WINDOWS_CUI).
const imageSubsystemWindowsCUI = 3

// isConsoleExecutable reports whether the file is a PE image built for the
// console subsystem. Such programs (syncthing.exe, frpc.exe, ...) own no GUI
// window: the only window they show is the console host, and closing that
// console terminates the process. Non-PE files and unreadable paths return
// false so callers fall back to the regular GUI handling.
func isConsoleExecutable(path string) bool {
	f, err := pe.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	switch hdr := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		return hdr.Subsystem == imageSubsystemWindowsCUI
	case *pe.OptionalHeader64:
		return hdr.Subsystem == imageSubsystemWindowsCUI
	default:
		return false
	}
}

// consoleExecutableCheck is the probe used by the service; tests override it
// to exercise the launch decision without shipping a real console binary.
var consoleExecutableCheck = isConsoleExecutable
