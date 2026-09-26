//go:build windows

package traybox

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	shell32                    = windows.NewLazySystemDLL("shell32.dll")
	procShellNotifyIconGetRect = shell32.NewProc("Shell_NotifyIconGetRect")
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW          = user32.NewProc("FindWindowExW")
)

// hwndMessage is HWND_MESSAGE: many tray icons belong to message-only windows.
const hwndMessage = ^uintptr(2)

// overflowClass is the window of the system's hidden-icons flyout.
const overflowClass = "TopLevelWindowForOverflowXamlIsland"

// Located is an icon that is currently in the notification area together
// with the window that owns it.
type Located struct {
	Icon
	Owner win.HWND
}

type notifyIconIdentifier struct {
	CbSize   uint32
	HWnd     win.HWND
	UID      uint32
	GUIDItem windows.GUID
}

func iconRect(owner win.HWND, uid uint32) (win.RECT, bool) {
	id := notifyIconIdentifier{HWnd: owner, UID: uid}
	id.CbSize = uint32(unsafe.Sizeof(id))
	var r win.RECT
	hr, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&r)))
	return r, hr == 0 && r.Right > r.Left
}

// Locate keeps the icons whose program is running and whose icon Explorer
// currently shows, excluding the given program (WinTray itself). An icon is
// found through the windows of its program's processes, as the settings do
// not record the owning window.
func Locate(icons []Icon, exclude string) []Located {
	wanted := make(map[string]bool)
	for _, icon := range icons {
		if !strings.EqualFold(icon.ExePath, exclude) {
			wanted[strings.ToLower(icon.ExePath)] = true
		}
	}
	pids := processesByPath(wanted)
	if len(pids) == 0 {
		return nil
	}
	windowsByPath := make(map[string][]win.HWND)
	for _, hwnd := range allWindows() {
		var pid uint32
		win.GetWindowThreadProcessId(hwnd, &pid)
		if path, ok := pids[pid]; ok {
			windowsByPath[path] = append(windowsByPath[path], hwnd)
		}
	}
	type iconID struct {
		owner win.HWND
		uid   uint32
	}
	seen := make(map[iconID]bool)
	var out []Located
	for _, icon := range icons {
		for _, hwnd := range windowsByPath[strings.ToLower(icon.ExePath)] {
			if _, ok := iconRect(hwnd, icon.UID); !ok {
				continue
			}
			// Old entries of the same program may repeat a uID.
			if id := (iconID{hwnd, icon.UID}); !seen[id] {
				seen[id] = true
				out = append(out, Located{Icon: icon, Owner: hwnd})
			}
		}
	}
	return out
}

// processesByPath maps the IDs of running processes to their lower-case
// image path, for the wanted paths only.
func processesByPath(wanted map[string]bool) map[uint32]string {
	out := make(map[uint32]string)
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if path := processPath(entry.ProcessID); wanted[path] {
			out[entry.ProcessID] = path
		}
	}
	return out
}

func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return ""
	}
	return strings.ToLower(windows.UTF16ToString(buf[:size]))
}

var (
	enumMu      sync.Mutex
	enumResult  []win.HWND
	enumWindows = syscall.NewCallback(func(hwnd win.HWND, _ uintptr) uintptr {
		enumResult = append(enumResult, hwnd)
		return 1
	})
)

// allWindows returns top-level and message-only windows.
func allWindows() []win.HWND {
	enumMu.Lock()
	enumResult = nil
	_ = windows.EnumWindows(enumWindows, nil)
	out := enumResult
	enumResult = nil
	enumMu.Unlock()
	var after uintptr
	for {
		hwnd, _, _ := procFindWindowExW.Call(hwndMessage, after, 0, 0)
		if hwnd == 0 {
			return out
		}
		out = append(out, win.HWND(hwnd))
		after = hwnd
	}
}

// Activate clicks the icon as if the user had. An icon in the hidden-icons
// flyout is only clickable while the flyout is open, so the flyout is opened
// first; the cursor is put back afterwards. Must not run on the UI thread:
// it waits for the flyout to appear.
func Activate(icon Located, action Action) error {
	r, ok := iconRect(icon.Owner, icon.UID)
	if !ok {
		return fmt.Errorf("%s: the icon is no longer in the notification area", icon.DisplayName())
	}
	var saved win.POINT
	win.GetCursorPos(&saved)
	defer win.SetCursorPos(saved.X, saved.Y)

	if !icon.Promoted {
		var err error
		if r, err = openFlyoutAt(icon, r); err != nil {
			return err
		}
	}
	x, y := (r.Left+r.Right)/2, (r.Top+r.Bottom)/2
	switch action {
	case ActionRightClick:
		click(x, y, win.MOUSEEVENTF_RIGHTDOWN, win.MOUSEEVENTF_RIGHTUP)
	case ActionDoubleClick:
		click(x, y, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP)
		click(x, y, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP)
	default:
		click(x, y, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP)
	}
	return nil
}

// ErrFlyoutUnavailable means the hidden-icons flyout did not open, for
// example because the taskbar's hidden icon menu is turned off.
var ErrFlyoutUnavailable = errors.New("the taskbar's hidden icons menu did not open; make sure \"Hidden icon menu\" is on in the taskbar settings")

// openFlyoutAt opens the flyout unless it is open already and returns where
// the icon is inside it once the flyout has stopped moving. While the flyout
// is closed, Explorer reports the flyout button as the icon's position.
func openFlyoutAt(icon Located, r win.RECT) (win.RECT, error) {
	if _, open := flyoutRect(); !open {
		click((r.Left+r.Right)/2, (r.Top+r.Bottom)/2, win.MOUSEEVENTF_LEFTDOWN, win.MOUSEEVENTF_LEFTUP)
	}
	var last win.RECT
	stable := 0
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(40 * time.Millisecond) {
		fr, open := flyoutRect()
		if !open {
			continue
		}
		cur, ok := iconRect(icon.Owner, icon.UID)
		if !ok || !inside(cur, fr) {
			stable = 0
			continue
		}
		// The flyout slides in; clicking during the animation would miss.
		if cur == last {
			stable++
		} else {
			stable = 0
		}
		last = cur
		if stable >= 2 {
			return cur, nil
		}
	}
	return win.RECT{}, ErrFlyoutUnavailable
}

func flyoutRect() (win.RECT, bool) {
	class, _ := syscall.UTF16PtrFromString(overflowClass)
	hwnd := win.FindWindow(class, nil)
	var r win.RECT
	if hwnd == 0 || !win.IsWindowVisible(hwnd) || !win.GetWindowRect(hwnd, &r) {
		return r, false
	}
	return r, true
}

func inside(r, outer win.RECT) bool {
	return r.Left >= outer.Left && r.Top >= outer.Top && r.Right <= outer.Right && r.Bottom <= outer.Bottom
}

// click presses and releases a mouse button at a screen position. The pause
// lets XAML buttons see a real press, as with a user's click.
func click(x, y int32, down, up uint32) {
	win.SetCursorPos(x, y)
	time.Sleep(30 * time.Millisecond)
	sendMouse(down)
	time.Sleep(50 * time.Millisecond)
	sendMouse(up)
	time.Sleep(30 * time.Millisecond)
}

func sendMouse(flags uint32) {
	input := win.MOUSE_INPUT{Type: win.INPUT_MOUSE, Mi: win.MOUSEINPUT{DwFlags: flags}}
	win.SendInput(1, unsafe.Pointer(&input), int32(unsafe.Sizeof(input)))
}
