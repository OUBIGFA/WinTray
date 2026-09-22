//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/lxn/walk"
	"wintray/internal/config"
	"wintray/internal/ipc"
	"wintray/internal/logging"
	"wintray/internal/orchestrator"
	"wintray/internal/tray"
)

// A host process keeps retrying to place its icon for a while: right after
// logon the notification area may not accept icons yet.
const (
	hostAddAttempts = 30
	hostAddDelay    = time.Second
)

func hostMutexName(pid uint32) string {
	return fmt.Sprintf("WinTray_Host_%d", pid)
}

// spawnHostProcess starts a detached "wintray.exe --host" that owns the
// program's tray icon independently of this process, so the icon stays usable
// after WinTray exits. When the spawn fails nobody can bring the hidden window
// back, so the caller must restore it.
func spawnHostProcess(hw tray.HostedWindow, language string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	spec := hostSpec{PID: hw.ProcessID, Handle: hw.Handle, Name: hw.Name, ExePath: hw.ExePath, Language: language}
	cmd := exec.Command(exe, hostArgs(spec)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// runHostMode is the detached host process. It owns exactly one hosted tray
// icon and ends when the hosted program does. It touches neither the settings,
// the single-instance lock nor the main tray icon, so the main WinTray process
// may exit, restart or never run while the icon keeps working.
func runHostMode(args []string) int {
	logger := newHostLogger()
	defer logger.Close()

	spec, err := parseHostArgs(args)
	if err != nil {
		logger.Warn(fmt.Sprintf("host mode: invalid launch %q: %v", args, err))
		return 2
	}
	lock, alreadyHosted, err := ipc.Acquire(hostMutexName(spec.PID))
	if err != nil {
		logger.Warn(fmt.Sprintf("host mode: lock failed for pid=%d: %v", spec.PID, err))
		return 1
	}
	defer lock.Close()
	if alreadyHosted {
		logger.Info(fmt.Sprintf("host mode: %s pid=%d already hosted, exiting", spec.Name, spec.PID))
		return 0
	}
	logger.Info(fmt.Sprintf("host mode: %s pid=%d hwnd=0x%X", spec.Name, spec.PID, spec.Handle))

	hw := tray.HostedWindow{Name: spec.Name, ExePath: spec.ExePath, Handle: spec.Handle, ProcessID: spec.PID}
	host := tray.NewHost(spec.Language, logger, orchestrator.FindConsoleWindow)
	host.SetOnEmpty(func() {
		logger.Info(fmt.Sprintf("host mode: %s pid=%d ended, exiting", spec.Name, spec.PID))
		walk.App().Exit(0)
	})

	if err := addHostedWithRetry(host, hw, logger); err != nil {
		logger.Warn(fmt.Sprintf("host mode: giving up on %s pid=%d: %v", spec.Name, spec.PID, err))
		host.Restore(hw)
		return 1
	}
	return runHostMessageLoop(logger)
}

// runHostMessageLoop pumps messages until the host asks to exit. The loop is
// walk's own: callbacks queued with Synchronize (the process watcher hands
// icon removal to the UI thread that way) only run from a walk message loop,
// so a hidden walk window drives it.
func runHostMessageLoop(logger *logging.Logger) int {
	loopWindow, err := walk.NewMainWindow()
	if err != nil {
		logger.Warn(fmt.Sprintf("host mode: message loop window failed: %v", err))
		return 1
	}
	defer loopWindow.Dispose()
	loopWindow.Hide()
	return loopWindow.Run()
}

// addHostedWithRetry keeps trying to create the icon unless the program is
// gone, in which case there is nothing left to host.
func addHostedWithRetry(host *tray.Host, hw tray.HostedWindow, logger *logging.Logger) error {
	var err error
	for attempt := 1; attempt <= hostAddAttempts; attempt++ {
		if err = host.TryAdd(hw); err == nil {
			return nil
		}
		if errors.Is(err, tray.ErrHostedProcessUnavailable) {
			return err
		}
		logger.Warn(fmt.Sprintf("host mode: add attempt %d/%d failed for %s pid=%d: %v", attempt, hostAddAttempts, hw.Name, hw.ProcessID, err))
		time.Sleep(hostAddDelay)
	}
	return err
}

// newHostLogger appends to the shared WinTray log. A nil logger is a valid
// no-op logger, so a host without a usable data directory still runs.
func newHostLogger() *logging.Logger {
	appDir, err := config.AppDirWithError()
	if err != nil {
		return nil
	}
	logger, err := logging.New(appDir)
	if err != nil {
		return nil
	}
	return logger
}
