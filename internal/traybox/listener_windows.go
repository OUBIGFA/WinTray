//go:build windows

package traybox

import (
	"cmp"
	"encoding/binary"
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
	procPostIconMessageW            = user32.NewProc("PostMessageW")
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

	timerHeal    = 2
	timerRefresh = 3

	healInterval = time.Second
	// Selected applications are refreshed after Explorer has recreated its
	// notification area; no taskbar window or z-order is impersonated.
	refreshDelay = 2 * time.Second

	forwardTimeout = 4000 // milliseconds

	// msgSync asks the listener thread to apply a changed program selection.
	msgSync         = win.WM_APP + 1
	msgStop         = win.WM_APP + 2
	msgUpdatePaths  = win.WM_APP + 3
	msgBridgeFailed = win.WM_APP + 4
	msgBridgeResume = win.WM_APP + 5
	msgBridgeEpoch  = win.WM_APP + 6
	msgClose        = win.WM_APP + 7 // terminal shutdown; never roll collection back
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
	template        []byte
	seq             uint64
	versionSeq      uint64
	versionApplied  uint64 // latest successful SETVERSION, not merely latest request
	revision        uint64 // protects a newer update from a pending restore
	recoveryPID     uint32
	recoveryCreated uint64
}

type listenerConfig struct {
	className string
	// Only explicitly registered WinTray owner windows use a hosted path;
	// WinTray's own icon must continue to be excluded from collection.
	hostedOwners *sync.Map
	// target returns the window requests are passed on to.
	target func(self win.HWND) win.HWND
	// native receives Shell tray packets through the private broker.
	// Protocol tests send packets directly to an isolated fake shell.
	native      bool
	newRecovery func() (*recoveryClient, error)
	onError     func(error)
}

type selectionChange struct {
	paths []string
	done  chan error
}

// listener owns a private, non-topmost broker window. Its window procedure and
// timers run on one locked OS thread; other goroutines only read or post.
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

	hwnd           win.HWND
	closing        bool
	done           chan struct{}
	stopResult     chan error
	recovery       *recoveryClient
	recoveryFailed bool // listener-thread only
	bridgeFailed   bool // retry restoration, never resume hiding after a lost route

	packetToken    uint64 // listener-thread only; saved across synchronous reentry
	resumePending  uint64
	resumedToken   uint64
	resumeError    uintptr
	observedToken  uint32
	observedOffset uint32

	bridgeRefresh chan struct{}
	changes       chan selectionChange
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
	// A global Shell_TrayWnd changes what unrelated taskbar clients find.
	// Refuse it even in tests so the old interception path cannot return.
	if strings.EqualFold(cfg.className, trayWindowClass) {
		return nil, errors.New("the tray broker must not impersonate the Windows taskbar")
	}
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
		bridgeRefresh:  make(chan struct{}, 1),
		changes:        make(chan selectionChange, 1),
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
	if l.cfg.newRecovery != nil {
		var err error
		l.recovery, err = l.cfg.newRecovery()
		if err != nil {
			ready <- err
			return
		}
		// On an unexpected message-loop exit, closing the pipe also restores.
		defer l.recovery.close()
	}

	instance := win.GetModuleHandle(nil)
	className, err := registerListenerClass(l.cfg.className, instance)
	if err != nil {
		ready <- err
		return
	}
	// The window is never shown. WS_EX_TOOLWINDOW keeps it off the taskbar.
	hwnd := win.CreateWindowEx(win.WS_EX_TOOLWINDOW, className, nil, win.WS_POPUP,
		0, 0, 0, 0, 0, 0, instance, nil)
	if hwnd == 0 {
		ready <- fmt.Errorf("create the %s window failed", l.cfg.className)
		return
	}
	l.mu.Lock()
	l.hwnd = hwnd
	l.mu.Unlock()
	// Permit native packets from same-user applications at different integrity
	// levels; the bridge itself still obeys process access and injection policy.
	procChangeWindowMessageFilterEx.Call(uintptr(hwnd), win.WM_COPYDATA, msgfltAllow, 0)
	procChangeWindowMessageFilterEx.Call(uintptr(hwnd), uintptr(l.taskbarCreated), msgfltAllow, 0)
	win.SetTimer(hwnd, timerHeal, uint32(healInterval.Milliseconds()), 0)
	ready <- nil
	// Icons that already exist are registered again through the broker.
	if err := l.sync(); err != nil {
		l.reportError(err)
	}

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

