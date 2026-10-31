//go:build windows

package orchestrator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/lxn/win"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
	"wintray/internal/config"
)

var weChatFixtureWindows startupFixture
var weChatFixtureMode string
var weChatFixtureQt bool
var weChatFixtureRendered bool
var weChatFixtureClicks int
var weChatFixtureButton win.HWND
var weChatFixturePressed bool
var weChatFixtureProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case win.WM_COMMAND:
		// Qt exposes Invoke but does not activate the login button through it.
		if weChatFixtureQt {
			return 0
		}
		if w>>16 == 0 {
			weChatFixtureClicks++
			if weChatFixtureMode == "ready" {
				win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[0]), win.SW_HIDE)
				win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[1]), win.SW_SHOWNOACTIVATE)
			}
		}
		return 0
	case win.WM_LBUTTONDOWN, win.WM_LBUTTONUP:
		if weChatFixtureQt {
			point := win.POINT{X: int32(int16(l & 0xffff)), Y: int32(int16(l >> 16))}
			win.ClientToScreen(hwnd, &point)
			var rect win.RECT
			win.GetWindowRect(weChatFixtureButton, &rect)
			inside := point.X >= rect.Left && point.X < rect.Right && point.Y >= rect.Top && point.Y < rect.Bottom
			if msg == win.WM_LBUTTONDOWN {
				weChatFixturePressed = inside
			} else if inside && weChatFixturePressed {
				weChatFixturePressed = false
				weChatFixtureClicks++
				if weChatFixtureMode == "ready" {
					win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[0]), win.SW_HIDE)
					win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[1]), win.SW_SHOWNOACTIVATE)
				}
			}
			return 0
		}
	case win.WM_SYSCOMMAND:
		if w&0xfff0 == scClose {
			win.ShowWindow(hwnd, win.SW_HIDE)
			return 0
		}
	case win.WM_APP + 20:
		return uintptr(weChatFixtureClicks)
	case win.WM_APP + 21:
		win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[1]), win.SW_SHOWNOACTIVATE)
		return 1
	}
	return win.DefWindowProc(hwnd, msg, w, l)
})

// A separate GUI process exercises the real system UIA provider. Its main
// window exists hidden before login, and its login button is six levels deep.
func TestWeChatUIAProcess(t *testing.T) {
	if len(os.Args) != 5 || os.Args[2] != "--" {
		return
	}
	runtime.LockOSThread()
	weChatFixtureMode = os.Args[4]
	weChatFixtureQt = strings.HasPrefix(weChatFixtureMode, "qt-")
	weChatFixtureMode = strings.TrimPrefix(weChatFixtureMode, "qt-")
	weChatFixtureRendered = strings.HasPrefix(weChatFixtureMode, "rendered-")
	weChatFixtureMode = strings.TrimPrefix(weChatFixtureMode, "rendered-")
	for _, name := range []string{"WeChatLoginWnd", "mmui::LoginWindow", "WeChatMainWndForPC", "QtTestQWindowIcon", "MMUIRenderSubWindowHW", "WinTrayWeChatPanel"} {
		wc := win.WNDCLASSEX{LpfnWndProc: weChatFixtureProc, HInstance: win.GetModuleHandle(nil), LpszClassName: syscall.StringToUTF16Ptr(name)}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if win.RegisterClassEx(&wc) == 0 {
			os.Exit(2)
		}
	}
	create := func(class, name string, parent win.HWND, style uint32) win.HWND {
		return win.CreateWindowEx(win.WS_EX_NOACTIVATE, syscall.StringToUTF16Ptr(class), syscall.StringToUTF16Ptr(name), style, 10, 10, 240, 150, parent, 0, win.GetModuleHandle(nil), nil)
	}
	loginClass := "WeChatLoginWnd"
	if weChatFixtureQt {
		loginClass = "mmui::LoginWindow"
	}
	login := create(loginClass, "WinTray login test", 0, win.WS_OVERLAPPEDWINDOW)
	mainClass, mainTitle := "WeChatMainWndForPC", "WinTray main test"
	if weChatFixtureRendered {
		mainClass, mainTitle = "QtTestQWindowIcon", "微信"
		if weChatFixtureMode == "unknown-title" {
			mainTitle = "Unrelated Qt window"
		}
	}
	main := create(mainClass, mainTitle, 0, win.WS_OVERLAPPEDWINDOW)
	// New clients can expose only a native render surface after login. It
	// already exists under the hidden main window before login completes.
	if weChatFixtureRendered && weChatFixtureMode != "no-surface" {
		surface := create("MMUIRenderSubWindowHW", "", main, win.WS_CHILD|win.WS_VISIBLE)
		if weChatFixtureMode == "hidden-surface" {
			win.ShowWindow(surface, win.SW_HIDE)
		}
	}
	weChatFixtureWindows = startupFixture{Owner: uintptr(login), Forms: []uintptr{uintptr(login), uintptr(main)}}
	parent := login
	for i := 0; i < 5; i++ {
		parent = create("WinTrayWeChatPanel", "", parent, win.WS_CHILD|win.WS_VISIBLE)
	}
	name := "进入微信"
	if weChatFixtureMode == "qr" {
		name = "扫描二维码"
	}
	button := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("BUTTON"), syscall.StringToUTF16Ptr(name), win.WS_CHILD|win.WS_VISIBLE|win.BS_PUSHBUTTON, 0, 0, 100, 28, parent, 1, win.GetModuleHandle(nil), nil)
	weChatFixtureButton = button
	if weChatFixtureMode == "disabled" {
		win.EnableWindow(button, false)
	}
	if weChatFixtureMode == "ambiguous" {
		win.CreateWindowEx(0, syscall.StringToUTF16Ptr("BUTTON"), syscall.StringToUTF16Ptr(name), win.WS_CHILD|win.WS_VISIBLE|win.BS_PUSHBUTTON, 0, 30, 100, 28, parent, 2, win.GetModuleHandle(nil), nil)
	}
	win.ShowWindow(login, win.SW_SHOWNOACTIVATE)
	data, _ := json.Marshal(weChatFixtureWindows)
	if err := os.WriteFile(os.Args[3], data, 0600); err != nil {
		os.Exit(3)
	}
	var msg win.MSG
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
	os.Exit(0)
}

