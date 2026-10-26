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
	"syscall"
	"testing"
	"time"
	"unsafe"
	"wintray/internal/config"
)

var weChatFixtureWindows startupFixture
var weChatFixtureMode string
var weChatFixtureClicks int
var weChatFixtureProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case win.WM_COMMAND:
		if w>>16 == 0 {
			weChatFixtureClicks++
			if weChatFixtureMode == "ready" {
				win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[0]), win.SW_HIDE)
				win.ShowWindow(win.HWND(weChatFixtureWindows.Forms[1]), win.SW_SHOWNOACTIVATE)
			}
		}
		return 0
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
	for _, name := range []string{"WeChatLoginWnd", "WeChatMainWndForPC", "WinTrayWeChatPanel"} {
		wc := win.WNDCLASSEX{LpfnWndProc: weChatFixtureProc, HInstance: win.GetModuleHandle(nil), LpszClassName: syscall.StringToUTF16Ptr(name)}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if win.RegisterClassEx(&wc) == 0 {
			os.Exit(2)
		}
	}
	create := func(class, name string, parent win.HWND, style uint32) win.HWND {
		return win.CreateWindowEx(win.WS_EX_NOACTIVATE, syscall.StringToUTF16Ptr(class), syscall.StringToUTF16Ptr(name), style, 10, 10, 240, 150, parent, 0, win.GetModuleHandle(nil), nil)
	}
	login := create("WeChatLoginWnd", "WinTray login test", 0, win.WS_OVERLAPPEDWINDOW)
	main := create("WeChatMainWndForPC", "WinTray main test", 0, win.WS_OVERLAPPEDWINDOW)
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
	entry, file := weChatFixtureEntry(t, "ready")
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
	for _, mode := range []string{"phone", "qr", "disabled", "ambiguous"} {
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
			if mode == "phone" {
				want = 1
			}
			if clicks := win.SendMessage(win.HWND(state.Owner), win.WM_APP+20, 0, 0); clicks != want {
				t.Fatalf("clicks=%d want=%d", clicks, want)
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
		t.Fatalf("installed client login: %+v", result)
	}
	t.Logf("installed client login confirmed: %s", result.Code)
}
