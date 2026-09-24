//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/ipc"
	"wintray/internal/logging"
	"wintray/internal/orchestrator"
)

// These opt-in tests exercise the real session/message loop, processes and
// hosted icons, but inject a no-op registrar and use only temporary settings,
// log files and a unique activation event. No user autorun entries are touched.
func TestMainSessionHostingLifecycle(t *testing.T) {
	if os.Getenv("WINTRAY_UI_TEST") != "1" {
		t.Skip("set WINTRAY_UI_TEST=1 on an interactive Windows desktop")
	}
	for _, mode := range []string{"last program exits", "manual exit restores", "exit during startup", "settings remain open", "no hosted programs"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Cleanup(func() {
				if t.Failed() {
					data, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
					t.Logf("session log:\n%s", data)
				}
			})
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			image, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			sessionExe := filepath.Join(dir, "wintray-session-test.exe")
			if err := os.WriteFile(sessionExe, image, 0o600); err != nil {
				t.Fatal(err)
			}
			settings := config.DefaultSettings()
			settings.StartupIntervalSeconds = 0
			if mode == "exit during startup" {
				settings.StartupIntervalSeconds = 120
			}
			var releases []string
			if mode != "no hosted programs" {
				for i := 0; i < 2; i++ {
					exe := filepath.Join(dir, fmt.Sprintf("console-%d.exe", i))
					if err := os.WriteFile(exe, image, 0o600); err != nil {
						t.Fatal(err)
					}
					release := filepath.Join(dir, fmt.Sprintf("release-%d", i))
					releases = append(releases, release)
					settings.ManagedApps = append(settings.ManagedApps, config.ManagedAppEntry{
						Name: fmt.Sprintf("WinTray test %d", i), ExePath: exe, RunOnStartup: true,
						Args:         "-test.run=^TestSessionConsoleProcess$ -- " + syscall.EscapeArg(release),
						TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true},
					})
				}
			}
			if err := config.NewStore(filepath.Join(dir, "settings.json")).Save(settings); err != nil {
				t.Fatal(err)
			}
			manifest, err := filepath.Abs("../../build/WinTray.exe.manifest")
			if err != nil {
				t.Fatal(err)
			}
			eventName := fmt.Sprintf("WinTray_SessionTest_%d_%d", os.Getpid(), time.Now().UnixNano())
			cmd := exec.Command(sessionExe, "-test.run=^TestMainSessionProcess$", "--", dir, manifest, eventName)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var waitErr error
			go func() { waitErr = cmd.Wait(); close(done) }()
			t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
			awaitExit := func() {
				t.Helper()
				select {
				case <-done:
					if waitErr != nil {
						t.Fatalf("session failed: %v", waitErr)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("session did not exit")
				}
			}
			if mode == "no hosted programs" {
				awaitExit()
				return
			}

			var processes []windows.Handle
			var consoleWindows []win.HWND
			startedReleases := releases
			if mode == "exit during startup" {
				startedReleases = releases[:1]
			}
			for _, release := range startedReleases {
				var pid uint32
				waitSessionCondition(t, "console PID file", func() bool {
					data, err := os.ReadFile(release + ".pid")
					value, parseErr := strconv.ParseUint(string(data), 10, 32)
					pid = uint32(value)
					return err == nil && parseErr == nil && pid != 0
				})
				h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, pid)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = windows.TerminateProcess(h, 0)
					_, _ = windows.WaitForSingleObject(h, 3000)
					_ = windows.CloseHandle(h)
				})
				processes = append(processes, h)
				var hwnd uintptr
				waitSessionCondition(t, "hidden console window", func() bool { hwnd = orchestrator.FindConsoleWindow(pid); return hwnd != 0 })
				if win.IsWindowVisible(win.HWND(hwnd)) {
					t.Fatal("hosted console should start hidden")
				}
				consoleWindows = append(consoleWindows, win.HWND(hwnd))
			}
			waitSessionCondition(t, "both hosted icons", func() bool {
				data, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
				return strings.Count(string(data), "tray host: added ") == len(startedReleases)
			})
			if visibleSessionWindow(uint32(cmd.Process.Pid)) != 0 {
				t.Fatal("automatic-exit session flashed its settings window")
			}
			// The session executable may occur only once: hosting must not spawn
			// additional --host copies of it.
			if countSessionProcesses(t) != 1 {
				t.Fatal("hosting spawned additional WinTray session processes")
			}
			select {
			case <-done:
				t.Fatal("main process exited while hosting programs")
			default:
			}

			if mode != "last program exits" {
				if !ipc.TrySignalActivation(eventName) {
					t.Fatal("could not reopen settings")
				}
				var hwnd win.HWND
				waitSessionCondition(t, "reopened settings", func() bool { hwnd = visibleSessionWindow(uint32(cmd.Process.Pid)); return hwnd != 0 })
				if mode == "settings remain open" {
					for _, release := range releases {
						if err := os.WriteFile(release, nil, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					waitSessionCondition(t, "hosted programs ended", func() bool {
						data, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
						return strings.Count(string(data), " exited") == 2
					})
					select {
					case <-done:
						t.Fatal("last program ending closed the open settings")
					default:
					}
					if !win.IsWindowVisible(hwnd) {
						t.Fatal("settings were hidden unexpectedly")
					}
					win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
					awaitExit()
					return
				}
				button := findSessionExitButton(hwnd)
				if button == 0 {
					t.Fatal("Exit WinTray button not found")
				}
				// Send the native button notification. BM_CLICK is unreliable
				// across processes when Windows refuses foreground activation.
				controlID, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("GetDlgCtrlID").Call(uintptr(button))
				win.SendMessage(win.GetParent(button), win.WM_COMMAND, controlID, uintptr(button))
				awaitExit()
				if mode == "exit during startup" {
					if _, err := os.Stat(releases[1] + ".pid"); !os.IsNotExist(err) {
						t.Fatal("pending program launched after explicit exit")
					}
				}
				for i, hwnd := range consoleWindows {
					waitSessionCondition(t, "restored console", func() bool { return win.IsWindowVisible(hwnd) })
					if status, _ := windows.WaitForSingleObject(processes[i], 0); status != uint32(windows.WAIT_TIMEOUT) {
						t.Fatal("manual exit killed a hosted program")
					}
				}
				return
			}
			if err := os.WriteFile(releases[0], nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if status, _ := windows.WaitForSingleObject(processes[0], 3000); status != windows.WAIT_OBJECT_0 {
				t.Fatal("first hosted program did not exit")
			}
			select {
			case <-done:
				t.Fatal("session exited while second program remained")
			default:
			}
			if err := os.WriteFile(releases[1], nil, 0o600); err != nil {
				t.Fatal(err)
			}
			awaitExit()
		})
	}
}

