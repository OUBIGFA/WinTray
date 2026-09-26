//go:build windows

package tray

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/branding"
	"wintray/internal/i18n"
	"wintray/internal/traybox"
)

const (
	trayActionOpenSettings = 1
	trayActionRunSilently  = 2
	trayActionExit         = 3
	trayBoxedBase          = 100
	trayIDLimit            = 9000
)

type Controller struct {
	notifyIcon    *walk.NotifyIcon
	window        *walk.MainWindow
	showMain      func()
	runSilently   func()
	exitApp       func()
	language      string
	exitRequested bool
	box           *traybox.Box
	logger        Logger
}

func New(
	window *walk.MainWindow,
	showMainWindow func(),
	runSilently func(),
	exitApp func(),
	language string,
	visible bool,
	box *traybox.Box,
	logger Logger,
) (*Controller, error) {
	ni, err := walk.NewNotifyIcon(window)
	if err != nil {
		return nil, err
	}

	c := &Controller{
		notifyIcon:  ni,
		window:      window,
		showMain:    showMainWindow,
		runSilently: runSilently,
		exitApp:     exitApp,
		language:    language,
		box:         box,
		logger:      logger,
	}

	if appIcon, iconErr := branding.AppIcon(); iconErr == nil && appIcon != nil {
		if err = ni.SetIcon(appIcon); err != nil {
			disposeNotifyIcon(ni)
			return nil, err
		}
	}

	// Keep Walk's built-in context menu empty. Its notify-icon handler opens
	// that menu from WM_CONTEXTMENU after MouseUp; the native menu below is
	// shown from MouseUp instead, so there is exactly one menu lifecycle.
	ni.MouseUp().Attach(func(_, _ int, button walk.MouseButton) {
		if button == walk.RightButton {
			c.showContextMenu()
		}
	})
	ni.MouseDown().Attach(func(_, _ int, button walk.MouseButton) {
		if button == walk.LeftButton && !c.exitRequested && c.showMain != nil {
			c.showMain()
		}
	})

	c.SetLanguage(language)
	if err = ni.SetVisible(visible); err != nil {
		disposeNotifyIcon(ni)
		return nil, err
	}
	return c, nil
}

func (c *Controller) showContextMenu() {
	if c == nil || c.window == nil || c.exitRequested {
		return
	}

	hMenu := win.CreatePopupMenu()
	if hMenu == 0 {
		return
	}
	defer win.DestroyMenu(hMenu)

	msg := i18n.For(c.language)
	var view traybox.View
	if c.box != nil && c.box.HasSelection() {
		view = c.box.View()
		if view.Err != nil {
			c.logger.Warn(fmt.Sprintf("tray box: %v", view.Err))
		}
		bitmaps := c.appendBox(hMenu, view, msg)
		defer func() {
			for _, b := range bitmaps {
				win.DeleteObject(win.HGDIOBJ(b))
			}
		}()
	}
	if !appendTrayMenuItem(hMenu, menuItem{id: trayActionOpenSettings, text: msg.TrayOpenSettings}) ||
		!appendTrayMenuItem(hMenu, menuItem{id: trayActionRunSilently, text: msg.RunSilently}) ||
		!appendTrayMenuItem(hMenu, menuItem{id: trayActionExit, text: msg.TrayExit}) {
		return
	}

	hwnd := c.window.Handle()
	win.SetForegroundWindow(hwnd)
	var point win.POINT
	if !win.GetCursorPos(&point) {
		return
	}

	flags := uint32(win.TPM_NOANIMATION | win.TPM_RETURNCMD | win.TPM_RIGHTBUTTON)
	actionID := win.TrackPopupMenuEx(
		hMenu,
		flags,
		point.X,
		point.Y,
		hwnd,
		nil,
	)
	// Required by the Windows tray-menu contract after TrackPopupMenuEx.
	win.PostMessage(hwnd, win.WM_NULL, 0, 0)

	if id := uint32(actionID); id >= trayBoxedBase && id < trayIDLimit {
		c.runBoxAction(view, id)
		return
	}
	switch uint32(actionID) {
	case trayActionOpenSettings:
		if c.showMain != nil && !c.exitRequested {
			c.showMain()
		}
	case trayActionRunSilently:
		if c.runSilently != nil && !c.exitRequested {
			c.runSilently()
		}
	case trayActionExit:
		if c.exitApp != nil && !c.exitRequested {
			c.exitRequested = true
			c.exitApp()
		}
	}
}

