//go:build windows

package startup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wintray/internal/config"
)

func TestIncompleteMigrationBackupIsNotTreatedAsEmptyState(t *testing.T) {
	for _, data := range []string{
		`{}`, `{"version":1}`, `{"version":1,"apps":null}`,
		`{"version":1,"apps":{"task":{"exePath":"C:\\app.exe","approvals":[{"path":"Software\\Test","name":"App"}]}}}`,
	} {
		path := filepath.Join(t.TempDir(), "startup-migrations.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMigrationState(path); err == nil {
			t.Fatalf("incomplete backup accepted: %s", data)
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != data {
			t.Fatal("invalid recovery data was overwritten")
		}
	}
}

func TestDuplicateTaskIdentifiersOrExecutablesNeverRegister(t *testing.T) {
	for _, samePath := range []bool{false, true} {
		fake := newFakeSchtasks()
		tasks := testAppTasks(t, fake)
		tasks.findEntries = func(string) ([]startupEntry, error) {
			t.Fatal("conflicting settings should fail before reading or suppressing a source")
			return nil, nil
		}
		settings := taskSettings(`C:\Apps\One.exe`)
		other := settings.ManagedApps[0]
		if samePath {
			other.ID = "2"
		} else {
			other.ExePath = `C:\Apps\Two.exe`
		}
		settings.ManagedApps = append(settings.ManagedApps, other)
		if err := tasks.Sync(settings, false); err == nil || !strings.Contains(err.Error(), "multiple WinTray entries") {
			t.Fatalf("conflicting task settings accepted: %v", err)
		}
		if len(fake.callNames("/Create")) != 0 {
			t.Fatal("conflicting settings created a replacement")
		}
	}
}

func TestNativeTaskDoesNotSilentlyOverrideConflictingRunCommand(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	settings := taskSettings(`C:\Apps\app.exe`)
	original := nativeTaskFixture("Original", settings.ManagedApps[0].ExePath, "--native", tasks.userSID)
	fake.tasks["Original"] = original
	if err := run.SetStringValue("Other profile", `"C:\Apps\app.exe" --other-profile`); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(settings, false); err == nil {
		t.Fatal("conflicting original launch settings were silently selected")
	}
	if value, err := readApproval(source.root, source.approvalPath, "Other profile"); err != nil || value.Exists {
		t.Fatal("conflicting original Run entry was suppressed")
	}
	if fake.tasks["Original"] != original || len(fake.callNames("/Create")) != 0 {
		t.Fatal("native task was replaced")
	}
}

func TestDisabledRunEntryDoesNotPreventReusingNativeTask(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	_, run, approved := withRunSource(t, tasks)
	settings := taskSettings(`C:\Apps\app.exe`)
	original := nativeTaskFixture("Original", settings.ManagedApps[0].ExePath, "--native", tasks.userSID)
	fake.tasks["Original"] = original
	if err := run.SetStringValue("App", `"C:\Apps\app.exe" --obsolete`); err != nil {
		t.Fatal(err)
	}
	if err := approved.SetBinaryValue("App", []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if fake.tasks["Original"] != original || len(fake.callNames("/Create")) != 0 {
		t.Fatal("valid native task was not preserved")
	}
}

func TestLaunchNowRoutesToOriginalTaskWithoutCopyingIt(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	entry := taskSettings(`C:\Apps\app.exe`).ManagedApps[0]
	original := nativeTaskFixture("Original admin task", entry.ExePath, "--native", tasks.userSID)
	fake.tasks["Original admin task"] = original
	var runs []string
	tasks.run = func(args ...string) ([]byte, error) {
		if args[0] == "/Run" {
			if len(args) != 3 || args[1] != "/TN" {
				t.Fatalf("unexpected Run arguments: %v", args)
			}
			runs = append(runs, args[2])
			return nil, nil
		}
		return fake.run(args...)
	}
	if err := tasks.launchNow(entry); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0] != "Original admin task" || fake.tasks["Original admin task"] != original {
		t.Fatalf("original action/principal not used: %v", runs)
	}
	delete(fake.tasks, "Original admin task")
	if err := tasks.launchNow(entry); err == nil || len(runs) != 1 {
		t.Fatal("missing task fell back to another launch")
	}
	spec := tasks.spec(config.LogonTaskApp{Entry: entry, DelaySeconds: 10})
	spec.exePath = `C:\Different\app.exe`
	fake.tasks[spec.name], _ = appTaskXML(tasks.userSID, spec)
	if err := tasks.launchNow(entry); err == nil || len(runs) != 1 {
		t.Fatal("externally replaced task action was launched")
	}
}
