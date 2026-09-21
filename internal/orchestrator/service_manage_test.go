package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wintray/internal/config"
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

func TestStartNow_FollowsEntryWindowBehavior(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "app.exe")
	if err := os.WriteFile(exePath, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub exe: %v", err)
	}
	base := config.ManagedAppEntry{Name: "App", ExePath: exePath, RunOnStartup: true}

	cases := []struct {
		name       string
		closeAfter bool
		hidden     bool
		wantAction string
	}{
		{name: "no option checked leaves the window alone", wantAction: ""},
		{name: "close window after launch acts on the window", closeAfter: true, wantAction: "close"},
		{name: "launch hidden in background leaves the window alone", hidden: true, wantAction: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := base
			entry.LaunchHiddenInBackground = tc.hidden
			entry.TrayBehavior.AutoMinimizeAndHideOnLaunch = tc.closeAfter
			enum := &testEnumerator{windows: []ManagedWindowInfo{
				{Handle: 0x101, ProcessID: 1234, ProcessName: "app", ProcessPath: exePath, Title: "App Main", ClassName: "AppWindow"},
			}}
			mgr := &testManager{}
			svc := NewService(enum, mgr, &testLogger{})

			got := svc.StartNow(context.Background(), entry, 0)
			if !got.Managed {
				t.Fatalf("StartNow = %+v, want managed", got)
			}
			if got.Action != tc.wantAction {
				t.Fatalf("StartNow action = %q, want %q", got.Action, tc.wantAction)
			}
			if tc.wantAction == "" && len(mgr.closeCalls)+len(mgr.hideCalls) != 0 {
				t.Fatalf("window was touched: close=%d hide=%d", len(mgr.closeCalls), len(mgr.hideCalls))
			}
			if tc.wantAction == "close" && len(mgr.closeCalls) == 0 {
				t.Fatalf("window was not acted on")
			}
		})
	}
}

