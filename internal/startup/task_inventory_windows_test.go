//go:build windows

package startup

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func nativeTaskFixture(name, path, args, sid string) string {
	return `<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><URI>\` + xmlText(name) + `</URI></RegistrationInfo>
  <Principals><Principal id="App"><UserId>` + sid + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Triggers><LogonTrigger><UserId>` + sid + `</UserId><Delay>PT3S</Delay></LogonTrigger></Triggers>
  <Actions Context="App"><Exec><Command>` + xmlText(path) + `</Command><Arguments>` + xmlText(args) + `</Arguments><WorkingDirectory>D:\Original profile</WorkingDirectory></Exec></Actions>
</Task>`
}

func TestNativeLogonTaskDiscoveryPreservesUserAndDisabledSemantics(t *testing.T) {
	const sid = "S-1-5-21-1"
	const path = `D:\Apps\客户端 & app.exe`
	original := nativeTaskFixture("Vendor folder\\Original task", path, `--startup "a b"`, sid)
	for _, tc := range []struct {
		name  string
		xml   string
		found bool
		err   bool
	}{
		{"enabled by default", original, true, false},
		{"task disabled", strings.Replace(original, "<Triggers>", "<Settings><Enabled>false</Enabled></Settings><Triggers>", 1), false, true},
		{"trigger disabled", strings.Replace(original, "<LogonTrigger>", "<LogonTrigger><Enabled>false</Enabled>", 1), false, true},
		{"different principal", strings.Replace(original, "<UserId>"+sid, "<UserId>S-1-5-21-99", 1), false, true},
		{"different trigger user", strings.Replace(original, "<LogonTrigger><UserId>"+sid, "<LogonTrigger><UserId>S-1-5-21-99", 1), false, true},
		{"all-user trigger but same interactive principal", strings.Replace(original, "<LogonTrigger><UserId>"+sid+"</UserId>", "<LogonTrigger>", 1), true, false},
		{"noninteractive", strings.Replace(original, "InteractiveToken", "S4U", 1), false, true},
		{"expired", strings.Replace(original, "<LogonTrigger>", "<LogonTrigger><EndBoundary>2000-01-01T00:00:00Z</EndBoundary>", 1), false, true},
		{"future", strings.Replace(original, "<LogonTrigger>", "<LogonTrigger><StartBoundary>2099-01-01T00:00:00Z</StartBoundary>", 1), false, true},
		{"updater argument is not an executable match", nativeTaskFixture("Updater", `C:\Updater.exe`, path, sid), false, true},
		{"same filename at other path", nativeTaskFixture("Other", `E:\Apps\客户端 & app.exe`, "", sid), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, data := range [][]byte{[]byte(tc.xml), utf16LEWithBOM(tc.xml)} {
				var task taskDefinition
				if err := decodeTaskXML(data, &task); err != nil {
					t.Fatal(err)
				}
				name, err := findNativeLogonTask([]taskDefinition{task}, path, sid, "WinTrayApp-"+sid+"-")
				if (name != "") != tc.found || (err != nil) != tc.err {
					t.Fatalf("name=%q err=%v, want found=%t error=%t", name, err, tc.found, tc.err)
				}
			}
		})
	}
}

func TestNativeTaskConflictAndTaskInventoryErrors(t *testing.T) {
	original := nativeTaskFixture("One", `C:\app.exe`, "--startup", "S-1-5-21-1")
	var one, two taskDefinition
	if err := decodeTaskXML([]byte(original), &one); err != nil {
		t.Fatal(err)
	}
	if err := decodeTaskXML([]byte(strings.Replace(original, `\One`, `\Two`, 1)), &two); err != nil {
		t.Fatal(err)
	}
	if _, err := findNativeLogonTask([]taskDefinition{one, two}, `C:\app.exe`, "S-1-5-21-1", "WinTrayApp-"); err == nil {
		t.Fatal("ambiguous tasks were silently selected")
	}
	for _, run := range []func(...string) ([]byte, error){
		func(...string) ([]byte, error) { return nil, errors.New("access denied") },
		func(...string) ([]byte, error) { return []byte("<Tasks>broken"), nil },
	} {
		if _, err := readTaskInventory(run); err == nil {
			t.Fatal("unreadable inventory treated as no startup tasks")
		}
	}
}

func TestAppTaskFingerprintDoesNotHideEditedAction(t *testing.T) {
	spec := appTaskSpec{exePath: `C:\Apps\app.exe`, args: "--startup", workingDir: `D:\Profile`, delay: 10}
	definition, fingerprint := appTaskXML("S-1-5-21-1", spec)
	if !appTaskUpToDate([]byte(definition), "S-1-5-21-1", spec, fingerprint) {
		t.Fatal("unaltered task is not recognised")
	}
	for _, changed := range []string{
		strings.Replace(definition, "--startup", "--show-window", 1),
		strings.Replace(definition, `D:\Profile`, `C:\Wrong directory`, 1),
		strings.Replace(definition, "LeastPrivilege", "HighestAvailable", 1),
		strings.Replace(definition, "PT10S", "PT30S", 1),
	} {
		if appTaskUpToDate([]byte(changed), "S-1-5-21-1", spec, fingerprint) {
			t.Fatal("stale description fingerprint concealed changed launch attributes")
		}
	}
}

func TestLogonBoundaryWithoutTimezoneUsesLocalTime(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)
	if !logonBoundaryActive("2026-09-27T09:00:00", "2026-09-27T11:00:00", now) || logonBoundaryActive("invalid", "", now) {
		t.Fatal("local/unreadable boundaries were misclassified")
	}
}

func TestNativeLogonTaskLocalProbe(t *testing.T) {
	path := os.Getenv("WINTRAY_NATIVE_TASK_PROBE_EXE")
	if path == "" {
		t.Skip("set WINTRAY_NATIVE_TASK_PROBE_EXE for read-only native task discovery")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := readTaskInventory(runSchtasks)
	if err != nil {
		t.Fatal(err)
	}
	name, err := findNativeLogonTask(inventory, path, user.User.Sid.String(), "WinTrayApp-")
	if err != nil || name == "" {
		t.Fatalf("native task for %s: name=%q err=%v", path, name, err)
	}
	entries, err := findStartupEntries(path)
	if err != nil {
		t.Fatalf("read-only ordinary startup discovery: %v", err)
	}
	t.Logf("native task preserved: %s -> %s; ordinary startup entries=%d", name, path, len(entries))
}
