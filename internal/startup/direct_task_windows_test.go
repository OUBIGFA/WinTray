//go:build windows

package startup

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"wintray/internal/config"
)

func TestProgramWithoutNativeStartupGetsItsOwnLogonTask(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	tasks.findEntries = func(string) ([]startupEntry, error) { return nil, nil }
	settings := taskSettings(`D:\Portable app\program.exe`)
	settings.ManagedApps[0].Args = `--profile "one two" --background`
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	var task taskDefinition
	if err := decodeTaskXML([]byte(fake.tasks[tasks.namePrefix+"1"]), &task); err != nil {
		t.Fatal(err)
	}
	if len(task.Actions.Exec) != 1 {
		t.Fatalf("actions: %+v", task.Actions)
	}
	action := task.Actions.Exec[0]
	// The task starts WinTray's helper, which waits for WinTray and then
	// launches the configured command from the program's folder.
	args, err := windows.DecomposeCommandLine(action.Arguments)
	if err != nil || action.Command != tasks.selfExe || len(args) != 3 || args[0] != AppTaskHelperConfigured ||
		args[1] != settings.ManagedApps[0].Args || args[2] != settings.ManagedApps[0].ExePath ||
		action.WorkingDirectory != filepath.Dir(settings.ManagedApps[0].ExePath) || !task.logonFor(tasks.userSID) {
		t.Fatalf("configured startup was lost: %+v args=%q err=%v", task, args, err)
	}
	state, err := loadMigrationState(tasks.statePath)
	if err != nil || len(state.Apps) != 0 {
		t.Fatalf("direct launch fabricated original-startup backups: %+v %v", state, err)
	}
	fake.reset()
	if err := tasks.Sync(settings, false); err != nil || len(fake.callNames("/Create")) != 0 {
		t.Fatalf("unchanged task was recreated: %v", err)
	}
	settings.ManagedApps[0].Args = "--new-profile"
	if err := tasks.Sync(settings, false); err != nil || len(fake.callNames("/Create")) != 1 {
		t.Fatalf("new arguments were not applied: %v", err)
	}
	if err := tasks.Sync(config.Settings{}, false); err != nil || len(fake.tasks) != 0 {
		t.Fatalf("disabled startup left a task: %v", err)
	}
}

func TestDirectLogonTaskAdoptsNewNativeStartupWithoutDuplicates(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	settings := taskSettings(`C:\Apps\app.exe`)
	settings.ManagedApps[0].Args = "--configured"
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if err := run.SetStringValue("Native", `"C:\Apps\app.exe" --native-silent`); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	var task taskDefinition
	if err := decodeTaskXML([]byte(fake.tasks[tasks.namePrefix+"1"]), &task); err != nil {
		t.Fatal(err)
	}
	if len(task.Actions.Exec) != 1 || task.Actions.Exec[0].Command != tasks.selfExe {
		t.Fatalf("native startup was not adopted: %+v", task)
	}
	if enabled, err := runEntryApproved(source.root, source.approvalPath, "Native"); err != nil || enabled {
		t.Fatalf("duplicate trigger remains: %v", err)
	}
	if err := tasks.Sync(config.Settings{}, false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := runEntryApproved(source.root, source.approvalPath, "Native"); err != nil || !enabled {
		t.Fatalf("original startup was not restored: %v", err)
	}
}

func TestStartupMatchesFileIdentityNotOnlyPathSpelling(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "program.exe")
	alias := filepath.Join(dir, "other-path.exe")
	other := filepath.Join(dir, "different.exe")
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(exe, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if !sameExecutablePath(exe, alias) {
		t.Fatal("an existing startup for the same file under another path was missed")
	}
	if sameExecutablePath(exe, other) {
		t.Fatal("different executables with the same content were confused")
	}
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	if err := run.SetStringValue("Alias", `"`+alias+`" --original`); err != nil {
		t.Fatal(err)
	}
	entries, err := findRunStartupEntries(exe, []runSource{source})
	if err != nil || len(entries) != 1 || entries[0].path != alias || entries[0].args != "--original" {
		t.Fatalf("original alias lost: %+v %v", entries, err)
	}
}
