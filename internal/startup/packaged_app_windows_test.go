//go:build windows

package startup

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPackagedAppTaskPreservesIdentityAndArguments(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "logon", true: "frequency"}[limited], func(t *testing.T) {
			fake := newFakeSchtasks()
			tasks := testAppTasks(t, fake)
			tasks.findEntries = func(string) ([]startupEntry, error) {
				return nil, errors.New("packaged apps must not be treated as executable startup entries")
			}
			settings := taskSettings("shell:AppsFolder\\Example.App_123456789abcd!Reader")
			entry := &settings.ManagedApps[0]
			entry.Args = "--server \"two words\" --empty \"\""
			entry.Schedule.Enabled = limited
			entry.Schedule.FrequencyEnabled = limited
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatal(err)
			}
			name := tasks.namePrefix + entry.ID
			var definition taskDefinition
			if err := decodeTaskXML([]byte(fake.tasks[name]), &definition); err != nil {
				t.Fatal(err)
			}
			if len(definition.Actions.Exec) != 1 {
				t.Fatalf("task has no single activation action: %+v", definition.Actions)
			}
			action := definition.Actions.Exec[0]
			arguments, err := windows.DecomposeCommandLine(action.Arguments)
			if err != nil || len(arguments) != 3 || arguments[0] != AppTaskHelperConfigured || arguments[1] != entry.Args || arguments[2] != entry.ExePath || action.Command != tasks.selfExe || action.WorkingDirectory != "" {
				t.Fatalf("activation identity or arguments changed: action=%+v args=%q err=%v", action, arguments, err)
			}
			run := tasks.run
			launched := false
			tasks.run = func(arguments ...string) ([]byte, error) {
				if len(arguments) == 3 && arguments[0] == "/Run" && arguments[1] == "/TN" && arguments[2] == name {
					launched = true
					return nil, nil
				}
				return run(arguments...)
			}
			if err := tasks.launchNow(*entry); err != nil {
				t.Fatalf("cannot run the registered store-app task: %v", err)
			}
			if !launched {
				t.Fatal("the configured store-app task was not run")
			}
			entry.LaunchViaLogonTask, entry.RunOnStartup, entry.Schedule.Enabled = false, false, false
			if err := tasks.Sync(settings, false); err != nil {
				t.Fatal(err)
			}
			if _, exists := fake.tasks[name]; exists {
				t.Fatal("normal mode kept the store-app startup task")
			}
		})
	}
}
