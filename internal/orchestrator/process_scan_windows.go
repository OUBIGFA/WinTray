//go:build windows

package orchestrator

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func hasRunningProcessByIdentity(expectedPath, expectedName string) bool {
	return findRunningProcessByIdentity(expectedPath, expectedName) != 0
}

// findRunningProcessByIdentity returns the PID of the first process matching
// the executable identity, or 0 when none is running.
func findRunningProcessByIdentity(expectedPath, expectedName string) uint32 {
	expectedPath = normalizePath(expectedPath)
	targetIdentity := normalizeIdentity(expectedName)
	if expectedPath == "" && targetIdentity == "" {
		return 0
	}

	hSnapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(hSnapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err = windows.Process32First(hSnapshot, &pe); err != nil {
		return 0
	}

	for {
		if processIdentityMatches(pe.ProcessID, windows.UTF16ToString(pe.ExeFile[:]), expectedPath, targetIdentity) {
			return pe.ProcessID
		}
		err = windows.Process32Next(hSnapshot, &pe)
		if err != nil {
			break
		}
	}

	return 0
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
	return normalizePath(fullPath) == expectedPath
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
