//go:build windows

package tray

import (
	"errors"
	"fmt"
	"sync"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/branding"
	"wintray/internal/i18n"
)

// HostedWindow describes a program whose window WinTray keeps hidden on its
// behalf. Handle may be 0 when the window has not been located yet; the host
// then resolves it through LookupWindow on demand.
type HostedWindow struct {
	Name      string
	ExePath   string
	Handle    uintptr
	ProcessID uint32
}

// Logger is the subset of the application logger the host needs.
type Logger interface {
	Info(msg string)
	Warn(msg string)
}

// Host owns one tray icon per hidden program so the user can show, hide or
// quit a program that has no tray icon of its own (console programs such as
// syncthing.exe). All methods must be called on the UI thread unless noted.
type Host struct {
	language     string
	logger       Logger
	lookupWindow func(pid uint32) uintptr
	onEmpty      func()

	mu    sync.Mutex
	items map[uint32]*hostedItem
}

type hostedItem struct {
	host      *Host
	info      HostedWindow
	form      *walk.MainWindow
	icon      *walk.NotifyIcon
	ownedIcon walk.Image
	show      *walk.Action
	hide      *walk.Action
	release   *walk.Action
	quit      *walk.Action
	stop      chan struct{}
	closed    bool
}

// NewHost creates an empty host. lookupWindow resolves the window of a hosted
// process when the stored handle is missing or no longer valid.
func NewHost(language string, logger Logger, lookupWindow func(pid uint32) uintptr) *Host {
	return &Host{
		language:     language,
		logger:       logger,
		lookupWindow: lookupWindow,
		items:        map[uint32]*hostedItem{},
	}
}

// SetOnEmpty registers a callback invoked (on the UI thread) when the last
// hosted program goes away.
func (h *Host) SetOnEmpty(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onEmpty = fn
}

// Count reports how many programs are currently hosted. Safe from any goroutine.
func (h *Host) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.items)
}

// ErrHostedProcessUnavailable reports that the program to host is not running
// or cannot be opened, so retrying the hand-off is pointless.
var ErrHostedProcessUnavailable = errors.New("hosted process unavailable")

// Add creates a tray icon for the program. Adding the same process again only
// refreshes the stored window handle. When the icon cannot be created the
// window is shown again so the program never stays unreachable.
func (h *Host) Add(w HostedWindow) error {
	if err := h.TryAdd(w); err != nil {
		h.Restore(w)
		return err
	}
	return nil
}

// TryAdd is Add without the automatic restore: callers that retry (the
// notification area may refuse icons right after logon) restore the window
// themselves once they give up.
func (h *Host) TryAdd(w HostedWindow) error {
	if w.ProcessID == 0 {
		return errors.New("hosted window without process id")
	}
	h.mu.Lock()
	if existing, ok := h.items[w.ProcessID]; ok {
		if w.Handle != 0 {
			existing.info.Handle = w.Handle
		}
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	// Open before publishing the icon: a short-lived process may disappear
	// before an asynchronous watcher can open it. Never leave an unmonitored
	// item behind if opening or creating the tray icon fails.
	process, err := openHostedProcess(windows.SYNCHRONIZE, false, w.ProcessID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrHostedProcessUnavailable, err)
	}
	item, err := createHostedItem(h, w)
	if err != nil {
		_ = windows.CloseHandle(process)
		return err
	}
	h.mu.Lock()
	h.items[w.ProcessID] = item
	h.mu.Unlock()
	h.logger.Info(fmt.Sprintf("tray host: added %s pid=%d hwnd=0x%X", w.Name, w.ProcessID, w.Handle))
	go item.watchProcess(process, item.form.Handle())
	return nil
}

var (
	openHostedProcess = windows.OpenProcess
	createHostedItem  = (*Host).newItem
)

// Restore recovers a hidden program not yet accepted by the host. It never
// activates the window and does not create an icon or take ownership.
func (h *Host) Restore(w HostedWindow) {
	if w.ProcessID == 0 {
		return
	}
	item := hostedItem{host: h, info: w}
	item.showWindow(false)
}

// RestoreWindow shows a hidden program's window again without activating it.
// It is the recovery path when no host process will own the icon.
func RestoreWindow(w HostedWindow, lookupWindow func(pid uint32) uintptr, logger Logger) {
	NewHost("", logger, lookupWindow).Restore(w)
}

