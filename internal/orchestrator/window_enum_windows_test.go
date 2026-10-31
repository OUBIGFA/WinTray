//go:build windows

package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"wintray/internal/config"
)

// A background launch reads titles from WinTray's own HWNDs. Reading an
// in-process title sends WM_GETTEXTLENGTH to the window thread, which can also
// be checking the selected program's running state while rendering. Exercise
// that reentry in a child so a lock inversion cannot hang the whole test suite.
func TestWindowEnumerationReentry(t *testing.T) {
	if len(os.Args) == 4 && os.Args[2] == "--" {
		runWindowEnumerationReentry(t, os.Args[3] == "hidden")
		return
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"visible", "hidden"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, self, "-test.run=^TestWindowEnumerationReentry$", "--", mode)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("window thread could not enumerate during a background title read: %v\n%s", err, output)
			}
		})
	}
}

func runWindowEnumerationReentry(t *testing.T, includeHidden bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	watchdog := time.AfterFunc(5*time.Second, func() {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		fmt.Fprintf(os.Stderr, "window enumeration deadlocked:\n%s", buf[:n])
		os.Exit(2)
	})
	defer watchdog.Stop()

	svc := NewService(NewWin32WindowEnumerator(), NewWin32WindowManager(), &testLogger{})
	missing := config.ManagedAppEntry{ExePath: `C:\WinTray-enumeration-test\missing-program.exe`}
	entered := false
	callback := syscall.NewCallback(func(hwnd win.HWND, msg uint32, w, l uintptr) uintptr {
		switch msg {
		case win.WM_GETTEXTLENGTH:
			if !entered {
				entered = true // the nested scan must use the ordinary title handler
				if svc.IsProgramRunning(missing) {
					t.Error("unrelated window was treated as the missing program")
				}
			}
		case win.WM_APP + 42:
			win.PostQuitMessage(0)
			return 0
		}
		return win.DefWindowProc(hwnd, msg, w, l)
	})
	class := syscall.StringToUTF16Ptr("WinTrayEnumerationReentry")
	wc := win.WNDCLASSEX{LpfnWndProc: callback, HInstance: win.GetModuleHandle(nil), LpszClassName: class}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if win.RegisterClassEx(&wc) == 0 {
		t.Fatal("register enumeration window")
	}
	defer win.UnregisterClass(class)
	hwnd := win.CreateWindowEx(win.WS_EX_NOACTIVATE, class, syscall.StringToUTF16Ptr("Enumeration fixture"), win.WS_OVERLAPPEDWINDOW, 10, 10, 240, 100, 0, 0, wc.HInstance, nil)
	if hwnd == 0 {
		t.Fatal("create enumeration window")
	}
	defer win.DestroyWindow(hwnd)
	if !includeHidden {
		win.ShowWindow(hwnd, win.SW_SHOWNOACTIVATE)
	}
	// Window creation must not consume the reentry before the background scan.
	entered = false
	result := make(chan []ManagedWindowInfo, 1)
	go func() {
		result <- enumerateTopLevelWindows(includeHidden)
		win.PostMessage(hwnd, win.WM_APP+42, 0, 0)
	}()
	var msg win.MSG
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
	if !entered {
		t.Fatal("background enumeration did not request the window title")
	}
	for _, window := range <-result {
		if window.Handle == uintptr(hwnd) {
			if window.Title != "Enumeration fixture" || window.ClassName != "WinTrayEnumerationReentry" || window.ProcessID != uint32(os.Getpid()) || window.IsVisible == includeHidden {
				t.Fatalf("enumeration lost window metadata: %+v", window)
			}
			return
		}
	}
	t.Fatal("enumeration lost its own window during reentry")
}
