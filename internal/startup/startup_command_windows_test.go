//go:build windows

package startup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	idmExe   = `D:\Data\_Program Files (x86)\Internet Download Manager\IDMan.exe`
	idmRun   = idmExe + ` /onboot`
	filenExe = `C:\Program Files\Filen\Filen.exe`
	filenRun = filenExe + ` --hidden`
)

// commandOutcome names what splitStartupCommandFor decided, so tables read as
// behaviour rather than error values.
func commandOutcome(err error) string {
	var ambiguous ambiguousStartupCommand
	switch {
	case err == nil:
		return "match"
	case errors.Is(err, errNotStartupTarget):
		return "skip"
	case errors.As(err, &ambiguous) && ambiguous.unquoted:
		return "unquoted"
	case errors.As(err, &ambiguous):
		return "indirect"
	}
	return "error: " + err.Error()
}

// shadowedApp lays out <dir>\My Apps\app.exe, the unquoted path of which
// Windows tries as <dir>\My(.exe) first; a non-empty shadow creates that
// shorter file.
func shadowedApp(t *testing.T, shadow string) string {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "My Apps", "app.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	files := []string{exe}
	if shadow != "" {
		files = append(files, filepath.Join(dir, shadow))
	}
	for _, name := range files {
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return exe
}

func TestSplitStartupCommandForResolvesLikeWindows(t *testing.T) {
	for _, tc := range []struct {
		name, command, expected string
		want, path, args        string
	}{
		// The unquoted Run values of IDM and Filen; Windows starts them fine.
		{"IDM unquoted with spaces", idmRun, idmExe, "match", idmExe, "/onboot"},
		{"Filen unquoted with spaces", filenRun, filenExe, "match", filenExe, "--hidden"},
		{"another program's unquoted command", filenRun, idmExe, "skip", "", ""},
		{"unquoted without arguments", filenExe, filenExe, "match", filenExe, ""},
		{"tab and repeated separators keep the tail", filenExe + "\t  --hidden  \"a b\" ", filenExe, "match", filenExe, `--hidden  "a b" `},
		{"case and clean path", `c:\program files\filen\.\FILEN.EXE --hidden`, filenExe, "match", `c:\program files\filen\.\FILEN.EXE`, "--hidden"},
		{"leading spaces", "  " + filenRun, filenExe, "match", filenExe, "--hidden"},
		// Only an exact executable followed by a separator is a match.
		{"longer file name after the target", filenExe + `.launcher --hidden`, filenExe, "indirect", "", ""},
		{"target without separator in quotes", `"` + filenExe + `"--hidden`, filenExe, "indirect", "", ""},
		// Wrappers start another program with the target as an argument.
		{"cmd wrapper", `cmd /c "` + filenExe + `" --hidden`, filenExe, "indirect", "", ""},
		{"rundll32 wrapper", `C:\Windows\System32\rundll32.exe ` + filenExe + `,Start`, filenExe, "indirect", "", ""},
		{"path only in arguments", `"C:\Windows\explorer.exe" "` + filenExe + `"`, filenExe, "indirect", "", ""},
		// Regressions for commands that already parsed.
		{"quoted with spaces", `"` + filenExe + `" --hidden`, filenExe, "match", filenExe, "--hidden"},
		{"quoted keeps literal tail", `"D:\Native app\程序.exe" --startup "a b" --x=%V%`, `D:\Native app\程序.exe`, "match", `D:\Native app\程序.exe`, `--startup "a b" --x=%V%`},
		{"unquoted without spaces", `D:\Software\ElegantClipboard\elegant-clipboard.exe --hidden`, `D:\Software\ElegantClipboard\elegant-clipboard.exe`, "match", `D:\Software\ElegantClipboard\elegant-clipboard.exe`, "--hidden"},
		{"same name elsewhere", `"E:\Other\Filen.exe" --hidden`, filenExe, "skip", "", ""},
		{"relative executable", `Filen.exe --hidden`, filenExe, "skip", "", ""},
		{"empty", ``, filenExe, "skip", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, args, err := splitStartupCommandFor(tc.command, tc.expected)
			if got := commandOutcome(err); got != tc.want || path != tc.path || args != tc.args {
				t.Fatalf("splitStartupCommandFor(%q, %q) = %q, %q, %s; want %q, %q, %s", tc.command, tc.expected, path, args, got, tc.path, tc.args, tc.want)
			}
		})
	}
}

