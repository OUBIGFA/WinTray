//go:build windows

package orchestrator

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/config"
)

type startupFixture struct {
	Owner uintptr
	Forms []uintptr
}

var fixture startupFixture
var fixtureRefusesClose bool
var fixturePartiallyRefusesClose bool
var fixtureOwnerCloses int
var fixtureFormCloses int
var fixtureProc = syscall.NewCallback(func(hwnd win.HWND, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case win.WM_SYSCOMMAND:
		if w&0xfff0 == scClose {
			if uintptr(hwnd) == fixture.Owner {
				fixtureOwnerCloses++
				return 0
			}
			fixtureFormCloses++
			if !fixtureRefusesClose && !(fixturePartiallyRefusesClose && len(fixture.Forms) > 1 && uintptr(hwnd) == fixture.Forms[1]) {
				win.SetTimer(hwnd, 17, 180, 0)
			}
			return 0
		}
	case win.WM_TIMER:
		win.KillTimer(hwnd, w)
		win.ShowWindow(hwnd, win.SW_HIDE)
		return 0
	case win.WM_APP + 20:
		return uintptr(fixtureOwnerCloses)
	case win.WM_APP + 21:
		for _, form := range fixture.Forms {
			win.ShowWindow(win.HWND(form), win.SW_SHOWNOACTIVATE)
		}
		return 1
	case win.WM_APP + 22:
		return uintptr(fixtureFormCloses)
	}
	return win.DefWindowProc(hwnd, msg, w, l)
})

// The fixture owns an invisible application/tray receiver and two normal UI
// windows. They are offscreen so a failed visual-shield test cannot disturb
// the user's desktop. Only this isolated helper is ever launched/terminated.
func TestStartupVisibilityProcess(t *testing.T) {
	if len(os.Args) != 5 || os.Args[2] != "--" {
		return
	}
	runtime.LockOSThread()
	fixtureRefusesClose = os.Args[4] == "refuse"
	fixturePartiallyRefusesClose = os.Args[4] == "partial"
	class := syscall.StringToUTF16Ptr("WinTrayStartupVisibilityFixture")
	wc := win.WNDCLASSEX{LpfnWndProc: fixtureProc, HInstance: win.GetModuleHandle(nil), LpszClassName: class}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if win.RegisterClassEx(&wc) == 0 {
		os.Exit(2)
	}
	owner := win.CreateWindowEx(0, class, nil, win.WS_POPUP, -32000, -32000, 1, 1, 0, 0, wc.HInstance, nil)
	fixture.Owner = uintptr(owner)
	for i := 0; i < 2; i++ {
		form := win.CreateWindowEx(0, class, syscall.StringToUTF16Ptr("Fixture UI"), win.WS_OVERLAPPEDWINDOW, -32000, -32000, 120, 80, owner, 0, wc.HInstance, nil)
		fixture.Forms = append(fixture.Forms, uintptr(form))
		if os.Args[4] == "layered" {
			win.SetWindowLong(form, win.GWL_EXSTYLE, exLayered)
			procSetLayeredWindowAttributes.Call(uintptr(form), 0x112233, 173, 3)
		}
		if os.Args[4] != "silent" {
			win.ShowWindow(form, win.SW_SHOWDEFAULT)
		}
	}
	data, _ := json.Marshal(fixture)
	if err := os.WriteFile(os.Args[3], data, 0600); err != nil {
		os.Exit(3)
	}
	var msg win.MSG
	var pin runtime.Pinner
	pin.Pin(&msg)
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
	os.Exit(0)
}

func startupFixtureEntry(t *testing.T, mode string) (config.ManagedAppEntry, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Optional isolated 32-bit helper exercises a 64-bit manager controlling
	// legacy 32-bit windows. It is built from this same test fixture.
	if helper := os.Getenv("WINTRAY_STARTUP_FIXTURE_EXE"); helper != "" {
		self = helper
	}
	image, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	// Make the copied fixture a GUI PE; no console window is involved.
	// The original test executable is never modified.
	pe := int(binary.LittleEndian.Uint32(image[0x3c:]))
	binary.LittleEndian.PutUint16(image[pe+24+68:], 2)
	dir := t.TempDir()
	exe := filepath.Join(dir, "startup-fixture.exe")
	if err := os.WriteFile(exe, image, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "windows.json")
	entry := config.ManagedAppEntry{Name: "Startup fixture", ExePath: exe, Args: "-test.run=^TestStartupVisibilityProcess$ -- " + syscall.EscapeArg(file) + " " + mode,
		TrayBehavior: config.TrayBehavior{AutoMinimizeAndHideOnLaunch: true, CloseDelaySeconds: 1}}
	t.Cleanup(func() {
		if pid := findRunningProcessByIdentity(exe, "startup-fixture"); pid != 0 {
			process, _ := os.FindProcess(int(pid))
			if process != nil {
				_ = process.Kill()
				_, _ = process.Wait()
			}
		}
	})
	return entry, file
}

