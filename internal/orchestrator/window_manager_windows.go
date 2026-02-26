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
	procSendMessageW    = user32.NewProc("SendMessageW")
	procShowWindowAsync = user32.NewProc("ShowWindowAsync")
)

const (
	wmSysCommand = 0x0112
	wmClose      = 0x0010
	scClose      = 0xF060
	scMinimize   = 0xF020
	swHide       = 0
	swMinimize   = 6
)

type Win32WindowManager struct{}

func NewWin32WindowManager() *Win32WindowManager { return &Win32WindowManager{} }

func (m *Win32WindowManager) MinimizeWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
	}
	ok, _, callErr := procPostMessageW.Call(hwnd, wmSysCommand, scMinimize, 0)
	if ok != 0 {
		return true, nil
	}
	v, _, showErr := procShowWindowAsync.Call(hwnd, swMinimize)
	if v != 0 {
		return true, nil
	}
	if showErr != nil && showErr != syscall.Errno(0) {
		return false, fmt.Errorf("showwindowasync minimize failed: %w", showErr)
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return false, fmt.Errorf("post sc_minimize failed: %w", callErr)
	}
	return false, errors.New("minimize request was not accepted")
}

func (m *Win32WindowManager) HideWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
	}
	v, _, callErr := procShowWindowAsync.Call(hwnd, swHide)
	if v != 0 {
		return true, nil
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return false, fmt.Errorf("showwindowasync hide failed: %w", callErr)
	}
	return false, errors.New("hide request was not accepted")
}

func (m *Win32WindowManager) CloseWindow(hwnd uintptr) (bool, error) {
	if !isWindow(hwnd) {
		return false, errors.New("target window is not valid")
	}
	ok, _, callErr := procPostMessageW.Call(hwnd, wmSysCommand, scClose, 0)
	if ok != 0 {
		return true, nil
	}
	_, _, sendErr := procSendMessageW.Call(hwnd, wmClose, 0, 0)
	if sendErr != nil && sendErr != syscall.Errno(0) {
		if callErr != nil && callErr != syscall.Errno(0) {
			return false, fmt.Errorf("post sc_close failed: %v; send wm_close failed: %w", callErr, sendErr)
		}
		return false, fmt.Errorf("send wm_close failed: %w", sendErr)
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return false, fmt.Errorf("post sc_close failed: %w", callErr)
	}
	return true, nil
}

func isWindow(hwnd uintptr) bool {
	v, _, _ := procIsWindow.Call(hwnd)
	return v != 0
}

var _ = syscall.Errno(0)
