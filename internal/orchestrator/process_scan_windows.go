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
	visitRunningProcesses(func(pid uint32, name string) bool {
		return !processIdentityMatches(pid, name, expectedPath, targetIdentity) || fn(pid)
	})
}

// visitRunningProcesses owns one snapshot for the whole operation. Callers
// can match several executables without enumerating the desktop once per app.
func visitRunningProcesses(visit func(uint32, string) bool) {
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
		if !visit(pe.ProcessID, windows.UTF16ToString(pe.ExeFile[:])) {
			return
		}
		if err = windows.Process32Next(hSnapshot, &pe); err != nil {
			return
		}
	}
}

// runningProcessStartsByPath returns the oldest instance of each requested
// executable. Results belong only to this check: caching across checks would
// confuse restarted processes or reused PIDs. Keys are normalized lower-case
// paths, and names only prefilter candidates; a full path must still match.
func runningProcessStartsByPath(paths []string) map[string]time.Time {
	if len(paths) == 0 {
		return nil
	}
	wanted := make(map[string][]string)
	for _, path := range paths {
		path = strings.ToLower(normalizePath(path))
		if path == "" {
			continue
		}
		base := filepath.Base(path)
		name := normalizeIdentity(strings.TrimSuffix(base, filepath.Ext(base)))
		wanted[name] = append(wanted[name], path)
	}
	if len(wanted) == 0 {
		return nil
	}
	starts := make(map[string]time.Time)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	visitRunningProcesses(func(pid uint32, name string) bool {
		name = strings.TrimSpace(name)
		candidates := wanted[normalizeIdentity(strings.TrimSuffix(name, filepath.Ext(name)))]
		if pid == 0 || len(candidates) == 0 {
			return true
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			return true
		}
		defer windows.CloseHandle(h)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
			return true
		}
		path := normalizePath(windows.UTF16ToString(buf[:size]))
		// Read identity and creation time from the same handle so a PID reused
		// during the scan cannot lend its age to a different executable.
		var creation, exit, kernel, user windows.Filetime
		if windows.GetProcessTimes(h, &creation, &exit, &kernel, &user) != nil {
			return true
		}
		started := time.Unix(0, creation.Nanoseconds())
		for _, expectedPath := range candidates {
			// Preserve EqualFold semantics for Unicode directory names;
			// lower-case map equality alone is not equivalent.
			if !executablePathsMatch(path, expectedPath) {
				continue
			}
			if previous, ok := starts[expectedPath]; !ok || started.Before(previous) {
				starts[expectedPath] = started
			}
		}
		return true
	})
	return starts
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
	return executablePathsMatch(fullPath, expectedPath)
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
