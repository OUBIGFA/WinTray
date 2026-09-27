//go:build windows

package traybox

import (
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// hwndMessage is HWND_MESSAGE: many tray icons belong to message-only windows.
const hwndMessage = ^uintptr(2)

// processesByPath maps the IDs of running processes to their lower-case
// image path, for the wanted lower-case paths only.
func processesByPath(wanted map[string]bool) map[uint32]string {
	out := make(map[uint32]string)
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if path := processPath(entry.ProcessID); wanted[path] {
			out[entry.ProcessID] = path
		}
	}
	return out
}

func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return ""
	}
	return canonicalIconPath(windows.UTF16ToString(buf[:size]))
}

func canonicalIconPath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return strings.ToLower(filepath.Clean(path))
}

var (
	enumMu      sync.Mutex
	enumResult  []win.HWND
	enumWindows = syscall.NewCallback(func(hwnd win.HWND, _ uintptr) uintptr {
		enumResult = append(enumResult, hwnd)
		return 1
	})
)

// allWindows returns top-level and message-only windows.
func allWindows() []win.HWND {
	enumMu.Lock()
	enumResult = nil
	_ = windows.EnumWindows(enumWindows, nil)
	out := enumResult
	enumResult = nil
	enumMu.Unlock()
	var after uintptr
	for {
		hwnd, _, _ := procFindWindowExW.Call(hwndMessage, after, 0, 0)
		if hwnd == 0 {
			return out
		}
		out = append(out, win.HWND(hwnd))
		after = hwnd
	}
}