// appendBox lists only icons selected in the program editor. Their bitmaps
// must outlive the menu.
func (c *Controller) appendBox(hMenu win.HMENU, view traybox.View, msg i18n.Messages) []win.HBITMAP {
	var bitmaps []win.HBITMAP
	iconSize := int(win.GetSystemMetrics(win.SM_CXSMICON))
	bitmapOf := func(icon traybox.Icon) win.HBITMAP {
		b, err := traybox.MenuBitmap(icon.Snapshot, iconSize)
		if err != nil {
			return 0
		}
		bitmaps = append(bitmaps, b)
		return b
	}

	for i, l := range view.Boxed {
		id := uint32(trayBoxedBase + i)
		if id >= trayIDLimit {
			break
		}
		appendTrayMenuItem(hMenu, menuItem{id: id, text: l.DisplayName(), bitmap: bitmapOf(l.Icon)})
	}
	if len(view.Boxed) > 0 {
		appendTrayMenuItem(hMenu, menuItem{separator: true})
	}
	return bitmaps
}

// runBoxAction opens a selected program from its icon in WinTray's menu.
func (c *Controller) runBoxAction(view traybox.View, id uint32) {
	i := int(id - trayBoxedBase)
	if i >= len(view.Boxed) {
		return
	}
	icon := view.Boxed[i]
	// Opening the hidden-icons flyout takes a moment; the UI thread
	// must keep pumping meanwhile.
	go func() {
		if err := traybox.Activate(icon, traybox.ActionDoubleClick); err != nil {
			c.logger.Warn(fmt.Sprintf("tray box click failed: %s %v", icon.ExePath, err))
			c.window.Synchronize(func() { c.showBoxError(err) })
		}
	}()
}

func (c *Controller) showBoxError(err error) {
	if c.exitRequested {
		return
	}
	walk.MsgBox(c.window, i18n.For(c.language).TrayBoxFailedTitle, err.Error(), walk.MsgBoxIconWarning)
}

type menuItem struct {
	id        uint32
	text      string
	submenu   win.HMENU
	bitmap    win.HBITMAP
	checked   bool
	disabled  bool
	separator bool
}

func appendTrayMenuItem(hMenu win.HMENU, m menuItem) bool {
	position := win.GetMenuItemCount(hMenu)
	if position < 0 {
		return false
	}
	item := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{}))}
	if m.separator {
		item.FMask = win.MIIM_FTYPE
		item.FType = win.MFT_SEPARATOR
		return win.InsertMenuItem(hMenu, uint32(position), true, &item)
	}
	label, err := syscall.UTF16PtrFromString(m.text)
	if err != nil {
		return false
	}
	item.FMask = win.MIIM_FTYPE | win.MIIM_ID | win.MIIM_STRING | win.MIIM_STATE
	item.FType = win.MFT_STRING
	item.WID = m.id
	item.DwTypeData = label
	if m.checked {
		item.FState |= win.MFS_CHECKED
	}
	if m.disabled {
		item.FState |= win.MFS_DISABLED
	}
	if m.submenu != 0 {
		item.FMask |= win.MIIM_SUBMENU
		item.HSubMenu = m.submenu
	}
	if m.bitmap != 0 {
		item.FMask |= win.MIIM_BITMAP
		item.HbmpItem = m.bitmap
	}
	return win.InsertMenuItem(hMenu, uint32(position), true, &item)
}

func (c *Controller) SetLanguage(language string) {
	if c == nil || c.notifyIcon == nil {
		return
	}
	c.language = language
	_ = c.notifyIcon.SetToolTip(i18n.For(language).TrayToolTip)
}

func (c *Controller) SetVisible(visible bool) error {
	if c == nil || c.notifyIcon == nil {
		return nil
	}
	return c.notifyIcon.SetVisible(visible)
}

func (c *Controller) Dispose() {
	if c == nil || c.notifyIcon == nil {
		return
	}
	disposeNotifyIcon(c.notifyIcon)
	c.notifyIcon = nil
}
