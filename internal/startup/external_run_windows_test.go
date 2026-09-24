//go:build windows

package startup

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestRunCommandMatchesExecutableNotArgumentsOrName(t *testing.T) {
	for _, tc := range []struct {
		name, command, path string
		want                bool
	}{
		{"QQ background", `"D:\Software\QQ\QQ.exe" /background`, `D:\Software\QQ\QQ.exe`, true},
		{"unquoted", `D:\Software\QQ\QQ.exe /background`, `D:\Software\QQ\QQ.exe`, true},
		{"spaces and case", `"d:\Program Files\qq\QQ.exe" /background`, `D:\Program Files\QQ\qq.exe`, true},
		{"clean path", `"D:/Software/QQ/./QQ.exe"`, `D:\Software\QQ\QQ.exe`, true},
		{"same name elsewhere", `"E:\Other\QQ.exe"`, `D:\Software\QQ\QQ.exe`, false},
		{"path only in arguments", `"C:\Windows\explorer.exe" "D:\Software\QQ\QQ.exe"`, `D:\Software\QQ\QQ.exe`, false},
		{"filename prefix", `"D:\Software\QQ\QQ.exe.bak"`, `D:\Software\QQ\QQ.exe`, false},
		{"relative executable", `QQ.exe`, `D:\Software\QQ\QQ.exe`, false},
		{"empty", ``, `D:\Software\QQ\QQ.exe`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runCommandMatches(tc.command, tc.path); got != tc.want {
				t.Fatalf("runCommandMatches(%q, %q) = %t, want %t", tc.command, tc.path, got, tc.want)
			}
		})
	}
}

var testRunKeyID atomic.Uint64

// All writes use disposable keys, never the real Run or StartupApproved keys.
func testRunSource(t *testing.T) (runSource, registry.Key, registry.Key) {
	t.Helper()
	base := fmt.Sprintf(`Software\WinTrayTests-%d-%d`, os.Getpid(), testRunKeyID.Add(1))
	source := runSource{registry.CURRENT_USER, base + `\Run`, registry.WOW64_64KEY, base + `\Approved`, "test"}
	key, _, err := registry.CreateKey(source.root, source.path, registry.ALL_ACCESS|source.view)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		key.Close()
		registry.DeleteKey(source.root, source.path)
		registry.DeleteKey(source.root, base)
	})
	approved, _, err := registry.CreateKey(source.root, source.approvalPath, registry.ALL_ACCESS|source.view)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		approved.Close()
		registry.DeleteKey(source.root, source.approvalPath)
	})
	return source, key, approved
}

func TestFindEnabledRunEntryHonorsApprovalAndFullPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   byte
		want    bool
		wantErr bool
	}{
		{name: "absent approval is enabled", want: true},
		{name: "enabled", state: 2, want: true},
		{name: "disabled in task manager", state: 3},
		{name: "enabled alternate state", state: 6, want: true},
		{name: "disabled alternate state", state: 7},
		{name: "unknown approval is not absence", state: 99, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, key, approved := testRunSource(t)
			if err := key.SetStringValue("not-an-app-name", `"D:\Software\QQ\QQ.exe" /background`); err != nil {
				t.Fatal(err)
			}
			if tc.state != 0 {
				data := make([]byte, 12)
				data[0] = tc.state
				if err := approved.SetBinaryValue("not-an-app-name", data); err != nil {
					t.Fatal(err)
				}
			}
			got, err := findEnabledRunEntry(`D:\Software\QQ\QQ.exe`, []runSource{source})
			if (err != nil) != tc.wantErr || (got != "") != tc.want {
				t.Fatalf("source=%q err=%v, want found=%t error=%t", got, err, tc.want, tc.wantErr)
			}
			other, err := findEnabledRunEntry(`E:\Other\QQ.exe`, []runSource{source})
			if err != nil || other != "" {
				t.Fatalf("unrelated path matched: %q, %v", other, err)
			}
		})
	}
}

func TestFindEnabledRunEntryExpandsEnvironmentAndChecksAllSources(t *testing.T) {
	source, key, _ := testRunSource(t)
	t.Setenv("WINTRAY_TEST_APP_DIR", `D:\Software\QQ`)
	if err := key.SetExpandStringValue("QQNT", `"%WINTRAY_TEST_APP_DIR%\QQ.exe" /background`); err != nil {
		t.Fatal(err)
	}
	missing := source
	missing.path += `\Missing`
	got, err := findEnabledRunEntry(`D:\Software\QQ\QQ.exe`, []runSource{missing, source})
	if err != nil || got != `test\QQNT` {
		t.Fatalf("source=%q err=%v, want expanded Run entry", got, err)
	}
}

func TestFindEnabledRunEntryDisabledDuplicateDoesNotHideEnabledEntry(t *testing.T) {
	disabled, key, approved := testRunSource(t)
	enabled, other, _ := testRunSource(t)
	for _, k := range []registry.Key{key, other} {
		if err := k.SetStringValue("app", `"D:\Apps\app.exe"`); err != nil {
			t.Fatal(err)
		}
	}
	data := make([]byte, 12)
	data[0] = 3
	if err := approved.SetBinaryValue("app", data); err != nil {
		t.Fatal(err)
	}
	got, err := findEnabledRunEntry(`D:\Apps\app.exe`, []runSource{disabled, enabled})
	if err != nil || got == "" {
		t.Fatalf("enabled duplicate was missed: %q, %v", got, err)
	}
}

func TestFindEnabledRunEntryLocalProbe(t *testing.T) {
	path := os.Getenv("WINTRAY_STARTUP_PROBE_EXE")
	if path == "" {
		t.Skip("set WINTRAY_STARTUP_PROBE_EXE for a read-only check of a locally registered app")
	}
	source, err := FindEnabledRunEntry(path)
	if err != nil || !strings.Contains(source, `\`) {
		t.Fatalf("startup probe for %q: source=%q err=%v", path, source, err)
	}
	t.Logf("enabled external startup: %s -> %s", source, path)
}