func waitSessionCondition(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func visibleSessionWindow(pid uint32) win.HWND {
	for _, w := range orchestrator.NewWin32WindowEnumerator().EnumerateTopLevelWindows() {
		if w.ProcessID == pid && w.Title == "WinTray" {
			return win.HWND(w.Handle)
		}
	}
	return 0
}

func findSessionExitButton(parent win.HWND) win.HWND {
	getText := windows.NewLazySystemDLL("user32.dll").NewProc("GetWindowTextW")
	for child := win.GetWindow(parent, win.GW_CHILD); child != 0; child = win.GetWindow(child, win.GW_HWNDNEXT) {
		var text [128]uint16
		getText.Call(uintptr(child), uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
		if windows.UTF16ToString(text[:]) == "退出 WinTray" {
			return child
		}
		if found := findSessionExitButton(child); found != 0 {
			return found
		}
	}
	return 0
}

func countSessionProcesses(t *testing.T) int {
	t.Helper()
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	count := 0
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if windows.UTF16ToString(entry.ExeFile[:]) == "wintray-session-test.exe" {
			count++
		}
	}
	return count
}

func TestMainSessionProcess(t *testing.T) {
	if len(os.Args) != 6 || os.Args[2] != "--" {
		return
	}
	dir, manifest, eventName := os.Args[3], os.Args[4], os.Args[5]
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	source, err := windows.UTF16PtrFromString(manifest)
	if err != nil {
		t.Fatal(err)
	}
	act := win.CreateActCtx(&win.ACTCTX{Source: source})
	if act == win.HANDLE(windows.InvalidHandle) {
		t.Fatal("test manifest unavailable")
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	defer kernel.NewProc("ReleaseActCtx").Call(uintptr(act))
	cookie, ok := win.ActivateActCtx(act)
	if !ok {
		t.Fatal("activate test manifest")
	}
	defer kernel.NewProc("DeactivateActCtx").Call(0, cookie)
	store := config.NewStore(filepath.Join(dir, "settings.json"))
	settings, err := store.LoadWithError()
	if err != nil {
		t.Fatal(err)
	}
	logger, err := logging.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	code := runMainSession([]string{"--autorun", "--background"}, settings, sessionServices{
		store: store, logger: logger, activationName: eventName, setRunAtLogon: func(config.Settings) {},
	})
	if code != 0 {
		t.Fatalf("session exit code %d", code)
	}
}

func TestSessionConsoleProcess(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	release := os.Args[3]
	if err := os.WriteFile(release+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("parent did not release console helper")
}