// SetLanguage relabels every hosted icon's menu.
func (h *Host) SetLanguage(language string) {
	h.language = language
	h.mu.Lock()
	items := h.snapshotLocked()
	h.mu.Unlock()
	for _, item := range items {
		item.applyLanguage(language)
	}
}

// RestoreAll shows every hidden window again and removes the icons. It is
// meant for WinTray shutdown so no program is left running invisibly.
func (h *Host) RestoreAll() {
	h.mu.Lock()
	items := h.snapshotLocked()
	h.mu.Unlock()
	for _, item := range items {
		item.showWindow(false)
		h.remove(item, false)
	}
}

func (h *Host) snapshotLocked() []*hostedItem {
	items := make([]*hostedItem, 0, len(h.items))
	for _, item := range h.items {
		items = append(items, item)
	}
	return items
}

func (h *Host) newItem(w HostedWindow) (*hostedItem, error) {
	// walk binds one NotifyIcon per window, so each hosted icon gets its own
	// never-shown owner window.
	form, err := walk.NewMainWindow()
	if err != nil {
		return nil, err
	}
	icon, err := walk.NewNotifyIcon(form)
	if err != nil {
		form.Dispose()
		return nil, err
	}
	item := &hostedItem{host: h, info: w, form: form, icon: icon, stop: make(chan struct{})}

	img, owned := programIcon(w.ExePath)
	if owned {
		item.ownedIcon = img
	}
	if img != nil {
		if err := icon.SetIcon(img); err != nil {
			item.disposeResources()
			return nil, err
		}
	}

	item.show = walk.NewAction()
	item.show.Triggered().Attach(func() { item.showWindow(true) })
	item.hide = walk.NewAction()
	item.hide.Triggered().Attach(item.hideWindow)
	item.release = walk.NewAction()
	item.release.Triggered().Attach(item.releaseHosting)
	item.quit = walk.NewAction()
	item.quit.Triggered().Attach(item.quitProgram)
	actions := icon.ContextMenu().Actions()
	_ = actions.Add(item.show)
	_ = actions.Add(item.hide)
	_ = actions.Add(walk.NewSeparatorAction())
	_ = actions.Add(item.release)
	_ = actions.Add(walk.NewSeparatorAction())
	_ = actions.Add(item.quit)

	icon.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			item.toggleWindow()
		}
	})

	item.applyLanguage(h.language)
	if err := icon.SetVisible(true); err != nil {
		item.disposeResources()
		return nil, err
	}
	return item, nil
}

func programIcon(exePath string) (walk.Image, bool) {
	if exePath != "" {
		if img, err := walk.NewIconExtractedFromFileWithSize(exePath, 0, 16); err == nil && img != nil {
			return img, true
		}
	}
	if img, err := branding.AppIcon(); err == nil && img != nil {
		return img, false
	}
	return nil, false
}

func (item *hostedItem) applyLanguage(language string) {
	msg := i18n.For(language)
	_ = item.icon.SetToolTip(item.info.Name)
	item.show.SetText(msg.HostedShowWindow)
	item.hide.SetText(msg.HostedHideWindow)
	item.release.SetText(msg.HostedReleaseWindow)
	item.quit.SetText(fmt.Sprintf(msg.HostedQuitProgram, item.info.Name))
}

// window returns the live window handle, re-resolving it when needed.
func (item *hostedItem) window() win.HWND {
	if hostedWindowMatchesProcess(item.info.Handle, item.info.ProcessID) {
		return win.HWND(item.info.Handle)
	}
	if item.host.lookupWindow != nil {
		if hwnd := item.host.lookupWindow(item.info.ProcessID); hostedWindowMatchesProcess(hwnd, item.info.ProcessID) {
			item.info.Handle = hwnd
			return win.HWND(hwnd)
		}
	}
	return 0
}

func hostedWindowMatchesProcess(hwnd uintptr, pid uint32) bool {
	if hwnd == 0 || pid == 0 {
		return false
	}
	var owner uint32
	_, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &owner)
	return err == nil && owner == pid
}

func (item *hostedItem) showWindow(activate bool) {
	hwnd := item.window()
	if hwnd == 0 {
		item.host.logger.Warn(fmt.Sprintf("tray host: no window to show for %s pid=%d", item.info.Name, item.info.ProcessID))
		return
	}
	win.ShowWindow(hwnd, win.SW_SHOW)
	if win.IsIconic(hwnd) {
		win.ShowWindow(hwnd, win.SW_RESTORE)
	}
	if activate {
		win.SetForegroundWindow(hwnd)
	}
}

