package orchestrator

import (
	"context"
	"fmt"
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
	_, ok := svc.manageFirstMatchingWindow(
		context.Background(),
		func(ManagedWindowInfo) bool { return true },
		expectedPath,
		"app",
		nil,
		nil,
		0,
		"hide",
		0,
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
			want:   false,
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

func TestStart_ConsoleExecutableIsHiddenToTray(t *testing.T) {
	// Use an isolated copy of the test executable: invalid PE files must fail
	// to launch, and the running parent must not count as this managed app.
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(dir, "syncthing.exe")
	if err := os.WriteFile(exePath, image, 0o600); err != nil {
		t.Fatalf("write helper exe: %v", err)
	}
	const consoleHwnd = uintptr(0x5A5A)
	restore := overrideConsoleSeams(t, func(path string) bool { return path == exePath }, 0, consoleHwnd, false)
	defer restore()

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
			releaseFile := filepath.Join(dir, launch.name+".done")
			entry.Args = runnerHelperArgs("wait", releaseFile)
			mgr := &testManager{}
			svc := NewService(enum, mgr, &testLogger{})
			got := launch.run(svc)
			if got.Hidden != nil && got.Hidden.ProcessID != 0 {
				process, err := os.FindProcess(int(got.Hidden.ProcessID))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_ = process.Kill()
					_, _ = process.Wait()
				})
				if err := os.WriteFile(releaseFile, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !got.Managed || got.Code != ResultHiddenToTray || got.Hidden == nil {
				t.Fatalf("result = %+v, want managed hidden_to_tray with hidden window", got)
			}
			if got.Hidden.Handle != consoleHwnd || got.Hidden.ProcessID == 0 {
				t.Fatalf("hidden = %+v, want handle 0x%X with a live pid", *got.Hidden, consoleHwnd)
			}
			if len(mgr.closeCalls) != 0 || len(mgr.hideCalls) != 0 {
				t.Fatalf("console program window was touched: close=%d hide=%d", len(mgr.closeCalls), len(mgr.hideCalls))
			}
		})
	}
}

func TestConsoleProgramAlreadyRunning_IsHiddenToTrayWithoutClosing(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "syncthing.exe")
	if err := os.WriteFile(exePath, []byte("stub"), 0o600); err != nil {
		t.Fatalf("write stub exe: %v", err)
	}
	const pid = uint32(4321)
	const consoleHwnd = uintptr(0x777)
	restore := overrideConsoleSeams(t, func(path string) bool { return path == exePath }, pid, consoleHwnd, true)
	defer restore()
	entry := config.ManagedAppEntry{Name: "Syncthing", ExePath: exePath, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}}
	visibleConsole := ManagedWindowInfo{Handle: consoleHwnd, ProcessID: pid, ProcessName: "syncthing", ProcessPath: exePath, Title: exePath, ClassName: "ConsoleWindowClass"}

	for _, tc := range []struct {
		name string
		run  func(*Service) Result
	}{
		{name: "HideExisting", run: func(svc *Service) Result { return svc.HideExisting(context.Background(), entry, 10) }},
		{name: "StartAndManage", run: func(svc *Service) Result { return svc.StartAndManage(context.Background(), entry, 10) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &testManager{}
			svc := NewService(&testEnumerator{windows: []ManagedWindowInfo{visibleConsole}}, mgr, &testLogger{})
			started := time.Now()
			got := tc.run(svc)
			if elapsed := time.Since(started); elapsed > 2*time.Second {
				t.Fatalf("took %s, adoption must not wait for the retry window", elapsed)
			}
			if !got.Managed || got.Code != ResultHiddenToTray || got.Hidden == nil {
				t.Fatalf("result = %+v, want hidden_to_tray", got)
			}
			if got.Hidden.Handle != consoleHwnd || got.Hidden.ProcessID != pid {
				t.Fatalf("hidden = %+v, want {0x%X %d}", *got.Hidden, consoleHwnd, pid)
			}
			if len(mgr.closeCalls) != 0 {
				t.Fatalf("console window received close: %v", mgr.closeCalls)
			}
			if len(mgr.hideCalls) != 1 || mgr.hideCalls[0] != consoleHwnd {
				t.Fatalf("hide calls = %v, want [0x%X]", mgr.hideCalls, consoleHwnd)
			}
		})
	}
}

