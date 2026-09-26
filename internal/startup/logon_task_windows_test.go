//go:build windows

package startup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestLogonTaskXMLEscapesPathsAndFingerprintsDefinition(t *testing.T) {
	xmlA, fpA := logonTaskXML("S-1-5-21-1", `D:\A&B\<WinTray>.exe`, "--background --autorun")
	if !strings.Contains(xmlA, `<Command>D:\A&amp;B\&lt;WinTray&gt;.exe</Command>`) ||
		!strings.Contains(xmlA, `<WorkingDirectory>D:\A&amp;B</WorkingDirectory>`) {
		t.Fatalf("paths not escaped:\n%s", xmlA)
	}
	if !strings.Contains(xmlA, fpA) {
		t.Fatalf("fingerprint %q missing from definition", fpA)
	}
	if _, fpB := logonTaskXML("S-1-5-21-1", `D:\A&B\<WinTray>.exe`, "--autorun"); fpB == fpA {
		t.Fatal("different arguments must change the fingerprint")
	}
	if _, fpC := logonTaskXML("S-1-5-21-1", `E:\WinTray.exe`, "--background --autorun"); fpC == fpA {
		t.Fatal("a moved executable must change the fingerprint")
	}
}

func TestTaskUpToDateReadsBothEncodingsAndRejectsDisabled(t *testing.T) {
	const fp = "wintray-task-0123456789abcdef"
	enabled := "<Description>x " + fp + "</Description><Enabled>true</Enabled>"
	disabled := enabled + "<Enabled>false</Enabled>"
	for _, tc := range []struct {
		name     string
		exported []byte
		want     bool
	}{
		{"ansi", []byte(enabled), true},
		{"utf16", utf16LEWithBOM(enabled), true},
		{"other definition", []byte(strings.Replace(enabled, fp, "wintray-task-ffffffffffffffff", 1)), false},
		{"disabled ansi", []byte(disabled), false},
		{"disabled utf16", utf16LEWithBOM(disabled), false},
	} {
		if got := taskUpToDate(tc.exported, fp); got != tc.want {
			t.Errorf("%s: taskUpToDate = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// Registers a real, uniquely named task for the current user and removes it:
// the definition must be accepted without administrator rights and a second
// Sync must recognise the registered task instead of re-creating it.
func TestLogonTaskRegistersWithSchtasks(t *testing.T) {
	task, err := NewLogonTask()
	if err != nil {
		t.Fatal(err)
	}
	task.name = fmt.Sprintf("WinTrayTest-%d", os.Getpid())
	var calls []string
	run := task.run
	task.run = func(args ...string) ([]byte, error) {
		calls = append(calls, args[0])
		return run(args...)
	}
	t.Cleanup(func() { _, _ = run("/Delete", "/TN", task.name, "/F") })
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")

	if err = task.Sync(exe, "/c exit"); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	calls = nil
	if err = task.Sync(exe, "/c exit"); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if strings.Join(calls, ",") != "/Query" {
		t.Fatalf("unchanged task was re-registered: %v", calls)
	}
	calls = nil
	if err = task.Sync(exe, "/c exit 1"); err != nil {
		t.Fatalf("changed Sync: %v", err)
	}
	if strings.Join(calls, ",") != "/Query,/Create" {
		t.Fatalf("changed arguments not registered: %v", calls)
	}
	if err = task.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err = run("/Query", "/TN", task.name); err == nil {
		t.Fatal("task still exists after Remove")
	}
	if err = task.Remove(); err != nil {
		t.Fatalf("Remove of a missing task: %v", err)
	}
}

type fakeTask struct {
	syncErr, removeErr error
	synced             []string
	removed            int
}

func (f *fakeTask) Sync(exePath, args string) error {
	f.synced = append(f.synced, exePath+" "+args)
	return f.syncErr
}

func (f *fakeTask) Remove() error {
	f.removed++
	return f.removeErr
}

func testRegistrar(t *testing.T, task logonTask) (*Registrar, registry.Key) {
	t.Helper()
	path := fmt.Sprintf(`Software\WinTrayTests-%d-%d\Run`, os.Getpid(), testRunKeyID.Add(1))
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		key.Close()
		registry.DeleteKey(registry.CURRENT_USER, path)
		registry.DeleteKey(registry.CURRENT_USER, filepath.Dir(path))
	})
	return &Registrar{appName: "WinTray", runPath: path, task: task}, key
}

func TestRegistrarPrefersLogonTaskAndRemovesRunValue(t *testing.T) {
	task := &fakeTask{}
	r, key := testRegistrar(t, task)
	if err := key.SetStringValue("WinTray", `"D:\old\WinTray.exe" --autorun`); err != nil {
		t.Fatal(err)
	}
	taskErr, err := r.Apply(`D:\WinTray.exe`, "--autorun", true)
	if taskErr != nil || err != nil {
		t.Fatalf("Apply: taskErr=%v err=%v", taskErr, err)
	}
	if len(task.synced) != 1 || task.synced[0] != `D:\WinTray.exe --autorun` {
		t.Fatalf("task not synced: %v", task.synced)
	}
	if _, _, err = key.GetStringValue("WinTray"); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("Run value kept next to the logon task: %v", err)
	}
	// Settings saves repeat the same request; no further schtasks calls.
	_, _ = r.Apply(`D:\WinTray.exe`, "--autorun", true)
	if len(task.synced) != 1 {
		t.Fatalf("unchanged request re-synced: %v", task.synced)
	}
}

func TestRegistrarFallsBackToRunValueWhenTaskFails(t *testing.T) {
	task := &fakeTask{syncErr: errors.New("access denied")}
	r, key := testRegistrar(t, task)
	taskErr, err := r.Apply(`D:\Win Tray\WinTray.exe`, "--background --autorun", true)
	if err != nil || taskErr == nil {
		t.Fatalf("Apply: taskErr=%v err=%v, want task error only", taskErr, err)
	}
	got, _, err := key.GetStringValue("WinTray")
	if err != nil || got != `"D:\Win Tray\WinTray.exe" --background --autorun` {
		t.Fatalf("Run value = %q, %v", got, err)
	}
	// A failed task is retried on the next request rather than cached.
	task.syncErr = nil
	if taskErr, err = r.Apply(`D:\Win Tray\WinTray.exe`, "--background --autorun", true); taskErr != nil || err != nil {
		t.Fatalf("retry: taskErr=%v err=%v", taskErr, err)
	}
	if _, _, err = key.GetStringValue("WinTray"); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("Run value kept after the task succeeded: %v", err)
	}
}