// setPaths replaces the selection and queues maintenance. Safe from any
// goroutine; native visibility changes are applied on the listener thread.
func (l *listener) setPaths(paths []string) {
	if hwnd := l.replacePaths(paths); hwnd != 0 {
		win.PostMessage(hwnd, msgSync, 0, 0)
	}
}

// replacePaths does not post work, so a selection transaction can finish
// restoring removals before publishing its additions or a refresh request.
func (l *listener) replacePaths(paths []string) win.HWND {
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
	return hwnd
}

func (l *listener) updatePaths(paths []string) error {
	change := selectionChange{paths: slices.Clone(paths), done: make(chan error, 1)}
	l.changes <- change
	if win.PostMessage(l.hwnd, msgUpdatePaths, 0, 0) == 0 {
		<-l.changes
		return errors.New("could not update tray collection selection")
	}
	select {
	case err := <-change.done:
		return err
	case <-l.done:
		return errors.New("tray broker stopped while updating selection")
	}
}

func (l *listener) applyPathChange(change selectionChange) {
	l.mu.Lock()
	if l.closing {
		l.mu.Unlock()
		change.done <- errors.New("tray collection is unavailable until its recovery component is restarted")
		return
	}
	before := make([]string, 0, len(l.paths))
	for path := range l.paths {
		before = append(before, path)
	}
	l.mu.Unlock()
	var retained []string
	for _, path := range change.paths {
		if path = canonicalIconPath(path); Contains(before, path) {
			retained = append(retained, path)
		}
	}
	// Withhold additions until all removals and their recovery snapshot are
	// confirmed. Shell forwarding can reenter here with a new native ADD;
	// hiding it before commit would leave an orphan after a failed rollback.
	l.replacePaths(retained)
	err := l.restoreIcons(false)
	if err == nil {
		err = l.syncRecovery()
		if err != nil {
			l.failRecovery(err)
		}
	}
	if err != nil {
		l.setPaths(before) // refresh already restored members of the old set
	} else {
		l.setPaths(change.paths)
	}
	change.done <- err
}

func (l *listener) reportError(err error) {
	l.logger.Warn(fmt.Sprintf("tray box: %v", err))
	if l.cfg.onError != nil {
		l.cfg.onError(err)
	}
}

// stop hands every hidden icon back to Explorer and ends the thread. It
// keeps the listener alive on failure. Each forwarded request has its own
// timeout; never abandon a shutdown that could still destroy the listener.
func (l *listener) stop() error { return l.stopWithMessage(msgStop) }

// close is terminal. Unlike deselection, a rejected restoration must release
// the broker and let the acknowledged recovery snapshot finish out of process.
func (l *listener) close() error { return l.stopWithMessage(msgClose) }

