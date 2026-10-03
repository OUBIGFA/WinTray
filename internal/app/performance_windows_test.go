//go:build windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/ipc"
)

type sessionResources struct {
	PrivateBytes    uintptr
	WorkingSetBytes uintptr
	Handles         uint32
	GDI             uintptr
	USER            uintptr
}

// Native counters include Go and Windows allocations. WorkingSetBytes is the
// total working set, not the private working set. No forced GC or trimming.
func readSessionResources(t *testing.T, pid uint32) sessionResources {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	var counters struct {
		Size, PageFaults                                                     uint32
		PeakWorkingSet, WorkingSet, PeakPagedPool, PagedPool                 uintptr
		PeakNonPagedPool, NonPagedPool, Pagefile, PeakPagefile, PrivateUsage uintptr
	}
	counters.Size = uint32(unsafe.Sizeof(counters))
	proc := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	if ok, _, err := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size)); ok == 0 {
		t.Fatal(err)
	}
	var count uint32
	if ok, _, err := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount").Call(uintptr(h), uintptr(unsafe.Pointer(&count))); ok == 0 {
		t.Fatal(err)
	}
	gui := windows.NewLazySystemDLL("user32.dll").NewProc("GetGuiResources")
	gdi, _, _ := gui.Call(uintptr(h), 0)
	user, _, _ := gui.Call(uintptr(h), 1)
	if gdi == 0 || user == 0 {
		t.Fatal("session GUI resource counters unavailable")
	}
	return sessionResources{PrivateBytes: counters.PrivateUsage, WorkingSetBytes: counters.WorkingSet, Handles: count, GDI: gdi, USER: user}
}

// Opt-in end-to-end measurements use the real session in a new process, with
// isolated settings and no system startup registration. Startup ends when
// the session dispatches its login queue. First-open ends at visible controls;
// these are warm filesystem-cache samples, not a Windows cold-boot benchmark.
func TestSessionStartupAndMemory(t *testing.T) {
	if os.Getenv("WINTRAY_PERF_TEST") != "1" {
		t.Skip("set WINTRAY_PERF_TEST=1 on an interactive Windows desktop")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := filepath.Abs("../../build/WinTray.exe.manifest")
	if err != nil {
		t.Fatal(err)
	}
	for sample := 0; sample < 3; sample++ {
		t.Run(fmt.Sprint(sample), func(t *testing.T) {
			dir := t.TempDir()
			settings := config.DefaultSettings()
			settings.ExitAfterManagedAppsCompleted = false
			if err := config.NewStore(filepath.Join(dir, "settings.json")).Save(settings); err != nil {
				t.Fatal(err)
			}
			event := fmt.Sprintf("WinTray_Perf_%d_%d", os.Getpid(), time.Now().UnixNano())
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestMainSessionProcess$", "--", dir, manifest, event)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			started := time.Now()
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
			pid := uint32(cmd.Process.Pid)
			waitSessionCondition(t, "startup queue dispatch", func() bool {
				data, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
				return strings.Contains(string(data), "autorun mode: run managed apps")
			})
			startupMS := float64(time.Since(started).Microseconds()) / 1000
			time.Sleep(time.Second)
			background := readSessionResources(t, pid)
			opened := time.Now()
			if !ipc.TrySignalActivation(event) {
				t.Fatal("activation unavailable")
			}
			var hwnd win.HWND
			waitSessionCondition(t, "usable settings", func() bool {
				hwnd = visibleSessionWindow(pid)
				return hwnd != 0 && findSessionButton(hwnd, "退出 WinTray") != 0
			})
			openMS := float64(time.Since(opened).Microseconds()) / 1000
			time.Sleep(200 * time.Millisecond)
			visible := readSessionResources(t, pid)
			win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
			waitSessionCondition(t, "settings hidden", func() bool { return !win.IsWindowVisible(hwnd) })
			time.Sleep(200 * time.Millisecond)
			hidden := readSessionResources(t, pid)
			if !ipc.TrySignalActivation(event) {
				t.Fatal("reopening unavailable")
			}
			waitSessionCondition(t, "settings reopened", func() bool { return win.IsWindowVisible(hwnd) })
			clickSessionButton(t, hwnd, "退出 WinTray")
			if err := cmd.Wait(); err != nil {
				t.Fatalf("session exit: %v: %s", err, output.String())
			}
			report, err := json.Marshal(struct {
				StartupMS                   float64
				FirstOpenMS                 float64
				Background, Visible, Hidden sessionResources
			}{startupMS, openMS, background, visible, hidden})
			if err != nil {
				t.Fatal(err)
			}
			t.Log(string(report))
		})
	}
}