func TestRegistrarDisableRemovesTaskAndRunValue(t *testing.T) {
	task := &fakeTask{}
	r, key := testRegistrar(t, task)
	if err := key.SetStringValue("WinTray", `"D:\WinTray.exe" --autorun`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(`D:\WinTray.exe`, "--autorun", false); err != nil {
		t.Fatal(err)
	}
	if task.removed != 1 {
		t.Fatalf("task removed %d times", task.removed)
	}
	if _, _, err := key.GetStringValue("WinTray"); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("Run value kept: %v", err)
	}
	task.removeErr = errors.New("busy")
	r.applied = ""
	if _, err := r.Apply(`D:\WinTray.exe`, "--autorun", false); err == nil {
		t.Fatal("task removal failure was not reported")
	}
}

func TestRegistrarRemoveIgnoresCacheAndAllowsReRegistration(t *testing.T) {
	task := &fakeTask{}
	r, key := testRegistrar(t, task)
	if _, err := r.Apply(`D:\WinTray.exe`, "--autorun", true); err != nil {
		t.Fatal(err)
	}
	// A Run value written by an older version must go as well.
	if err := key.SetStringValue("WinTray", `"D:\WinTray.exe" --autorun`); err != nil {
		t.Fatal(err)
	}
	if err := r.Remove(); err != nil {
		t.Fatal(err)
	}
	if task.removed != 1 {
		t.Fatalf("task removed %d times", task.removed)
	}
	if _, _, err := key.GetStringValue("WinTray"); !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("Run value kept: %v", err)
	}
	if _, err := r.Apply(`D:\WinTray.exe`, "--autorun", true); err != nil {
		t.Fatal(err)
	}
	if len(task.synced) != 2 {
		t.Fatalf("switching back on after Remove did not register again: %v", task.synced)
	}
}