func awaitStartupFixture(t *testing.T, file string) startupFixture {
	t.Helper()
	var state startupFixture
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(file)
		if err == nil && json.Unmarshal(data, &state) == nil && len(state.Forms) == 2 {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("startup helper did not publish its windows")
	return state
}

func TestStartupVisibilityClosesBehindShieldThenRestoresNativeInteraction(t *testing.T) {
	entry, file := startupFixtureEntry(t, "normal")
	entry.TrayBehavior.CloseDelaySeconds = 3
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan Result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- svc.StartAndManage(ctx, entry, 5)
	}()
	defer func() {
		cancel()
		<-done // restore attributes even if an assertion fails mid-startup
	}()
	state := awaitStartupFixture(t, file)
	deadline := time.Now().Add(2 * time.Second)
	// WinEvents arrive asynchronously for each form. The ownership property
	// is set before transparency, so it is not a completion signal, nor does
	// the first form being ready imply that the second one is ready.
	for _, form := range state.Forms {
		var color, flags uint32
		var alpha byte
		var ok uintptr
		for {
			ok, _, _ = procGetLayeredWindowAttributes.Call(form, uintptr(unsafe.Pointer(&color)), uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags)))
			if ok != 0 && alpha == 0 && flags&lwaAlpha != 0 || !time.Now().Before(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if ok == 0 || alpha != 0 || flags&lwaAlpha == 0 {
			t.Fatalf("window %x not visually shielded during close delay: ok=%d alpha=%d flags=%x", form, ok, alpha, flags)
		}
	}
	if win.SendMessage(win.HWND(state.Owner), win.WM_APP+22, 0, 0) != 0 {
		t.Fatal("close delay was bypassed before both windows were shielded")
	}
	select {
	case got := <-result:
		if !got.Managed || got.Code != ResultManaged {
			t.Fatalf("result=%+v", got)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("startup management did not finish")
	}
	if win.SendMessage(win.HWND(state.Owner), win.WM_APP+20, 0, 0) != 0 {
		t.Fatal("hidden application owner was closed")
	}
	if win.SendMessage(win.HWND(state.Owner), win.WM_APP+22, 0, 0) != 2 {
		t.Fatal("both actual UI windows must process a close")
	}
	for _, form := range state.Forms {
		if isWindowVisible(form) || visibilityOwner(form) != 0 || win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE)&(exLayered|exToolWindow|exNoActivate) != 0 {
			t.Fatalf("window %x was not natively closed and released", form)
		}
	}
	// Simulate the app's own tray callback: no WinTray-hosted replacement is
	// involved, and later UI shows must not be hidden by a surviving watcher.
	win.SendMessage(win.HWND(state.Owner), win.WM_APP+21, 0, 0)
	for _, form := range state.Forms {
		if !waitVisible(form, true) {
			t.Fatal("native restore no longer works")
		}
	}
}

func TestStartupVisibilityDoesNotMistakeTransparencyForNativeClose(t *testing.T) {
	entry, file := startupFixtureEntry(t, "refuse")
	entry.TrayBehavior.CloseDelaySeconds = 0
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	got := svc.StartAndManage(context.Background(), entry, 1)
	state := awaitStartupFixture(t, file)
	if got.Managed || got.Code != ResultNoWindowManaged {
		t.Fatalf("refused close reported success: %+v", got)
	}
	for _, form := range state.Forms {
		if !waitVisible(form, true) || visibilityOwner(form) != 0 || win.GetWindowLong(win.HWND(form), win.GWL_EXSTYLE)&exLayered != 0 {
			t.Fatal("timeout left a permanently invisible UI")
		}
	}
}

func TestStartupVisibilityCancellationRestoresTemporaryAttributes(t *testing.T) {
	entry, file := startupFixtureEntry(t, "normal")
	entry.TrayBehavior.CloseDelaySeconds = 30
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	result := make(chan Result, 1)
	go func() { result <- svc.StartAndManage(ctx, entry, 5) }()
	state := awaitStartupFixture(t, file)
	cancel()
	select {
	case got := <-result:
		if got.Managed {
			t.Fatal("cancelled delay reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not restore windows")
	}
	for _, form := range state.Forms {
		if !waitVisible(form, true) || visibilityOwner(form) != 0 {
			t.Fatal("cancelled delay stranded hidden UI")
		}
	}
	if win.SendMessage(win.HWND(state.Owner), win.WM_APP+22, 0, 0) != 0 {
		t.Fatal("close delay was bypassed during cancellation")
	}
}
