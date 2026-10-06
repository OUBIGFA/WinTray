//go:build windows

package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"

	"github.com/egoist/mygo"
	native "github.com/egoist/mygo/ui"
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"wintray/internal/branding"
	"wintray/internal/config"
	"wintray/internal/i18n"
)

type Callbacks struct {
	OnSave           func(config.Settings)
	OnToggleTrayBox  func(string, bool) error
	OnOpenLogs       func()
	OnCleanupRestore func()
	OnRemoveLogon    func()
	OnLaunchNow      func(config.ManagedAppEntry)
	OnCheckUpdate    func()
	OnOpenRepository func()
	OnExit           func()
	OnRunSilently    func()
	OnHideToTray     func()
}

// MainWindow owns the mygo desktop and an invisible owner for the existing
// native tray icons. Only mygo renders controls; the tray owner exists even
// before the first settings window and has no fonts, file icons or renderer.
type MainWindow struct {
	mw                            *walk.MainWindow
	settings                      config.Settings
	callbacks                     Callbacks
	desktop                       desktopState
	onSettingsPage                bool
	launchNowBusy, checkingUpdate bool
	allowClose                    bool
	starting                      []func()
	bounds                        mygo.Rectangle
	queueMu                       sync.Mutex
	queue                         []func()
	disposed                      bool
	iconsCancel                   context.CancelFunc
}

const dispatchMessage = win.WM_APP + 0x51

var dispatcherWindows sync.Map
var commonControls = windows.NewLazySystemDLL("comctl32.dll")
var defSubclass = commonControls.NewProc("DefSubclassProc")
var setSubclass = commonControls.NewProc("SetWindowSubclass")
var dispatchProc = syscall.NewCallback(func(hwnd win.HWND, message uint32, wParam, lParam, subclassID, refData uintptr) uintptr {
	if message == dispatchMessage {
		if value, ok := dispatcherWindows.Load(hwnd); ok {
			value.(*MainWindow).drain()
		}
		return 0
	}
	result, _, _ := defSubclass.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return result
})

func NewBackgroundWindow(initial config.Settings, callbacks Callbacks) (*MainWindow, error) {
	return NewMainWindow(initial, callbacks)
}
func NewMainWindow(initial config.Settings, callbacks Callbacks) (*MainWindow, error) {
	w := &MainWindow{settings: initial, callbacks: callbacks}
	w.settings.ManagedApps = append([]config.ManagedAppEntry(nil), initial.ManagedApps...)
	return w, nil
}

// Mygo must initialize common controls before any tray-owner window exists.
// Sharing its activation context also avoids two frameworks owning nested
// application-wide initialization lifetimes.
func (w *MainWindow) initOwner() error {
	owner, err := walk.NewMainWindow()
	if err != nil {
		return err
	}
	w.queueMu.Lock()
	w.mw = owner
	w.queueMu.Unlock()
	dispatcherWindows.Store(owner.Handle(), w)
	if ok, _, _ := setSubclass.Call(uintptr(owner.Handle()), dispatchProc, dispatchMessage, 0); ok == 0 {
		dispatcherWindows.Delete(owner.Handle())
		owner.Dispose()
		return fmt.Errorf("install main-thread dispatcher")
	}
	owner.SetTitle("WinTray tray owner")
	return nil
}

// Synchronize is asynchronous even on the UI thread. Native menus must finish
// dispatching before callbacks destroy an icon or its owner. One message
// drains a batch; background work never needs a polling timer to wake the UI.
func (w *MainWindow) Synchronize(f func()) {
	w.queueMu.Lock()
	if w.disposed {
		w.queueMu.Unlock()
		return
	}
	first := len(w.queue) == 0
	w.queue = append(w.queue, f)
	var hwnd win.HWND
	if w.mw != nil {
		hwnd = w.mw.Handle()
	}
	w.queueMu.Unlock()
	if first && hwnd != 0 {
		win.PostMessage(hwnd, dispatchMessage, 0, 0)
	}
}
func (w *MainWindow) synchronize(f func()) { w.Synchronize(f) }
func (w *MainWindow) drain() {
	w.queueMu.Lock()
	pending := w.queue
	w.queue = nil
	w.queueMu.Unlock()
	for _, f := range pending {
		f()
	}
	if w.desktop.window != nil {
		w.desktop.window.Invalidate()
	}
}
func (w *MainWindow) OnStarting(f func()) { w.starting = append(w.starting, f) }

