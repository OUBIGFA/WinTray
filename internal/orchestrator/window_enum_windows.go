//go:build windows

package orchestrator

import (
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                     = syscall.NewLazyDLL("user32.dll")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procGetClassNameW          = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procGetWindow              = user32.NewProc("GetWindow")
	procGetWindowLongPtrW      = user32.NewProc("GetWindowLongPtrW")
	procIsIconic               = user32.NewProc("IsIconic")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	enumWindowsCallback        = syscall.NewCallback(enumWindowsProc)
)

const (
	gwOwner        = 4
	wsExToolWindow = 0x00000080
)

var gwlExStyle = int32(-20)

type Win32WindowEnumerator struct{}

var enumHandlesMu sync.Mutex
var activeEnumHandles []uintptr

func NewWin32WindowEnumerator() *Win32WindowEnumerator { return &Win32WindowEnumerator{} }

func (e *Win32WindowEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo {
	return enumerateTopLevelWindows(false)
}

// Explicit Show Window also needs hidden windows; startup matching retains its
// visible-only enumeration so this action does not change automatic closing.
func enumerateTopLevelWindows(includeHidden bool) []ManagedWindowInfo {
	handles := enumerateTopLevelHandles()
	result := make([]ManagedWindowInfo, 0, len(handles))
	foreground, _, _ := procGetForegroundWindow.Call()
	processes := make(map[uint32][2]string)
	for _, hwnd := range handles {
		if !includeHidden && !isWindowVisible(hwnd) {
			continue
		}
		result = append(result, windowInfo(hwnd, foreground, processes))
	}
	return result
}

// Only the callback's handle collection needs serialization. GetWindowText*
// sends messages to our own window threads; holding this lock while reading
// titles would deadlock a UI thread that checks running state during rendering.
func enumerateTopLevelHandles() []uintptr {
	enumHandlesMu.Lock()
	defer enumHandlesMu.Unlock()
	defer func() { activeEnumHandles = nil }()
	_, _, _ = procEnumWindows.Call(enumWindowsCallback, 0)
	return activeEnumHandles
}

func enumWindowsProc(hwnd uintptr, _ uintptr) uintptr {
	activeEnumHandles = append(activeEnumHandles, hwnd)
	return 1
}

// windowInfo also describes hidden UI while a startup shield is active.
// The ordinary desktop enumerator above deliberately remains visible-only.
func windowInfo(hwnd, foreground uintptr, processes map[uint32][2]string) ManagedWindowInfo {
	titleLen, _, _ := procGetWindowTextLengthW.Call(hwnd)
	titleBuf := make([]uint16, titleLen+1)
	_, _, _ = procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&titleBuf[0])), uintptr(len(titleBuf)))

	classBuf := make([]uint16, 256)
	_, _, _ = procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&classBuf[0])), uintptr(len(classBuf)))

	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlExStyle))
	isTool := (exStyle & wsExToolWindow) != 0
	isMin, _, _ := procIsIconic.Call(hwnd)

	var pid uint32
	_, _, _ = procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	info, ok := processes[pid]
	if !ok {
		info = [2]string{}
		if pid != 0 {
			info[0], info[1] = processInfo(pid)
		}
		if processes != nil {
			processes[pid] = info
		}
	}

	return ManagedWindowInfo{
		Handle:       hwnd,
		ProcessID:    pid,
		ProcessName:  info[0],
		ProcessPath:  info[1],
		Title:        windows.UTF16ToString(titleBuf),
		ClassName:    windows.UTF16ToString(classBuf),
		IsVisible:    isWindowVisible(hwnd),
		IsMinimized:  isMin != 0,
		IsForeground: hwnd == foreground,
		OwnerHandle:  owner,
		IsToolWindow: isTool,
	}
}

func processInfo(pid uint32) (string, string) {
	if pid == 0 {
		return "", ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	sz := uint32(len(buf))
	err = windows.QueryFullProcessImageName(h, 0, &buf[0], &sz)
	if err != nil {
		return "", ""
	}
	path := windows.UTF16ToString(buf[:sz])
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	if ext != "" {
		name = name[:len(name)-len(ext)]
	}
	return name, path
}
