//go:build !windows

package orchestrator

type Win32WindowEnumerator struct{}

func NewWin32WindowEnumerator() *Win32WindowEnumerator { return &Win32WindowEnumerator{} }

func (e *Win32WindowEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo {
	return nil
}
