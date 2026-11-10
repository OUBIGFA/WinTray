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
	if len(wanted) == 0 {
		return nil
	}
	out := make(map[uint32]string)
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	// Browsers and other multi-process apps share image paths. Resolve each
	// image only once per snapshot, with no stale cache across restarts or
	// junction changes. The large query buffer also belongs to this scan.
	resolved := make(map[string]string)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		image := processImagePath(entry.ProcessID, buf)
		if image == "" {
			continue
		}
		path, ok := resolved[image]
		if !ok {
			path = canonicalIconPath(image)
			resolved[image] = path
		}
		if wanted[path] {
			out[entry.ProcessID] = path
		}
	}
	return out
}

func processPath(pid uint32) string {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	return canonicalIconPath(processImagePath(pid, buf))
}

// processImagePath leaves canonicalization to the caller so a snapshot can
// reuse both its path buffer and the resolution of repeated image paths.
func processImagePath(pid uint32, buf []uint16) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func canonicalIconPath(path string) string {
	if path == "" {
		return ""
	}
	// Resolve the opened file, including directory junctions. EvalSymlinks
	// does not follow mount-point reparse tags on current Go versions, so a
	// configured junction alias otherwise never matches the process image.
	// Zero access and full sharing avoid requiring file-read privileges or
	// blocking an application update while querying its identity.
	if name, err := windows.UTF16PtrFromString(path); err == nil {
		h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err == nil {
			buf := make([]uint16, windows.MAX_LONG_PATH+1)
			n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
			windows.CloseHandle(h)
			if err == nil && n < uint32(len(buf)) {
				path = windows.UTF16ToString(buf[:n])
				if strings.HasPrefix(path, `\\?\UNC\`) {
					path = `\\` + path[len(`\\?\UNC\`):]
				} else {
					path = strings.TrimPrefix(path, `\\?\`)
				}
			}
		}
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
