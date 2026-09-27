//go:build windows

package startup

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wintray/internal/config"
)

func taskSettings(path string) config.Settings {
	return config.Settings{RunAtLogon: true, StartupIntervalSeconds: 3, ManagedApps: []config.ManagedAppEntry{
		{ID: "1", Name: "Example", ExePath: path, RunOnStartup: true, LaunchViaLogonTask: true},
	}}
}

func withRunSource(t *testing.T, tasks *AppTasks) (runSource, registry.Key, registry.Key) {
	t.Helper()
	source, run, approved := testRunSource(t)
	tasks.findEntries = func(path string) ([]startupEntry, error) { return findRunStartupEntries(path, []runSource{source}) }
	return source, run, approved
}

func TestAppTasksReuseNativeTaskAndRemoveLegacyDuplicate(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	settings := taskSettings(`D:\Apps\karing.exe`)
	original := nativeTaskFixture("Karing Autorun", settings.ManagedApps[0].ExePath, "--launch_startup", tasks.userSID)
	fake.tasks["Karing Autorun"] = original
	legacy := tasks.spec(config.LogonTaskApp{Entry: settings.ManagedApps[0], DelaySeconds: 10})
	fake.tasks[legacy.name], _ = appTaskXML(tasks.userSID, legacy)
	var reports []string
	tasks.Log = func(message string) { reports = append(reports, message) }

	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if fake.tasks["Karing Autorun"] != original {
		t.Fatal("native task was rewritten (arguments/privileges/delay/conditions must stay intact)")
	}
	if _, exists := fake.tasks[legacy.name]; exists || len(fake.callNames("/Create")) != 0 {
		t.Fatal("a second task still owns the program's startup")
	}
	if len(reports) != 1 || !strings.Contains(reports[0], "Karing Autorun") {
		t.Fatalf("missing diagnostic of the chosen original entry: %v", reports)
	}
	fake.reset()
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.callNames("/Create")) != 0 || fake.tasks["Karing Autorun"] != original {
		t.Fatal("repeated sync modified original startup")
	}
	if err := tasks.Sync(config.Settings{}, false); err != nil || fake.tasks["Karing Autorun"] != original {
		t.Fatalf("turning WinTray off must release, not delete, the app's task: %v", err)
	}
}

