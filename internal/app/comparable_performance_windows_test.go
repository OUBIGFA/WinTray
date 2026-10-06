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
	"syscall"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/config"
	"wintray/internal/ipc"
)

// Run the exact same probe against both UI implementations. Keep readiness
// (including accessibility-provider initialization) in separate processes so
// it cannot distort the memory/idle CPU samples. All samples use warm caches.
func TestSessionComparablePerformance(t *testing.T) {
	if os.Getenv("WINTRAY_COMPARE_PERF") != "1" {
		t.Skip("set WINTRAY_COMPARE_PERF=1 on an interactive Windows desktop")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := filepath.Abs("../../build/WinTray.exe.manifest")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"resources", "controls"} {
		for sample := 0; sample < 3; sample++ {
			t.Run(fmt.Sprintf("%s/%d", mode, sample), func(t *testing.T) {
				dir := t.TempDir()
				settings := config.DefaultSettings()
				settings.ExitAfterManagedAppsCompleted = false
				if err := config.NewStore(filepath.Join(dir, "settings.json")).Save(settings); err != nil {
					t.Fatal(err)
				}
				event := fmt.Sprintf("WinTray_Compare_%d_%d", os.Getpid(), time.Now().UnixNano())
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, self, "-test.run=^TestMainSessionProcess$", "--", dir, manifest, event)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				// The samples measure the real session; termination is outside the timed
				// region and cannot accidentally trigger any real startup registration.
				defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
				pid := uint32(cmd.Process.Pid)
				waitSessionCondition(t, "activation event", func() bool {
					data, _ := os.ReadFile(filepath.Join(dir, "wintray.log"))
					return bytes.Contains(data, []byte("autorun mode: run managed apps"))
				})
				time.Sleep(time.Second)
				report := struct {
					Mode                                 string
					FirstOpenMS                          float64
					Background, Visible, Hidden          sessionResources
					BackgroundCPU, VisibleCPU, HiddenCPU float64 // percent of one logical core
				}{Mode: mode}
				if mode == "resources" {
					report.BackgroundCPU = comparableIdleCPU(t, pid)
					report.Background = readSessionResources(t, pid)
				}
				started := time.Now()
				if !ipc.TrySignalActivation(event) {
					t.Fatal("activation unavailable")
				}
				var hwnd win.HWND
				waitSessionCondition(t, "settings window", func() bool {
					hwnd = visibleSessionWindow(pid)
					return hwnd != 0 && (mode != "controls" || sessionHasButton(hwnd, "退出 WinTray"))
				})
				report.FirstOpenMS = float64(time.Since(started).Microseconds()) / 1000
				if mode == "resources" {
					time.Sleep(200 * time.Millisecond)
					report.VisibleCPU = comparableIdleCPU(t, pid)
					report.Visible = readSessionResources(t, pid)
					win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
					waitSessionCondition(t, "settings hidden", func() bool { return !win.IsWindowVisible(hwnd) })
					time.Sleep(200 * time.Millisecond)
					report.HiddenCPU = comparableIdleCPU(t, pid)
					report.Hidden = readSessionResources(t, pid)
				} else if err := sessionButton(hwnd, "退出 WinTray", true); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				t.Log(string(data))
			})
		}
	}
}

func comparableIdleCPU(t *testing.T, pid uint32) float64 {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	read := func() uint64 {
		var created, exited, kernel, user windows.Filetime
		if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
			t.Fatal(err)
		}
		return (uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)) + (uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime))
	}
	before := read()
	started := time.Now()
	time.Sleep(time.Second)
	return float64(read()-before) * 1e-7 / time.Since(started).Seconds() * 100
}
