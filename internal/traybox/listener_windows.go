//go:build windows

package traybox

import (
	"cmp"
	"errors"
	"fmt"
	"image"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	user32                          = windows.NewLazySystemDLL("user32.dll")
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procSendMessageTimeoutW         = user32.NewProc("SendMessageTimeoutW")
	procSendNotifyMessageW          = user32.NewProc("SendNotifyMessageW")
	procFindWindowExW               = user32.NewProc("FindWindowExW")
	procGetShellWindow              = user32.NewProc("GetShellWindow")
	procInSendMessage               = user32.NewProc("InSendMessage")
	procChangeWindowMessageFilterEx = user32.NewProc("ChangeWindowMessageFilterEx")
	procAllowSetForegroundWindow    = user32.NewProc("AllowSetForegroundWindow")
	procIsWindow                    = user32.NewProc("IsWindow")
	procShellNotifyIconGetRect      = shell32.NewProc("Shell_NotifyIconGetRect")
)

func isWindow(hwnd win.HWND) bool {
	r, _, _ := procIsWindow.Call(uintptr(hwnd))
	return r != 0
}

const (
	trayWindowClass = "Shell_TrayWnd"

	timerRaise   = 1
	timerHeal    = 2
	timerRefresh = 3

	raiseInterval = 100 * time.Millisecond
	healInterval  = time.Second
	// Programs re-add their icons when Explorer restarts. Some reach the new
	// taskbar before this window is on top again, so they are asked once more.
	refreshDelay = 2 * time.Second

	forwardTimeout = 4000 // milliseconds

	// msgSync asks the listener thread to apply a changed program selection.
	msgSync = win.WM_APP + 1
	msgStop = win.WM_APP + 2
	// Explorer posts rather than sends this message; sending it can deadlock.
	msgPostOnly = win.WM_USER + 372

	smtoAbortIfHung = 0x0002
	msgfltAllow     = 1
)

// Logger is the subset of the application logger the box needs.
type Logger interface {
	Info(msg string)
	Warn(msg string)
}

// trackedIcon is what the listener knows about an icon of a selected program.
type trackedIcon struct {
	Icon
	// hidden is the state the program itself asked for; such an icon is not
	// listed and stays hidden when it is handed back to Explorer.
	hidden bool
	// A rejected collection is rolled back to the original native ADD.
	// Do not advertise it as collected or repeatedly hide its later updates.
	collectionFailed bool
	nativeRestored   bool
	// template is the program's latest request, reused for state changes.
	template       []byte
	seq            uint64
	versionSeq     uint64
	versionApplied uint64 // latest successful SETVERSION, not merely latest request
	revision       uint64 // protects a newer update from a pending restore
}

type listenerConfig struct {
	className string
	// Only explicitly registered WinTray owner windows use a hosted path;
	// WinTray's own icon must continue to be excluded from collection.
	hostedOwners *sync.Map
	// target returns the window requests are passed on to.
	target func(self win.HWND) win.HWND
	// raise keeps the listener above Explorer's taskbar.
	raise bool
}

// listener owns the Shell_TrayWnd window. Its window procedure and timers run
// on one locked OS thread; other goroutines only read snapshots or post.
type listener struct {
	cfg            listenerConfig
	selfPath       string
	logger         Logger
	taskbarCreated uint32

	mu      sync.Mutex
	paths   map[string]bool
	pending map[string]bool
	icons   map[iconID]*trackedIcon
	seq     uint64

	hwnd       win.HWND
	closing    bool
	done       chan struct{}
	stopResult chan error

	// Pointer state, like window ordering, belongs to the listener thread.
	pointerHook     uintptr
	pointerYielded  bool
	pointerResumeAt time.Time
}

// registeredClasses keeps window classes registered for the life of the
// process, so the box can stop and start again.
var (
	classMu           sync.Mutex
	registeredClasses = map[string]*uint16{}
)

func registerListenerClass(name string, instance win.HINSTANCE) (*uint16, error) {
	classMu.Lock()
	defer classMu.Unlock()
	if class, ok := registeredClasses[name]; ok {
		return class, nil
	}
	class, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	wc := win.WNDCLASSEX{LpfnWndProc: listenerProc, HInstance: instance, LpszClassName: class}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if win.RegisterClassEx(&wc) == 0 {
		return nil, fmt.Errorf("register the %s window class failed", name)
	}
	registeredClasses[name] = class
	return class, nil
}

