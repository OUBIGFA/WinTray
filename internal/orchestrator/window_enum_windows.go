//go:build windows

package orchestrator

import (
	"path/filepath"
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
)

const (
	gwOwner        = 4
	wsExToolWindow = 0x00000080
)

var gwlExStyle = int32(-20)

type Win32WindowEnumerator struct{}

func NewWin32WindowEnumerator() *Win32WindowEnumerator { return &Win32WindowEnumerator{} }

func (e *Win32WindowEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo {
	result := make([]ManagedWindowInfo, 0)
	foreground, _, _ := procGetForegroundWindow.Call()
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		v, _, _ := procIsWindowVisible.Call(hwnd)
		if v == 0 {
			return 1
		}

		titleLen, _, _ := procGetWindowTextLengthW.Call(hwnd)
		titleBuf := make([]uint16, titleLen+1)
		if len(titleBuf) > 0 {
			_, _, _ = procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&titleBuf[0])), uintptr(len(titleBuf)))
		}

		classBuf := make([]uint16, 256)
		_, _, _ = procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&classBuf[0])), uintptr(len(classBuf)))

		owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
		exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlExStyle))
		isTool := (exStyle & wsExToolWindow) != 0
		isMin, _, _ := procIsIconic.Call(hwnd)

		var pid uint32
		_, _, _ = procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		pname, ppath := processInfo(pid)

		item := ManagedWindowInfo{
			Handle:       hwnd,
			ProcessID:    pid,
			ProcessName:  pname,
			ProcessPath:  ppath,
			Title:        windows.UTF16ToString(titleBuf),
			ClassName:    windows.UTF16ToString(classBuf),
			IsVisible:    true,
			IsMinimized:  isMin != 0,
			IsForeground: hwnd == foreground,
			OwnerHandle:  owner,
			IsToolWindow: isTool,
		}
		result = append(result, item)
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return result
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
