//go:build windows

package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

var (
	procGetGUIThreadInfo    = menuUser32.NewProc("GetGUIThreadInfo")
	procPostMenuMessage     = menuUser32.NewProc("PostMessageW")
	procMenuWindowFromPoint = menuUser32.NewProc("WindowFromPoint")
	procAttachMenuInput     = menuUser32.NewProc("AttachThreadInput")
)

const (
	programMenuOpenWait     = 1500 * time.Millisecond
	programMenuCloseWait    = time.Second
	programMenuCleanupTimer = 0x5743
)

var errProgramMenuClose = errors.New("program menu did not close")

// Resolve the registration before opening it. The snapshot must precede the
// callback so unrelated windows that were already visible cannot be adopted.
type boxMenuExpansion struct {
	owner  win.HWND
	open   func() error
	report func(error)
}

type menuThreadInfo struct {
	size, flags                                        uint32
	active, focus, capture, menuOwner, moveSize, caret win.HWND
	caretRect                                          win.RECT
}

func queryMenuThread(thread uint32) menuThreadInfo {
	info := menuThreadInfo{size: uint32(unsafe.Sizeof(menuThreadInfo{}))}
	if thread != 0 {
		procGetGUIThreadInfo.Call(uintptr(thread), uintptr(unsafe.Pointer(&info)))
	}
	return info
}

type programMenuWindow struct {
	hwnd, owner              win.HWND
	thread                   uint32
	native, popup, focusOnly bool
	rect                     win.RECT
	gui                      menuThreadInfo
}

type programWindowScan struct {
	pid     uint32
	windows []programMenuWindow
	threads map[uint32]menuThreadInfo
}

var (
	programMenuScanMu sync.Mutex
	programMenuScan   *programWindowScan
)

// A single synchronous callback avoids leaking a Go callback slot on every
// timer tick. The scan stays reachable while EnumWindows calls back into Go.
var enumProgramMenuWindowsProc = syscall.NewCallback(func(hwnd win.HWND, _ uintptr) uintptr {
	scan := programMenuScan
	if scan == nil {
		return 1
	}
	if !win.IsWindowVisible(hwnd) {
		return 1
	}
	var pid uint32
	thread := win.GetWindowThreadProcessId(hwnd, &pid)
	if pid != scan.pid {
		return 1
	}
	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	var rect win.RECT
	if !win.GetWindowRect(hwnd, &rect) || rect.Right <= rect.Left || rect.Bottom <= rect.Top {
		return 1
	}
	info, ok := scan.threads[thread]
	if !ok {
		info = queryMenuThread(thread)
		scan.threads[thread] = info
	}
	var class [64]uint16
	n, _ := win.GetClassName(hwnd, &class[0], len(class))
	popup := style&win.WS_POPUP != 0 && style&(win.WS_CHILD|win.WS_THICKFRAME) == 0 && style&win.WS_CAPTION != win.WS_CAPTION
	// Some frameworks render a menu in a fixed-size top-level window
	// without system-menu controls. Adopt it only if the callback gives
	// it focus. This surface may only lose activation, never receive a
	// key or a close command (it could also be an application panel).
	focusOnly := !popup && style&(win.WS_CHILD|win.WS_THICKFRAME|win.WS_SYSMENU) == 0
	scan.windows = append(scan.windows, programMenuWindow{
		hwnd: hwnd, owner: win.GetWindow(hwnd, win.GW_OWNER), thread: thread,
		native: n > 0 && syscall.UTF16ToString(class[:n]) == "#32768", popup: popup, focusOnly: focusOnly, rect: rect, gui: info,
	})
	return 1
})

func scanProgramMenuWindows(pid uint32) []programMenuWindow {
	programMenuScanMu.Lock()
	defer programMenuScanMu.Unlock()
	scan := &programWindowScan{pid: pid, threads: make(map[uint32]menuThreadInfo)}
	programMenuScan = scan
	defer func() { programMenuScan = nil }()
	procEnumWindows.Call(enumProgramMenuWindowsProc, 0)
	return scan.windows
}

// programMenu is one callback's lifetime, including asynchronous appearance
// and cancellation. Sending a message never marks it closed: only observing
// its windows and native menu loop end permits another expansion.
type programMenu struct {
	target        boxMenuExpansion
	parent        win.HWND
	pid           uint32
	started       time.Time
	before        map[win.HWND]bool
	known         map[win.HWND]bool
	cancelled     map[win.HWND]bool
	nativeOwners  map[uint32]win.HWND
	windows       []programMenuWindow
	seen, closing bool
	closeSince    time.Time
	err           error
}

func newProgramMenu(target boxMenuExpansion, parent win.HWND, now time.Time) (*programMenu, error) {
	var pid uint32
	if target.owner == 0 || target.open == nil || win.GetWindowThreadProcessId(target.owner, &pid) == 0 {
		return nil, errors.New("program menu owner is unavailable")
	}
	p := &programMenu{
		target: target, parent: parent, pid: pid, started: now,
		before: make(map[win.HWND]bool), known: make(map[win.HWND]bool),
		cancelled: make(map[win.HWND]bool), nativeOwners: make(map[uint32]win.HWND),
	}
	for _, window := range scanProgramMenuWindows(pid) {
		p.before[window.hwnd] = true
	}
	return p, nil
}