func TestHasExistingManagedWindow_IgnoresUnrelatedWindowWithNameInTitle(t *testing.T) {
	exePath := `C:\Tools\123.exe`
	cases := []struct {
		name   string
		window ManagedWindowInfo
		want   bool
	}{
		{
			name:   "unrelated window mentioning the program name in its title",
			window: ManagedWindowInfo{Handle: 0x201, ProcessID: 10, ProcessName: "chrome", ProcessPath: `C:\Chrome\chrome.exe`, Title: "报表 123 汇总", ClassName: "Chrome_WidgetWin_1"},
			want:   false,
		},
		{
			name:   "same program running from the configured path",
			window: ManagedWindowInfo{Handle: 0x202, ProcessID: 11, ProcessName: "123", ProcessPath: exePath, Title: "123", ClassName: "AppWindow"},
			want:   true,
		},
		{
			name:   "same program name running from another folder",
			window: ManagedWindowInfo{Handle: 0x203, ProcessID: 12, ProcessName: "123", ProcessPath: `D:\Other\123.exe`, Title: "123", ClassName: "AppWindow"},
			want:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&testEnumerator{windows: []ManagedWindowInfo{tc.window}}, &testManager{}, &testLogger{})
			got := svc.hasExistingManagedWindow(normalizePath(exePath), "123")
			if got != tc.want {
				t.Fatalf("hasExistingManagedWindow = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestIsConsoleExecutable(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if !isConsoleExecutable(self) {
		t.Fatalf("test binary %s should be detected as a console executable", self)
	}

	stub := filepath.Join(t.TempDir(), "stub.exe")
	if err := os.WriteFile(stub, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if isConsoleExecutable(stub) {
		t.Fatal("non-PE stub must not be detected as a console executable")
	}
	if isConsoleExecutable(filepath.Join(t.TempDir(), "missing.exe")) {
		t.Fatal("missing file must not be detected as a console executable")
	}

	gui := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	if _, err := os.Stat(gui); err != nil {
		t.Skipf("GUI sample %s not available: %v", gui, err)
	}
	if isConsoleExecutable(gui) {
		t.Fatalf("GUI executable %s must not be detected as a console executable", gui)
	}
}

func TestStart_ConsoleExecutableLaunchesHiddenInsteadOfClosingWindow(t *testing.T) {
	// The stub launch falls back to a short-lived cmd.exe whose working
	// directory is the stub folder, so clean that folder up with a retry
	// instead of t.TempDir, which fails while cmd.exe is still exiting.
	dir, err := os.MkdirTemp("", "wintray-console-stub-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		for i := 0; i < 20; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Logf("stub dir %s left behind (still in use)", dir)
	})
	exePath := filepath.Join(dir, "syncthing.exe")
	if err := os.WriteFile(exePath, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub exe: %v", err)
	}
	original := consoleExecutableCheck
	consoleExecutableCheck = func(path string) bool { return path == exePath }
	t.Cleanup(func() { consoleExecutableCheck = original })

	entry := config.ManagedAppEntry{
		Name:         "Syncthing",
		ExePath:      exePath,
		RunOnStartup: true,
		TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true},
	}
	// No window is visible before launch, so the service goes down the real
	// launch path rather than the already-running branch.
	enum := &testEnumerator{}

	for _, launch := range []struct {
		name string
		run  func(*Service) Result
	}{
		{name: "StartAndManage", run: func(svc *Service) Result { return svc.StartAndManage(context.Background(), entry, 0) }},
		{name: "StartNow", run: func(svc *Service) Result { return svc.StartNow(context.Background(), entry, 0) }},
	} {
		t.Run(launch.name, func(t *testing.T) {
			mgr := &testManager{}
			svc := NewService(enum, mgr, &testLogger{})
			got := launch.run(svc)
			if !got.Managed || got.Code != ResultStartedHidden {
				t.Fatalf("result = %+v, want managed with code %s", got, ResultStartedHidden)
			}
			if len(mgr.closeCalls) != 0 || len(mgr.hideCalls) != 0 {
				t.Fatalf("console program window was touched: close=%d hide=%d", len(mgr.closeCalls), len(mgr.hideCalls))
			}
		})
	}
}

func TestTryManageAndVerify_ConsoleHostWindowIsHiddenNotClosed(t *testing.T) {
	cases := []struct {
		name   string
		window ManagedWindowInfo
	}{
		{name: "conhost window", window: ManagedWindowInfo{Handle: 0x401, ProcessID: 1, ProcessName: "syncthing", Title: `C:\Tools\syncthing.exe`, ClassName: "ConsoleWindowClass"}},
		{name: "windows terminal window", window: ManagedWindowInfo{Handle: 0x402, ProcessID: 2, ProcessName: "WindowsTerminal", Title: "syncthing", ClassName: "CASCADIA_HOSTING_WINDOW_CLASS"}},
		{name: "shell process window", window: ManagedWindowInfo{Handle: 0x403, ProcessID: 3, ProcessName: "cmd", Title: "frpc", ClassName: "SomeClass"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &testManager{}
			svc := NewService(&testEnumerator{}, mgr, &testLogger{})
			if !svc.tryManageAndVerify(context.Background(), tc.window, closeAllowedScoreThreshold, "close") {
				t.Fatal("tryManageAndVerify(close) = false, want true")
			}
			if len(mgr.closeCalls) != 0 {
				t.Fatalf("console host window received close: %v", mgr.closeCalls)
			}
			if len(mgr.hideCalls) != 1 || mgr.hideCalls[0] != tc.window.Handle {
				t.Fatalf("hide calls = %v, want [0x%X]", mgr.hideCalls, tc.window.Handle)
			}
		})
	}

	gui := ManagedWindowInfo{Handle: 0x404, ProcessID: 4, ProcessName: "app", Title: "App", ClassName: "AppWindow"}
	mgr := &testManager{}
	svc := NewService(&testEnumerator{}, mgr, &testLogger{})
	if !svc.tryManageAndVerify(context.Background(), gui, closeAllowedScoreThreshold, "close") {
		t.Fatal("tryManageAndVerify(close) on GUI window = false, want true")
	}
	if len(mgr.closeCalls) != 1 || len(mgr.hideCalls) != 0 {
		t.Fatalf("GUI window should still be closed: close=%v hide=%v", mgr.closeCalls, mgr.hideCalls)
	}
}
