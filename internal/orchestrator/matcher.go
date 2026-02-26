package orchestrator

import (
	"path/filepath"
	"strings"
)

const closeAllowedScoreThreshold = 500

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	p := strings.Trim(path, " \t\r\n\"")
	if p == "" {
		return ""
	}
	full, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return strings.TrimRight(full, `\\/`)
}

func matchesExecutable(window ManagedWindowInfo, expectedExePath, expectedProcessName string) bool {
	norm := normalizePath(window.ProcessPath)
	if norm != "" && expectedExePath != "" && strings.EqualFold(norm, expectedExePath) {
		return true
	}
	return expectedProcessName != "" && strings.EqualFold(window.ProcessName, expectedProcessName)
}

func computeCandidateScore(window ManagedWindowInfo, expectedExePath, expectedProcessName string, launchedPID *uint32, baseline map[uintptr]struct{}) int {
	score := 0
	if launchedPID != nil && window.ProcessID == *launchedPID {
		score += 1000
	}
	if p := normalizePath(window.ProcessPath); p != "" && expectedExePath != "" && strings.EqualFold(p, expectedExePath) {
		score += 500
	}
	if expectedProcessName != "" && strings.EqualFold(window.ProcessName, expectedProcessName) {
		score += 250
	}
	if baseline != nil {
		if _, ok := baseline[window.Handle]; !ok {
			score += 200
		}
	}
	if window.Title != "" {
		score += 50
	}
	if window.ClassName != "" {
		score += 10
	}
	if window.IsToolWindow {
		score -= 80
	}
	if window.OwnerHandle != 0 {
		score -= 60
	}
	return score
}