func (w *MainWindow) Run() int {
	defer func() {
		if w.iconsCancel != nil {
			w.iconsCancel()
		}
		w.queueMu.Lock()
		w.disposed = true
		w.queue = nil
		w.queueMu.Unlock()
		if w.mw != nil {
			dispatcherWindows.Delete(w.mw.Handle())
			w.mw.Dispose()
		}
	}()
	// This mostly-static utility is faster and smaller with mygo's dirty-rect
	// CPU renderer; avoid loading a D3D driver for a settings window. Keep an
	// explicit MYGO_GPU override available for diagnostics.
	if _, set := os.LookupEnv("MYGO_GPU"); !set {
		if err := os.Setenv("MYGO_GPU", "0"); err != nil {
			log.Printf("configure native renderer: %v", err)
			return 1
		}
	}
	mygo.App.SetName("WinTray")
	mygo.App.OnWindowAllClosed(func() {})
	var initErr error
	mygo.App.WhenReady(func() {
		if initErr = w.initOwner(); initErr != nil {
			mygo.App.Quit()
			return
		}
		for _, f := range w.starting {
			f()
		}
		w.starting = nil
		win.PostMessage(w.mw.Handle(), dispatchMessage, 0, 0)
	})
	if err := mygo.App.Run(); err != nil {
		log.Printf("desktop: %v", err)
		return 1
	}
	if initErr != nil {
		log.Printf("tray owner: %v", initErr)
		return 1
	}
	return 0
}

func (w *MainWindow) ShowMainWindow() {
	w.Synchronize(func() {
		if w.allowClose {
			return
		}
		if w.desktop.window == nil {
			// Releasing the whole surface when hidden keeps GPU and text
			// caches out of the long-lived, background-only tray session.
			w.onSettingsPage = false
			width, height := 1200, 850
			if w.bounds.Width > 0 {
				width, height = w.bounds.Width, w.bounds.Height
			}
			w.desktop.window = mygo.NewWindow(mygo.WindowOptions{
				Title: i18n.For(w.settings.Language).WindowTitle, Width: width, Height: height, MinWidth: 1040, MinHeight: 720, Hidden: true, Content: native.View(w.nativeView),
			})
			window := w.desktop.window
			if w.bounds.Width > 0 {
				window.SetBounds(w.bounds)
			}
			if icon, err := branding.AppIcon(); err == nil {
				hwnd := win.HWND(window.NativeHandle())
				if err = w.mw.SetIcon(icon); err == nil {
					win.SendMessage(hwnd, win.WM_SETICON, 1, win.SendMessage(w.mw.Handle(), win.WM_GETICON, 1, 0))
					win.SendMessage(hwnd, win.WM_SETICON, 0, win.SendMessage(w.mw.Handle(), win.WM_GETICON, 0, 0))
				}
			}
			window.OnClose(func(e *mygo.CloseEvent) {
				if w.allowClose {
					return
				}
				e.PreventDefault()
				w.Synchronize(func() {
					w.HideMainWindow()
					if w.callbacks.OnHideToTray != nil {
						w.callbacks.OnHideToTray()
					}
				})
			})
			w.loadNativeIcons()
		}
		w.desktop.window.Show()
		w.desktop.window.Restore()
		w.desktop.window.Focus()
	})
}

func (w *MainWindow) HideMainWindow() {
	w.commitNativeEdits()
	if w.iconsCancel != nil {
		w.iconsCancel()
		w.iconsCancel = nil
	}
	if window := w.desktop.window; window != nil {
		first, _ := w.desktop.list.Visible()
		w.bounds = window.Bounds()
		w.desktop.window = nil
		window.Destroy()
		w.desktop.icons = nil
		w.desktop.numberCommits = nil
		w.desktop.frameNumberCommits = nil
		w.resetNativeList(first)
	}
}

func (w *MainWindow) RequestExplicitClose() {
	w.Synchronize(func() {
		if w.allowClose {
			return
		}
		w.commitNativeEdits()
		w.allowClose = true
		if w.desktop.window != nil {
			w.desktop.window.Destroy()
			w.desktop.window = nil
		}
		mygo.App.Quit()
	})
}

