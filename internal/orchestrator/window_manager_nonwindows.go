//go:build !windows

package orchestrator

type Win32WindowManager struct{}

func NewWin32WindowManager() *Win32WindowManager { return &Win32WindowManager{} }

func (m *Win32WindowManager) MinimizeWindow(_ uintptr) (bool, error) { return false, nil }
func (m *Win32WindowManager) HideWindow(_ uintptr) (bool, error) { return false, nil }
func (m *Win32WindowManager) CloseWindow(_ uintptr) (bool, error) { return false, nil }
