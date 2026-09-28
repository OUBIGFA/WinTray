//go:build windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	startupUser32         = windows.NewLazySystemDLL("user32.dll")
	startupGetShellWindow = startupUser32.NewProc("GetShellWindow")
	startupFindWindowEx   = startupUser32.NewProc("FindWindowExW")
	startupSendMessage    = startupUser32.NewProc("SendMessageTimeoutW")
)

// WaitStartupReady holds a logon helper until both WinTray's marker is signaled
// and the desktop Shell is responsive. A timeout or cancellation is a failure,
// never permission to launch. The marker is checked again after probing Shell
// so a WinTray that exited during the wait cannot release the helper.
func WaitStartupReady(ctx context.Context, name string, poll time.Duration) error {
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return fmt.Errorf("WinTray ready marker: %w", err)
	}
	return waitStartupReady(ctx, poll, func() error {
		h, err := windows.OpenEvent(windows.SYNCHRONIZE, false, namePtr)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(h)
		state, err := windows.WaitForSingleObject(h, 0)
		if err != nil {
			return err
		}
		if state != windows.WAIT_OBJECT_0 {
			return errors.New("marker is not signaled")
		}
		return nil
	}, nativeDesktopShell().check)
}

// WaitDesktopShell waits for the actual desktop Shell and its own taskbar,
// not another process's Shell_TrayWnd (such as WinTray's tray collector).
// Readiness is probed immediately; poll only spaces unsuccessful probes.
func WaitDesktopShell(ctx context.Context, poll time.Duration) error {
	return waitReadiness(ctx, poll, "desktop Shell", nativeDesktopShell().check)
}

func waitStartupReady(ctx context.Context, poll time.Duration, marker func() error, shell func(context.Context) error) error {
	return waitReadiness(ctx, poll, "startup readiness", func(ctx context.Context) error {
		if err := marker(); err != nil {
			return fmt.Errorf("WinTray ready: %w", err)
		}
		if err := shell(ctx); err != nil {
			return fmt.Errorf("desktop Shell: %w", err)
		}
		if err := marker(); err != nil {
			return fmt.Errorf("WinTray ready: %w", err)
		}
		return nil
	})
}

func waitReadiness(ctx context.Context, poll time.Duration, stage string, probe func(context.Context) error) error {
	if poll <= 0 {
		return errors.New("readiness poll interval must be positive")
	}
	pending := stage
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for %s: %w", pending, err)
		}
		err := probe(ctx)
		if err != nil {
			pending = stage + ": " + err.Error()
		}
		// A successful probe must not overrule a concurrent timeout/cancel.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("waiting for %s: %w", pending, ctxErr)
		}
		if err == nil {
			return nil
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("waiting for %s: %w", pending, ctx.Err())
		case <-timer.C:
		}
	}
}

// These probes keep readiness tests independent of the user's Explorer and do
// not create fake taskbars or change system state.
type desktopShellProbe struct {
	shellWindow func() uintptr
	processID   func(uintptr) uint32
	nextTaskbar func(uintptr) uintptr
	responsive  func(context.Context, uintptr) bool
}

func (p desktopShellProbe) check(ctx context.Context) error {
	shell := p.shellWindow()
	if shell == 0 {
		return errors.New("desktop window is absent")
	}
	pid := p.processID(shell)
	if pid == 0 {
		return errors.New("desktop process is unavailable")
	}
	var after uintptr
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tray := p.nextTaskbar(after)
		if tray == 0 {
			return fmt.Errorf("no Shell_TrayWnd owned by desktop pid=%d", pid)
		}
		after = tray
		if p.processID(tray) != pid {
			continue
		}
		if !p.responsive(ctx, shell) || !p.responsive(ctx, tray) {
			return fmt.Errorf("desktop/taskbar pid=%d did not respond to WM_NULL", pid)
		}
		if p.shellWindow() != shell || p.processID(shell) != pid || p.processID(tray) != pid {
			return errors.New("desktop/taskbar changed during readiness probe")
		}
		return nil
	}
}

func nativeDesktopShell() desktopShellProbe {
	class, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	return desktopShellProbe{
		shellWindow: func() uintptr {
			h, _, _ := startupGetShellWindow.Call()
			return h
		},
		processID: func(h uintptr) uint32 {
			var pid uint32
			win.GetWindowThreadProcessId(win.HWND(h), &pid)
			return pid
		},
		nextTaskbar: func(after uintptr) uintptr {
			h, _, _ := startupFindWindowEx.Call(0, after, uintptr(unsafe.Pointer(class)), 0)
			return h
		},
		responsive: func(ctx context.Context, h uintptr) bool {
			if ctx.Err() != nil {
				return false
			}
			// Bound each synchronous message, including when cancellation has
			// no deadline. Do not use SMTO_NOTIMEOUTIFNOTHUNG: it can wait forever.
			wait := 100 * time.Millisecond
			if deadline, ok := ctx.Deadline(); ok {
				wait = min(wait, time.Until(deadline))
			}
			if wait <= 0 {
				return false
			}
			const flags = 0x0001 | 0x0002 | 0x0020 // BLOCK | ABORTIFHUNG | ERRORONEXIT
			milliseconds := max(1, (wait+time.Millisecond-1)/time.Millisecond)
			// WM_NULL changes no window state. Its result is normally zero;
			// readiness depends on SendMessageTimeout's success return instead.
			r, _, _ := startupSendMessage.Call(h, win.WM_NULL, 0, 0, flags, uintptr(milliseconds), 0)
			return r != 0
		},
	}
}
