//go:build windows

package orchestrator

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// startupVisibility is a short-lived, non-injected visual shield, not a
// drawing barrier. Out-of-context WinEvents are asynchronous: applications
// which display immediately can still show a frame before we see them.
// Crucially, transparency does not clear WS_VISIBLE, so it cannot masquerade
// as the application's own close-to-tray action.
type startupVisibility struct {
	path      string
	logger    Logger
	token     uintptr
	thread    uint32
	commands  chan visibilityCommand
	done      chan struct{}
	stopOnce  sync.Once
	stopErr   error
	windows   map[uintptr]*veiledWindow // owned by the message-loop thread
	paths     map[uint32]string
	warned    map[uintptr]bool
	taskbar   *startupTaskbar
	restoring bool // do not re-shield from WinEvents dispatched by COM during release
}

type visibilityCommand struct {
	apply func()
	stop  bool
	done  chan struct{}
}

type veiledWindow struct {
	addedStyle    int32
	color         uint32
	alpha         byte
	flags         uint32
	layered       bool
	taskbarHidden bool
}

const (
	visibilityMessage       = win.WM_APP + 73
	exLayered         int32 = 0x00080000
	exNoActivate      int32 = 0x08000000
	exToolWindow      int32 = 0x80
	lwaAlpha                = 2
)

var (
	visibilitySerial               atomic.Uint64
	visibilitySessions             sync.Map
	visibilityHooks                sync.Map
	visibilityProperty             = syscall.StringToUTF16Ptr("WinTray.StartupVisibility")
	procPostThreadMessage          = user32.NewProc("PostThreadMessageW")
	procGetProp                    = user32.NewProc("GetPropW")
	procSetProp                    = user32.NewProc("SetPropW")
	procRemoveProp                 = user32.NewProc("RemovePropW")
	procSetWinEventHook            = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent             = user32.NewProc("UnhookWinEvent")
	procGetLayeredWindowAttributes = user32.NewProc("GetLayeredWindowAttributes")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	visibilityEvent                = syscall.NewCallback(func(hook, event, hwnd uintptr, objectID, childID int32, thread, stamp uint32) uintptr {
		if objectID != 0 || childID != 0 || hwnd == 0 {
			return 0
		}
		if value, ok := visibilityHooks.Load(hook); ok {
			g := value.(*startupVisibility)
			if event == 0x8001 {
				delete(g.windows, hwnd)
				delete(g.warned, hwnd)
			} else {
				g.track(hwnd)
			}
		}
		return 0
	})
	visibilityEnum = syscall.NewCallback(func(hwnd, token uintptr) uintptr {
		if value, ok := visibilitySessions.Load(token); ok {
			value.(*startupVisibility).track(hwnd)
		}
		return 1
	})
)

func visibilityOwner(hwnd uintptr) uintptr {
	owner, _, _ := procGetProp.Call(hwnd, uintptr(unsafe.Pointer(visibilityProperty)))
	return owner
}

func beginStartupVisibility(path string, logger Logger) (*startupVisibility, error) {
	g := &startupVisibility{path: normalizePath(path), logger: logger, token: uintptr(visibilitySerial.Add(1)),
		commands: make(chan visibilityCommand, 8), done: make(chan struct{}),
		windows: make(map[uintptr]*veiledWindow), paths: make(map[uint32]string), warned: make(map[uintptr]bool)}
	ready := make(chan error, 1)
	go g.run(ready)
	if err := <-ready; err != nil {
		<-g.done
		return nil, err
	}
	return g, nil
}