func (l *listener) stopWithMessage(message uint32) error {
	select {
	case <-l.done:
		return nil
	default:
	}
	if win.PostMessage(l.hwnd, message, 0, 0) == 0 {
		return errors.New("could not request tray icon restoration")
	}
	select {
	case err := <-l.stopResult:
		if err == nil || message == msgClose {
			<-l.done
		}
		return err
	case <-l.done:
		// A terminal close can fail restoration and still end the thread.
		// Do not lose its diagnostic when both channels are ready.
		select {
		case err := <-l.stopResult:
			return err
		default:
			return nil
		}
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
		if !l.closing && !t.hidden && !t.collectionFailed && l.paths[strings.ToLower(t.ExePath)] {
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
		if err := l.sync(); err != nil {
			l.reportError(err)
		}
		return 0
	case msgUpdatePaths:
		l.applyPathChange(<-l.changes)
		return 0
	case l.taskbarCreated:
		// Delay only re-registration, never reposition the native taskbar.
		win.SetTimer(l.hwnd, timerRefresh, uint32(refreshDelay.Milliseconds()), 0)
		return 0
	case msgBridgeEpoch:
		l.resumePending, l.resumedToken = 0, 0
		l.observedToken, l.observedOffset = 0, 0
		return 0
	case msgBridgeResume:
		if l.resumedToken != 0 && int32(uint32(wParam)-uint32(l.resumedToken)) <= 0 {
			return 0
		}
		l.resumePending, l.resumeError = uint64(wParam), lParam
		if l.packetToken == 0 {
			l.resumeBridge()
		}
		return 0
	case msgBridgeFailed:
		l.bridgeFailed = true
		// Execute restoration on this thread after any in-flight packet,
		// so a lost receiving lease cannot keep stale hidden icons alive.
		l.mu.Lock()
		l.closing = true
		l.mu.Unlock()
		if err := l.restoreIcons(true); err != nil {
			l.reportError(err)
		}
		return 0
	case msgStop, msgClose:
		l.stopResult <- l.shutdown(msg == msgClose)
		return 0
	case win.WM_CLOSE:
		if err := l.shutdown(true); err != nil {
			l.logger.Warn(fmt.Sprintf("tray box: %v", err))
		}
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
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
	if cds != nil && cds.DwData == bridgeObservedData {
		if !l.cfg.native || cds.CbData < 52 || (cds.CbData-8)%44 != 0 || cds.CbData > 8+4096*44 || cds.LpData == 0 {
			return 0
		}
		return l.observeNativeBatch(unsafe.Slice((*byte)(foreignPointer(cds.LpData)), cds.CbData))
	}
	if cds != nil && cds.DwData == bridgePacketData {
		if !l.cfg.native || cds.CbData <= 24 || cds.CbData > 65560 || cds.LpData == 0 {
			return 0
		}
		wire := unsafe.Slice((*byte)(foreignPointer(cds.LpData)), cds.CbData)
		previous := l.packetToken
		l.packetToken = bridgeWord(wire[16:])
		started := time.Now()
		defer func() {
			if elapsed := time.Since(started); elapsed >= 400*time.Millisecond {
				d, _ := parseTrayData(wire[24:])
				l.logger.Warn(fmt.Sprintf("tray box: slow request token=%d kind=%d message=%d owner=0x%X uid=%d duration=%s", l.packetToken, bridgeWord(wire), d.Message, d.HWnd, d.UID, elapsed.Round(time.Millisecond)))
			}
			l.packetToken = previous
			if previous == 0 && l.resumePending != 0 {
				win.PostMessage(l.hwnd, msgBridgeResume, uintptr(l.resumePending), l.resumeError)
			}
		}()
		original := copyDataStruct{uintptr(bridgeWord(wire)), uint32(len(wire) - 24), uintptr(unsafe.Pointer(&wire[24]))}
		var pin runtime.Pinner
		pin.Pin(&original)
		defer pin.Unpin()
		return l.onCopyData(uintptr(bridgeWord(wire[8:])), uintptr(unsafe.Pointer(&original)))
	}
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
	if isNew {
		l.logger.Info(fmt.Sprintf("tray box: received native icon from %s (hwnd=0x%X uid=%d message=%d flags=0x%X)", path, d.HWnd, d.UID, d.Message, d.Flags))
	}
	if err := l.syncRecovery(); err != nil {
		l.failRecovery(err)
		collect = false
	}
	out := data
	if collect {
		out = withHidden(data)
	}
	result, delivered := l.sendRequestResult(wParam, out)
	// A refresh is not an Explorer restart: its original registration may
	// still exist. An explicit duplicate ADD rejection can be translated to
	// MODIFY, preserving all new icon/callback fields and the hidden state.
	// Return that native success, or clients which cache the ADD result can
	// disable future registrations (notably Flutter's system_tray).
	needHide := isNew
	if d.Message == nimAdd {
		needHide = result == 0
	}
	if collect && d.Message != nimDelete && needHide {
		var hiddenResult uintptr
		var hiddenDelivered bool
		if d.Message == nimAdd && delivered {
			hiddenResult, hiddenDelivered = l.updateDuplicateAdd(wParam, idOf(d), out, revision)
			if hiddenDelivered && hiddenResult != 0 {
				if isNew {
					l.logger.Info(fmt.Sprintf("tray box: existing native icon accepted for recollection: %s (hwnd=0x%X uid=%d)", path, d.HWnd, d.UID))
				}
				return hiddenResult
			}
		} else {
			// A first-seen MODIFY needs a separate state-only hide. After an
			// ADD timeout, do not replay fields/notifications of unknown outcome.
			hiddenResult, hiddenDelivered = l.hideCurrent(idOf(d), uintptr(d.HWnd))
		}
		if isNew && delivered && result == 0 && hiddenDelivered && hiddenResult == 0 {
			return l.rollbackRejectedCollection(wParam, path, d, data, revision, result)
		}
	}
	return result
}

// updateDuplicateAdd only updates an existing native registration. A newer
// reentrant request or selection change must never be overwritten by a replay.
func (l *listener) updateDuplicateAdd(wParam uintptr, id iconID, data []byte, revision uint64) (uintptr, bool) {
	l.mu.Lock()
	t := l.icons[id]
	current := t != nil && t.revision == revision && !t.collectionFailed && !l.closing && l.paths[strings.ToLower(t.ExePath)]
	l.mu.Unlock()
	if !current {
		return 0, false
	}
	request := append([]byte(nil), data...)
	put(request, offMessage, nimModify)
	return l.sendRequestResult(wParam, request)
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
		l.reportError(fmt.Errorf("could not collect %s; its original native icon was restored, and collection for this icon stays disabled until reselected", path))
	} else {
		l.reportError(fmt.Errorf("could not confirm the original native registration for %s (delivered=%t result=%d)", path, delivered, originalResult))
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
	// Shell32 uses a zero packed position as 'not found'. Keep the origin
	// nonzero even when the cursor straddles the desktop origin; the extent
	// is always positive for a collected icon.
	if left == 0 && top == 0 {
		top--
	}
	return rectAnswer(q.Part, left, top, right, bottom)
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
	case timerHeal:
		l.heal()
	case timerRefresh:
		win.KillTimer(l.hwnd, timerRefresh)
		l.mu.Lock()
		for p := range l.paths {
			l.pending[p] = true
		}
		l.mu.Unlock()
		if err := l.sync(); err != nil {
			l.reportError(err)
		}
	}
}

// heal hides selected icons whose native state became visible,
// and drops icons whose window is gone.
func (l *listener) heal() {
	if l.recovery != nil && !l.recoveryFailed {
		select {
		case <-l.recovery.done:
			l.failRecovery(errors.New("tray recovery process exited"))
		default:
		}
	}
	if l.recoveryFailed || l.bridgeFailed {
		if err := l.restoreIcons(true); err != nil {
			l.logger.Warn(fmt.Sprintf("tray box: %v", err))
		}
		return
	}
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
	if err := l.syncRecovery(); err != nil {
		l.failRecovery(err)
		return
	}
	for _, c := range checks {
		if l.shownByExplorer(c.template) {
			l.logger.Info(fmt.Sprintf("tray box: icon hwnd=0x%X uid=%d appeared on the taskbar; hiding it again", c.id.hwnd, c.id.uid))
			l.hideCurrent(c.id, 0)
		}
	}
}

// sync hands icons of deselected programs back to Explorer and asks newly
// selected programs to register their icons again.
func (l *listener) sync() error {
	if err := l.restoreIcons(false); err != nil {
		return err
	}
	if err := l.syncRecovery(); err != nil {
		l.failRecovery(err)
		return err
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
	// A selection change can contain many programs. Share one process/window
	// scan for the whole batch rather than repeating it for every path.
	var refreshed map[string]int
	if l.cfg.native {
		// Confirm the receiving route off this thread before refreshing only
		// selected programs (including explicitly registered hosted owners).
		select {
		case l.bridgeRefresh <- struct{}{}:
		default:
		}
	} else {
		refreshed = postTaskbarCreated(pending, l.taskbarCreated)
	}
	for _, p := range pending {
		n := refreshed[p]
		// These icons live in WinTray, so the hosted executable's process
		// enumeration cannot find them when collection is enabled later.
		if !l.cfg.native && l.cfg.hostedOwners != nil {
			l.cfg.hostedOwners.Range(func(_, value any) bool {
				owner := value.(hostedIconOwner)
				if owner.path == p && owner.refresh != nil {
					owner.refresh()
					n++
				}
				return true
			})
		}
		if !l.cfg.native || n != 0 {
			l.logger.Info(fmt.Sprintf("tray box: asked %d windows of %s to register their icons", n, p))
		}
	}
	return nil
}

// restoreIcons retains failed restores for retry. A selection transaction
// keeps all records until success; terminal close instead leaves the recovery
// companion's acknowledged snapshot intact before releasing the listener.
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
		request, revision, path, owner, uid := stateRequest(t.template, false), t.revision, t.ExePath, t.owner, t.uid
		l.mu.Unlock()
		if result, delivered := l.sendRequestResult(0, request); result == 0 {
			errs = append(errs, fmt.Errorf("could not restore tray icon of %s (hwnd=0x%X uid=%d delivered=%t)", path, owner, uid, delivered))
			continue
		}
		l.mu.Lock()
		// A failed companion permanently releases collection for this listener.
		// Retain failed restores for retry, but stop touching successful ones.
		if (l.recoveryFailed || l.bridgeFailed) && l.icons[id] == t && t.revision == revision {
			t.nativeRestored = true
		}
		if !all && l.icons[id] == t && t.revision == revision && !l.paths[strings.ToLower(t.ExePath)] {
			delete(l.icons, id)
		}
		l.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (l *listener) shutdown(terminal bool) error {
	l.mu.Lock()
	l.closing = true // reentrant requests must pass through without being hidden
	l.mu.Unlock()
	restoreErr := l.restoreIcons(true)
	if restoreErr != nil && !terminal {
		l.mu.Lock()
		l.closing = l.recoveryFailed
		l.mu.Unlock()
		return restoreErr
	}
	if !win.DestroyWindow(l.hwnd) {
		err := errors.Join(restoreErr, errors.New("could not stop the tray icon listener"))
		if terminal {
			// Do not keep pumping/healing after the application has committed
			// to exit; closing the recovery pipe remains the fallback.
			win.PostQuitMessage(0)
		} else {
			l.mu.Lock()
			l.closing = l.recoveryFailed
			l.mu.Unlock()
		}
		return err
	}
	// DestroyWindow also removes its timers. On failure retain the recovery
	// companion's last acknowledged snapshot, not an empty "success" frame.
	l.mu.Lock()
	clear(l.icons)
	clear(l.paths)
	l.mu.Unlock()
	if restoreErr == nil && l.recovery != nil && !l.recoveryFailed {
		if err := l.recovery.stop(); err != nil {
			l.logger.Warn(fmt.Sprintf("tray box: stop recovery: %v", err))
		}
	}
	return restoreErr
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
	// The taskbar calls us synchronously. Tag the return leg so its subclass
	// invokes Explorer directly rather than recursively entering this broker.
	var envelope []byte
	var wrapped copyDataStruct
	property, _, _ := user32.NewProc("GetPropW").Call(uintptr(tray), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("WinTray.ShellBridge.Route.v3"))))
	if l.cfg.native && msg == win.WM_COPYDATA && lParam != 0 && property != uintptr(l.hwnd) {
		original := (*copyDataStruct)(foreignPointer(lParam))
		if original.DwData == copyDataNotifyIcon && original.LpData != 0 {
			data, ok := parseTrayData(unsafe.Slice((*byte)(foreignPointer(original.LpData)), original.CbData))
			// A cancelled/expired route must never let a delayed broker send
			// an old hide/notification directly after the subclass is gone.
			// Restoration changes only the visibility of an existing icon.
			if !ok || data.Message != nimModify || data.Flags & ^uint32(nifState|nifGUID) != 0 || data.Flags&nifState == 0 || data.State&nisHidden != 0 {
				return 0, false
			}
		}
	}
	if l.cfg.native && property == uintptr(l.hwnd) && msg == win.WM_COPYDATA && lParam != 0 {
		original := (*copyDataStruct)(foreignPointer(lParam))
		envelope = make([]byte, 24+int(original.CbData))
		binary.LittleEndian.PutUint64(envelope, uint64(original.DwData))
		binary.LittleEndian.PutUint64(envelope[8:], uint64(wParam))
		binary.LittleEndian.PutUint64(envelope[16:], l.packetToken)
		if original.CbData != 0 && original.LpData != 0 {
			copy(envelope[24:], unsafe.Slice((*byte)(foreignPointer(original.LpData)), original.CbData))
		}
		wrapped = copyDataStruct{bridgeForwardData, uint32(len(envelope)), uintptr(unsafe.Pointer(&envelope[0]))}
		output.Pin(&envelope[0])
		output.Pin(&wrapped)
		wParam, lParam = uintptr(l.hwnd), uintptr(unsafe.Pointer(&wrapped))
	}
	if r, _, _ := procSendMessageTimeoutW.Call(uintptr(tray), uintptr(msg), wParam, lParam,
		smtoAbortIfHung, forwardTimeout, uintptr(unsafe.Pointer(&result))); r == 0 {
		return 0, false
	}
	if l.cfg.native && msg == win.WM_COPYDATA && result == 0x57544558 {
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
func (l *listener) shownByExplorer(template []byte) bool {
	d, ok := parseTrayData(template)
	if !ok {
		return false
	}
	if l.cfg.native {
		// Shell32 queries would reenter the broker and receive our synthetic
		// click anchor. Send the wire query through the broker return
		// leg to observe Explorer's actual visibility instead.
		query := make([]byte, rectQuerySize)
		put(query, 0, trayDataSignature)
		put(query, offRectPart, rectPartExtent)
		put(query, 8, 32)
		put(query, offRectHWnd, d.HWnd)
		put(query, offRectUID, d.UID)
		if d.usesGUID() {
			copy(query[offRectGUID:], d.GUID[:])
		}
		cds := copyDataStruct{copyDataIconRect, uint32(len(query)), uintptr(unsafe.Pointer(&query[0]))}
		var pinned runtime.Pinner
		pinned.Pin(&cds)
		pinned.Pin(&query[0])
		defer pinned.Unpin()
		result := l.forward(win.WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds)))
		return int16(result) > 0 && int16(result>>16) > 0
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

// postTaskbarCreated tells each selected process's windows that the taskbar
// was created. Counts stay per executable for diagnostics and hosted icons.
func postTaskbarCreated(paths []string, msg uint32) map[string]int {
	if len(paths) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path != "" {
			wanted[path] = true
		}
	}
	return postTaskbarCreatedToProcesses(processesByPath(wanted), msg)
}

func postTaskbarCreatedToProcesses(pids map[uint32]string, msg uint32) map[string]int {
	if len(pids) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, hwnd := range allWindows() {
		if l := activeListener.Load(); l != nil && hwnd == l.hwnd {
			continue // never ask our listener to re-register itself
		}
		var pid uint32
		win.GetWindowThreadProcessId(hwnd, &pid)
		if path, ok := pids[pid]; ok && win.PostMessage(hwnd, msg, 0, 0) != 0 {
			counts[path]++
		}
	}
	return counts
}
