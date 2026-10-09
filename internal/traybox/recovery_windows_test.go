//go:build windows

package traybox

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// The icon owner and shell survive in this process. Only the collector is
// killed, exactly as when Task Manager terminates WinTray without its cleanup.
func TestCollectedIconsReturnAfterForcedTermination(t *testing.T) {
	for _, action := range []string{"kill collector", "normal exit", "kill recovery"} {
		t.Run(action, func(t *testing.T) {
			f := startFakeShell(t)
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd, l, recoveryPID := startCrashCollector(t, f.target, self)
			owner := uint32(f.program)
			for _, uid := range []uint32{71, 72, 73, 74, 75} {
				req := trayRequest{message: nimAdd, hwnd: owner, uid: uid, flags: nifTip, tip: "crash recovery fixture"}
				if uid == 72 || uid == 75 {
					req.flags |= nifState
					req.state, req.mask = nisHidden, nisHidden
				}
				send(l, req)
			}
			send(l, trayRequest{message: nimDelete, hwnd: owner, uid: 73})
			send(l, trayRequest{message: nimModify, hwnd: owner, uid: 74, flags: nifState, state: nisHidden, mask: nisHidden})
			send(l, trayRequest{message: nimModify, hwnd: owner, uid: 75, flags: nifState, mask: nisHidden})
			guid := [16]byte{81, 4, 9}
			send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 76, flags: nifGUID, guid: guid})
			send(l, trayRequest{message: nimModify, flags: nifGUID | nifTip, guid: guid, tip: "GUID-only update"})
			got, _, _ := f.snapshot()
			if len(got) < 5 || !isHidden(got[0]) {
				t.Fatalf("fixture was not collected: %+v", got)
			}
			f.clear()
			switch action {
			case "kill collector":
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			case "normal exit":
				win.PostMessage(l.hwnd, win.WM_CLOSE, 0, 0)
			case "kill recovery":
				helper, err := os.FindProcess(int(recoveryPID))
				if err != nil {
					t.Fatal(err)
				}
				if err := helper.Kill(); err != nil {
					t.Fatal(err)
				}
				_ = helper.Release()
			}
			waitFor(t, "original icons shown after "+action, func() bool {
				requests, _, _ := f.snapshot()
				shown := map[uint32]bool{}
				for _, r := range requests {
					if isShown(r) {
						shown[r.UID] = true
					}
				}
				return shown[71] && shown[75] && (shown[76] || action != "kill collector" && len(shown) >= 3)
			})
			requests, _, _ := f.snapshot()
			for _, r := range requests {
				if r.UID == 72 || r.UID == 73 || r.UID == 74 {
					t.Fatalf("restored a hidden or deleted icon: %+v", r)
				}
			}
			if action == "kill recovery" {
				// Once released, idle healing must not keep overriding native
				// state or sending redundant restoration requests each second.
				time.Sleep(1200 * time.Millisecond)
				after, _, _ := f.snapshot()
				if len(after) != len(requests) {
					t.Fatalf("repeated restoration after release: %d -> %d", len(requests), len(after))
				}
				f.clear()
				send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 80})
				requests, _, _ = f.snapshot()
				if len(requests) != 1 || isHidden(requests[0]) {
					t.Fatalf("continued collecting without recovery: %+v", requests)
				}
			}
		})
	}
}

