//go:build windows

package orchestrator

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var procShellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// SHELLEXECUTEINFOW includes the unused members to preserve the Windows ABI.
type shellExecuteInfo struct {
	size, mask                        uint32
	window                            windows.Handle
	verb, file, parameters, directory *uint16
	show                              int32
	instance                          windows.Handle
	idList                            uintptr
	class                             *uint16
	classKey                          windows.Handle
	hotKey                            uint32
	monitor, process                  windows.Handle
}

func isAutoHotkeyScript(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".ahk")
}

// Use the user's file association, not a guessed AutoHotkey.exe on PATH. In
// particular, the v2 launcher selects v1/v2 from the script's requirements.
// This is an explicit script path, never a fallback for a failed .exe launch.
func startAssociatedScript(path, args, workingDirectory string, hidden bool) (*startedProcess, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr := win.CoInitializeEx(nil, win.COINIT_APARTMENTTHREADED|win.COINIT_DISABLE_OLE1DDE); hr < 0 {
		return nil, fmt.Errorf("initialize script launch COM: HRESULT 0x%08X", uint32(hr))
	}
	defer win.CoUninitialize()
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	parameters, err := windows.UTF16PtrFromString(args)
	if err != nil {
		return nil, err
	}
	directory, err := windows.UTF16PtrFromString(workingDirectory)
	if err != nil {
		return nil, err
	}
	verb, _ := windows.UTF16PtrFromString("open")
	info := shellExecuteInfo{
		size: uint32(unsafe.Sizeof(shellExecuteInfo{})),
		// NOCLOSEPROCESS retains ownership; NOASYNC is required on this worker
		// without a message loop; FLAG_NO_UI leaves error reporting to WinTray.
		mask: 0x40 | 0x100 | 0x400,
		verb: verb, file: file, parameters: parameters, directory: directory,
		show: windows.SW_SHOWNORMAL,
	}
	if hidden {
		info.show = windows.SW_HIDE
	}
	if ok, _, err := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		return nil, &os.PathError{Op: "ShellExecuteEx", Path: path, Err: err}
	}
	if info.process == 0 {
		return nil, errors.New("script association returned no process handle")
	}
	defer windows.CloseHandle(info.process)
	pid, err := windows.GetProcessId(info.process)
	if err != nil {
		return nil, err
	}
	// Keep the shell's handle alive until os.Process has acquired its own,
	// even if a short-lived script or version-selecting launcher already exited.
	process, err := os.FindProcess(int(pid))
	if err != nil {
		return nil, err
	}
	return &startedProcess{Process: process}, nil
}
