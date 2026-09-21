//go:build windows

package orchestrator

import (
	"context"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// consoleWindowClass is the window class of a legacy conhost console window.
// Console windows created hidden (SW_HIDE) stay in conhost, so this is the
// class WinTray looks for when it needs to bring a console program back.
const consoleWindowClass = "consolewindowclass"

// consoleWindowWait bounds how long a launch waits for the hidden console
// window of a freshly started console program to exist.
const consoleWindowWait = 3 * time.Second

const stillActive = 259

var (
	consoleEnumMu       sync.Mutex
	consoleEnumTarget   uint32
	consoleEnumResult   uintptr
	consoleEnumCallback = syscall.NewCallback(consoleEnumProc)
)

// FindConsoleWindow returns the conhost window attached to the process, visible
// or not, or 0 when the process owns no console window.
func FindConsoleWindow(pid uint32) uintptr {
	if pid == 0 {
		return 0
	}
	consoleEnumMu.Lock()
	defer consoleEnumMu.Unlock()
	consoleEnumTarget = pid
	consoleEnumResult = 0
	_, _, _ = procEnumWindows.Call(consoleEnumCallback, 0)
	return consoleEnumResult
}

func consoleEnumProc(hwnd uintptr, _ uintptr) uintptr {
	var pid uint32
	_, _, _ = procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != consoleEnumTarget {
		return 1
	}
	classBuf := make([]uint16, 64)
	_, _, _ = procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&classBuf[0])), uintptr(len(classBuf)))
	if strings.ToLower(windows.UTF16ToString(classBuf)) != consoleWindowClass {
		return 1
	}
	consoleEnumResult = hwnd
	return 0
}

// waitForConsoleWindow polls for the console window of pid until it exists,
// the timeout elapses, the process ends, or the context is cancelled. A zero
// timeout performs a single lookup.
func waitForConsoleWindow(ctx context.Context, pid uint32, timeout time.Duration) uintptr {
	deadline := time.Now().Add(timeout)
	for {
		if hwnd := FindConsoleWindow(pid); hwnd != 0 {
			return hwnd
		}
		if timeout <= 0 || time.Now().After(deadline) || !processAlive(pid) {
			return 0
		}
		if !waitWithContext(ctx, 100*time.Millisecond) {
			return 0
		}
	}
}

// processAlive reports whether the process still runs; processes that cannot
// be opened are treated as alive so the caller keeps waiting.
func processAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}

// Seams for tests: the launch decision and window bookkeeping are exercised
// without real console processes.
var (
	consoleWindowLookup  = waitForConsoleWindow
	runningProcessLookup = findRunningProcessByIdentity
	windowVisibleCheck   = isWindowVisible
)
