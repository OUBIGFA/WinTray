//go:build windows

package startup

import (
	"encoding/json"
	"strings"
	"testing"
	"wintray/internal/config"
)

func frequencySettings(t *testing.T, path string) config.Settings {
	t.Helper()
	settings := taskSettings(path)
	settings.ManagedApps[0].LaunchViaLogonTask = false
	if err := json.Unmarshal([]byte(`{"enabled":true,"frequencyEnabled":true,"frequencyDays":1,"frequencyRuns":1}`), &settings.ManagedApps[0].Schedule); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestFrequencyRejectsScriptStartupThatCannotBeTakenOver(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	tasks.findEntries = func(string) ([]startupEntry, error) {
		return []startupEntry{{enabled: true, label: "external script startup"}}, nil
	}
	if err := tasks.Sync(frequencySettings(t, `C:\Scripts\daily.ps1`), false); err == nil {
		t.Fatal("script's external startup silently bypasses its quota")
	}
}

func TestFrequencyNativeTaskRejectsExternalEditsWithoutOverwriting(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	settings := frequencySettings(t, `C:\Apps\daily.exe`)
	original := strings.ReplaceAll(nativeTaskFixture("Daily", settings.ManagedApps[0].ExePath, "--original", tasks.userSID), "HighestAvailable", "LeastPrivilege")
	fake.tasks["Daily"] = original
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	edited := strings.ReplaceAll(fake.tasks["Daily"], "--original", "--user-change")
	fake.tasks["Daily"] = edited
	if err := tasks.Sync(config.Settings{}, false); err == nil {
		t.Fatal("external edit was overwritten")
	}
	if fake.tasks["Daily"] != edited {
		t.Fatal("external task changes were lost")
	}
	state, err := loadMigrationState(tasks.statePath)
	if err != nil || state.Apps[tasks.namePrefix+"1"].NativeTask == nil {
		t.Fatal("failed restore lost its backup")
	}
}

func TestFrequencyNativeTaskKeepsNonLogonTriggersAndUnknownSettings(t *testing.T) {
	const original = `<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger><CalendarTrigger><ScheduleByDay><DaysInterval>7</DaysInterval></ScheduleByDay></CalendarTrigger></Triggers><Settings><RestartOnFailure><Interval>PT5M</Interval><Count>3</Count></RestartOnFailure></Settings><Actions><Exec><Command>C:\App.exe</Command><Arguments> --literal &amp; spaced </Arguments></Exec></Actions></Task>`
	got, err := removeLogonTriggers(original)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "<LogonTrigger><Enabled>true</Enabled></LogonTrigger>", "", 1)
	if got != want {
		t.Fatalf("task content changed beyond logon trigger: %s", got)
	}
}

func TestFrequencyTakesOverRunAndRestoresWhenDisabled(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	const path = `C:\Apps\daily.exe`
	if err := run.SetStringValue("Daily", `"C:\Apps\daily.exe" --original`); err != nil {
		t.Fatal(err)
	}
	settings := frequencySettings(t, path)
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := runEntryApproved(source.root, source.approvalPath, "Daily"); err != nil || enabled {
		t.Fatalf("original startup bypasses limit: enabled=%t err=%v", enabled, err)
	}
	if len(fake.tasks) != 1 {
		t.Fatalf("want one demand task, got %d", len(fake.tasks))
	}
	for _, definition := range fake.tasks {
		var task taskDefinition
		if err := decodeTaskXML([]byte(definition), &task); err != nil {
			t.Fatal(err)
		}
		if task.logonFor(tasks.userSID) {
			t.Fatal("replacement must wait for WinTray's frequency decision")
		}
	}
	settings.ManagedApps[0].Schedule.Enabled = false
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := runEntryApproved(source.root, source.approvalPath, "Daily"); err != nil || !enabled || len(fake.tasks) != 0 {
		t.Fatalf("original not restored: %t %v", enabled, err)
	}
}

func TestFrequencyRestoresRunWhenStartupIsUnchecked(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	if err := run.SetStringValue("Daily", `"C:\Apps\daily.exe" --original`); err != nil {
		t.Fatal(err)
	}
	settings := frequencySettings(t, `C:\Apps\daily.exe`)
	settings.ManagedApps[0].Schedule.AutoExitMinutes = 5
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	settings.ManagedApps[0].RunOnStartup = false
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	enabled, err := runEntryApproved(source.root, source.approvalPath, "Daily")
	if err != nil || !enabled || len(fake.tasks) != 0 || config.ScheduledRunLimit(settings.ManagedApps[0]) != 5 {
		t.Fatalf("startup off did not restore native startup and preserve auto-exit: %t %v", enabled, err)
	}
}

func TestFrequencyTakesOverNativeTaskAndRestoresDefinition(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	tasks.elevateSelf = func(args ...string) error {
		_, err := fake.run("/Create", "/TN", args[1], "/XML", args[2], "/F")
		return err
	}
	settings := frequencySettings(t, `C:\Apps\daily.exe`)
	original := nativeTaskFixture("Daily", settings.ManagedApps[0].ExePath, "--original", tasks.userSID)
	fake.tasks["Daily"] = original
	if err := tasks.Sync(settings, true); err != nil {
		t.Fatal(err)
	}
	var task taskDefinition
	if err := decodeTaskXML([]byte(fake.tasks["Daily"]), &task); err != nil {
		t.Fatal(err)
	}
	if task.logonFor(tasks.userSID) {
		t.Fatal("native task bypasses frequency decision")
	}
	if task.Actions.Exec[0].Arguments != "--original" {
		t.Fatal("original arguments lost")
	}
	fake.reset()
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.callNames("/Create")) != 0 {
		t.Fatal("unchanged tasks rewritten")
	}
	if err := tasks.Sync(config.Settings{}, true); err != nil {
		t.Fatal(err)
	}
	if fake.tasks["Daily"] != original || len(fake.tasks) != 1 {
		t.Fatal("native definition not restored")
	}
}