// Only one listener exists per process; the window procedure finds it here.
var (
	activeListener atomic.Pointer[listener]
	listenerProc   = syscall.NewCallback(func(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
		if l := activeListener.Load(); l != nil && l.hwnd == hwnd {
			return l.wndProc(msg, wParam, lParam)
		}
		return win.DefWindowProc(hwnd, msg, wParam, lParam)
	})
)

var errListenerRunning = errors.New("the tray icon listener is already running")

func startListener(cfg listenerConfig, selfPath string, paths []string, logger Logger) (*listener, error) {
	l := &listener{
		cfg:            cfg,
		selfPath:       selfPath,
		logger:         logger,
		taskbarCreated: win.RegisterWindowMessage(syscall.StringToUTF16Ptr("TaskbarCreated")),
		paths:          map[string]bool{},
		pending:        map[string]bool{},
		icons:          map[iconID]*trackedIcon{},
		done:           make(chan struct{}),
		stopResult:     make(chan error, 1),
	}
	l.setPaths(paths)
	ready := make(chan error, 1)
	go l.run(ready)
	if err := <-ready; err != nil {
		<-l.done
		return nil, err
	}
	return l, nil
}

func (l *listener) run(ready chan<- error) {
	runtime.LockOSThread()
	defer close(l.done)
	if !activeListener.CompareAndSwap(nil, l) {
		ready <- errListenerRunning
		return
	}
	defer activeListener.Store(nil)

	instance := win.GetModuleHandle(nil)
	className, err := registerListenerClass(l.cfg.className, instance)
	if err != nil {
		ready <- err
		return
	}
	// The window is never shown. WS_EX_TOOLWINDOW keeps it off the taskbar.
	hwnd := win.CreateWindowEx(win.WS_EX_TOOLWINDOW|win.WS_EX_TOPMOST, className, nil, win.WS_POPUP,
		0, 0, 0, 0, 0, 0, instance, nil)
	if hwnd == 0 {
		ready <- fmt.Errorf("create the %s window failed", l.cfg.className)
		return
	}
	l.mu.Lock()
	l.hwnd = hwnd
	l.mu.Unlock()
	if l.cfg.raise {
		if err := l.startPointerHook(); err != nil {
			win.DestroyWindow(hwnd)
			ready <- err
			return
		}
		defer l.stopPointerHook()
	}
	// Programs running elevated may still reach WinTray when it runs elevated.
	procChangeWindowMessageFilterEx.Call(uintptr(hwnd), win.WM_COPYDATA, msgfltAllow, 0)
	procChangeWindowMessageFilterEx.Call(uintptr(hwnd), uintptr(l.taskbarCreated), msgfltAllow, 0)
	l.keepOnTop()
	if l.cfg.raise {
		win.SetTimer(hwnd, timerRaise, uint32(raiseInterval.Milliseconds()), 0)
	}
	win.SetTimer(hwnd, timerHeal, uint32(healInterval.Milliseconds()), 0)
	ready <- nil
	// Icons that already exist are registered again, now through this window.
	l.sync()

	// GetMessage can dispatch sent messages before writing its result. Those
	// callbacks reenter Go and can move its stack. Pin the native output
	// buffer or a queued click/stop can be written to the abandoned stack.
	var m win.MSG
	var messages runtime.Pinner
	messages.Pin(&m)
	defer messages.Unpin()
	for win.GetMessage(&m, 0, 0, 0) > 0 {
		win.TranslateMessage(&m)
		win.DispatchMessage(&m)
	}
}

// setPaths replaces the selection. Safe from any goroutine; the listener
// thread hides and restores icons afterwards.
func (l *listener) setPaths(paths []string) {
	next := make(map[string]bool, len(paths))
	self := canonicalIconPath(l.selfPath)
	for _, p := range paths {
		if p = canonicalIconPath(p); p != "" && p != self {
			next[p] = true
		}
	}
	l.mu.Lock()
	for p := range next {
		if !l.paths[p] {
			l.pending[p] = true
		}
	}
	l.paths = next
	hwnd := l.hwnd
	l.mu.Unlock()
	if hwnd != 0 {
		win.PostMessage(hwnd, msgSync, 0, 0)
	}
}

