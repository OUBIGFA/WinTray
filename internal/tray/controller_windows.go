//go:build windows

package tray

import (
	"context"
	"errors"
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
	// TPM_RIGHTBUTTON executes a normal command on right-click and suppresses
	// WM_MENURBUTTONUP. Leave it unset so the owner can open the icon's menu
	// (the same owner notification used by Explorer++'s MenuController).
	trayMenuFlags = win.TPM_NOANIMATION | win.TPM_RETURNCMD
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
	dispatch      func(func())
	openCancel    context.CancelFunc
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
	var icons []traybox.Icon
	var bitmaps []win.HBITMAP
	if c.box != nil && c.box.HasSelection() {
		icons = c.box.Icons()
		bitmaps = appendBox(hMenu, icons)
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

	flags := uint32(trayMenuFlags)
	var actionID win.BOOL
	rightClicked := trackBoxRightClicks(hwnd, func() {
		if c.box != nil && refreshBoxMenu(hMenu, icons, bitmaps, c.box.Icons()) {
			redrawOpenBoxMenu()
		}
	}, func() {
		actionID = win.TrackPopupMenuEx(hMenu, flags, point.X, point.Y, hwnd, nil)
	})
	// Required by the Windows tray-menu contract after TrackPopupMenuEx.
	win.PostMessage(hwnd, win.WM_NULL, 0, 0)

	if rightClicked != 0 {
		c.runBoxAction(icons, rightClicked, traybox.ActionRightClick)
		return
	}
	if id := uint32(actionID); id >= trayBoxedBase && id < trayIDLimit {
		action := traybox.ActionClick
		if win.GetKeyState(win.VK_SHIFT) < 0 {
			action = traybox.ActionDoubleClick
		}
		c.runBoxAction(icons, id, action)
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
			// The app may decline exit if collected icons cannot be restored.
			// Dispose marks the controller closed only once exit is accepted.
			c.exitApp()
		}
	}
}

// appendBox lists the collected icons with their current images. The
// bitmaps must outlive the menu.
func appendBox(hMenu win.HMENU, icons []traybox.Icon) []win.HBITMAP {
	var bitmaps []win.HBITMAP
	iconSize := int(win.GetSystemMetrics(win.SM_CXSMICON))
	for i, icon := range icons {
		id := uint32(trayBoxedBase + i)
		if id >= trayIDLimit {
			break
		}
		item := menuItem{id: id, text: icon.DisplayName(), disabled: !icon.Clickable()}
		b, _ := traybox.MenuBitmap(icon.Image, iconSize)
		bitmaps = append(bitmaps, b) // one slot per row, including blank frames
		item.bitmap = b
		appendTrayMenuItem(hMenu, item)
	}
	if len(icons) > 0 {
		appendTrayMenuItem(hMenu, menuItem{separator: true})
	}
	return bitmaps
}

// runBoxAction clicks a collected icon on the user's behalf.
func (c *Controller) runBoxAction(icons []traybox.Icon, id uint32, action traybox.Action) {
	if c.openCancel != nil {
		c.openCancel()
		c.openCancel = nil
	}
	i := int(id - trayBoxedBase)
	if i < 0 || i >= len(icons) {
		return
	}
	icon := icons[i]
	// Registration, version and callback may change while the menu is open.
	// Deliver only to the current native icon, never the stale snapshot.
	if c.box != nil {
		found := false
		for _, current := range c.box.Icons() {
			if icon.SameIcon(current) {
				icon, found = current, true
				break
			}
		}
		if !found {
			c.showBoxError(icon, traybox.ErrIconGone)
			return
		}
	}
	if action == traybox.ActionClick {
		ctx, cancel := context.WithCancel(context.Background())
		c.openCancel = cancel
		go func() {
			defer cancel()
			latest := func() (traybox.Icon, error) {
				type result struct {
					icon traybox.Icon
					err  error
				}
				done := make(chan result, 1)
				c.deferUI(func() {
					if c.box != nil && !c.exitRequested {
						for _, current := range c.box.Icons() {
							if icon.SameIcon(current) {
								done <- result{icon: current}
								return
							}
						}
					}
					done <- result{err: traybox.ErrIconGone}
				})
				select {
				case r := <-done:
					return r.icon, r.err
				case <-ctx.Done():
					return traybox.Icon{}, ctx.Err()
				}
			}
			if err := traybox.OpenIcon(ctx, icon, latest); err != nil && !errors.Is(err, context.Canceled) {
				c.deferUI(func() { c.showBoxError(icon, err) })
			}
		}()
		return
	}
	if err := traybox.Activate(icon, action); err != nil {
		c.logger.Warn(fmt.Sprintf("tray box click failed: %s %v", icon.ExePath, err))
		c.showBoxError(icon, err)
	}
}

func (c *Controller) showBoxError(icon traybox.Icon, err error) {
	if c.exitRequested {
		return
	}
	msg := i18n.For(c.language)
	body := fmt.Sprintf(msg.TrayBoxClickFailedBody, icon.DisplayName(), err)
	switch {
	case errors.Is(err, traybox.ErrIconGone):
		body = fmt.Sprintf(msg.TrayBoxIconGoneBody, icon.DisplayName())
	case errors.Is(err, traybox.ErrNotClickable):
		body = fmt.Sprintf(msg.TrayBoxNotClickableBody, icon.DisplayName())
	}
	walk.MsgBox(c.window, msg.TrayBoxFailedTitle, body, walk.MsgBoxIconWarning)
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

func (c *Controller) SetDispatcher(dispatch func(func())) { c.dispatch = dispatch }
func (c *Controller) deferUI(f func()) {
	if c.dispatch != nil {
		c.dispatch(f)
		return
	}
	c.window.Synchronize(f)
	win.PostMessage(c.window.Handle(), win.WM_NULL, 0, 0)
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
	c.exitRequested = true
	if c.openCancel != nil {
		c.openCancel()
		c.openCancel = nil
	}
	disposeNotifyIcon(c.notifyIcon)
	c.notifyIcon = nil
}