func (g *startupVisibility) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(g.done)
	g.thread = windows.GetCurrentThreadId()
	var err error
	g.taskbar, err = newStartupTaskbar()
	if err != nil {
		// Shell readiness/permissions must not turn a best-effort visual
		// improvement into a failure to launch or close the application.
		g.logger.Warn(fmt.Sprintf("startup taskbar shield unavailable: %v", err))
	} else {
		defer g.taskbar.close() // after g.restore, on the same COM apartment
	}
	var msg win.MSG
	var pin runtime.Pinner
	pin.Pin(&msg)
	defer pin.Unpin()
	win.PeekMessage(&msg, 0, 0, 0, win.PM_NOREMOVE)                                       // create the command queue
	hook, _, err := procSetWinEventHook.Call(0x8000, 0x8002, 0, visibilityEvent, 0, 0, 2) // OUTOFCONTEXT | SKIPOWNPROCESS
	if hook == 0 {
		ready <- fmt.Errorf("watch startup windows: %v", err)
		return
	}
	visibilitySessions.Store(g.token, g)
	visibilityHooks.Store(hook, g)
	defer visibilitySessions.Delete(g.token)
	defer visibilityHooks.Delete(hook)
	defer procUnhookWinEvent.Call(hook)
	timer := win.SetTimer(0, 0, 100, 0)
	if timer == 0 {
		ready <- errors.New("create startup window scan timer")
		return
	}
	defer win.KillTimer(0, timer)
	defer g.restore()
	g.scan()
	ready <- nil
	for win.GetMessage(&msg, 0, 0, 0) > 0 {
		switch msg.Message {
		case visibilityMessage, win.WM_TIMER:
			// The timer also drains commands if PostThreadMessage failed
			// (e.g. a full queue). Never block on a stale wake-up message.
			if g.drainCommands() {
				return
			}
			if msg.Message == win.WM_TIMER {
				g.scan()
			}
		default:
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
	}
	g.stopErr = errors.New("startup visibility message loop stopped unexpectedly")
}

func (g *startupVisibility) drainCommands() bool {
	for {
		select {
		case command := <-g.commands:
			command.apply()
			close(command.done)
			if command.stop {
				return true
			}
		default:
			return false
		}
	}
}

func (g *startupVisibility) scan() {
	clear(g.paths) // cache only one scan, not a potentially reused PID
	procEnumWindows.Call(visibilityEnum, g.token)
}

func (g *startupVisibility) invoke(apply func(), stop bool) {
	command := visibilityCommand{apply: apply, stop: stop, done: make(chan struct{})}
	select {
	case g.commands <- command:
	case <-g.done:
		return
	}
	if ok, _, err := procPostThreadMessage.Call(uintptr(g.thread), visibilityMessage, 0, 0); ok == 0 {
		g.logger.Warn(fmt.Sprintf("startup visibility wake-up failed; timer will drain command: %v", err))
	}
	select {
	case <-command.done:
	case <-g.done:
	}
}

func (g *startupVisibility) track(hwnd uintptr) {
	if g.restoring || !isWindow(hwnd) {
		return
	}
	if old := g.windows[hwnd]; old != nil {
		if visibilityOwner(hwnd) == g.token {
			g.hideTaskbar(hwnd, old)
			return
		}
		delete(g.windows, hwnd) // the HWND was destroyed/reused
	}
	var pid uint32
	procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == uint32(syscall.Getpid()) {
		return
	}
	path, known := g.paths[pid]
	if !known {
		_, path = processInfo(pid)
		g.paths[pid] = path
	}
	if !executablePathsMatch(path, g.path) {
		return
	}
	// Revalidate a positive cache hit before changing another process's UI.
	if _, currentPath := processInfo(pid); !executablePathsMatch(currentPath, g.path) {
		return
	}
	style := win.GetWindowLong(win.HWND(hwnd), win.GWL_STYLE)
	exStyle := win.GetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE)
	// Never touch invisible application owners, message-only tray receivers,
	// child controls or utility windows. Hidden UI may be shielded before
	// its first show, but is never shown or closed by the shield itself.
	if style&win.WS_CHILD != 0 || style&win.WS_SYSMENU == 0 || exStyle&exToolWindow != 0 {
		return
	}
	if visibilityOwner(hwnd) != 0 {
		return
	}
	v := &veiledWindow{}
	v.layered = exStyle&exLayered != 0
	if v.layered {
		ok, _, _ := procGetLayeredWindowAttributes.Call(hwnd, uintptr(unsafe.Pointer(&v.color)), uintptr(unsafe.Pointer(&v.alpha)), uintptr(unsafe.Pointer(&v.flags)))
		if ok == 0 {
			g.warn(hwnd, "per-pixel layered window cannot be safely shielded")
			return
		}
	}
	// Keep the shell's window classification intact. Adding TOOLWINDOW or
	// NOACTIVATE to an already registered taskbar window suppresses its later
	// HSHELL_WINDOWDESTROYED notification when the app hides it on close.
	// Explorer then retains an unresponsive button even after styles restore.
	// Transparency is best effort; it must not alter the native tray lifecycle.
	v.addedStyle = exLayered &^ exStyle
	if ok, _, _ := procSetProp.Call(hwnd, uintptr(unsafe.Pointer(visibilityProperty)), g.token); ok == 0 {
		g.warn(hwnd, "window property access denied")
		return
	}
	win.SetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE, exStyle|v.addedStyle)
	ok, _, _ := procSetLayeredWindowAttributes.Call(hwnd, uintptr(v.color), 0, uintptr(v.flags|lwaAlpha))
	if ok == 0 {
		win.SetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE, exStyle)
		procRemoveProp.Call(hwnd, uintptr(unsafe.Pointer(visibilityProperty)))
		g.warn(hwnd, "window transparency access denied")
		return
	}
	g.windows[hwnd] = v
	g.hideTaskbar(hwnd, v)
	win.SetWindowPos(win.HWND(hwnd), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
}

