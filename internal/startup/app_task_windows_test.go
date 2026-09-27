//go:build windows

package startup

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wintray/internal/config"
)

// fakeSchtasks answers the schtasks calls AppTasks makes and keeps the
// definition of every task it was asked to create, the way the real tool
// exports a registered task back.
type fakeSchtasks struct {
	calls     [][]string
	tasks     map[string]string
	createErr error
	deleteErr error
	listErr   error
}

func newFakeSchtasks() *fakeSchtasks {
	return &fakeSchtasks{tasks: make(map[string]string)}
}

func (f *fakeSchtasks) run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "/Query":
		if len(args) > 2 && args[1] == "/TN" {
			if definition, ok := f.tasks[args[2]]; ok {
				return []byte(definition), nil
			}
			return nil, errors.New("task not found")
		}
		if len(args) > 1 && args[1] == "/XML" {
			if f.listErr != nil {
				return nil, f.listErr
			}
			var out strings.Builder
			out.WriteString("<Tasks>")
			for name, definition := range f.tasks {
				var task taskDefinition
				_ = decodeTaskXML([]byte(definition), &task)
				task.Registration.URI = `\` + name
				data, err := xml.Marshal(task)
				if err != nil {
					return nil, err
				}
				out.Write(data)
			}
			out.WriteString("</Tasks>")
			return []byte(out.String()), nil
		}
		var out strings.Builder
		for name := range f.tasks {
			fmt.Fprintf(&out, "\"%s\",\"N/A\",\"Ready\"\n", `\`+name)
		}
		out.WriteString("\"\\Microsoft\\Windows\\Other\",\"N/A\",\"Ready\"\n")
		return []byte(out.String()), nil
	case "/Create":
		if f.createErr != nil {
			return nil, f.createErr
		}
		name, path := args[2], args[4]
		definition, err := readTaskDefinition(path)
		if err != nil {
			return nil, err
		}
		f.tasks[name] = definition
		return nil, nil
	case "/Delete":
		if f.deleteErr != nil {
			return nil, f.deleteErr
		}
		delete(f.tasks, args[2])
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected schtasks call: %v", args)
}

func (f *fakeSchtasks) callNames(verb string) []string {
	var names []string
	for _, call := range f.calls {
		if call[0] == verb {
			names = append(names, call[2])
		}
	}
	return names
}

func (f *fakeSchtasks) reset() {
	f.calls = nil
}

func readTaskDefinition(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) >= 2 {
		data = data[2:] // BOM
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		units = append(units, uint16(data[i])|uint16(data[i+1])<<8)
	}
	return string(utf16.Decode(units)), nil
}

func testAppTasks(t *testing.T, fake *fakeSchtasks) *AppTasks {
	t.Helper()
	return &AppTasks{
		userSID:       "S-1-5-21-1",
		selfExe:       `C:\WinTray\WinTray.exe`,
		namePrefix:    "WinTrayApp-S-1-5-21-1-",
		statePath:     filepath.Join(t.TempDir(), "startup-migrations.json"),
		run:           fake.run,
		findEntries:   func(string) ([]startupEntry, error) { return nil, nil },
		writeApproval: writeUserApproval,
	}
}

func TestAppTaskXMLCarriesDelayRunLevelAndFingerprint(t *testing.T) {
	spec := appTaskSpec{name: "WinTrayApp-S-1-5-21-1-1", appName: "A&B <app>", exePath: `D:\A&B\app.exe`, args: "--min", workingDir: `D:\A&B`, delay: 25}
	xmlA, fpA := appTaskXML("S-1-5-21-1", spec)
	if !strings.Contains(xmlA, "<Delay>PT25S</Delay>") || !strings.Contains(xmlA, "<RunLevel>LeastPrivilege</RunLevel>") {
		t.Fatalf("definition lacks delay or run level:\n%s", xmlA)
	}
	if !strings.Contains(xmlA, `<Command>D:\A&amp;B\app.exe</Command>`) || !strings.Contains(xmlA, `<WorkingDirectory>D:\A&amp;B</WorkingDirectory>`) {
		t.Fatalf("paths not escaped:\n%s", xmlA)
	}
	if !strings.Contains(xmlA, fpA) {
		t.Fatalf("fingerprint %q missing from definition", fpA)
	}
	immediate := spec
	immediate.delay = 0
	if xmlNow, fpNow := appTaskXML("S-1-5-21-1", immediate); strings.Contains(xmlNow, "<Delay>") || fpNow == fpA {
		t.Fatalf("an undelayed task must start at sign-in without a Delay element:\n%s", xmlNow)
	}
	elevated := spec
	elevated.highest = true
	if xml, _ := appTaskXML("S-1-5-21-1", elevated); !strings.Contains(xml, "<RunLevel>HighestAvailable</RunLevel>") {
		t.Fatalf("elevated definition lacks highest available:\n%s", xml)
	}
	for _, changed := range []appTaskSpec{
		func() appTaskSpec { s := spec; s.delay = 30; return s }(),
		func() appTaskSpec { s := spec; s.args = "--other"; return s }(),
		func() appTaskSpec { s := spec; s.exePath = `D:\A&B\other.exe`; return s }(),
		elevated,
	} {
		if _, fp := appTaskXML("S-1-5-21-1", changed); fp == fpA {
			t.Fatalf("a changed definition kept the fingerprint %q", fp)
		}
	}
}

func TestResourceDataDemandsElevation(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"utf8 require", []byte("<requestedExecutionLevel level=\"requireAdministrator\"/>"), true},
		{"utf16 require", utf16LE("<requestedExecutionLevel level=\"requireAdministrator\"/>"), true},
		{"utf8 highest", []byte(`level="highestAvailable"`), true},
		{"asInvoker", []byte(`level="asInvoker"`), false},
		{"empty", nil, false},
	} {
		if got := resourceDataDemandsElevation(tc.data); got != tc.want {
			t.Errorf("%s: resourceDataDemandsElevation = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestRunAsAdminCompatFlagReadsLayersKey(t *testing.T) {
	path := fmt.Sprintf(`Software\WinTrayTests-%d\Layers`, os.Getpid())
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		key.Close()
		registry.DeleteKey(registry.CURRENT_USER, path)
	})
	original := compatLayersRegistryPath
	compatLayersRegistryPath = path
	t.Cleanup(func() { compatLayersRegistryPath = original })

	const exe = `C:\Apps\Admin tool.exe`
	if runAsAdminCompatFlag(exe) {
		t.Fatal("unset compatibility flag reported as run as administrator")
	}
	if err := key.SetStringValue(exe, "~ RUNASADMIN"); err != nil {
		t.Fatal(err)
	}
	if !runAsAdminCompatFlag(exe) {
		t.Fatal("RUNASADMIN compatibility flag not detected")
	}
	if runAsAdminCompatFlag(`C:\Apps\Other.exe`) {
		t.Fatal("flag on one program leaked to another")
	}
}

func TestAppTasksSyncReconcilesWantedSet(t *testing.T) {
	restoreElevation := requiresElevationForLaunch
	requiresElevationForLaunch = func(string) bool { return false }
	t.Cleanup(func() { requiresElevationForLaunch = restoreElevation })

	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	_, nativeRun, _ := withRunSource(t, tasks)
	for name, command := range map[string]string{"First": `"C:\Apps\One.exe" --native`, "Second": `"C:\Apps\Two.exe" --native`} {
		if err := nativeRun.SetStringValue(name, command); err != nil {
			t.Fatal(err)
		}
	}
	settings := config.Settings{
		RunAtLogon:             true,
		StartupIntervalSeconds: 3,
		ManagedApps: []config.ManagedAppEntry{
			{ID: "1", Name: "First", ExePath: `C:\Apps\One.exe`, RunOnStartup: true, LaunchViaLogonTask: true, Args: "--a"},
			{ID: "2", Name: "Second", ExePath: `C:\Apps\Two.exe`, RunOnStartup: true, LaunchViaLogonTask: true},
			{ID: "3", Name: "Paused", ExePath: `C:\Apps\Three.exe`, RunOnStartup: false, LaunchViaLogonTask: true},
		},
	}

	if err := tasks.Sync(settings, false); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if got := fake.callNames("/Create"); len(got) != 2 {
		t.Fatalf("created %v, want the two enabled task programs only", got)
	}
	// A task of a program that is gone must disappear as well.
	fake.tasks[tasks.namePrefix+"999"] = "stale definition"

	fake.reset()
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if creates, deletes := fake.callNames("/Create"), fake.callNames("/Delete"); len(creates) != 0 || len(deletes) != 1 || deletes[0] != tasks.namePrefix+"999" {
		t.Fatalf("unchanged settings creates=%v deletes=%v, want only the orphan removed", creates, deletes)
	}

	settings.ManagedApps[0].Args = "--changed"
	fake.reset()
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatalf("changed sync: %v", err)
	}
	if got := fake.callNames("/Create"); len(got) != 0 {
		t.Fatalf("WinTray arguments overwrote original startup: creates=%v", got)
	}
	settings.StartupIntervalSeconds = 5
	fake.reset()
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatalf("interval change: %v", err)
	}
	if got := fake.callNames("/Create"); len(got) != 0 {
		t.Fatalf("changed interval created %v; launch at boot starts at sign-in, not staggered", got)
	}
	settings.ManagedApps[1].Schedule = config.Schedule{Enabled: true, StartDelayMinutes: 1}
	fake.reset()
	if err := tasks.Sync(settings, false); err != nil {
		t.Fatalf("scheduled start change: %v", err)
	}
	if got := fake.callNames("/Create"); len(got) != 1 || got[0] != tasks.namePrefix+"2" {
		t.Fatalf("scheduled start created %v, want only the second task's delay changed", got)
	}

	if err := tasks.Sync(config.Settings{RunAtLogon: false, StartupIntervalSeconds: 3}, false); err != nil {
		t.Fatalf("sign-in off sync: %v", err)
	}
	for _, name := range []string{tasks.namePrefix + "1", tasks.namePrefix + "2"} {
		if _, ok := fake.tasks[name]; ok {
			t.Fatalf("task %s survived WinTray sign-in being switched off", name)
		}
	}
}

func TestAppTasksElevatedRegistrationUsesHelperOnlyWhenAllowed(t *testing.T) {
	if IsProcessElevated() {
		t.Skip("this test needs a non-elevated process to exercise the helper")
	}
	restoreElevation := requiresElevationForLaunch
	requiresElevationForLaunch = func(string) bool { return true }
	t.Cleanup(func() { requiresElevationForLaunch = restoreElevation })

	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	_, nativeRun, _ := withRunSource(t, tasks)
	if err := nativeRun.SetStringValue("Admin", `"C:\Apps\Admin.exe" --native`); err != nil {
		t.Fatal(err)
	}
	var elevated [][]string
	tasks.elevateSelf = func(args ...string) error {
		elevated = append(elevated, args)
		if args[0] == AppTaskHelperRegister {
			definition, err := readTaskDefinition(args[2])
			if err != nil {
				return err
			}
			fake.tasks[args[1]] = definition
		} else {
			delete(fake.tasks, args[1])
		}
		return nil
	}
	settings := config.Settings{
		RunAtLogon:             true,
		StartupIntervalSeconds: 3,
		ManagedApps: []config.ManagedAppEntry{
			{ID: "1", Name: "Admin", ExePath: `C:\Apps\Admin.exe`, RunOnStartup: true, LaunchViaLogonTask: true},
		},
	}

	// The startup check may not prompt: it reports what waits for a save.
	if err := tasks.Sync(settings, false); !errors.Is(err, errElevationNeeded) {
		t.Fatalf("sync without elevation = %v, want the elevation-needed report", err)
	}
	if len(elevated) != 0 {
		t.Fatalf("elevation helper started without permission: %v", elevated)
	}

	if err := tasks.Sync(settings, true); err != nil {
		t.Fatalf("sync with elevation: %v", err)
	}
	if len(elevated) != 1 || elevated[0][0] != AppTaskHelperRegister || elevated[0][1] != tasks.namePrefix+"1" {
		t.Fatalf("elevation helper calls = %v, want one register call for the task", elevated)
	}

	if err := tasks.Sync(settings, true); err != nil || len(elevated) != 1 {
		t.Fatalf("unchanged elevated task asked again: calls=%v err=%v", elevated, err)
	}
	// Deletion of a task the helper registered may need the helper too.
	fake.deleteErr = errors.New("access is denied")
	elevated = nil
	if err := tasks.Sync(config.Settings{RunAtLogon: false, StartupIntervalSeconds: 3}, true); err != nil {
		t.Fatalf("removal sync: %v", err)
	}
	if len(elevated) != 1 || elevated[0][0] != AppTaskHelperDelete || elevated[0][1] != tasks.namePrefix+"1" {
		t.Fatalf("elevation helper calls = %v, want one delete call for the task", elevated)
	}
}

// Registers a real, uniquely named task without administrator rights: the
// definition must be accepted by the real schtasks and a second sync must
// recognise the registered task instead of re-creating it.
// requireSchtasks skips a real-registration test when this environment
// blocks the Task Scheduler tool (a sandbox or a security policy may),
// because no registration can be attempted then.
func requireSchtasks(t *testing.T) {
	t.Helper()
	if _, err := runSchtasks("/Query", "/FO", "CSV", "/NH"); err != nil {
		t.Skipf("schtasks unusable in this environment: %v", err)
	}
}

func TestAppTasksRegistersWithSchtasks(t *testing.T) {
	requireSchtasks(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	tasks := &AppTasks{
		userSID:    user.User.Sid.String(),
		selfExe:    self,
		namePrefix: fmt.Sprintf("WinTrayTestApp-%d-", os.Getpid()),
		run:        runSchtasks,
	}
	t.Cleanup(func() { _, _ = runSchtasks("/Delete", "/TN", tasks.namePrefix+"1", "/F") })
	spec := appTaskSpec{
		name:    tasks.namePrefix + "1",
		appName: "WinTray test app",
		exePath: filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"),
		args:    "/c exit",
		delay:   0, // launch at boot: no Delay element at all
	}

	if err := tasks.syncOne(spec, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, fingerprint := appTaskXML(tasks.userSID, spec)
	out, err := runSchtasks("/Query", "/TN", spec.name, "/XML")
	if err != nil || !taskUpToDate(out, fingerprint) {
		t.Fatalf("registered task not recognised: err=%v xml=%s", err, oemText(out))
	}
	var calls []string
	run := tasks.run
	tasks.run = func(args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		return run(args...)
	}
	if err := tasks.syncOne(spec, false); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if strings.Join(calls, ",") != "/Query" {
		t.Fatalf("unchanged task was re-registered: %v\nexported: %s", calls, taskXMLText(out))
	}
	tasks.run = run

	if err := tasks.remove(spec.name, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := runSchtasks("/Query", "/TN", spec.name); err == nil {
		t.Fatal("task still exists after remove")
	}
	if err := tasks.remove(spec.name, false); err != nil {
		t.Fatalf("removing a missing task: %v", err)
	}
}
