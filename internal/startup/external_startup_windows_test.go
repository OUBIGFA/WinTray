//go:build windows

package startup

import (
	"errors"
	"strings"
	"testing"
)

func TestExternalStartupLookupPreservesOriginalOwners(t *testing.T) {
	const path = `C:\Apps\Example.exe`
	const sid = "S-1-5-21-1"
	native := nativeTaskFixture("Vendor\\Startup", path, "--silent", sid)
	for _, tc := range []struct {
		name, task, want string
		entries          []startupEntry
		wantError        bool
	}{
		{name: "Run entry", entries: []startupEntry{{label: "HKCU Run Example", enabled: true}}, want: "HKCU Run Example"},
		{name: "startup shortcut", entries: []startupEntry{{label: "Startup/Example.lnk", enabled: true}}, want: "Startup/Example.lnk"},
		{name: "native task", task: native, want: "Task Scheduler\\Vendor\\Startup"},
		{name: "disabled Run permits normal launch", entries: []startupEntry{{label: "Run"}}},
		{name: "disabled task permits normal launch", task: strings.Replace(native, "<Triggers>", "<Settings><Enabled>false</Enabled></Settings><Triggers>", 1)},
		{name: "another user's task", task: nativeTaskFixture("Other", path, "", "S-1-5-21-2")},
		{name: "WinTray replacement is not original", task: nativeTaskFixture("WinTrayApp-other", path, "", sid)},
		{name: "no startup permits normal launch"},
		{name: "unrelated task permits normal launch", task: nativeTaskFixture("Other", `C:\Other.exe`, "", sid)},
		{name: "indirect startup rejects a duplicate", task: nativeTaskFixture("Wrapper", `C:\Other.exe`, path, sid), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tasks []taskDefinition
			if tc.task != "" {
				var task taskDefinition
				if err := decodeTaskXML([]byte(tc.task), &task); err != nil {
					t.Fatal(err)
				}
				tasks = append(tasks, task)
			}
			source, err := findEnabledStartupEntry(path, func(string) ([]startupEntry, error) { return tc.entries, nil }, func() ([]taskDefinition, string, error) {
				if len(tc.entries) > 0 && tc.entries[0].enabled {
					t.Fatal("an original entry must not need a scheduled-task query")
				}
				return tasks, sid, nil
			})
			if source != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("source=%q error=%v; want %q error=%t", source, err, tc.want, tc.wantError)
			}
		})
	}
}

func TestExternalStartupLookupDoesNotTreatUnreadableAsAbsent(t *testing.T) {
	want := errors.New("access denied")
	for _, entriesFail := range []bool{false, true} {
		source, err := findEnabledStartupEntry(`C:\Apps\Example.exe`, func(string) ([]startupEntry, error) {
			if entriesFail {
				return nil, want
			}
			return nil, nil
		}, func() ([]taskDefinition, string, error) { return nil, "", want })
		if source != "" || !errors.Is(err, want) {
			t.Fatalf("unreadable startup returned source=%q err=%v", source, err)
		}
	}
}