// launchedWindowEnumerator stands in for the desktop: it reports one window
// for the program at exePath from the moment that program runs, and records
// when it was first scanned while running. No real window is involved.
type launchedWindowEnumerator struct {
	exePath   string
	pid       uint32
	firstScan time.Time
}

func (e *launchedWindowEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo {
	if e.pid == 0 {
		e.pid = findRunningProcessByIdentity(e.exePath, "qq")
	}
	if e.pid == 0 {
		return nil
	}
	if e.firstScan.IsZero() {
		e.firstScan = time.Now()
	}
	return []ManagedWindowInfo{{Handle: 0x101, ProcessID: e.pid, ProcessName: "qq", ProcessPath: e.exePath, Title: "QQ", ClassName: "Chrome_WidgetWin_1"}}
}

// timedManager records when the first close request was made.
type timedManager struct {
	testManager
	closeAt time.Time
}

func (m *timedManager) CloseWindow(hwnd uintptr) (bool, error) {
	if m.closeAt.IsZero() {
		m.closeAt = time.Now()
	}
	return m.testManager.CloseWindow(hwnd)
}

func TestStart_CloseDelayHoldsWindowActionAfterLaunch(t *testing.T) {
	// A real launch of an isolated copy of the test binary, treated as a GUI
	// program, so the delay sits between process start and the first window
	// scan exactly as it does for a program with a login window.
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(dir, "qq.exe")
	if err := os.WriteFile(exePath, image, 0o600); err != nil {
		t.Fatalf("write helper exe: %v", err)
	}
	restore := overrideConsoleSeams(t, func(string) bool { return false }, 0, 0, false)
	defer restore()
	const quick = 2 * time.Second

	for i, tc := range []struct {
		name         string
		delaySeconds int
		cancelAfter  time.Duration
		launchNow    bool
		wantCode     ResultCode
	}{
		{name: "no delay acts on the window right away", wantCode: ResultManaged},
		{name: "delay holds the window scan and the close", delaySeconds: 2, wantCode: ResultManaged},
		{name: "shutdown during the delay leaves the window alone", delaySeconds: 30, cancelAfter: 300 * time.Millisecond, wantCode: ResultNoWindowManaged},
		{name: "launch now still counts a launch cancelled during the delay", delaySeconds: 30, cancelAfter: 300 * time.Millisecond, launchNow: true, wantCode: ResultStartedOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			releaseFile := filepath.Join(dir, fmt.Sprintf("release-%d.done", i))
			entry := config.ManagedAppEntry{
				Name:         "QQ",
				ExePath:      exePath,
				Args:         runnerHelperArgs("wait", releaseFile),
				RunOnStartup: true,
				TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: tc.delaySeconds},
			}
			enum := &launchedWindowEnumerator{exePath: exePath}
			t.Cleanup(func() { releaseHelper(t, exePath, releaseFile, enum.pid) })
			mgr := &timedManager{}
			svc := NewService(enum, mgr, &testLogger{})
			ctx := context.Background()
			if tc.cancelAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.cancelAfter)
				defer cancel()
			}

			started := time.Now()
			var got Result
			if tc.launchNow {
				got = svc.StartNow(ctx, entry, 5)
			} else {
				got = svc.StartAndManage(ctx, entry, 5)
			}
			elapsed := time.Since(started)
			if got.Code != tc.wantCode {
				t.Fatalf("result = %+v, want code %s", got, tc.wantCode)
			}
			if tc.cancelAfter > 0 {
				if elapsed >= quick || !enum.firstScan.IsZero() || len(mgr.closeCalls)+len(mgr.hideCalls) != 0 {
					t.Fatalf("cancelled launch took %s, scanned=%t close=%v hide=%v; want no wait and no window action", elapsed, !enum.firstScan.IsZero(), mgr.closeCalls, mgr.hideCalls)
				}
				return
			}
			if enum.firstScan.IsZero() || len(mgr.closeCalls) != 1 {
				t.Fatalf("scanned=%t close=%v; want one close after a scan", !enum.firstScan.IsZero(), mgr.closeCalls)
			}
			wait := time.Duration(tc.delaySeconds) * time.Second
			created, known := processStartTime(enum.pid)
			if !known {
				t.Fatalf("creation time of helper pid %d unavailable", enum.pid)
			}
			if scanAfter := enum.firstScan.Sub(created); scanAfter < wait {
				t.Fatalf("window scanned %s after the process started, want at least %s", scanAfter, wait)
			}
			if closeAfter := mgr.closeAt.Sub(created); closeAfter < wait {
				t.Fatalf("window closed %s after the process started, want at least %s", closeAfter, wait)
			}
			if tc.delaySeconds == 0 && elapsed >= quick {
				t.Fatalf("launch without delay took %s, want under %s", elapsed, quick)
			}
		})
	}
}

