//go:build windows

package startup

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"wintray/internal/config"
)

// Build a real Shell Link with the Windows COM implementation, not a parser
// mock. Nothing here is placed in the user's actual Startup folder.
func createTestStartupShortcut(t *testing.T, path string, value shortcutLaunch) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := comResult(uintptr(win.CoInitializeEx(nil, win.COINIT_APARTMENTTHREADED)), "COM init"); err != nil {
		t.Fatal(err)
	}
	defer win.CoUninitialize()
	var raw unsafe.Pointer
	hr := win.CoCreateInstance((*win.CLSID)(unsafe.Pointer(&clsidShellLink)), nil, win.CLSCTX_INPROC_SERVER,
		(*win.IID)(unsafe.Pointer(&iidShellLinkW)), &raw)
	if err := comResult(uintptr(hr), "create Shell Link"); err != nil {
		t.Fatal(err)
	}
	defer releaseCOM(raw)
	link := (*shellLink)(raw)
	for _, field := range []struct {
		method uintptr
		value  string
	}{
		{link.vtbl.SetPath, value.path}, {link.vtbl.SetArguments, value.arguments}, {link.vtbl.SetWorkingDirectory, value.directory},
	} {
		text, err := windows.UTF16PtrFromString(field.value)
		if err != nil {
			t.Fatal(err)
		}
		r, _, _ := syscall.SyscallN(field.method, uintptr(raw), uintptr(unsafe.Pointer(text)))
		if err := comResult(r, "set Shell Link value"); err != nil {
			t.Fatal(err)
		}
	}
	r, _, _ := syscall.SyscallN(link.vtbl.SetShowCmd, uintptr(raw), uintptr(value.show))
	if err := comResult(r, "set show command"); err != nil {
		t.Fatal(err)
	}
	var persistent unsafe.Pointer
	r, _, _ = syscall.SyscallN(link.vtbl.QueryInterface, uintptr(raw), uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&persistent)))
	if err := comResult(r, "get IPersistFile"); err != nil {
		t.Fatal(err)
	}
	defer releaseCOM(persistent)
	file := (*persistFile)(persistent)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	r, _, _ = syscall.SyscallN(file.vtbl.Save, uintptr(persistent), uintptr(unsafe.Pointer(name)), 1)
	if err := comResult(r, "save Shell Link"); err != nil {
		t.Fatal(err)
	}
	if value.runAsAdmin {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		flags := binary.LittleEndian.Uint32(data[20:24]) | 0x2000
		binary.LittleEndian.PutUint32(data[20:24], flags)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartupShortcutKeepsArgumentsDirectoryShowAndAdminFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "启动 & shortcut.lnk")
	want := shortcutLaunch{
		path: filepath.Join(dir, "程序 & app.exe"), arguments: `--startup "a b" --profile="c\\d"`,
		directory: dir, show: 7, runAsAdmin: true, // SW_SHOWMINNOACTIVE
	}
	createTestStartupShortcut(t, path, want)
	got, err := readStartupShortcut(path)
	if err != nil || got != want {
		t.Fatalf("shortcut attributes changed: got=%+v want=%+v err=%v", got, want, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := findFolderStartupEntries(want.path, dir, false)
	if err != nil || len(entries) != 1 {
		t.Fatalf("find shortcut: entries=%+v err=%v", entries, err)
	}
	entry := entries[0]
	if entry.shortcut != path || entry.args != want.arguments || entry.workingDir != want.directory || entry.show != 7 || !entry.highest {
		t.Fatalf("startup discovery discarded shortcut semantics: %+v", entry)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("reading/resolving modified the original .lnk")
	}
	// Validation must happen before asking ShellExecute to start anything.
	if err := LaunchStartupShortcut(path, `C:\unrelated.exe`); err == nil {
		t.Fatal("changed shortcut target was not rejected")
	}
}

func TestShortcutMigrationUsesOriginalLinkRatherThanRebuildingCommand(t *testing.T) {
	fake := newFakeSchtasks()
	tasks := testAppTasks(t, fake)
	source, _, _ := testRunSource(t)
	const exe = `C:\Apps\Original.exe`
	const shortcut = `C:\User startup\Original.lnk`
	tasks.findEntries = func(string) ([]startupEntry, error) {
		approval, err := readApproval(source.root, source.approvalPath, "Original.lnk")
		if err != nil {
			return nil, err
		}
		enabled, err := approvalEnabled(approval)
		return []startupEntry{{path: exe, shortcut: shortcut, args: "--original", workingDir: `D:\Data`, show: 7,
			label: shortcut, approvalPath: source.approvalPath, approvalName: "Original.lnk", approval: approval, enabled: enabled}}, err
	}
	if err := tasks.Sync(taskSettings(exe), false); err != nil {
		t.Fatal(err)
	}
	var definition taskDefinition
	if err := decodeTaskXML([]byte(fake.tasks[tasks.namePrefix+"1"]), &definition); err != nil {
		t.Fatal(err)
	}
	action := definition.Actions.Exec[0]
	args, err := windows.DecomposeCommandLine(action.Arguments)
	if err != nil || len(args) != 3 || args[0] != AppTaskHelperShortcut || args[1] != shortcut || args[2] != exe || action.WorkingDirectory != `D:\Data` {
		t.Fatalf("task reconstructed/lost original shortcut: %+v args=%q err=%v", action, args, err)
	}
	if err := tasks.Sync(taskSettings(exe), false); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Sync(config.Settings{}, false); err != nil {
		t.Fatal(err)
	}
}
