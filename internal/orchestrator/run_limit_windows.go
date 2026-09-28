//go:build windows

package orchestrator

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"wintray/internal/config"
	"wintray/internal/stringutil"
)

// RunLimitCheck is the outcome of one pass over scheduled run times.
type RunLimitCheck struct {
	// Running counts programs with a run time that are still running;
	// WinTray has to stay until it has ended them.
	Running int
	// Next is when the earliest of them is due, zero when none is running.
	Next time.Time
}

// EndOverduePrograms ends every program that has run longer than its
// scheduled run time. Like the closing delay, the run time counts from the
// creation of the program's oldest process, whoever started it. The program
// ends for good: all of its processes and the processes they started.
func (s *Service) EndOverduePrograms(entries []config.ManagedAppEntry, now time.Time) RunLimitCheck {
	var check RunLimitCheck
	for path := range ambiguousRunLimitPaths(entries) {
		expectedName := stringutil.TrimExt(filepath.Base(path))
		if _, ok := runningProcessStartLookup(path, expectedName); !ok {
			continue
		}
		// The executable identity cannot distinguish these entries, so ending
		// it could kill an instance belonging to the other schedule/arguments.
		// Keep the run-limit worker alive rather than reporting completion.
		check.Running++
		if next := now.Add(30 * time.Second); check.Next.IsZero() || next.Before(check.Next) {
			check.Next = next
		}
		s.logger.Warn(fmt.Sprintf("scheduled run time skipped: executable %s has ambiguous duplicate entries (different arguments or limits); process left running", path))
	}
	for _, entry := range uniqueRunLimitEntries(entries) {
		limit := time.Duration(config.ScheduledRunLimit(entry)) * time.Minute
		expectedPath := normalizePath(entry.ExePath)
		expectedName := stringutil.TrimExt(filepath.Base(entry.ExePath))
		started, ok := runningProcessStartLookup(expectedPath, expectedName)
		if !ok {
			continue
		}
		due := started.Add(limit)
		if now.Before(due) {
			check.Running++
			if check.Next.IsZero() || due.Before(check.Next) {
				check.Next = due
			}
			continue
		}
		var pids []uint32
		forEachRunningProcessByIdentity(expectedPath, expectedName, func(pid uint32) bool {
			pids = append(pids, pid)
			return true
		})
		ended := s.terminateProcesses(pids)
		s.logger.Info(fmt.Sprintf("scheduled run time over: %s ran %s (limit %s), ended %d processes", entry.Name, now.Sub(started).Round(time.Second), limit, ended))
		if s.hasExistingManagedProcess(expectedPath, expectedName) {
			// TerminateProcess is asynchronous and can succeed for only some
			// instances (for example an elevated sibling may be inaccessible).
			// Observe survivors instead of treating any success as completion.
			check.Running++
			if check.Next.IsZero() || now.Before(check.Next) {
				check.Next = now
			}
		}
	}
	return check
}

// uniqueRunLimitEntries returns one entry per executable only when duplicate
// entries describe the same invocation and limit. Ambiguous groups are omitted
// because process identity cannot tell their instances apart.
func uniqueRunLimitEntries(entries []config.ManagedAppEntry) []config.ManagedAppEntry {
	unique := make([]config.ManagedAppEntry, 0, len(entries))
	indices := make(map[string]int)
	ambiguous := ambiguousRunLimitPaths(entries)
	for _, entry := range entries {
		limit := config.ScheduledRunLimit(entry)
		if limit <= 0 || entry.ExePath == "" {
			continue
		}
		path := strings.ToLower(normalizePath(entry.ExePath))
		if ambiguous[path] {
			continue
		}
		if _, ok := indices[path]; ok {
			continue
		}
		indices[path] = len(unique)
		unique = append(unique, entry)
	}
	return unique
}

// ambiguousRunLimitPaths identifies duplicate executable entries whose launch
// arguments or effective run limits differ and at least one has a run limit.
// Unlimited duplicates need no polling and must not keep WinTray resident.
// The process matcher has no argument-level identity, so neither limited
// entry in an ambiguous group can be safely terminated.
func ambiguousRunLimitPaths(entries []config.ManagedAppEntry) map[string]bool {
	type runLimitSignature struct {
		args  string
		limit int
	}
	signatures := make(map[string]runLimitSignature)
	ambiguous := make(map[string]bool)
	for _, entry := range entries {
		limit := config.ScheduledRunLimit(entry)
		if entry.ExePath == "" {
			continue
		}
		path := strings.ToLower(normalizePath(entry.ExePath))
		signature := runLimitSignature{args: entry.Args, limit: limit}
		if previous, ok := signatures[path]; ok && previous != signature && (previous.limit > 0 || limit > 0) {
			ambiguous[path] = true
			continue
		}
		signatures[path] = signature
	}
	return ambiguous
}

// terminateProcessTrees ends the given processes and every process started
// by them. A child is only followed when it was created after its parent, so
// a reused process ID never takes an unrelated program down.
func terminateProcessTrees(roots []uint32) int {
	children := map[uint32][]uint32{}
	if snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0); err == nil {
		var pe windows.ProcessEntry32
		pe.Size = uint32(unsafe.Sizeof(pe))
		for err = windows.Process32First(snapshot, &pe); err == nil; err = windows.Process32Next(snapshot, &pe) {
			if pe.ProcessID != pe.ParentProcessID {
				children[pe.ParentProcessID] = append(children[pe.ParentProcessID], pe.ProcessID)
			}
		}
		windows.CloseHandle(snapshot)
	}
	seen := map[uint32]bool{}
	var tree []uint32
	var walk func(pid uint32)
	walk = func(pid uint32) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		tree = append(tree, pid)
		parentStart, parentKnown := processStartTime(pid)
		for _, child := range children[pid] {
			if childStart, ok := processStartTime(child); ok && parentKnown && !childStart.Before(parentStart) {
				walk(child)
			}
		}
	}
	for _, pid := range roots {
		walk(pid)
	}
	ended := 0
	for _, pid := range tree {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			continue
		}
		if windows.TerminateProcess(h, 1) == nil {
			ended++
		}
		windows.CloseHandle(h)
	}
	return ended
}