func weChatFixtureEntry(t *testing.T, mode string) (config.ManagedAppEntry, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	pe := int(binary.LittleEndian.Uint32(data[0x3c:]))
	binary.LittleEndian.PutUint16(data[pe+24+68:], 2)
	dir := t.TempDir()
	path := filepath.Join(dir, "Weixin.exe")
	file := filepath.Join(dir, "windows.json")
	if err := os.WriteFile(path, data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pid := findRunningProcessByIdentity(path, ""); pid != 0 {
			p, err := os.FindProcess(int(pid))
			if err == nil {
				_ = p.Kill()
				_, _ = p.Wait()
			}
		}
	})
	return config.ManagedAppEntry{Name: "WeChat fixture", ExePath: path, Args: "-test.run=^TestWeChatUIAProcess$ -- " + syscall.EscapeArg(file) + " " + mode, RunOnStartup: true, WeChatAutoLogin: true, TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true}}, file
}

func TestWeChatNativeLoginAndDismissal(t *testing.T) {
	for _, mode := range []string{"ready", "qt-ready", "qt-rendered-ready"} {
		t.Run(mode, func(t *testing.T) { testWeChatNativeLoginAndDismissal(t, mode) })
	}
}

func testWeChatNativeLoginAndDismissal(t *testing.T, mode string) {
	t.Helper()
	entry, file := weChatFixtureEntry(t, mode)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	svc.externalStartupLookup = func(string) (string, error) { return "", nil }
	svc.wechatLogin = func(ctx context.Context, path string) (HiddenWindow, error) {
		state := awaitStartupFixture(t, file)
		if visibilityOwner(state.Forms[0]) != 0 {
			t.Error("login window was shielded")
		}
		return loginWeChat(ctx, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := svc.StartAndManage(ctx, entry, 2)
	state := awaitStartupFixture(t, file)
	if !result.Managed || result.Action != "close" {
		t.Fatalf("native login failed: %+v", result)
	}
	if clicks := win.SendMessage(win.HWND(state.Owner), win.WM_APP+20, 0, 0); clicks != 1 {
		t.Fatalf("Invoke clicked %d times", clicks)
	}
	if !svc.IsProgramRunning(entry) || isWindowVisible(state.Forms[0]) || isWindowVisible(state.Forms[1]) {
		t.Fatal("login/dismissal did not preserve running process with hidden windows")
	}
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	if !waitVisible(state.Forms[1], true) {
		t.Fatal("main window cannot be restored")
	}
}

func TestWeChatNativeIncompleteLoginStaysVisible(t *testing.T) {
	for _, mode := range []string{"phone", "qr", "disabled", "ambiguous", "qt-phone", "qt-qr", "qt-disabled", "qt-ambiguous", "qt-rendered-phone", "qt-rendered-qr"} {
		t.Run(mode, func(t *testing.T) {
			entry, file := weChatFixtureEntry(t, mode)
			cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
			if err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Release()
			state := awaitStartupFixture(t, file)
			svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			defer cancel()
			result := svc.StartNow(ctx, entry, 0)
			if result.Code != ResultWeChatLoginIncomplete || result.Managed {
				t.Fatalf("incomplete login result: %+v", result)
			}
			if !isWindowVisible(state.Forms[0]) || visibilityOwner(state.Forms[0]) != 0 || !svc.IsProgramRunning(entry) {
				t.Fatal("login window was hidden or process exited")
			}
			want := uintptr(0)
			if strings.HasSuffix(mode, "phone") {
				want = 1
			}
			if clicks := win.SendMessage(win.HWND(state.Owner), win.WM_APP+20, 0, 0); clicks != want {
				t.Fatalf("clicks=%d want=%d", clicks, want)
			}
		})
	}
}

// At logon Windows has already launched Weixin, and window dismissal is off.
// Login must still run through the automatic startup path in this configuration.
func TestWeChatNativeTaskLaunchLogin(t *testing.T) {
	for _, mode := range []string{"qt-ready", "qt-rendered-ready"} {
		t.Run(mode, func(t *testing.T) { testWeChatNativeTaskLaunchLogin(t, mode) })
	}
}

func testWeChatNativeTaskLaunchLogin(t *testing.T, mode string) {
	t.Helper()
	entry, file := weChatFixtureEntry(t, mode)
	entry.LaunchViaLogonTask = true
	entry.TrayBehavior.AutoMinimizeAndHideOnLaunch = false
	cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
	if err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Release()
	state := awaitStartupFixture(t, file)
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	result := svc.StartAndManage(ctx, entry, 0)
	if !result.Managed || result.Action != "" || !isWindowVisible(state.Forms[1]) || isWindowVisible(state.Forms[0]) {
		t.Fatalf("task-launched login did not reach the visible main window: %+v", result)
	}
	if clicks := win.SendMessage(win.HWND(state.Owner), win.WM_APP+20, 0, 0); clicks != 1 {
		t.Fatalf("task-launched login clicked %d times", clicks)
	}
}

// A generic Qt shell or an unrelated rendered window is not proof of login.
func TestWeChatNativeRenderedMainIdentity(t *testing.T) {
	for _, mode := range []string{"qt-rendered-ready", "qt-rendered-unknown-title", "qt-rendered-no-surface", "qt-rendered-hidden-surface"} {
		t.Run(mode, func(t *testing.T) {
			entry, file := weChatFixtureEntry(t, mode)
			cmd, err := startProcess(entry.ExePath, entry.Args, launchVisible)
			if err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Release()
			state := awaitStartupFixture(t, file)
			win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			a, err := newWeChatAutomation()
			if err != nil {
				t.Fatal(err)
			}
			defer a.close()
			got, err := a.observe(context.Background(), HiddenWindow{Handle: state.Forms[1], ProcessID: uint32(cmd.Process.Pid)})
			want := weChatUnknown
			if mode == "qt-rendered-ready" {
				want = weChatMainWindow
			}
			if err != nil || got.Kind != want {
				t.Fatalf("rendered main observation=%+v err=%v, want kind %d", got, err, want)
			}
		})
	}
}

// Explicit opt-in: exercise an installed client without changing its settings
// or terminating it. A QR/phone prompt remains visible for the user.
func TestWeChatInstalledClient(t *testing.T) {
	path := os.Getenv("WINTRAY_WECHAT_LIVE_EXE")
	if path == "" {
		t.Skip("set WINTRAY_WECHAT_LIVE_EXE to test an installed client")
	}
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &liveEscapeLogger{t})
	entry := config.ManagedAppEntry{Name: "WeChat", ExePath: path, RunOnStartup: true, WeChatAutoLogin: true}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	result := svc.StartNow(ctx, entry, 0)
	if !result.Managed {
		logWeChatInstalledWindows(t, path)
		t.Fatalf("installed client login: %+v", result)
	}
	t.Logf("installed client login confirmed: %s", result.Code)
}

// Include native/provider identities on failure without logging account data.
func logWeChatInstalledWindows(t *testing.T, path string) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	a, err := newWeChatAutomation()
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	for _, window := range enumerateTopLevelWindows(true) {
		if !executablePathsMatch(window.ProcessPath, path) {
			continue
		}
		t.Logf("hwnd=%#x pid=%d native class=%q visible=%t owner=%#x candidate=%t", window.Handle, window.ProcessID, window.ClassName, window.IsVisible, window.OwnerHandle, weChatCandidateClass(window.ClassName))
		if !window.IsVisible {
			continue
		}
		ctx := context.Background()
		w := HiddenWindow{Handle: window.Handle, ProcessID: window.ProcessID}
		root, kind, err := a.root(ctx, w)
		if err != nil {
			t.Fatal(err)
		}
		class, err := root.text(uiaCurrentClassName)
		root.release()
		if err != nil {
			t.Fatal(err)
		}
		observation, err := a.observe(ctx, w)
		t.Logf("UIA class=%q kind=%d ready=%t error=%v", class, kind, observation.Ready, err)
	}
}