func (p *programMenu) observe() {
	candidates := scanProgramMenuWindows(p.pid)
	p.windows = p.windows[:0]
	// A framework can own its popup without taking capture (ToolStrip),
	// or keep capture on a root while showing a separate submenu (Qt).
	// Grow only that owner chain, never all windows of the process.
	for changed := true; changed; {
		changed = false
		for _, window := range candidates {
			if p.before[window.hwnd] || window.native && window.gui.menuOwner == p.parent || p.known[window.hwnd] {
				continue
			}
			captured := window.gui.capture == window.hwnd || win.IsChild(window.hwnd, window.gui.capture)
			focused := window.gui.active == window.hwnd && (window.gui.focus == window.hwnd || win.IsChild(window.hwnd, window.gui.focus))
			if window.native || window.popup && (captured || window.owner == p.target.owner || p.known[window.owner]) || window.focusOnly && focused {
				p.known[window.hwnd], changed = true, true
			}
		}
	}
	for _, window := range candidates {
		if !p.known[window.hwnd] {
			continue
		}
		p.windows = append(p.windows, window)
		p.seen = true
		if window.native && window.gui.menuOwner != 0 && window.gui.menuOwner != p.parent {
			p.nativeOwners[window.thread] = window.gui.menuOwner
		}
	}
}

func (p *programMenu) contains(point win.POINT) bool {
	// Rectangle overlap alone could mistake a covered parent arrow for a
	// different expansion, or keep a popup alive behind another window.
	at := menuWindowFromPoint(point)
	for _, window := range p.windows {
		if pointInMenuRect(window.rect, point) && (at == window.hwnd || win.IsChild(window.hwnd, at)) {
			return true
		}
	}
	return false
}

// POINT is passed as one 64-bit value on Win64, but as two stack words on
// Win32. lxn/win's older two-argument wrapper drops Y on Win64.
func menuWindowFromPoint(point win.POINT) win.HWND {
	var hwnd uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		packed := uint64(uint32(point.X)) | uint64(uint32(point.Y))<<32
		hwnd, _, _ = procMenuWindowFromPoint.Call(uintptr(packed))
	} else {
		hwnd, _, _ = procMenuWindowFromPoint.Call(uintptr(point.X), uintptr(point.Y))
	}
	return win.HWND(hwnd)
}

func pointInMenuRect(rect win.RECT, point win.POINT) bool {
	return point.X >= rect.Left && point.X < rect.Right && point.Y >= rect.Top && point.Y < rect.Bottom
}

func (p *programMenu) done(now time.Time) bool {
	if len(p.windows) != 0 {
		return false
	}
	for thread, owner := range p.nativeOwners {
		if info := queryMenuThread(thread); info.menuOwner == owner && info.flags&0x14 != 0 {
			return false
		}
	}
	// Do not discard an in-flight callback just because the menu has not
	// painted yet. Cancellation continues to watch for its late popup.
	return p.seen || now.Sub(p.started) >= programMenuOpenWait
}

func (p *programMenu) close(now time.Time) {
	p.closing = true
	// Do not transfer focus halfway through a real click or drag. A late
	// popup can appear after cancellation was requested but before release.
	for _, button := range []uintptr{win.VK_LBUTTON, win.VK_RBUTTON, win.VK_MBUTTON} {
		state, _, _ := procGetAsyncKeyState.Call(button)
		if int16(state) < 0 {
			return
		}
	}
	if p.closeSince.IsZero() && len(p.windows) != 0 {
		p.closeSince = now
	}
	p.cancelWindows()
	if !p.closeSince.IsZero() && !p.done(now) && now.Sub(p.closeSince) >= programMenuCloseWait {
		p.fail(errProgramMenuClose)
	}
}

func (p *programMenu) fail(err error) {
	if p.err == nil {
		p.err = err
		if !errors.Is(err, errProgramMenuClose) {
			p.err = fmt.Errorf("%w: %w", errProgramMenuClose, err)
		}
		if p.target.report != nil {
			p.target.report(p.err)
		}
	}
}

func postProgramMenuMessage(hwnd win.HWND, message uint32, wParam, lParam uintptr) error {
	if r, _, err := procPostMenuMessage.Call(uintptr(hwnd), uintptr(message), wParam, lParam); r == 0 {
		// A popup can disappear between the observation and cancellation.
		if win.GetWindowThreadProcessId(hwnd, nil) == 0 {
			return nil
		}
		return err
	}
	return nil
}