func (g *startupVisibility) warn(hwnd uintptr, reason string) {
	if !g.warned[hwnd] {
		g.warned[hwnd] = true
		g.logger.Warn(fmt.Sprintf("startup visual shield unavailable: hwnd=0x%X path=%s: %s (native close remains best effort)", hwnd, g.path, reason))
	}
}

func (g *startupVisibility) candidates() []ManagedWindowInfo {
	var result []ManagedWindowInfo
	g.invoke(func() {
		g.scan()
		for hwnd := range g.windows {
			if !isWindow(hwnd) || visibilityOwner(hwnd) != g.token || !isWindowVisible(hwnd) {
				continue
			}
			w := windowInfo(hwnd, 0, nil)
			result = append(result, w)
		}
	}, false)
	return result
}

// restore removes only attributes added by this session. Native visibility
// remains untouched: a successful close stays closed, a refused close stays
// usable, and an app that started silently is not accidentally opened.
func (g *startupVisibility) restore() {
	g.restoring = true
	var errs []error
	for hwnd, v := range g.windows {
		if !isWindow(hwnd) || visibilityOwner(hwnd) != g.token {
			continue
		}
		if v.layered {
			if ok, _, err := procSetLayeredWindowAttributes.Call(hwnd, uintptr(v.color), uintptr(v.alpha), uintptr(v.flags)); ok == 0 {
				errs = append(errs, fmt.Errorf("restore window 0x%X transparency: %v", hwnd, err))
			}
		}
		style := win.GetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE)
		win.SetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE, style&^v.addedStyle)
		if win.GetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE)&v.addedStyle != 0 {
			errs = append(errs, fmt.Errorf("restore window 0x%X styles failed", hwnd))
		}
		procRemoveProp.Call(hwnd, uintptr(unsafe.Pointer(visibilityProperty)))
		win.SetWindowPos(win.HWND(hwnd), 0, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
		// Release the explicit taskbar suppression even after a native close.
		// AddTab re-enrolls a hidden window without showing it or its button;
		// omitting this would also suppress its next native tray restoration.
		// Untouched silent/owned/utility windows are never enrolled here.
		if v.taskbarHidden && taskbarEligibleWindow(hwnd) {
			if err := g.taskbar.setVisible(hwnd, true); err != nil {
				errs = append(errs, fmt.Errorf("restore window 0x%X taskbar button: %w", hwnd, err))
			}
		}
	}
	g.stopErr = errors.Join(g.stopErr, errors.Join(errs...))
}

func (g *startupVisibility) close() error {
	g.stopOnce.Do(func() {
		g.invoke(func() {}, true)
		<-g.done // includes restoration, unhooking and thread shutdown
	})
	return g.stopErr
}

func (s *Service) withStartupVisibility(path string) (*Service, error) {
	visibility, err := beginStartupVisibility(path, s.logger)
	if err != nil {
		s.logger.Error(err.Error())
		return nil, err
	}
	scoped := *s
	scoped.visibility = visibility
	scoped.enumerator = startupWindowEnumerator{base: s.enumerator, visibility: visibility}
	return &scoped, nil
}

func (s *Service) finishStartupVisibility(result *Result) {
	if err := s.visibility.close(); err != nil {
		s.logger.Error(fmt.Sprintf("startup visual shield restoration failed: %v", err))
		result.Managed = false
		result.Code = ResultNoWindowManaged
		result.Message = err.Error()
	}
}

type startupWindowEnumerator struct {
	base       WindowEnumerator
	visibility *startupVisibility
}

func (e startupWindowEnumerator) EnumerateTopLevelWindows() []ManagedWindowInfo {
	windows := e.base.EnumerateTopLevelWindows()
	shielded := e.visibility.candidates()
	byHandle := make(map[uintptr]int, len(windows))
	for i, w := range windows {
		byHandle[w.Handle] = i
	}
	for _, w := range shielded {
		if i, ok := byHandle[w.Handle]; ok {
			windows[i] = w
		} else {
			windows = append(windows, w)
		}
	}
	return windows
}
