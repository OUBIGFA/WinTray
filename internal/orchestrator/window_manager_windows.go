//go:build windows

package orchestrator

import (
	"errors"
	"fmt"
	"syscall"
)

var (
	procIsWindow        = user32.NewProc("IsWindow")
	procPostMessageW    = user32.NewProc("PostMessageW")
	procShowWindowAsync = user32.NewProc("ShowWindowAsync")
	procGetSystemMenu   = user32.NewProc("GetSystemMenu")
	procGetMenuState    = user32.NewProc("GetMenuState")
	procIsWindowEnabled = user32.NewProc("IsWindowEnabled")
)

// A missing/disabled close command is not permission to bypass the program's
// UI with WM_CLOSE. Esc-only launchers can otherwise exit instead of hiding.
var errNativeCloseUnavailable = errors.New("the window does not offer an enabled close command")

const (
	wmSysCommand = 0x0112
	wmClose      = 0x0010
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	scClose      = 0xF060
	vkEscape     = 0x1B
	swHide       = 0
)

type Win32WindowManager struct{}

func NewWin32WindowManager() *Win32WindowManager { return &Win32WindowManager{} }

func nativeCloseAvailable(hwnd uintptr) bool {
	if enabled, _, _ := procIsWindowEnabled.Call(hwnd); enabled == 0 {
		return false
	}
	menu, _, _ := procGetSystemMenu.Call(hwnd, 0)
	if menu == 0 {
		return false
	}
	state, _, _ := procGetMenuState.Call(menu, scClose, 0) // MF_BYCOMMAND
	return uint32(state) != 0xffffffff && state&3 == 0     // not GRAYED or DISABLED
}

func (m *Win32WindowManager) HideWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
	}
	ok, _, _ := procShowWindowAsync.Call(hwnd, swHide)
	if ok == 0 {
		return false, errors.New("showwindowasync hide failed")
	}
	return true, nil
}

func (m *Win32WindowManager) CloseWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
	}
	if !nativeCloseAvailable(hwnd) {
		return false, errNativeCloseUnavailable
	}
	ok, _, callErr := procPostMessageW.Call(hwnd, wmSysCommand, scClose, 0)
	if ok != 0 {
		return true, nil
	}
	ok, _, wmCloseErr := procPostMessageW.Call(hwnd, wmClose, 0, 0)
	if ok != 0 {
		return true, nil
	}
	if callErr != nil && callErr != syscall.Errno(0) && wmCloseErr != nil && wmCloseErr != syscall.Errno(0) {
		return false, fmt.Errorf("post sc_close failed: %v; post wm_close failed: %w", callErr, wmCloseErr)
	}
	if wmCloseErr != nil && wmCloseErr != syscall.Errno(0) {
		return false, fmt.Errorf("post wm_close failed: %w", wmCloseErr)
	}
	return false, fmt.Errorf("post close message failed")
}

func isWindow(hwnd uintptr) bool {
	v, _, _ := procIsWindow.Call(hwnd)
	return v != 0
}

func isWindowVisible(hwnd uintptr) bool {
	v, _, _ := procIsWindowVisible.Call(hwnd)
	return v != 0
}