// Windows runs the first existing prefix of an unquoted path. When a shorter
// file exists, it is what starts, so the target must not be assumed.
func TestSplitStartupCommandForRejectsShadowingShorterFile(t *testing.T) {
	for _, tc := range []struct {
		shadow, want string
	}{
		{"", "match"},
		{"My.exe", "unquoted"},
		{"My.bat", "unquoted"},
		{"My", "unquoted"},
	} {
		t.Run("shadow="+tc.shadow, func(t *testing.T) {
			exe := shadowedApp(t, tc.shadow)
			path, args, err := splitStartupCommandFor(exe+" --background", exe)
			if got := commandOutcome(err); got != tc.want {
				t.Fatalf("outcome = %s, want %s", got, tc.want)
			}
			if tc.want == "match" && (path != exe || args != "--background") {
				t.Fatalf("split = %q, %q", path, args)
			}
			if got := runCommandMatches(exe+" --background", exe); got != (tc.want == "match") {
				t.Fatalf("runCommandMatches = %t, disagrees with outcome %s", got, tc.want)
			}
		})
	}
}

func TestRunCommandMatchesUnquotedPathsWithSpaces(t *testing.T) {
	for _, tc := range []struct {
		name, command, path string
		want                bool
	}{
		{"IDM", idmRun, idmExe, true},
		{"Filen", filenRun, filenExe, true},
		{"Filen for IDM", filenRun, idmExe, false},
		{"wrapper", `cmd /c ` + filenRun, filenExe, false},
		{"longer file name", filenExe + `.launcher --hidden`, filenExe, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runCommandMatches(tc.command, tc.path); got != tc.want {
				t.Fatalf("runCommandMatches(%q, %q) = %t, want %t", tc.command, tc.path, got, tc.want)
			}
		})
	}
}

func TestFindRunStartupEntriesMigratesUnquotedCommand(t *testing.T) {
	source, run, _ := testRunSource(t)
	if err := run.SetStringValue("io.filen.desktop", filenRun); err != nil {
		t.Fatal(err)
	}
	if err := run.SetStringValue("IDMan", idmRun); err != nil {
		t.Fatal(err)
	}
	entries, err := findRunStartupEntries(filenExe, []runSource{source})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%+v err=%v, want the Filen entry only", entries, err)
	}
	if got := entries[0]; got.path != filenExe || got.args != "--hidden" || got.approvalName != "io.filen.desktop" || !got.enabled {
		t.Fatalf("entry = %+v", got)
	}
	enabled, err := findEnabledRunEntry(idmExe, []runSource{source})
	if err != nil || enabled != `test\IDMan` {
		t.Fatalf("external startup = %q, %v; want the IDM Run entry", enabled, err)
	}
}

func TestFindRunStartupEntriesRefusesAmbiguousCommand(t *testing.T) {
	exe := shadowedApp(t, "My.exe")
	for _, tc := range []struct {
		name, command string
		unquoted      bool
	}{
		{"shadowed unquoted path", exe + " --background", true},
		{"wrapper", `cmd /c "` + exe + `"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, run, _ := testRunSource(t)
			if err := run.SetStringValue("App", tc.command); err != nil {
				t.Fatal(err)
			}
			_, err := findRunStartupEntries(exe, []runSource{source})
			var refused *StartupCommandError
			if !errors.As(err, &refused) || refused.Source != `test\App` || refused.Unquoted != tc.unquoted {
				t.Fatalf("err = %v, want a refused %s command of test\\App", err, tc.name)
			}
		})
	}
}

func TestRunHelperReplaysUnquotedCommand(t *testing.T) {
	source, run, _ := testRunSource(t)
	if err := run.SetStringValue("IDMan", idmRun); err != nil {
		t.Fatal(err)
	}
	called := false
	err := launchStartupRunFrom(source.path, "IDMan", idmExe, func(path, args, dir string, show int32) error {
		called = true
		if path != idmExe || args != "/onboot" || dir != "" || show != 1 {
			t.Fatalf("original launch was rewritten: %q %q dir=%q show=%d", path, args, dir, show)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("launch: %v called=%t", err, called)
	}

	exe := shadowedApp(t, "My.exe")
	if err := run.SetStringValue("Shadowed", exe+" --background"); err != nil {
		t.Fatal(err)
	}
	err = launchStartupRunFrom(source.path, "Shadowed", exe, func(string, string, string, int32) error {
		t.Fatal("a command Windows resolves to another file was launched")
		return nil
	})
	var refused *StartupCommandError
	if !errors.As(err, &refused) || !refused.Unquoted {
		t.Fatalf("err = %v, want a refused unquoted command", err)
	}
}
