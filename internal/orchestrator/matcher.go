package orchestrator

import (
	"path/filepath"
	"strings"
	"unicode"
)

// closeAllowedScoreThreshold is the minimum confidence score required before
// any window action (close/hide) is taken. The scoring system works as follows:
//   - +1000: exact PID match (launched by us)
//   - +500:  exact executable path match
//   - +250:  process name match (case-insensitive)
//   - +200:  window not in pre-launch baseline (new window)
//   - +50:   window has a non-empty title
//   - +10:   window has a non-empty class name
//   - -80:   window has WS_EX_TOOLWINDOW style (auxiliary window)
//   - -60:   window has a non-zero owner (child/owned window)
//
// A threshold of 500 means at minimum an exact path match is required,
// or a PID match, before any action is attempted.
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

func normalizeIdentity(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r == '-' || r == '_' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func containsNormalizedIdentity(haystack, needle string) bool {
	n := normalizeIdentity(needle)
	if n == "" {
		return false
	}
	h := normalizeIdentity(haystack)
	if h == "" {
		return false
	}
	return strings.Contains(h, n)
}

func matchesExecutable(window ManagedWindowInfo, expectedExePath, expectedProcessName string) bool {
	norm := normalizePath(window.ProcessPath)
	if norm != "" && expectedExePath != "" && strings.EqualFold(norm, expectedExePath) {
		return true
	}
	return expectedProcessName != "" && strings.EqualFold(window.ProcessName, expectedProcessName)
}

func matchesExecutableWithIdentityFallback(window ManagedWindowInfo, expectedExePath, expectedProcessName string) bool {
	if matchesExecutable(window, expectedExePath, expectedProcessName) {
		return true
	}
	if containsNormalizedIdentity(window.ProcessName, expectedProcessName) {
		return true
	}
	if containsNormalizedIdentity(window.Title, expectedProcessName) {
		return true
	}
	if containsNormalizedIdentity(window.ClassName, expectedProcessName) {
		return true
	}

	return false
}

func computeCandidateScore(window ManagedWindowInfo, expectedExePath, expectedProcessName string, launchedPID *uint32, baseline map[uintptr]struct{}) int {
	score := 0
	if launchedPID != nil && window.ProcessID == *launchedPID {
		score += 1000
	}
	if p := normalizePath(window.ProcessPath); p != "" && expectedExePath != "" && strings.EqualFold(p, expectedExePath) {
		score += 500
	}
	if expectedProcessName != "" && normalizeIdentity(window.ProcessName) == normalizeIdentity(expectedProcessName) {
		score += 250
	}
	if normalizeIdentity(window.Title) == normalizeIdentity(expectedProcessName) && expectedProcessName != "" {
		score += 180
	} else if containsNormalizedIdentity(window.Title, expectedProcessName) {
		score += 90
	}
	if containsNormalizedIdentity(window.ClassName, expectedProcessName) {
		score += 40
	}
	if launchedPID != nil && isLikelyShellHost(window.ProcessName) && containsNormalizedIdentity(window.Title, expectedProcessName) {
		score += 180
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

func isLikelyShellHost(processName string) bool {
	p := strings.ToLower(strings.TrimSpace(processName))
	return p == "windowsterminal" || p == "cmd" || p == "conhost" || p == "powershell" || p == "pwsh" || p == "wt"
}

func isUnmanageableWindow(window ManagedWindowInfo) bool {
	className := strings.ToLower(strings.TrimSpace(window.ClassName))

	if className == "pseudoconsolewindow" {
		return true
	}

	if className == "tao thread event target" {
		return true
	}

	return false
}
