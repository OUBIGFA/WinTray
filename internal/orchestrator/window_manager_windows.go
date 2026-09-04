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
)

const (
	wmSysCommand = 0x0112
	wmClose      = 0x0010
	scClose      = 0xF060
	swHide       = 0
)

type Win32WindowManager struct{}

func NewWin32WindowManager() *Win32WindowManager { return &Win32WindowManager{} }

func (m *Win32WindowManager) HideWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
	}
	_, _, callErr := procShowWindowAsync.Call(hwnd, swHide)
	if callErr != nil && callErr != syscall.Errno(0) {
		return false, fmt.Errorf("showwindowasync hide failed: %w", callErr)
	}
	return true, nil
}

func (m *Win32WindowManager) CloseWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
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
