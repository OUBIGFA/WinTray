package orchestrator

import (
	"context"
	"testing"
)

type testEnumerator struct {
	windows []ManagedWindowInfo
}

func (e *testEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo {
	return e.windows
}

type testManager struct {
	closeCalls []uintptr
	hideCalls  []uintptr
}

func (m *testManager) CloseWindow(hwnd uintptr) (bool, error) {
	m.closeCalls = append(m.closeCalls, hwnd)
	return true, nil
}

func (m *testManager) HideWindow(hwnd uintptr) (bool, error) {
	m.hideCalls = append(m.hideCalls, hwnd)
	return true, nil
}

func (m *testManager) MinimizeWindow(hwnd uintptr) (bool, error) { return true, nil }

type testLogger struct{}

func (l *testLogger) Info(string)  {}
func (l *testLogger) Warn(string)  {}
func (l *testLogger) Error(string) {}

func TestManageFirstMatchingWindow_HideHandlesAllCandidatesInRound(t *testing.T) {
	enum := &testEnumerator{windows: []ManagedWindowInfo{
		{Handle: 0x101, ProcessID: 1234, ProcessName: "app", ProcessPath: `C:\Program Files\App\app.exe`, Title: "App Main", ClassName: "AppWindow"},
		{Handle: 0x102, ProcessID: 1234, ProcessName: "app", ProcessPath: `C:\Program Files\App\app.exe`, Title: "App Secondary", ClassName: "AppWindow"},
	}}
	mgr := &testManager{}
	svc := NewService(enum, mgr, &testLogger{})

	expectedPath := normalizePath(`C:\Program Files\App\app.exe`)
	ok := svc.manageFirstMatchingWindow(
		context.Background(),
		func(ManagedWindowInfo) bool { return true },
		expectedPath,
		"app",
		nil,
		nil,
		0,
		"hide",
	)

	if !ok {
		t.Fatalf("manageFirstMatchingWindow(hide) = false, want true")
	}
	if len(mgr.closeCalls) != 2 {
		t.Fatalf("close calls = %d, want 2", len(mgr.closeCalls))
	}
	if len(mgr.hideCalls) != 0 {
		t.Fatalf("hide calls = %d, want 0", len(mgr.hideCalls))
	}
}
