//go:build windows

package orchestrator

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func hasRunningProcessByIdentity(expectedPath, expectedName string) bool {
	expectedPath = normalizePath(expectedPath)
	targetIdentity := normalizeIdentity(expectedName)
	if expectedPath == "" && targetIdentity == "" {
		return false
	}

	hSnapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(hSnapshot)

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	if err = windows.Process32First(hSnapshot, &pe); err != nil {
		return false
	}

	for {
		if processIdentityMatches(pe.ProcessID, windows.UTF16ToString(pe.ExeFile[:]), expectedPath, targetIdentity) {
			return true
		}
		err = windows.Process32Next(hSnapshot, &pe)
		if err != nil {
			break
		}
	}

	return false
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
		if normalizeIdentity(base) == targetIdentity {
			if expectedPath == "" {
				return true
			}
			if p := queryProcessImagePath(pid); p != "" && strings.EqualFold(normalizePath(p), expectedPath) {
				return true
			}
		}
	}

	if expectedPath != "" {
		if p := queryProcessImagePath(pid); p != "" && strings.EqualFold(normalizePath(p), expectedPath) {
			return true
		}
	}

	return false
}

func queryProcessImagePath(pid uint32) string {
	hProc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(hProc)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err = windows.QueryFullProcessImageName(hProc, 0, &buf[0], &size); err != nil {
		return ""
	}
	if size == 0 {
		return ""
	}

	return windows.UTF16ToString(buf[:size])
}