func TestRecoverySerializesQuickRestart(t *testing.T) {
	f := startFakeShell(t)
	start := func() (*recoveryClient, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestTrayRecoveryProcess$", "--", strconv.FormatUint(uint64(f.target), 10))
		cmd.Stderr = os.Stderr
		return startRecovery(cmd)
	}
	first, err := start()
	if err != nil {
		t.Fatal(err)
	}
	defer first.abort()
	created, err := processCreation(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	icon := recoveryIcon{stateRequest(trayRequest{hwnd: uint32(f.program), uid: 1}.bytes(), false), uint32(os.Getpid()), created}
	if err := first.exchange(&recoveryFrame{Icons: []recoveryIcon{icon}}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f.setRequestHook(func(trayData) { close(entered); <-release })
	first.close()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("recovery did not start")
	}
	type started struct {
		client *recoveryClient
		err    error
	}
	ready := make(chan started, 1)
	go func() { c, err := start(); ready <- started{c, err} }()
	select {
	case got := <-ready:
		if got.client != nil {
			got.client.abort()
		}
		t.Fatalf("new collector started before previous recovery finished: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case got := <-ready:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if err := got.client.stop(); err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("new collector did not resume after recovery")
	}
}

func TestRecoveryUsesLastCompleteSnapshotOnBrokenMessage(t *testing.T) {
	f := startFakeShell(t)
	c, err := startRecovery(exec.Command(os.Args[0], "-test.run=^TestTrayRecoveryProcess$", "--", strconv.FormatUint(uint64(f.target), 10)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.abort()
	created, err := processCreation(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	icon := recoveryIcon{stateRequest(trayRequest{hwnd: uint32(f.program), uid: 1}.bytes(), false), uint32(os.Getpid()), created}
	if err := c.exchange(&recoveryFrame{Icons: []recoveryIcon{icon}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.input.Write([]byte(`{"Icons":[`)); err != nil {
		t.Fatal(err)
	}
	c.close()
	waitFor(t, "last acknowledged icon restored", func() bool { r, _, _ := f.snapshot(); return len(r) == 1 && isShown(r[0]) })
}

// The real shell test observes Shell_NotifyIconGetRect, not just our fake
// shell's messages. It only registers an icon belonging to this test process.
func TestCollectedIconsReturnAfterForcedTerminationLive(t *testing.T) {
	if os.Getenv("WINTRAY_TRAY_LIVE_TEST") != "1" {
		t.Skip("set WINTRAY_TRAY_LIVE_TEST=1 with WinTray closed")
	}
	f := startFakeShell(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd, collector, _ := startCrashCollector(t, 0, self)
	nid := win.NOTIFYICONDATA{HWnd: f.program, UID: 71, UFlags: win.NIF_ICON | win.NIF_TIP, HIcon: win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	if !win.Shell_NotifyIcon(win.NIM_ADD, &nid) {
		t.Fatal("register native test icon")
	}
	defer win.Shell_NotifyIcon(win.NIM_DELETE, &nid)
	request := trayRequest{hwnd: uint32(f.program), uid: 71}.bytes()
	// The collector deliberately supplies a synthetic rectangle to clients.
	// Probe Explorer directly while it is active to measure real visibility.
	explorer := &listener{hwnd: explorerTray(0)}
	waitFor(t, "native icon collected", func() bool {
		a := queryListenerRect(explorer, rectWire(uint32(f.program), 71, 1, [16]byte{}))
		b := queryListenerRect(explorer, rectWire(uint32(f.program), 71, 2, [16]byte{}))
		return a == 0 || int16(b) <= 0 || int16(b>>16) <= 0
	})
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "native Explorer icon visible after forced termination", func() bool { return !isWindow(collector.hwnd) && shownByExplorer(request) })
}

func startCrashCollector(t *testing.T, target win.HWND, selected string) (*exec.Cmd, *listener, uint32) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestCollectionCrashProcess$", "--", strconv.FormatUint(uint64(target), 10), selected)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	var hwnd uint64
	var recoveryPID uint32
	select {
	case line := <-ready:
		if _, err := fmt.Sscan(line, &hwnd, &recoveryPID); err != nil {
			t.Fatalf("collector startup: %q: %v", line, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("collector did not start")
	}
	helper, err := windows.OpenProcess(windows.SYNCHRONIZE, false, recoveryPID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		status, err := windows.WaitForSingleObject(helper, 20000)
		windows.CloseHandle(helper)
		if err != nil || status != windows.WAIT_OBJECT_0 {
			t.Errorf("recovery process did not exit: %d %v", status, err)
		}
	})
	return cmd, &listener{hwnd: win.HWND(hwnd)}, recoveryPID
}

func TestCollectionCrashProcess(t *testing.T) {
	if len(os.Args) != 5 || os.Args[2] != "--" {
		return
	}
	target, err := strconv.ParseUint(os.Args[3], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	cfg := listenerConfig{
		className: "WinTrayCrashTestTray",
		target:    func(win.HWND) win.HWND { return win.HWND(target) },
		newRecovery: func() (*recoveryClient, error) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestTrayRecoveryProcess$", "--", os.Args[3])
			return startRecovery(cmd)
		},
	}
	if target == 0 {
		cfg.className, cfg.target, cfg.raise = trayWindowClass, explorerTray, true
	}
	var l *listener
	if recoveryExe := os.Getenv("WINTRAY_RECOVERY_EXE"); target == 0 && recoveryExe != "" {
		// Exercise the shipped executable and the production Box wiring when
		// package validation supplies a companion, not a test-process helper.
		b := NewBox(filepath.Join(filepath.Dir(recoveryExe), "WinTray.exe"), recoveryTestLogger{})
		err = b.SetPaths([]string{os.Args[4]})
		l = b.listener
	} else {
		l, err = startListener(cfg, "", []string{os.Args[4]}, recoveryTestLogger{})
	}
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(uint64(l.hwnd), l.recovery.process.Pid)
	<-l.done
}

type recoveryTestLogger struct{}

func (recoveryTestLogger) Info(msg string) { fmt.Fprintln(os.Stderr, msg) }
func (recoveryTestLogger) Warn(msg string) { fmt.Fprintln(os.Stderr, msg) }

func TestTrayRecoveryProcess(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	target, err := strconv.ParseUint(os.Args[3], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(win.HWND) win.HWND { return win.HWND(target) }
	if target == 0 {
		resolve = explorerTray
	}
	err = runRecovery(os.Stdin, os.Stdout, resolve, `Local\WinTray_RecoveryTest_`+os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Stdout is a private acknowledgement pipe, just like the shipped
	// companion. Do not let the test harness append PASS/coverage reports.
	os.Exit(0)
}

func TestRecoveryRejectsReusedProcessIdentityAndRetriesShell(t *testing.T) {
	f := startFakeShell(t)
	created, err := processCreation(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	icon := recoveryIcon{stateRequest(trayRequest{hwnd: uint32(f.program), uid: 1}.bytes(), false), uint32(os.Getpid()), created + 1}
	if err := restoreRecoveryIcons([]recoveryIcon{icon}, func(win.HWND) win.HWND { return f.target }); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := f.snapshot(); len(got) != 0 {
		t.Fatal("recovery changed a different process's icon")
	}
	icon.Created = created
	attempts := 0
	f.setReply(func(trayData) uintptr {
		attempts++
		if attempts == 1 {
			return 0
		}
		return 1
	})
	if err := restoreRecoveryIcons([]recoveryIcon{icon}, func(win.HWND) win.HWND { return f.target }); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := f.snapshot(); len(got) != 2 || !isShown(got[1]) {
		t.Fatalf("did not retry native restoration: %+v", got)
	}
}

func TestMissingRecoveryDoesNotStartCollector(t *testing.T) {
	l, err := startListener(listenerConfig{newRecovery: func() (*recoveryClient, error) {
		return startRecovery(exec.Command(`C:\WinTray-test-only\missing-recovery.exe`))
	}}, "", []string{`C:\WinTray-test-only\selected.exe`}, quietLogger{})
	if err == nil || l != nil || activeListener.Load() != nil {
		t.Fatalf("started without recovery: %v %v", l, err)
	}
}