func TestRunMigrationPreservesOriginalCommandAndRestoresApproval(t *testing.T) {
	for _, existed := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent approval", true: "existing approval"}[existed], func(t *testing.T) {
			fake := newFakeSchtasks()
			tasks := testAppTasks(t, fake)
			source, run, approved := withRunSource(t, tasks)
			const path = `D:\程序 空格\app.exe`
			const command = `"D:\程序 空格\app.exe"  --startup "a b" --quote="c\\d"`
			if err := run.SetStringValue("NativeStartup", command); err != nil {
				t.Fatal(err)
			}
			before := approvalValue{}
			if existed {
				before = approvalValue{Exists: true, Data: []byte{6, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}}
				if err := approved.SetBinaryValue("NativeStartup", before.Data); err != nil {
					t.Fatal(err)
				}
			}
			settings := taskSettings(path)
			settings.ManagedApps[0].Args = "--not-the-original-arguments"
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatal(err)
			}
			if got, kind, err := run.GetStringValue("NativeStartup"); err != nil || kind != registry.SZ || got != command {
				t.Fatalf("program-owned command was modified: %q kind=%d err=%v", got, kind, err)
			}
			if enabled, err := runEntryApproved(source.root, source.approvalPath, "NativeStartup"); err != nil || enabled {
				t.Fatalf("original trigger was not suppressed: enabled=%t err=%v", enabled, err)
			}
			var definition taskDefinition
			if err := decodeTaskXML([]byte(fake.tasks[tasks.namePrefix+"1"]), &definition); err != nil {
				t.Fatal(err)
			}
			action := definition.Actions.Exec[0]
			args, err := windows.DecomposeCommandLine(action.Arguments)
			if err != nil || len(args) != 3 || args[0] != AppTaskHelperRun || args[1] != "NativeStartup" || args[2] != path || action.Command != tasks.selfExe || action.WorkingDirectory != "" {
				t.Fatalf("task lost source identity: action=%+v args=%q err=%v", action, args, err)
			}
			// A fresh AppTasks instance must honour the durable backup, not
			// mistake its own disabled entry for a user-disabled one.
			fresh := testAppTasks(t, fake)
			fresh.statePath, fresh.findEntries = tasks.statePath, tasks.findEntries
			fake.reset()
			if err := fresh.Sync(settings, false); err != nil || len(fake.callNames("/Create")) != 0 {
				t.Fatalf("restarted sync rewrote the task: err=%v creates=%v", err, fake.callNames("/Create"))
			}
			if err := fresh.Sync(config.Settings{}, false); err != nil {
				t.Fatal(err)
			}
			after, err := readApproval(source.root, source.approvalPath, "NativeStartup")
			if err != nil || !sameApproval(before, after) {
				t.Fatalf("original enable state not restored byte-for-byte: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

func TestRunMigrationRegistrationFailureRollsBack(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	if err := run.SetStringValue("App", `"C:\Apps\app.exe" --silent`); err != nil {
		t.Fatal(err)
	}
	fake.createErr = errors.New("registration/UAC declined")
	if err := tasks.Sync(taskSettings(`C:\Apps\app.exe`), false); err == nil {
		t.Fatal("failed registration was reported as success")
	}
	value, err := readApproval(source.root, source.approvalPath, "App")
	if err != nil || value.Exists {
		t.Fatalf("failed migration changed the original trigger: %+v %v", value, err)
	}
	state, err := loadMigrationState(tasks.statePath)
	if err != nil || len(state.Apps) != 0 || len(fake.tasks) != 0 {
		t.Fatalf("rollback left replacement/backups: %+v %v", state, err)
	}
}

func TestRunMigrationPartialSuppressionRollsBackWithoutRegistering(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	for _, name := range []string{"One", "Two"} {
		if err := run.SetStringValue(name, `"C:\Apps\app.exe" --silent`); err != nil {
			t.Fatal(err)
		}
	}
	writes := 0
	tasks.writeApproval = func(path, name string, old, value approvalValue) error {
		if value.Exists && value.Data[0] == 3 {
			writes++
			if writes == 2 {
				return errors.New("cannot disable second original entry")
			}
		}
		return writeUserApproval(path, name, old, value)
	}
	if err := tasks.Sync(taskSettings(`C:\Apps\app.exe`), false); err == nil {
		t.Fatal("partial suppression was accepted")
	}
	for _, name := range []string{"One", "Two"} {
		value, err := readApproval(source.root, source.approvalPath, name)
		if err != nil || value.Exists {
			t.Fatalf("%s not rolled back: %+v %v", name, value, err)
		}
	}
	if len(fake.callNames("/Create")) != 0 {
		t.Fatal("registered a task before suppressing every original trigger")
	}
}

func TestFailedTaskRemovalKeepsBackupUntilRetry(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, _ := withRunSource(t, tasks)
	if err := run.SetStringValue("App", `"C:\Apps\app.exe" --silent`); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(taskSettings(`C:\Apps\app.exe`), false); err != nil {
		t.Fatal(err)
	}
	fake.deleteErr = errors.New("access denied")
	if err := tasks.Sync(config.Settings{}, false); err == nil {
		t.Fatal("failed task removal was accepted")
	}
	if enabled, err := runEntryApproved(source.root, source.approvalPath, "App"); err != nil || enabled {
		t.Fatal("restored original before the replacement could be deleted (duplicate startup)")
	}
	fake.deleteErr = nil
	if err := tasks.Sync(config.Settings{}, false); err != nil {
		t.Fatal(err)
	}
	if value, err := readApproval(source.root, source.approvalPath, "App"); err != nil || value.Exists {
		t.Fatalf("retry did not restore original state: %+v %v", value, err)
	}
}

func TestMigrationNeverOverwritesLaterUserDisable(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, run, approved := withRunSource(t, tasks)
	if err := run.SetStringValue("App", `"C:\Apps\app.exe" --silent`); err != nil {
		t.Fatal(err)
	}
	settings := taskSettings(`C:\Apps\app.exe`)
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatal(err)
	}
	userDisabled := []byte{3, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0}
	if err := approved.SetBinaryValue("App", userDisabled); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(settings, false); !errors.Is(err, errApprovalChanged) {
		t.Fatalf("user change did not stop replacement startup: %v", err)
	}
	value, err := readApproval(source.root, source.approvalPath, "App")
	if err != nil || !bytes.Equal(value.Data, userDisabled) || len(fake.tasks) != 0 {
		t.Fatalf("user state changed or replacement survived: value=%+v err=%v tasks=%v", value, err, fake.tasks)
	}
}

func TestMigrationAmbiguousDisabledAndSharedSourcesFailClosed(t *testing.T) {
	for _, scenario := range []string{"different arguments", "disabled", "machine", "missing migrated source", "bad inventory", "bad backup"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newFakeSchtasks()
			tasks := testAppTasks(t, fake)
			source, run, approved := withRunSource(t, tasks)
			if err := run.SetStringValue("App", `"C:\Apps\app.exe" --silent`); err != nil {
				t.Fatal(err)
			}
			settings := taskSettings(`C:\Apps\app.exe`)
			switch scenario {
			case "different arguments":
				if err := run.SetStringValue("Other", `"C:\Apps\app.exe" --show`); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				if err := approved.SetBinaryValue("App", []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); err != nil {
					t.Fatal(err)
				}
			case "machine":
				find := tasks.findEntries
				tasks.findEntries = func(path string) ([]startupEntry, error) {
					entries, err := find(path)
					for i := range entries {
						entries[i].machine = true
					}
					return entries, err
				}
			case "missing migrated source":
				if err := tasks.Sync(settings, false); err != nil {
					t.Fatal(err)
				}
				if err := run.DeleteValue("App"); err != nil {
					t.Fatal(err)
				}
				fake.reset()
			case "bad inventory":
				fake.listErr = errors.New("cannot read scheduler")
			case "bad backup":
				if err := os.WriteFile(tasks.statePath, []byte("{broken"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := readApproval(source.root, source.approvalPath, "App")
			if err != nil {
				t.Fatal(err)
			}
			if err := tasks.Sync(settings, false); err == nil {
				t.Fatal("unsafe migration reported success")
			}
			after, err := readApproval(source.root, source.approvalPath, "App")
			if err != nil || (scenario != "missing migrated source" && !sameApproval(before, after)) || len(fake.callNames("/Create")) != 0 {
				t.Fatalf("unsafe source changed: before=%+v after=%+v err=%v", before, after, err)
			}
		})
	}
}

func TestRunHelperRetainsLiteralArgsAndReadsProgramUpdates(t *testing.T) {
	source, run, _ := testRunSource(t)
	const exe = `D:\Native app\程序.exe`
	for _, tc := range []struct {
		kind          uint32
		command, want string
	}{
		{registry.SZ, `"D:\Native app\程序.exe" --startup "a b" --literal=%WINTRAY_ARG%`, `--startup "a b" --literal=%WINTRAY_ARG%`},
		{registry.EXPAND_SZ, `"D:\Native app\程序.exe" --startup "%WINTRAY_ARG%"`, `--startup "updated value"`},
		{registry.SZ, `"D:\Native app\程序.exe" --changed-by-application`, `--changed-by-application`},
	} {
		t.Setenv("WINTRAY_ARG", "updated value")
		var err error
		if tc.kind == registry.SZ {
			err = run.SetStringValue("App", tc.command)
		} else {
			err = run.SetExpandStringValue("App", tc.command)
		}
		if err != nil {
			t.Fatal(err)
		}
		called := false
		err = launchStartupRunFrom(source.path, "App", exe, func(path, args, dir string, show int32) error {
			called = true
			if path != exe || args != tc.want || dir != "" || show != 1 {
				t.Fatalf("original launch was rewritten: %q %q dir=%q show=%d", path, args, dir, show)
			}
			return nil
		})
		if err != nil || !called {
			t.Fatalf("launch: %v called=%t", err, called)
		}
	}
	if err := launchStartupRunFrom(source.path, "App", `C:\Different.exe`, func(string, string, string, int32) error {
		t.Fatal("changed executable was launched through an old privilege grant")
		return nil
	}); err == nil {
		t.Fatal("changed target was not rejected")
	}
}