// stop hands every hidden icon back to Explorer and ends the thread. It
// keeps the listener alive on failure. Each forwarded request has its own
// timeout; never abandon a shutdown that could still destroy the listener.
func (l *listener) stop() error {
	select {
	case <-l.done:
		return nil
	default:
	}
	if win.PostMessage(l.hwnd, msgStop, 0, 0) == 0 {
		return errors.New("could not request tray icon restoration")
	}
	select {
	case err := <-l.stopResult:
		if err == nil {
			<-l.done
		}
		return err
	case <-l.done:
		return nil
	}
}

// snapshot lists the icons of selected programs that are currently shown, in
// the order they first appeared.
func (l *listener) snapshot() []Icon {
	type entry struct {
		icon Icon
		seq  uint64
	}
	l.mu.Lock()
	entries := make([]entry, 0, len(l.icons))
	for _, t := range l.icons {
		if !t.hidden && !t.collectionFailed && l.paths[strings.ToLower(t.ExePath)] {
			entries = append(entries, entry{t.Icon, t.seq})
		}
	}
	l.mu.Unlock()
	slices.SortFunc(entries, func(a, b entry) int { return cmp.Compare(a.seq, b.seq) })
	out := make([]Icon, 0, len(entries))
	for _, e := range entries {
		if isWindow(win.HWND(uintptr(e.icon.owner))) {
			out = append(out, e.icon)
		}
	}
	return out
}

func (l *listener) wndProc(msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_COPYDATA:
		return l.onCopyData(wParam, lParam)
	case win.WM_TIMER:
		l.onTimer(wParam)
		return 0
	case msgSync:
		l.sync()
		return 0
	case l.taskbarCreated:
		// Explorer restarted: take the top position again before programs
		// re-add their icons, and ask the selected ones again shortly after.
		l.keepOnTop()
		win.SetTimer(l.hwnd, timerRefresh, uint32(refreshDelay.Milliseconds()), 0)
		return 0
	case msgStop:
		l.stopResult <- l.shutdown()
		return 0
	case win.WM_CLOSE:
		if err := l.shutdown(); err != nil {
			l.logger.Warn(fmt.Sprintf("tray box: %v", err))
		}
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	if msg == win.WM_COMMAND || msg >= win.WM_USER {
		return l.forward(msg, wParam, lParam)
	}
	return win.DefWindowProc(l.hwnd, msg, wParam, lParam)
}