func (item *hostedItem) hideWindow() {
	if hwnd := item.window(); hwnd != 0 {
		win.ShowWindow(hwnd, win.SW_HIDE)
	}
}

func (item *hostedItem) toggleWindow() {
	hwnd := item.window()
	if hwnd == 0 {
		return
	}
	if win.IsWindowVisible(hwnd) && !win.IsIconic(hwnd) {
		win.ShowWindow(hwnd, win.SW_HIDE)
		return
	}
	item.showWindow(true)
}

// releaseHosting hands the program back to the user: its window is shown and
// activated, the tray icon goes away and the program keeps running on its own.
// This is the clean way out before uninstalling WinTray, and the counterpart of
// killing the host process, which would leave the window hidden for good.
func (item *hostedItem) releaseHosting() {
	item.host.logger.Info(fmt.Sprintf("tray host: releasing %s pid=%d", item.info.Name, item.info.ProcessID))
	item.showWindow(true)
	deferOnUIThread(item, func() { item.host.remove(item, true) })
}

// deferOnUIThread queues f behind the message being handled: removing an item
// disposes the icon's owner window, so it must not run inside that window's
// own menu callback. Tests replace it to run f at once.
var deferOnUIThread = func(item *hostedItem, f func()) {
	item.form.Synchronize(f)
	win.PostMessage(item.form.Handle(), win.WM_NULL, 0, 0)
}

// quitProgram asks the program to end. For a console program WM_CLOSE on the
// console window delivers CTRL_CLOSE_EVENT, which is the graceful path; a
// program without a window is terminated directly.
func (item *hostedItem) quitProgram() {
	if hwnd := item.window(); hwnd != 0 {
		win.PostMessage(hwnd, win.WM_CLOSE, 0, 0)
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, item.info.ProcessID)
	if err != nil {
		item.host.logger.Warn(fmt.Sprintf("tray host: cannot terminate %s pid=%d: %v", item.info.Name, item.info.ProcessID, err))
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
}

// watchProcess removes the icon once the hosted process ends. Runs on its own
// goroutine and hands the removal back to the UI thread.
func (item *hostedItem) watchProcess(h windows.Handle, wakeWindow win.HWND) {
	defer windows.CloseHandle(h)
	for {
		select {
		case <-item.stop:
			return
		default:
		}
		event, waitErr := windows.WaitForSingleObject(h, 1000)
		if waitErr != nil || event == windows.WAIT_OBJECT_0 {
			if waitErr != nil {
				item.host.logger.Warn(fmt.Sprintf("tray host: wait failed for %s pid=%d: %v", item.info.Name, item.info.ProcessID, waitErr))
			} else {
				item.host.logger.Info(fmt.Sprintf("tray host: %s pid=%d exited", item.info.Name, item.info.ProcessID))
			}
			item.form.Synchronize(func() {
				if item.closed {
					return
				}
				if waitErr != nil {
					item.showWindow(false)
				}
				item.host.remove(item, true)
			})
			win.PostMessage(wakeWindow, win.WM_NULL, 0, 0)
			return
		}
	}
}

func (item *hostedItem) disposeResources() {
	disposeNotifyIcon(item.icon)
	if item.ownedIcon != nil {
		item.ownedIcon.Dispose()
		item.ownedIcon = nil
	}
	if item.form != nil {
		item.form.Dispose()
	}
}

// Walk's NotifyIcon does not own its menu or image. The shared branding image
// remains alive; hostedItem separately releases only extracted program icons.
func disposeNotifyIcon(icon *walk.NotifyIcon) {
	if icon == nil {
		return
	}
	_ = icon.SetVisible(false)
	_ = icon.Dispose()
	icon.ContextMenu().Dispose()
}

func (h *Host) remove(item *hostedItem, notifyEmpty bool) {
	if item.closed {
		return
	}
	item.closed = true
	close(item.stop)
	item.disposeResources()

	h.mu.Lock()
	delete(h.items, item.info.ProcessID)
	empty := len(h.items) == 0
	onEmpty := h.onEmpty
	h.mu.Unlock()
	if notifyEmpty && empty && onEmpty != nil {
		onEmpty()
	}
}