// overrideProcessAges replaces the process age probes: the creation time of
// any process asked for by pid, and the creation time of the oldest running
// process of the program (zero time = not running).
func overrideProcessAges(t *testing.T, byPID func(uint32) (time.Time, bool), running time.Time) func() {
	t.Helper()
	origPID, origRunning := processStartLookup, runningProcessStartLookup
	processStartLookup = byPID
	runningProcessStartLookup = func(string, string) (time.Time, bool) { return running, !running.IsZero() }
	return func() { processStartLookup, runningProcessStartLookup = origPID, origRunning }
}

func TestQuietPeriodRemaining(t *testing.T) {
	now := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	pid := uint32(1234)
	entry := func(delaySeconds int) config.ManagedAppEntry {
		return config.ManagedAppEntry{Name: "QQ", TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: delaySeconds}}
	}
	for _, tc := range []struct {
		name      string
		delay     int
		launched  *uint32
		pidStart  time.Time
		runningAt time.Time
		want      time.Duration
	}{
		{name: "no delay configured", launched: &pid, pidStart: now},
		{name: "launched moments ago waits the rest", delay: 30, launched: &pid, pidStart: now.Add(-5 * time.Second), want: 25 * time.Second},
		{name: "launched process that cannot be inspected waits the full delay", delay: 30, launched: &pid, want: 30 * time.Second},
		{name: "self-started program waits the rest", delay: 30, runningAt: now.Add(-10 * time.Second), want: 20 * time.Second},
		{name: "long running program is handled right away", delay: 30, runningAt: now.Add(-40 * time.Second)},
		{name: "program not running has nothing to wait for", delay: 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restore := overrideProcessAges(t, func(uint32) (time.Time, bool) { return tc.pidStart, !tc.pidStart.IsZero() }, tc.runningAt)
			defer restore()
			if got := quietPeriodRemaining(entry(tc.delay), tc.launched, `C:\\QQ\\QQ.exe`, "qq", now); got != tc.want {
				t.Fatalf("quietPeriodRemaining = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestExistingProgram_WindowActionWaitsForCloseDelay(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "qq.exe")
	if err := os.WriteFile(exePath, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	const pid = uint32(1234)
	entry := config.ManagedAppEntry{Name: "QQ", ExePath: exePath, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 2}}
	window := ManagedWindowInfo{Handle: 0x101, ProcessID: pid, ProcessName: "qq", ProcessPath: exePath, Title: "QQ", ClassName: "Chrome_WidgetWin_1"}
	hideExisting := func(svc *Service) Result { return svc.HideExisting(context.Background(), entry, 5) }
	startAndManage := func(svc *Service) Result { return svc.StartAndManage(context.Background(), entry, 5) }
	startNow := func(svc *Service) Result { return svc.StartNow(context.Background(), entry, 5) }

	for _, tc := range []struct {
		name     string
		age      time.Duration
		run      func(*Service) Result
		wantCode ResultCode
		wantWait time.Duration
	}{
		{name: "HideExisting waits for a program that started itself a second ago", age: time.Second, run: hideExisting, wantCode: ResultManagedExisting, wantWait: time.Second},
		{name: "HideExisting acts at once on a long running program", age: time.Minute, run: hideExisting, wantCode: ResultManagedExisting},
		{name: "StartAndManage waits for an already running young program", age: time.Second, run: startAndManage, wantCode: ResultAlreadyRunningManaged, wantWait: time.Second},
		{name: "StartNow acts at once on a long running program", age: time.Minute, run: startNow, wantCode: ResultAlreadyRunningManaged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Now()
			processStart := started.Add(-tc.age)
			restore := overrideProcessAges(t, func(uint32) (time.Time, bool) { return processStart, true }, processStart)
			defer restore()
			mgr := &timedManager{}
			svc := NewService(&testEnumerator{windows: []ManagedWindowInfo{window}}, mgr, &testLogger{})

			got := tc.run(svc)
			elapsed := time.Since(started)
			if got.Code != tc.wantCode || len(mgr.closeCalls) != 1 {
				t.Fatalf("result = %+v close=%v, want code %s with one close", got, mgr.closeCalls, tc.wantCode)
			}
			if closeAfter := mgr.closeAt.Sub(started); closeAfter < tc.wantWait-50*time.Millisecond {
				t.Fatalf("window closed %s after the check, want at least %s", closeAfter, tc.wantWait)
			}
			if tc.wantWait == 0 && elapsed >= time.Second {
				t.Fatalf("took %s, want an immediate action", elapsed)
			}
		})
	}
}

func TestManageFirstMatchingWindow_SkipsWindowsInsideCloseDelay(t *testing.T) {
	const pid = uint32(1234)
	window := ManagedWindowInfo{Handle: 0x101, ProcessID: pid, ProcessName: "qq", ProcessPath: `C:\\QQ\\QQ.exe`, Title: "QQ", ClassName: "Chrome_WidgetWin_1"}
	expectedPath := normalizePath(`C:\\QQ\\QQ.exe`)
	for _, tc := range []struct {
		name         string
		age          time.Duration
		retrySeconds int
		wantManaged  bool
		wantWait     time.Duration
	}{
		{name: "young process window is left alone", age: 500 * time.Millisecond},
		{name: "old process window is closed", age: 3 * time.Second, wantManaged: true},
		{name: "window is closed once the process is old enough", age: 1500 * time.Millisecond, retrySeconds: 3, wantManaged: true, wantWait: 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Now()
			restore := overrideProcessAges(t, func(uint32) (time.Time, bool) { return started.Add(-tc.age), true }, time.Time{})
			defer restore()
			mgr := &timedManager{}
			svc := NewService(&testEnumerator{windows: []ManagedWindowInfo{window}}, mgr, &testLogger{})

			_, ok := svc.manageFirstMatchingWindow(context.Background(), func(ManagedWindowInfo) bool { return true }, expectedPath, "qq", nil, nil, tc.retrySeconds, "close", 2*time.Second)
			if ok != tc.wantManaged || (len(mgr.closeCalls) > 0) != tc.wantManaged {
				t.Fatalf("managed=%t close=%v, want managed=%t", ok, mgr.closeCalls, tc.wantManaged)
			}
			if tc.wantManaged {
				if closeAfter := mgr.closeAt.Sub(started); closeAfter < tc.wantWait-50*time.Millisecond {
					t.Fatalf("window closed %s after the scan started, want at least %s", closeAfter, tc.wantWait)
				}
			}
		})
	}
}

// releaseHelper lets the helper process exit and waits for it, so the next
// launch does not see it as an already running instance.
func releaseHelper(t *testing.T, exePath, releaseFile string, pid uint32) {
	t.Helper()
	if err := os.WriteFile(releaseFile, nil, 0o600); err != nil {
		t.Error(err)
	}
	if pid == 0 {
		pid = findRunningProcessByIdentity(exePath, "qq")
	}
	if pid == 0 {
		return
	}
	process, err := os.FindProcess(int(pid))
	if err != nil {
		return
	}
	_ = process.Kill()
	_, _ = process.Wait()
}

func TestHideExisting_ConsoleProgramNotRunning_ReturnsImmediately(t *testing.T) {
	exePath := filepath.Join(t.TempDir(), "syncthing.exe")
	restore := overrideConsoleSeams(t, func(path string) bool { return path == exePath }, 0, 0, false)
	defer restore()
	entry := config.ManagedAppEntry{Name: "Syncthing", ExePath: exePath, RunOnStartup: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}}
	svc := NewService(&testEnumerator{}, &testManager{}, &testLogger{})

	started := time.Now()
	got := svc.HideExisting(context.Background(), entry, 10)
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("took %s, must not wait for a window of a program that is not running", elapsed)
	}
	if got.Managed || got.Code != ResultNoExistingWindowManaged {
		t.Fatalf("result = %+v, want no existing window managed", got)
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
			managed, ok := svc.tryManageAndVerify(context.Background(), tc.window, closeAllowedScoreThreshold, "close")
			if !ok {
				t.Fatal("tryManageAndVerify(close) = false, want true")
			}
			if !managed.Hidden || managed.Handle != tc.window.Handle || managed.ProcessID != tc.window.ProcessID {
				t.Fatalf("managed = %+v, want hidden window 0x%X pid %d", managed, tc.window.Handle, tc.window.ProcessID)
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
	managed, ok := svc.tryManageAndVerify(context.Background(), gui, closeAllowedScoreThreshold, "close")
	if !ok {
		t.Fatal("tryManageAndVerify(close) on GUI window = false, want true")
	}
	if managed.Hidden {
		t.Fatalf("GUI window reported as hidden by WinTray: %+v", managed)
	}
	if len(mgr.closeCalls) != 1 || len(mgr.hideCalls) != 0 {
		t.Fatalf("GUI window should still be closed: close=%v hide=%v", mgr.closeCalls, mgr.hideCalls)
	}
}

// overrideConsoleSeams replaces the console probes: which files count as
// console programs, which pid a running instance has (0 = not running), which
// console window belongs to it (0 = none) and whether that window is visible.
func overrideConsoleSeams(t *testing.T, isConsole func(string) bool, runningPID uint32, hwnd uintptr, visible bool) func() {
	t.Helper()
	origCheck, origLookup, origRunning, origVisible := consoleExecutableCheck, consoleWindowLookup, runningProcessLookup, windowVisibleCheck
	consoleExecutableCheck = isConsole
	consoleWindowLookup = func(context.Context, uint32, time.Duration) uintptr { return hwnd }
	runningProcessLookup = func(string, string) uint32 { return runningPID }
	windowVisibleCheck = func(uintptr) bool { return visible }
	return func() {
		consoleExecutableCheck, consoleWindowLookup, runningProcessLookup, windowVisibleCheck = origCheck, origLookup, origRunning, origVisible
	}
}

func TestProcessStartTime_ReportsCurrentProcess(t *testing.T) {
	started, ok := processStartTime(uint32(os.Getpid()))
	if !ok {
		t.Fatal("creation time of the test process unavailable")
	}
	age := time.Since(started)
	if age < 0 || age > time.Hour {
		t.Fatalf("test process reported as started %s ago (%s), want a recent past time", age, started)
	}
	if _, ok := processStartTime(1234); ok {
		t.Fatal("pid 1234 is never a valid Windows pid, must report unavailable")
	}
}
