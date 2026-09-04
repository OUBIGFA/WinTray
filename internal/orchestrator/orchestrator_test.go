package orchestrator

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestNormalizeIdentity(t *testing.T) {
	if got := normalizeIdentity(" AI-Tool_Box "); got != "aitoolbox" {
		t.Fatalf("normalizeIdentity mismatch: got %q", got)
	}
	if got := normalizeIdentity("open claw"); got != "openclaw" {
		t.Fatalf("normalizeIdentity mismatch: got %q", got)
	}
}

func TestMatchesExecutableWithIdentityFallback_Normalized(t *testing.T) {
	window := ManagedWindowInfo{
		ProcessName: "com.ai-toolbox-siw",
		Title:       "AI Toolbox",
		ClassName:   "Chrome_WidgetWin_1",
	}

	if !matchesExecutableWithIdentityFallback(window, "", "ai-toolbox") {
		t.Fatal("expected normalized identity match for ai-toolbox vs AI Toolbox")
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

	// PID(1000) + path(500) + name(250) + new(200) + title(50) + class(10)
	// + normalized title contains (90) + normalized class contains (40) = 2140
	if score != 2140 {
		t.Errorf("expected score 2140, got %d", score)
	}

	// Window in baseline → no +200 bonus
	windowInBaseline := window
	windowInBaseline.Handle = 100
	score2 := computeCandidateScore(windowInBaseline, expectedPath, "app", &pid, baseline)
	if score2 != 1940 {
		t.Errorf("expected score 1940 for baseline window, got %d", score2)
	}

	// Tool window penalty
	toolWindow := window
	toolWindow.IsToolWindow = true
	score3 := computeCandidateScore(toolWindow, expectedPath, "app", &pid, baseline)
	if score3 != 2140-80 {
		t.Errorf("expected score %d for tool window, got %d", 2140-80, score3)
	}

	// Owned window penalty
	ownedWindow := window
	ownedWindow.OwnerHandle = 999
	score4 := computeCandidateScore(ownedWindow, expectedPath, "app", &pid, baseline)
	if score4 != 2140-60 {
		t.Errorf("expected score %d for owned window, got %d", 2140-60, score4)
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
		name     string
		window   ManagedWindowInfo
		unmanage bool
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

func TestIsPowerShellScript(t *testing.T) {
	if !isPowerShellScript(`C:\Scripts\start-all.ps1`) {
		t.Fatal("expected .ps1 to be recognized as PowerShell script")
	}
	if isPowerShellScript(`C:\Scripts\start-all.cmd`) {
		t.Fatal("expected .cmd to not be recognized as PowerShell script")
	}
}

func TestBuildLaunchCommand_PowerShellScript(t *testing.T) {
	cmd := buildLaunchCommand(`C:\My Scripts\start-all.ps1`, `-Mode "full run" -DryRun`, true)
	if !strings.EqualFold(filepath.Base(cmd.Path), "powershell.exe") {
		t.Fatalf("expected powershell.exe launcher, got %q", cmd.Path)
	}

	want := []string{"powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", `C:\My Scripts\start-all.ps1`, "-Mode", "full run", "-DryRun"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected args len: got=%d want=%d args=%v", len(cmd.Args), len(want), cmd.Args)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("arg[%d] mismatch: got=%q want=%q (all=%v)", i, cmd.Args[i], want[i], cmd.Args)
		}
	}

	if cmd.SysProcAttr == nil {
		t.Fatal("expected hidden launch to configure SysProcAttr")
	}
}

func TestBuildLaunchCommand_PythonWindowedScriptUsesPythonw(t *testing.T) {
	cmd := buildLaunchCommand(`C:\Scripts\start.pyw`, `--quiet`, true)
	if !strings.EqualFold(filepath.Base(cmd.Path), "pythonw.exe") {
		t.Fatalf("expected pythonw.exe launcher, got %q", cmd.Path)
	}
}

func TestBuildLaunchCommand_ExeWithArgs_UsesDirectProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific launch behavior")
	}

	cmd := buildLaunchCommand(`D:\Software\AI Toolbox\ai-toolbox.exe`, `--mode "full run" --port 8080`, false)
	if strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") {
		t.Fatalf("expected direct executable launch for .exe with args, got shell launcher path=%q", cmd.Path)
	}
	if !strings.EqualFold(filepath.Base(cmd.Path), "ai-toolbox.exe") {
		t.Fatalf("expected ai-toolbox.exe launcher, got %q", cmd.Path)
	}

	want := []string{"D:\\Software\\AI Toolbox\\ai-toolbox.exe", "--mode", "full run", "--port", "8080"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("unexpected args len: got=%d want=%d args=%v", len(cmd.Args), len(want), cmd.Args)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("arg[%d] mismatch: got=%q want=%q (all=%v)", i, cmd.Args[i], want[i], cmd.Args)
		}
	}
}

func TestBuildLaunchCommand_CmdScript_UsesShellLauncher(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific launch behavior")
	}

	cmd := buildLaunchCommand(`C:\Scripts\start.cmd`, `--foo bar`, false)
	if !strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") {
		t.Fatalf("expected cmd.exe launcher for cmd script, got %q", cmd.Path)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr with CmdLine for cmd script launcher")
	}
}

func TestBuildLaunchCommand_ExeHidden_ConfiguresNoWindow(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific launch behavior")
	}

	cmd := buildLaunchCommand(`D:\Tools\openclaw.exe`, "--silent", true)
	if !strings.EqualFold(filepath.Base(cmd.Path), "openclaw.exe") {
		t.Fatalf("expected openclaw.exe launcher, got %q", cmd.Path)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("expected hidden executable launch to configure SysProcAttr")
	}
	if cmd.SysProcAttr.CreationFlags != createNoWindow {
		t.Fatalf("expected CreationFlags=%d, got %d", createNoWindow, cmd.SysProcAttr.CreationFlags)
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("expected HideWindow=true for hidden executable launch")
	}
}
