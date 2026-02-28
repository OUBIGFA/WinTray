package orchestrator

import (
	"testing"

	"wintray/internal/config"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  ", ""},
		{`"C:\Program Files\app.exe"`, ``}, // after trim quotes, should resolve
		{`C:\Foo\Bar\`, ``},                // trailing backslash stripped
	}
	for _, tc := range tests {
		got := normalizePath(tc.input)
		if tc.input == "" || tc.input == "  " {
			if got != "" {
				t.Errorf("normalizePath(%q) = %q, want empty", tc.input, got)
			}
			continue
		}
		// For non-empty inputs, just ensure no trailing slash
		if len(got) > 0 && (got[len(got)-1] == '\\' || got[len(got)-1] == '/') {
			t.Errorf("normalizePath(%q) = %q, has trailing slash", tc.input, got)
		}
	}
}

func TestMatchesExecutable(t *testing.T) {
	window := ManagedWindowInfo{
		ProcessName: "notepad",
		ProcessPath: `C:\Windows\System32\notepad.exe`,
	}

	if !matchesExecutable(window, normalizePath(`C:\Windows\System32\notepad.exe`), "notepad") {
		t.Error("expected exact path + name match")
	}
	if !matchesExecutable(window, "", "notepad") {
		t.Error("expected process name match")
	}
	if !matchesExecutable(window, "", "Notepad") {
		t.Error("expected case-insensitive process name match")
	}
	if matchesExecutable(window, "", "chrome") {
		t.Error("should not match different process name")
	}
}

func TestMatchesExecutableWithIdentityFallback(t *testing.T) {
	window := ManagedWindowInfo{
		ProcessName: "electron",
		Title:       "My Notepad App",
		ClassName:   "Chrome_WidgetWin_1",
	}

	if !matchesExecutableWithIdentityFallback(window, "", "electron") {
		t.Error("expected exact process name match")
	}
	if !matchesExecutableWithIdentityFallback(window, "", "notepad") {
		t.Error("expected title fallback match for 'notepad'")
	}
	if matchesExecutableWithIdentityFallback(window, "", "firefox") {
		t.Error("should not match 'firefox'")
	}
}

func TestComputeCandidateScore(t *testing.T) {
	pid := uint32(1234)
	baseline := map[uintptr]struct{}{100: {}}

	// Window with exact PID match, path match, name match, new window, has title+class
	window := ManagedWindowInfo{
		Handle:      200,
		ProcessID:   1234,
		ProcessName: "app",
		ProcessPath: `C:\Program Files\app.exe`,
		Title:       "My App",
		ClassName:   "AppWindow",
	}
	expectedPath := normalizePath(`C:\Program Files\app.exe`)
	score := computeCandidateScore(window, expectedPath, "app", &pid, baseline)

	// PID(1000) + path(500) + name(250) + new(200) + title(50) + class(10) = 2010
	if score != 2010 {
		t.Errorf("expected score 2010, got %d", score)
	}

	// Window in baseline → no +200 bonus
	windowInBaseline := window
	windowInBaseline.Handle = 100
	score2 := computeCandidateScore(windowInBaseline, expectedPath, "app", &pid, baseline)
	if score2 != 1810 {
		t.Errorf("expected score 1810 for baseline window, got %d", score2)
	}

	// Tool window penalty
	toolWindow := window
	toolWindow.IsToolWindow = true
	score3 := computeCandidateScore(toolWindow, expectedPath, "app", &pid, baseline)
	if score3 != 2010-80 {
		t.Errorf("expected score %d for tool window, got %d", 2010-80, score3)
	}

	// Owned window penalty
	ownedWindow := window
	ownedWindow.OwnerHandle = 999
	score4 := computeCandidateScore(ownedWindow, expectedPath, "app", &pid, baseline)
	if score4 != 2010-60 {
		t.Errorf("expected score %d for owned window, got %d", 2010-60, score4)
	}
}

func TestComputeCandidateScore_BelowThreshold(t *testing.T) {
	// Window with only process name match + title + class = 250+50+10 = 310 < 500
	window := ManagedWindowInfo{
		Handle:      300,
		ProcessID:   9999,
		ProcessName: "app",
		Title:       "App",
		ClassName:   "Win",
	}
	score := computeCandidateScore(window, `C:\Other\different.exe`, "app", nil, nil)
	if score >= closeAllowedScoreThreshold {
		t.Errorf("expected score below threshold %d, got %d", closeAllowedScoreThreshold, score)
	}
}

func TestIsUnmanageableWindow(t *testing.T) {
	tests := []struct {
		name      string
		window    ManagedWindowInfo
		unmanage  bool
	}{
		{
			name:     "pseudoconsole",
			window:   ManagedWindowInfo{ClassName: "PseudoConsoleWindow"},
			unmanage: true,
		},
		{
			name:     "tao thread",
			window:   ManagedWindowInfo{ClassName: "tao thread event target"},
			unmanage: true,
		},
		{
			name:     "normal window",
			window:   ManagedWindowInfo{ClassName: "Chrome_WidgetWin_1"},
			unmanage: false,
		},
		{
			name:     "empty class",
			window:   ManagedWindowInfo{ClassName: ""},
			unmanage: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isUnmanageableWindow(tc.window)
			if got != tc.unmanage {
				t.Errorf("isUnmanageableWindow(%+v) = %v, want %v", tc.window, got, tc.unmanage)
			}
		})
	}
}

func TestMatchStrategy(t *testing.T) {
	withTitle := ManagedWindowInfo{Title: "Hello", ClassName: "Win"}
	noTitle := ManagedWindowInfo{Title: "", ClassName: "Win"}
	noClass := ManagedWindowInfo{Title: "Hello", ClassName: ""}
	empty := ManagedWindowInfo{}

	// MatchAny always true
	if !matchStrategy(empty, config.MatchAny) {
		t.Error("MatchAny should return true for any window")
	}

	// Empty strategy defaults to MatchAny
	if !matchStrategy(empty, "") {
		t.Error("empty strategy should return true")
	}

	// MatchProcessNameThenTitle accepts all windows (scoring handles confidence)
	if !matchStrategy(withTitle, config.MatchProcessNameThenTitle) {
		t.Error("ProcessNameThenTitle should accept window with title")
	}
	if !matchStrategy(noTitle, config.MatchProcessNameThenTitle) {
		t.Error("ProcessNameThenTitle should accept window without title (scoring handles filtering)")
	}

	// MatchTitleContains requires title
	if !matchStrategy(withTitle, config.MatchTitleContains) {
		t.Error("TitleContains should accept window with title")
	}
	if matchStrategy(noTitle, config.MatchTitleContains) {
		t.Error("TitleContains should reject window without title")
	}

	// MatchClassName requires class name
	if !matchStrategy(withTitle, config.MatchClassName) {
		t.Error("ClassName should accept window with class")
	}
	if matchStrategy(noClass, config.MatchClassName) {
		t.Error("ClassName should reject window without class")
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"--verbose", []string{"--verbose"}},
		{`--config "C:\My Path\cfg.json" --verbose`, []string{"--config", `C:\My Path\cfg.json`, "--verbose"}},
		{`  --flag   value  `, []string{"--flag", "value"}},
		{`"quoted arg"`, []string{"quoted arg"}},
		{`one two three`, []string{"one", "two", "three"}},
	}
	for _, tc := range tests {
		got := parseArgs(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("parseArgs(%q) = %v (len %d), want %v (len %d)", tc.input, got, len(got), tc.want, len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("parseArgs(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}
