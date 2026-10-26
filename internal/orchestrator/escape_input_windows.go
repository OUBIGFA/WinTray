//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"
	"unsafe"
)

var (
	procGetGUIThreadInfo = user32.NewProc("GetGUIThreadInfo")
	procIsChild          = user32.NewProc("IsChild")
	// Only Esc-only windows need activation. Serialize those short sessions so
	// concurrent startup workers cannot take focus away from each other.
	escapeInputGate = make(chan struct{}, 1)
)

type guiThreadInfo struct {
	cbSize        uint32
	flags         uint32
	hwndActive    uintptr
	hwndFocus     uintptr
	hwndCapture   uintptr
	hwndMenuOwner uintptr
	hwndMoveSize  uintptr
	hwndCaret     uintptr
	caretRect     [4]int32
}

// escapeFocus returns only the target or a real descendant, never an owned
// dialog/sibling on the same thread. Embedded input controls may belong to a
// different process (WebView2); their HWND ancestry, not PID equality, matters.
func escapeFocus(hwnd uintptr) uintptr {
	thread, _, _ := procGetWindowThreadProcess.Call(hwnd, 0)
	if thread == 0 {
		return 0
	}
	info := guiThreadInfo{cbSize: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	if ok, _, _ := procGetGUIThreadInfo.Call(thread, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 0
	}
	if escapeTargetBelongsTo(hwnd, info.hwndFocus) {
		return info.hwndFocus
	}
	return 0
}

func escapeTargetBelongsTo(hwnd, target uintptr) bool {
	if target == 0 || !isWindow(target) {
		return false
	}
	child, _, _ := procIsChild.Call(hwnd, target)
	return target == hwnd || child != 0
}

func windowIdentity(hwnd uintptr) HiddenWindow {
	var pid uint32
	procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return HiddenWindow{Handle: hwnd, ProcessID: pid}
}

// SendEscape posts a key pair to the program's own input queue, never global
// SendInput. Esc-only interfaces may need activation to focus their embedded
// renderer; a refused activation leaves them open instead of forcing a close.
func (m *Win32WindowManager) SendEscape(ctx context.Context, hwnd uintptr) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	identity := windowIdentity(hwnd)
	if !identity.valid() {
		return false, errors.New("target window is not valid")
	}
	if enabled, _, _ := procIsWindowEnabled.Call(hwnd); enabled == 0 {
		return false, errors.New("target window is disabled")
	}
	if !isWindowVisible(hwnd) {
		return true, nil // the program already dismissed it
	}

	activated := false
	if !nativeCloseAvailable(hwnd) && escapeFocus(hwnd) == 0 {
		select {
		case escapeInputGate <- struct{}{}:
			defer func() { <-escapeInputGate }()
		case <-ctx.Done():
			return false, ctx.Err()
		}
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if !identity.valid() || ctx.Err() != nil {
			return false, errors.New("escape target changed or request cancelled")
		}
		previous, _, _ := procGetForegroundWindow.Call()
		previousWindow := windowIdentity(previous)
		if previous != hwnd {
			if ok, _, _ := procSetForegroundWindow.Call(hwnd); ok == 0 {
				return false, ErrWindowActivation
			}
			activated = true
			defer func() {
				current, _, _ := procGetForegroundWindow.Call()
				// Never undo a user's focus change or activate a recycled HWND.
				if current == hwnd && previousWindow.valid() {
					procSetForegroundWindow.Call(previous)
				}
			}()
		}
		// Activation reaches cross-process renderers asynchronously. Let the
		// application choose its control; do not guess by class name or blast
		// Esc at every child. A root-only focus gets a short settling period.
		deadline := time.Now().Add(300 * time.Millisecond)
		for identity.valid() && isWindowVisible(hwnd) && time.Now().Before(deadline) {
			if focus := escapeFocus(hwnd); focus != 0 && focus != hwnd {
				break
			}
			if !waitWithContext(ctx, 25*time.Millisecond) {
				return false, ctx.Err()
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !identity.valid() {
		return false, errors.New("escape target no longer exists")
	}
	if !isWindowVisible(hwnd) {
		return true, nil
	}
	if activated {
		if current, _, _ := procGetForegroundWindow.Call(); current != hwnd {
			return false, errors.New("escape target lost foreground activation")
		}
	}
	target := escapeFocus(hwnd)
	if target == 0 {
		target = hwnd // native dialogs may process keyboard messages themselves
	}
	if !escapeTargetBelongsTo(hwnd, target) || !identity.valid() {
		return false, errors.New("escape input target changed")
	}
	keyDown := uintptr(1 | (0x01 << 16))
	keyUp := keyDown | (1 << 30) | (1 << 31)
	if ok, _, err := procPostMessageW.Call(target, wmKeyDown, vkEscape, keyDown); ok == 0 {
		return false, fmt.Errorf("post escape keydown: %w", err)
	}
	if ok, _, err := procPostMessageW.Call(target, wmKeyUp, vkEscape, keyUp); ok == 0 {
		return false, fmt.Errorf("post escape keyup: %w", err)
	}
	if activated {
		// Do not immediately restore focus: WebView renderers ignore keys once
		// deactivated. Keep this bounded and stop if the user changes windows.
		deadline := time.Now().Add(750 * time.Millisecond)
		for identity.valid() && isWindowVisible(hwnd) && time.Now().Before(deadline) {
			if current, _, _ := procGetForegroundWindow.Call(); current != hwnd {
				break
			}
			if !waitWithContext(ctx, 25*time.Millisecond) {
				return false, ctx.Err()
			}
		}
	}
	return true, nil
}
