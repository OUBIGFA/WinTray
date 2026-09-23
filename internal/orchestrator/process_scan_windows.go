//go:build windows

package orchestrator

import (
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func hasRunningProcessByIdentity(expectedPath, expectedName string) bool {
	return findRunningProcessByIdentity(expectedPath, expectedName) != 0
}

// findRunningProcessByIdentity returns the PID of the first process matching
// the executable identity, or 0 when none is running.
func findRunningProcessByIdentity(expectedPath, expectedName string) uint32 {
	var found uint32
	forEachRunningProcessByIdentity(expectedPath, expectedName, func(pid uint32) bool {
		found = pid
		return false
	})
	return found
}

// earliestRunningProcessStart returns the creation time of the oldest running
// process matching the executable identity. Programs that spawn helpers from
// their own image (browsers, Electron apps) are thus anchored on their main
// process.
func earliestRunningProcessStart(expectedPath, expectedName string) (time.Time, bool) {
	var earliest time.Time
	forEachRunningProcessByIdentity(expectedPath, expectedName, func(pid uint32) bool {
		if started, ok := processStartTime(pid); ok && (earliest.IsZero() || started.Before(earliest)) {
			earliest = started
		}
		return true
	})
	return earliest, !earliest.IsZero()
}

// forEachRunningProcessByIdentity calls fn for every running process matching
// the executable identity until fn returns false.
func forEachRunningProcessByIdentity(expectedPath, expectedName string, fn func(pid uint32) bool) {
	expectedPath = normalizePath(expectedPath)
	targetIdentity := normalizeIdentity(expectedName)
	if expectedPath == "" && targetIdentity == "" {
		return
	}

	hSnapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(hSnapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err = windows.Process32First(hSnapshot, &pe); err != nil {
		return
	}

	for {
		if processIdentityMatches(pe.ProcessID, windows.UTF16ToString(pe.ExeFile[:]), expectedPath, targetIdentity) && !fn(pe.ProcessID) {
			return
		}
		if err = windows.Process32Next(hSnapshot, &pe); err != nil {
			return
		}
	}
}

func processIdentityMatches(pid uint32, exeName, expectedPath, targetIdentity string) bool {
	if pid == 0 {
		return false
	}

	if targetIdentity != "" {
		base := strings.TrimSpace(exeName)
		if ext := filepath.Ext(base); ext != "" {
			base = base[:len(base)-len(ext)]
		}
		if normalizeIdentity(base) != targetIdentity {
			return false
		}
		if expectedPath == "" {
			return true
		}
	}

	if expectedPath == "" {
		return false
	}

	fullPath := processExecutablePath(pid)
	if fullPath == "" {
		return false
	}
	return strings.EqualFold(normalizePath(fullPath), expectedPath)
}

func processExecutablePath(pid uint32) string {
	hProcess, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(hProcess)

	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err = windows.QueryFullProcessImageName(hProcess, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// processStartTime reports when the process was created. Processes that
// cannot be opened report false.
func processStartTime(pid uint32) (time.Time, bool) {
	hProcess, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(hProcess)

	var creation, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(hProcess, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, creation.Nanoseconds()), true
}

// Seams for tests: process ages are simulated without real processes.
var (
	processStartLookup        = processStartTime
	runningProcessStartLookup = earliestRunningProcessStart
)
