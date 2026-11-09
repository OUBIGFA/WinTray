//go:build windows

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"wintray/internal/config"
)

// Override HKCR only in an isolated child. The test never changes the user's
// .ahk association or requires an installed interpreter to exercise the Shell.
func TestStartProcessAutoHotkeyAssociation(t *testing.T) {
	if len(os.Args) == 4 && os.Args[2] == "--" {
		runAutoHotkeyAssociationTest(t, os.Args[3])
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "hidden", "custom", "hidden-custom", "missing", "broken"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestStartProcessAutoHotkeyAssociation$", "--", mode)
			cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("association child: %v\n%s", err, output)
			}
		})
	}
}

func isolatedScriptAssociation(t *testing.T, command string) {
	t.Helper()
	path := fmt.Sprintf(`Software\WinTray-Test-Association-%d`, os.Getpid())
	root, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close(); _ = registry.DeleteKey(registry.CURRENT_USER, path) })
	if command != "" {
		// Register parent cleanup first so leaf keys are removed before parents.
		for _, sub := range []string{`.ahk`, `WinTray.Script`, `WinTray.Script\shell`, `WinTray.Script\shell\open`, `WinTray.Script\shell\open\command`} {
			key, _, err := registry.CreateKey(root, sub, registry.ALL_ACCESS)
			if err != nil {
				t.Fatal(err)
			}
			var value string
			switch sub {
			case `.ahk`:
				value = "WinTray.Script"
			case `WinTray.Script\shell\open\command`:
				value = command
			}
			if value != "" {
				if err := key.SetStringValue("", value); err != nil {
					t.Fatal(err)
				}
			}
			_ = key.Close()
			t.Cleanup(func() { _ = registry.DeleteKey(root, sub) })
		}
	}
	override := windows.NewLazySystemDLL("advapi32.dll").NewProc("RegOverridePredefKey")
	// Win32's HKEY_CLASSES_ROOT macro casts through signed LONG before HANDLE.
	classesRoot := int32(-2147483648)
	hkcr := uintptr(int64(classesRoot))
	if code, _, _ := override.Call(hkcr, uintptr(root)); code != 0 {
		t.Fatalf("override HKCR: %d", code)
	}
	t.Cleanup(func() { override.Call(hkcr, 0) })
}

type associatedScriptProbe struct {
	Args []string
	Dir  string
	Show uint16
}

func runAutoHotkeyAssociationTest(t *testing.T, mode string) {
	dir := t.TempDir()
	script := filepath.Join(dir, "中文 hotkeys.AHK")
	if err := os.WriteFile(script, []byte("association fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	command := syscall.EscapeArg(self) + ` -test.run=^TestAssociatedScriptProbe$ -- "%1" %*`
	if mode == "missing" {
		command = ""
	} else if mode == "broken" {
		command = `"` + filepath.Join(dir, "missing-interpreter.exe") + `" "%1" %*`
	}
	isolatedScriptAssociation(t, command)
	launch := launchVisible
	if strings.HasPrefix(mode, "hidden") {
		launch = launchNoWindow
	}
	want := []string{script, "", "two words", "中文", `say "hello"`}
	var quoted []string
	for _, arg := range want[1:] {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}
	entry := config.ManagedAppEntry{ExePath: script, Args: strings.Join(quoted, " ")}
	wantDir := dir
	if strings.Contains(mode, "custom") {
		wantDir = filepath.Join(dir, "工作 data")
		if err := os.Mkdir(wantDir, 0700); err != nil {
			t.Fatal(err)
		}
		entry.WorkingDirectory = wantDir
	}
	process, err := startConfiguredProcess(entry, launch)
	if mode == "missing" || mode == "broken" {
		if process != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
			t.Fatal("unusable association returned a process")
		}
		if err == nil {
			t.Fatal("unusable association reported success")
		}
		if mode == "missing" {
			if !errors.Is(err, windows.ERROR_NO_ASSOCIATION) {
				t.Fatalf("missing association error: %v", err)
			}
			svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})
			result := svc.StartNow(context.Background(), config.ManagedAppEntry{ExePath: script}, 1)
			if result.Managed || result.Code != ResultScriptNoAssociation {
				t.Fatalf("missing association did not reach the user-facing error: %+v", result)
			}
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	waitRunnerCommand(t, process)
	data, err := os.ReadFile(script + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var probe associatedScriptProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	show := uint16(windows.SW_SHOWNORMAL)
	if strings.HasPrefix(mode, "hidden") {
		show = windows.SW_HIDE
	}
	if !reflect.DeepEqual(probe.Args, want) || !strings.EqualFold(probe.Dir, wantDir) || probe.Show != show {
		t.Fatalf("association lost parameters/directory/show mode: %+v; want args=%q dir=%s show=%d", probe, want, wantDir, show)
	}
}

func TestAssociatedScriptProbe(t *testing.T) {
	if len(os.Args) < 4 || os.Args[2] != "--" || !isAutoHotkeyScript(os.Args[3]) {
		return
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var startup windows.StartupInfo
	windows.NewLazySystemDLL("kernel32.dll").NewProc("GetStartupInfoW").Call(uintptr(unsafe.Pointer(&startup)))
	data, err := json.Marshal(associatedScriptProbe{Args: os.Args[3:], Dir: dir, Show: startup.ShowWindow})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Args[3]+".json", data, 0600); err != nil {
		t.Fatal(err)
	}
}