// SetForegroundWindow alone cannot transfer activation out of every menu
// loop. Pair with the foreground and destination input queues for one native
// focus operation, then detach. No keys or pointer events are synthesized.
func activateMenuWindow(hwnd win.HWND) (err error) {
	thread := win.GetCurrentThreadId()
	target := win.GetWindowThreadProcessId(hwnd, nil)
	if target == 0 {
		return errors.New("menu focus destination is unavailable")
	}
	foreground := win.GetWindowThreadProcessId(win.GetForegroundWindow(), nil)
	var attached []uint32
	defer func() {
		for i := len(attached) - 1; i >= 0; i-- {
			if ok, _, detachErr := procAttachMenuInput.Call(uintptr(thread), uintptr(attached[i]), 0); ok == 0 {
				err = errors.Join(err, detachErr)
			}
		}
	}()
	for _, other := range []uint32{foreground, target} {
		if other == 0 || other == thread || len(attached) != 0 && other == attached[0] {
			continue
		}
		if ok, _, attachErr := procAttachMenuInput.Call(uintptr(thread), uintptr(other), 1); ok == 0 {
			return attachErr
		}
		attached = append(attached, other)
	}
	win.BringWindowToTop(hwnd)
	win.SetForegroundWindow(hwnd)
	win.SetFocus(hwnd)
	return nil
}

func dismissFocusMenu(window programMenuWindow, parent win.HWND) error {
	if err := activateMenuWindow(window.hwnd); err != nil {
		return err
	}
	return activateMenuWindow(parent)
}

func (p *programMenu) cancelWindows() {
	sent := make(map[win.HWND]bool)
	for _, window := range p.windows {
		if p.cancelled[window.hwnd] {
			continue
		}
		if window.native {
			// Entering activation on the popup itself makes Windows leave
			// its owner's menu mode. This also works when UIPI rejects
			// WM_CANCELMODE sent to an elevated owner. It is not a simulated
			// desktop click and cannot invoke an underlying application.
			// Still wait for both the popup and its native loop to disappear.
			info := queryMenuThread(window.thread)
			owner := info.menuOwner
			var pid uint32
			if owner == 0 || owner == p.parent || win.GetWindowThreadProcessId(owner, &pid) != window.thread || pid != p.pid {
				continue
			}
			p.cancelled[window.hwnd] = true
			if !sent[owner] {
				sent[owner] = true
				popup := window.hwnd
				// Activation from inside our own modal loop can end that
				// loop as well. Use a menu-free thread; the UI keeps watching
				// the target window and never treats dispatch as completion.
				go func() {
					runtime.LockOSThread()
					defer runtime.UnlockOSThread()
					win.SetForegroundWindow(popup)
				}()
			}
			continue
		}
		if window.focusOnly {
			p.cancelled[window.hwnd] = true
			// These menus close themselves on blur. Activation is safe even
			// if the surface is not a menu: no key or close command is sent.
			if err := dismissFocusMenu(window, p.parent); err != nil {
				p.fail(err)
			}
			continue
		}
		if !window.popup {
			continue // a repurposed document window is not a key target
		}
		p.cancelled[window.hwnd] = true
		// Qt and other framework popups own their input loop instead of
		// TrackPopupMenu. Escape targets only the observed popup, never a
		// global key press, an application main window or another process.
		if err := errors.Join(
			postProgramMenuMessage(window.hwnd, win.WM_KEYDOWN, win.VK_ESCAPE, 1),
			postProgramMenuMessage(window.hwnd, win.WM_KEYUP, win.VK_ESCAPE, 0xc0000001),
		); err != nil {
			p.fail(err)
		}
	}
}

// Keep cancellation alive when the parent menu exits while the callback is
// still opening. A new parent menu takes over this same closing lifetime;
// closing and opening can never run as independent, overlapping attempts.
var retiringProgramMenu *programMenu
var retiringProgramMenuOwner win.HWND
var programMenuCleanupProc uintptr

func init() {
	programMenuCleanupProc = syscall.NewCallback(func(hwnd win.HWND, _ uint32, _ uintptr, _ uint32) uintptr {
		p := retiringProgramMenu
		if p == nil || hwnd != retiringProgramMenuOwner {
			return 0
		}
		now := time.Now()
		p.observe()
		p.close(now)
		if p.done(now) {
			win.KillTimer(hwnd, programMenuCleanupTimer)
			retiringProgramMenu, retiringProgramMenuOwner = nil, 0
		} else if p.err != nil {
			// A program that declined dismissal must not cause a busy background
			// scan while waiting for the user to close its menu manually.
			win.SetTimer(hwnd, programMenuCleanupTimer, 250, programMenuCleanupProc)
		}
		return 0
	})
}

func takeRetiringProgramMenu() *programMenu {
	p := retiringProgramMenu
	if p != nil {
		win.KillTimer(retiringProgramMenuOwner, programMenuCleanupTimer)
		retiringProgramMenu, retiringProgramMenuOwner = nil, 0
	}
	return p
}

func retireProgramMenu(owner win.HWND, p *programMenu) {
	p.observe()
	p.close(time.Now())
	if p.done(time.Now()) {
		return
	}
	retiringProgramMenu, retiringProgramMenuOwner = p, owner
	if win.SetTimer(owner, programMenuCleanupTimer, 50, programMenuCleanupProc) == 0 {
		p.fail(errors.New("start program menu cancellation timer"))
	}
}