// Native is exclusively the never-shown owner of native tray menus/icons.
func (w *MainWindow) Native() *walk.MainWindow { return w.mw }
func (w *MainWindow) SetEnabled(on bool) {
	if w.desktop.window != nil {
		win.EnableWindow(win.HWND(w.desktop.window.NativeHandle()), on)
	}
}
func (w *MainWindow) Settings() config.Settings {
	s := w.settings
	s.ManagedApps = append([]config.ManagedAppEntry(nil), s.ManagedApps...)
	return s
}
func (w *MainWindow) save() {
	if w.callbacks.OnSave != nil {
		w.callbacks.OnSave(w.Settings())
	}
}
func (w *MainWindow) TurnOffRunAtLogon() {
	if w.settings.RunAtLogon {
		w.settings.RunAtLogon = false
		w.save()
	}
}
func (w *MainWindow) SetCollectedTrayIcon(id string, on bool) {
	for i := range w.settings.ManagedApps {
		if w.settings.ManagedApps[i].ID == id {
			w.settings.ManagedApps[i].CollectTrayIcon = on
			break
		}
	}
}
func (w *MainWindow) SetLanguage(language string) {
	w.Synchronize(func() {
		w.settings.Language = string(i18n.Resolve(language))
		if w.desktop.window != nil {
			w.desktop.window.SetTitle(i18n.For(language).WindowTitle)
		}
	})
}
func (w *MainWindow) SetLaunchNowBusy(busy bool)   { w.Synchronize(func() { w.launchNowBusy = busy }) }
func (w *MainWindow) SetCheckUpdateBusy(busy bool) { w.Synchronize(func() { w.checkingUpdate = busy }) }

func (w *MainWindow) ShowInfo(title, body string)  { w.showMessage(title, body, mygo.MessageInfo) }
func (w *MainWindow) ShowError(title, body string) { w.showMessage(title, body, mygo.MessageError) }
func (w *MainWindow) showMessage(title, body string, kind mygo.MessageType) {
	w.Synchronize(func() {
		if _, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: w.desktop.window, Title: title, Message: body, Type: kind}); err != nil {
			log.Printf("show dialog: %v", err)
		}
	})
}
func (w *MainWindow) Confirm(title, body string) bool {
	yes, no := "是", "否"
	if w.settings.Language == "en-US" {
		yes, no = "Yes", "No"
	}
	result, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: w.desktop.window, Title: title, Message: body, Type: mygo.MessageQuestion, Buttons: []string{yes, no}, DefaultButton: 1, CancelButton: 1})
	if err != nil {
		log.Printf("show confirmation: %v", err)
		return false
	}
	return result.Button == 0
}
func (w *MainWindow) ConfirmContext(ctx context.Context, title, body string) bool {
	answer := make(chan bool, 1)
	w.Synchronize(func() {
		if ctx.Err() == nil {
			answer <- w.Confirm(title, body)
		}
	})
	select {
	case yes := <-answer:
		return yes
	case <-ctx.Done():
		return false
	}
}

// File icon loading belongs to a worker, never the render function. One small
// bitmap per path is shared by visible rows and dropped with the desktop.
func (w *MainWindow) loadNativeIcons() {
	if w.iconsCancel != nil {
		w.iconsCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.iconsCancel = cancel
	window := w.desktop.window
	paths := make([]string, 0, len(w.settings.ManagedApps))
	for _, entry := range w.settings.ManagedApps {
		paths = append(paths, entry.ExePath)
	}
	go func() {
		icons := make(map[string]*native.Bitmap)
		for _, path := range paths {
			if ctx.Err() != nil {
				return
			}
			if _, ok := icons[path]; ok {
				continue
			}
			icon, err := walk.NewIconExtractedFromFileWithSize(path, 0, 24)
			if err != nil {
				continue
			}
			bmp, err := walk.NewBitmapFromIconForDPI(icon, walk.Size{Width: 24, Height: 24}, 96)
			icon.Dispose()
			if err != nil {
				continue
			}
			img, err := bmp.ToImage()
			bmp.Dispose()
			if err == nil {
				icons[path] = native.NewBitmap(img)
			}
		}
		w.Synchronize(func() {
			defer cancel()
			if w.desktop.window == window && ctx.Err() == nil {
				w.desktop.icons = icons
			}
		})
	}()
}