// foreignPointer turns an address the system passed in a message into a
// pointer. The memory is not Go's and stays valid while the message is handled.
func foreignPointer(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

type copyDataStruct struct {
	DwData uintptr
	CbData uint32
	LpData uintptr
}

func (l *listener) onCopyData(wParam, lParam uintptr) uintptr {
	cds := (*copyDataStruct)(foreignPointer(lParam))
	if cds != nil && cds.DwData == copyDataIconRect && cds.LpData != 0 {
		return l.onIconRect(wParam, lParam, unsafe.Slice((*byte)(foreignPointer(cds.LpData)), cds.CbData))
	}
	if cds == nil || cds.DwData != copyDataNotifyIcon || cds.LpData == 0 {
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	data := unsafe.Slice((*byte)(foreignPointer(cds.LpData)), cds.CbData)
	d, ok := parseTrayData(data)
	if !ok {
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	path := l.iconPath(d)
	selected := l.selected(path)
	l.mu.Lock()
	previous := l.icons[idOf(d)]
	tracked := previous != nil
	collect := selected && !l.closing && (previous == nil || !previous.collectionFailed)
	l.mu.Unlock()
	// Keep observing selected programs during shutdown, but do not hide
	// their new icons. If shutdown fails, those icons must remain tracked.
	if !selected && !tracked {
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	if d.Message == nimSetVersion {
		id := idOf(d)
		l.mu.Lock()
		t := l.icons[id]
		var versionSeq uint64
		if t != nil {
			t.versionSeq++
			versionSeq = t.versionSeq
		}
		l.mu.Unlock()
		result := l.forward(win.WM_COPYDATA, wParam, lParam)
		if result != 0 && t != nil {
			l.mu.Lock()
			if l.icons[id] == t && versionSeq > t.versionApplied {
				t.version, t.versionApplied = d.Version, versionSeq
			}
			l.mu.Unlock()
		}
		return result
	}
	if d.Message != nimAdd && d.Message != nimModify && d.Message != nimDelete {
		// NIM_SETFOCUS and unknown control messages do not set icon fields.
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	// Read the icon while the program waits for this call: it may destroy
	// the handle as soon as the call returns.
	var img *image.NRGBA
	if d.Flags&nifIcon != 0 && d.Icon != 0 && d.Message != nimDelete {
		var err error
		if img, err = iconImage(win.HICON(uintptr(d.Icon))); err != nil {
			l.logger.Warn(fmt.Sprintf("tray box: read icon of %s: %v", path, err))
		}
	}
	// SendMessageTimeout may dispatch another request while forwarding.
	// Record in arrival order, before that reentry can update or delete it.
	isNew, revision := l.record(path, d, data, img)
	out := data
	if collect {
		out = withHidden(data)
	}
	result, delivered := l.sendRequestResult(wParam, out)
	// An icon first seen through NIM_MODIFY is still shown by Explorer, and a
	// repeated NIM_ADD of an icon Explorer already has fails without changing
	// its state; both are hidden explicitly.
	needHide := isNew
	if d.Message == nimAdd {
		needHide = result == 0
	}
	if collect && d.Message != nimDelete && needHide {
		hiddenResult, hiddenDelivered := l.hideCurrent(idOf(d), uintptr(d.HWnd))
		if isNew && delivered && result == 0 && hiddenDelivered && hiddenResult == 0 {
			return l.rollbackRejectedCollection(wParam, path, d, data, revision, result)
		}
	}
	return result
}

// rollbackRejectedCollection undoes a rejected rewrite, not a successful
// collection. Only a definitively rejected ADD is safe to retry unmodified.
// Timeouts and newer/reentrant requests never authorize another registration.
func (l *listener) rollbackRejectedCollection(wParam uintptr, path string, d trayData, raw []byte, revision uint64, result uintptr) uintptr {
	id := idOf(d)
	l.mu.Lock()
	t := l.icons[id]
	if t == nil || t.revision != revision {
		l.mu.Unlock()
		return result
	}
	t.collectionFailed = true
	l.mu.Unlock()
	l.logger.Warn(fmt.Sprintf("tray box: collection rejected for %s (message=%d flags=0x%X bytes=%d cbSize=%d); restoring original native registration", path, d.Message, d.Flags, len(raw), le(raw, 8)))
	if d.Message != nimAdd {
		l.mu.Lock()
		if l.icons[id] == t && t.revision == revision {
			delete(l.icons, id)
		}
		l.mu.Unlock()
		return result
	}
	originalResult, delivered := l.sendRequestResult(wParam, raw)
	l.mu.Lock()
	if l.icons[id] == t && t.revision == revision {
		if delivered && originalResult != 0 {
			t.nativeRestored = true
		} else if delivered {
			delete(l.icons, id) // all three requests explicitly rejected
		}
	}
	l.mu.Unlock()
	if delivered && originalResult != 0 {
		l.logger.Warn(fmt.Sprintf("tray box: native icon restored for %s; collection failed and remains disabled for this icon until reselected", path))
	} else {
		l.logger.Warn(fmt.Sprintf("tray box: original native registration was not confirmed for %s (delivered=%t result=%d)", path, delivered, originalResult))
	}
	return originalResult
}

// onIconRect answers Shell_NotifyIconGetRect for collected icons. Explorer
// cannot place a hidden icon, and some programs (Rust tray-icon, for one)
// ignore a click on their icon when its position is unknown. Such an icon is
// placed under the cursor, where WinTray's menu delivered the click. The
// listener's own probes run on its thread and always reach Explorer.
func (l *listener) onIconRect(wParam, lParam uintptr, data []byte) uintptr {
	q, ok := parseRectQuery(data)
	// Queries originating from our own listener thread are visibility probes
	// for healing, not application requests for the collected icon's anchor.
	if !ok || !inSendMessage() {
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	l.mu.Lock()
	t := l.icons[q.ID]
	collected := t != nil && !t.collectionFailed && !l.closing && l.paths[strings.ToLower(t.ExePath)]
	l.mu.Unlock()
	if !collected {
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	var cursor win.POINT
	if !win.GetCursorPos(&cursor) {
		return l.forward(win.WM_COPYDATA, wParam, lParam)
	}
	// Do not ask Explorer first: hidden icons may have stale positions, and
	// a hung shell must not block the application's click callback.
	half := max(int32(1), win.GetSystemMetrics(win.SM_CXSMICON)/2)
	left, top, right, bottom := cursor.X-half, cursor.Y-half, cursor.X+half, cursor.Y+half
	// Shell32 uses a zero packed corner as 'not found'. Keep both corners
	// nonzero even when the cursor straddles the desktop origin.
	if left == 0 && top == 0 {
		top--
	}
	if right == 0 && bottom == 0 {
		bottom++
	}
	return rectAnswer(q.Corner, left, top, right, bottom)
}

// hideCurrent rechecks selection and identity after a reentrant shell call.
func (l *listener) hideCurrent(id iconID, wParam uintptr) (uintptr, bool) {
	l.mu.Lock()
	var request []byte
	if t := l.icons[id]; t != nil && !t.collectionFailed && !l.closing && l.paths[strings.ToLower(t.ExePath)] {
		request = stateRequest(t.template, true)
	}
	l.mu.Unlock()
	return l.sendRequestResult(wParam, request)
}

// iconPath resolves GUID-only updates through the original registration;
// they need not repeat the owning HWND or uID.
func (l *listener) iconPath(d trayData) string {
	if d.usesGUID() && d.Message != nimAdd {
		l.mu.Lock()
		t := l.icons[idOf(d)]
		if t != nil {
			path := t.ExePath
			l.mu.Unlock()
			return path
		}
		l.mu.Unlock()
	}
	if l.cfg.hostedOwners != nil {
		if owner, ok := l.cfg.hostedOwners.Load(d.HWnd); ok {
			return owner.(hostedIconOwner).path
		}
	}
	return windowProcessPath(d.HWnd)
}

func (l *listener) selected(path string) bool {
	if path == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.paths[path]
}

// record merges a request into the icon's state and reports whether the
// icon was not tracked before, plus this update's revision. img is the icon
// the request sets, if any.
func (l *listener) record(path string, d trayData, raw []byte, img *image.NRGBA) (bool, uint64) {
	id := idOf(d)
	l.mu.Lock()
	defer l.mu.Unlock()
	if d.Message == nimDelete {
		delete(l.icons, id)
		return false, 0
	}
	if d.Message != nimAdd && d.Message != nimModify {
		return false, 0
	}
	l.seq++
	t, known := l.icons[id]
	if !known {
		t = &trackedIcon{seq: l.seq}
		t.key = id
		t.ExePath = path
		l.icons[id] = t
	}
	t.revision = l.seq
	if d.HWnd != 0 {
		t.owner, t.uid = d.HWnd, d.UID
	}
	t.template = append(t.template[:0], raw...)
	if d.Flags&nifMessage != 0 {
		t.callback = d.Callback
	}
	if d.Flags&nifTip != 0 {
		t.Tooltip = d.Tip
	}
	if d.Flags&nifIcon != 0 {
		t.Image = img
	}
	if d.Flags&nifState != 0 && d.StateMask&nisHidden != 0 {
		t.hidden = d.State&nisHidden != 0
	}
	return !known, t.revision
}

func (l *listener) onTimer(id uintptr) {
	switch id {
	case timerRaise:
		l.keepOnTop()
	case timerHeal:
		l.heal()
	case timerRefresh:
		win.KillTimer(l.hwnd, timerRefresh)
		l.mu.Lock()
		for p := range l.paths {
			l.pending[p] = true
		}
		l.mu.Unlock()
		l.sync()
	}
}

// keepOnTop makes this the Shell_TrayWnd programs find first, except during
// native taskbar gestures. Its bounds preserve callers' relative coordinates.
func (l *listener) keepOnTop() {
	if !l.cfg.raise {
		return
	}
	if l.pointerYielded && l.pointerBusy(time.Now(), pointerButtonDown()) {
		return
	}
	class, _ := syscall.UTF16PtrFromString(l.cfg.className)
	if win.FindWindow(class, nil) != l.hwnd {
		win.SetWindowPos(l.hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	}
	var want, have win.RECT
	if tray := l.cfg.target(l.hwnd); tray != 0 && win.GetWindowRect(tray, &want) && win.GetWindowRect(l.hwnd, &have) && want != have {
		win.SetWindowPos(l.hwnd, 0, want.Left, want.Top, want.Right-want.Left, want.Bottom-want.Top, win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	}
}

// heal hides icons that reached Explorer directly, for example while another
// window was briefly on top, and drops icons whose window is gone.
func (l *listener) heal() {
	if err := l.restoreIcons(false); err != nil {
		l.logger.Warn(fmt.Sprintf("tray box: %v", err))
	}
	type check struct {
		id       iconID
		template []byte
	}
	var checks []check
	l.mu.Lock()
	for id, t := range l.icons {
		if !isWindow(win.HWND(uintptr(t.owner))) {
			delete(l.icons, id)
			continue
		}
		if !t.hidden && !t.collectionFailed && !l.closing && l.paths[strings.ToLower(t.ExePath)] {
			checks = append(checks, check{id, stateRequest(t.template, true)})
		}
	}
	l.mu.Unlock()
	for _, c := range checks {
		if shownByExplorer(c.template) {
			l.logger.Info(fmt.Sprintf("tray box: icon hwnd=0x%X uid=%d appeared on the taskbar; hiding it again", c.id.hwnd, c.id.uid))
			l.hideCurrent(c.id, 0)
		}
	}
}

// sync hands icons of deselected programs back to Explorer and asks newly
// selected programs to register their icons again.
func (l *listener) sync() {
	if err := l.restoreIcons(false); err != nil {
		l.logger.Warn(fmt.Sprintf("tray box: %v", err))
	}
	l.mu.Lock()
	pending := make([]string, 0, len(l.pending))
	for p := range l.pending {
		if l.paths[p] {
			pending = append(pending, p)
		}
	}
	clear(l.pending)
	l.mu.Unlock()
	for _, p := range pending {
		n := postTaskbarCreated(p, l.taskbarCreated)
		// These icons live in WinTray, so the hosted executable's process
		// enumeration cannot find them when collection is enabled later.
		if l.cfg.hostedOwners != nil {
			l.cfg.hostedOwners.Range(func(_, value any) bool {
				owner := value.(hostedIconOwner)
				if owner.path == p && owner.refresh != nil {
					owner.refresh()
					n++
				}
				return true
			})
		}
		l.logger.Info(fmt.Sprintf("tray box: asked %d windows of %s to register their icons", n, p))
	}
}

// restoreIcons keeps failed restores for the next heal/stop attempt. During
// shutdown all records survive until every restore succeeds, so a failed stop
// can resume collecting the previous selection.
func (l *listener) restoreIcons(all bool) error {
	l.mu.Lock()
	ids := make([]iconID, 0, len(l.icons))
	for id := range l.icons {
		ids = append(ids, id)
	}
	l.mu.Unlock()
	var errs []error
	for _, id := range ids {
		l.mu.Lock()
		t := l.icons[id]
		if t == nil || !all && l.paths[strings.ToLower(t.ExePath)] {
			l.mu.Unlock()
			continue
		}
		if t.hidden || t.nativeRestored || !isWindow(win.HWND(uintptr(t.owner))) {
			if !all {
				delete(l.icons, id)
			}
			l.mu.Unlock()
			continue
		}
		request, revision, path := stateRequest(t.template, false), t.revision, t.ExePath
		l.mu.Unlock()
		if l.sendRequest(0, request) == 0 {
			errs = append(errs, fmt.Errorf("could not restore tray icon of %s", path))
			continue
		}
		l.mu.Lock()
		if !all && l.icons[id] == t && t.revision == revision && !l.paths[strings.ToLower(t.ExePath)] {
			delete(l.icons, id)
		}
		l.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (l *listener) shutdown() error {
	l.mu.Lock()
	l.closing = true // reentrant requests must pass through without being hidden
	l.mu.Unlock()
	if err := l.restoreIcons(true); err != nil {
		l.mu.Lock()
		l.closing = false
		l.mu.Unlock()
		return err
	}
	if !win.DestroyWindow(l.hwnd) {
		l.mu.Lock()
		l.closing = false
		l.mu.Unlock()
		return errors.New("could not stop the tray icon listener")
	}
	// DestroyWindow also removes its timers.
	l.mu.Lock()
	clear(l.icons)
	clear(l.paths)
	l.mu.Unlock()
	return nil
}

// forward passes a message on to Explorer. Posted messages stay posted.
func (l *listener) forward(msg uint32, wParam, lParam uintptr) uintptr {
	result, _ := l.forwardResult(msg, wParam, lParam)
	return result
}

// forwardResult distinguishes an explicit shell rejection from a timeout or
// unavailable taskbar, where the eventual state of the icon is unknown.
func (l *listener) forwardResult(msg uint32, wParam, lParam uintptr) (uintptr, bool) {
	tray := l.cfg.target(l.hwnd)
	if tray == 0 {
		return 0, false
	}
	if msg != win.WM_COPYDATA && (msg == msgPostOnly || !inSendMessage()) {
		return 0, win.PostMessage(tray, msg, wParam, lParam) != 0
	}
	var result uintptr
	var output runtime.Pinner
	output.Pin(&result)
	defer output.Unpin()
	if r, _, _ := procSendMessageTimeoutW.Call(uintptr(tray), uintptr(msg), wParam, lParam,
		smtoAbortIfHung, forwardTimeout, uintptr(unsafe.Pointer(&result))); r == 0 {
		return 0, false
	}
	return result, true
}

// sendRequest delivers a Shell_NotifyIcon request straight to Explorer.
func (l *listener) sendRequest(wParam uintptr, data []byte) uintptr {
	result, _ := l.sendRequestResult(wParam, data)
	return result
}

func (l *listener) sendRequestResult(wParam uintptr, data []byte) (uintptr, bool) {
	if len(data) == 0 {
		return 0, false
	}
	cds := copyDataStruct{DwData: copyDataNotifyIcon, CbData: uint32(len(data)), LpData: uintptr(unsafe.Pointer(&data[0]))}
	var request runtime.Pinner
	request.Pin(&cds)
	request.Pin(&data[0])
	defer request.Unpin()
	result, delivered := l.forwardResult(win.WM_COPYDATA, wParam, uintptr(unsafe.Pointer(&cds)))
	runtime.KeepAlive(data)
	return result, delivered
}

func inSendMessage() bool {
	r, _, _ := procInSendMessage.Call()
	return r != 0
}

// explorerTray finds Explorer's own taskbar window, skipping this and any
// other program's Shell_TrayWnd so that forwarding never loops.
func explorerTray(self win.HWND) win.HWND {
	shell, _, _ := procGetShellWindow.Call()
	if shell == 0 {
		return 0
	}
	var shellPID uint32
	win.GetWindowThreadProcessId(win.HWND(shell), &shellPID)
	class, _ := syscall.UTF16PtrFromString(trayWindowClass)
	var after uintptr
	for {
		h, _, _ := procFindWindowExW.Call(0, after, uintptr(unsafe.Pointer(class)), 0)
		if h == 0 {
			return 0
		}
		after = h
		if win.HWND(h) == self {
			continue
		}
		var pid uint32
		win.GetWindowThreadProcessId(win.HWND(h), &pid)
		if pid == shellPID {
			return win.HWND(h)
		}
	}
}

type notifyIconIdentifier struct {
	CbSize   uint32
	HWnd     win.HWND
	UID      uint32
	GUIDItem windows.GUID
}

// shownByExplorer reports whether Explorer currently shows the icon a
// request describes, on the taskbar or in the hidden-icons flyout.
func shownByExplorer(template []byte) bool {
	d, ok := parseTrayData(template)
	if !ok {
		return false
	}
	id := notifyIconIdentifier{HWnd: win.HWND(uintptr(d.HWnd)), UID: d.UID}
	if d.usesGUID() {
		id.GUIDItem = *(*windows.GUID)(unsafe.Pointer(&d.GUID))
	}
	id.CbSize = uint32(unsafe.Sizeof(id))
	var r win.RECT
	hr, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&r)))
	return hr == 0 && r.Right > r.Left
}

// windowProcessPath returns the lower-case image path of a window's process.
func windowProcessPath(hwnd uint32) string {
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	win.GetWindowThreadProcessId(win.HWND(uintptr(hwnd)), &pid)
	if pid == 0 {
		return ""
	}
	return processPath(pid)
}

// postTaskbarCreated tells every window of the program's processes that the
// taskbar was created, which makes programs register their icons again.
func postTaskbarCreated(path string, msg uint32) int {
	pids := processesByPath(map[string]bool{path: true})
	n := 0
	for _, hwnd := range allWindows() {
		if l := activeListener.Load(); l != nil && hwnd == l.hwnd {
			continue // never ask our listener to re-register itself
		}
		var pid uint32
		win.GetWindowThreadProcessId(hwnd, &pid)
		if _, ok := pids[pid]; ok && win.PostMessage(hwnd, msg, 0, 0) != 0 {
			n++
		}
	}
	return n
}
