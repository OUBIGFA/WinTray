//go:build windows

package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// startNativeConsole leaves standard handles to Windows. exec.Cmd substitutes
// NUL for unspecified streams (and os.StartProcess always sets
// STARTF_USESTDHANDLES), which discards output even when a console exists.
// CREATE_NEW_CONSOLE without that flag preserves native console I/O, including
// output written while hidden. No helper shell, pipe or log renderer is needed.
func startNativeConsole(cmd *exec.Cmd, hidden bool) (*os.Process, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	application, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return nil, err
	}
	line := windows.ComposeCommandLine(cmd.Args)
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.CmdLine != "" {
		// cmd /s /c needs its own outer quote pair; do not re-escape it.
		line = cmd.SysProcAttr.CmdLine
	}
	commandLine, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return nil, err
	}
	var directory *uint16
	if cmd.Dir != "" {
		directory, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return nil, err
		}
	}
	info := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	if hidden {
		info.Flags, info.ShowWindow = windows.STARTF_USESHOWWINDOW, windows.SW_HIDE
	}
	var process windows.ProcessInformation
	// Inherit the environment, not WinTray's handles. Windows creates the
	// console's input and output handles in the child, even for a GUI parent.
	if err := windows.CreateProcess(application, commandLine, nil, nil, false,
		createNewConsole, nil, directory, &info, &process); err != nil {
		return nil, &os.PathError{Op: "CreateProcess", Path: cmd.Path, Err: err}
	}
	defer windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
	// Keep the original process handle alive until os.Process has acquired its
	// own handle, including for programs that exit immediately after launch.
	return os.FindProcess(int(process.ProcessId))
}

// hasNativeConsole keeps pythonw's GUI semantics. Programs that never create a
// console cannot recover output retroactively by displaying a new empty one.
func hasNativeConsole(path string) bool {
	return isCmdScript(path) || isPowerShellScript(path) ||
		strings.EqualFold(filepath.Ext(path), ".py") || consoleExecutableCheck(path)
}
