//go:build windows

package orchestrator

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"
	"unsafe"
	"wintray/internal/config"
)

var errLoginProcessExited = errors.New("program exited before login and tray startup completed")

// QQ NT's login and main windows share Chromium's native class. Only the
// logged-in panel exposes the classic/efficiency mode toggle. A QQ title,
// an input field or a running renderer alone must not authorize closing it.
// UIA selector reference: treers2/qq-operations, scripts/qq_lib.ps1 Get-QQMode.
func (a *weChatAutomation) qqMain(ctx context.Context, window HiddenWindow) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !window.valid() || !isWindowVisible(window.Handle) {
		return false, nil
	}
	root, err := weChatUIARead[*weChatUIAObject](a.object, uiaElementFromHandle, window.Handle)
	if err != nil || root == nil {
		return false, errors.Join(errors.New("read QQ UIA root"), err)
	}
	defer root.release()
	pid, err := weChatUIARead[int32](root, uiaCurrentProcessID)
	if err != nil || uint32(pid) != window.ProcessID {
		return false, err
	}
	class, err := root.text(uiaCurrentClassName)
	if err != nil || class != "Chrome_WidgetWin_1" {
		return false, err
	}
	name, err := root.text(uiaCurrentName)
	if err != nil || name != "QQ" {
		return false, err
	}
	elements, err := weChatUIARead[*weChatUIAObject](root, uiaFindAll, uiaTreeScopeDescendants, uintptr(unsafe.Pointer(a.condition)))
	if err != nil || elements == nil {
		return false, err
	}
	defer elements.release()
	count, err := weChatUIARead[int32](elements, uiaArrayLength)
	if err != nil {
		return false, err
	}
	if count < 0 || count > 8192 {
		return false, errors.New("unexpected QQ main tree size")
	}
	for i := int32(0); i < count; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		element, err := weChatUIARead[*weChatUIAObject](elements, uiaArrayElement, uintptr(i))
		if err != nil || element == nil {
			return false, errors.Join(errors.New("read QQ main element"), err)
		}
		main, err := qqMainToggle(element)
		element.release()
		if err != nil || main {
			return main, err
		}
	}
	return false, nil
}

func qqMainToggle(element *weChatUIAObject) (bool, error) {
	class, err := element.text(uiaCurrentClassName)
	if err != nil {
		return false, err
	}
	matched := false
	for _, token := range strings.Fields(class) {
		matched = matched || token == "narrow-toggler"
	}
	if !matched {
		return false, nil
	}
	name, err := element.text(uiaCurrentName)
	if err != nil {
		return false, err
	}
	switch name {
	case "切换为经典模式", "切换为效率模式", "切換為經典模式", "切換為效率模式":
		offscreen, err := weChatUIARead[int32](element, uiaCurrentIsOffscreen)
		return offscreen == 0, err
	}
	return false, nil
}

// Reuse the system UIA reader, but never call its login input methods for QQ.
func waitQQLogin(ctx context.Context, path string) (HiddenWindow, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a, err := newWeChatAutomation()
	if err != nil {
		return HiddenWindow{}, err
	}
	defer a.close()
	var lastErr error
	for ctx.Err() == nil {
		if !hasRunningProcessByIdentity(path, "") {
			return HiddenWindow{}, errLoginProcessExited
		}
		var mains []HiddenWindow
		panels := 0
		for _, w := range enumerateTopLevelWindows(false) {
			if w.OwnerHandle != 0 || w.IsToolWindow || !executablePathsMatch(w.ProcessPath, path) || w.ClassName != "Chrome_WidgetWin_1" || w.Title != "QQ" {
				continue
			}
			panels++
			window := HiddenWindow{Handle: w.Handle, ProcessID: w.ProcessID}
			main, err := a.qqMain(ctx, window)
			if err != nil {
				lastErr = err
				continue
			}
			if main {
				mains = append(mains, window)
			}
		}
		// Login panels count too: an existing account's main window must not
		// satisfy the wait for a different account that is still logging in.
		if panels == 1 && len(mains) == 1 && ctx.Err() == nil {
			return mains[0], nil
		}
		if !waitWithContext(ctx, 500*time.Millisecond) {
			break
		}
	}
	return HiddenWindow{}, errors.Join(ctx.Err(), lastErr)
}

// Re-read the current UI before each close attempt; a reused HWND or a return
// to the login page must never inherit a previous main-window classification.
func observeLoginMain(ctx context.Context, path string, window HiddenWindow) (bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if !window.valid() {
		return false, nil
	}
	_, currentPath := processInfo(window.ProcessID)
	if !executablePathsMatch(currentPath, path) {
		return false, nil
	}
	a, err := newWeChatAutomation()
	if err != nil {
		return false, err
	}
	defer a.close()
	if config.IsQQExecutable(currentPath) {
		return a.qqMain(ctx, window)
	}
	observation, err := a.observe(ctx, window)
	return observation.Kind == weChatMainWindow, err
}
