//go:build windows

package tray

import (
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"wintray/internal/branding"
	"wintray/internal/i18n"
)

const (
	trayActionOpenSettings = 1
	trayActionRunSilently  = 2
	trayActionExit         = 3
)

type Controller struct {
	notifyIcon    *walk.NotifyIcon
	window        *walk.MainWindow
	showMain      func()
	runSilently   func()
	exitApp       func()
	language      string
	exitRequested bool
}

func New(
	window *walk.MainWindow,
	showMainWindow func(),
	runSilently func(),
	exitApp func(),
	language string,
	visible bool,
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
	if !appendTrayMenuItem(hMenu, trayActionOpenSettings, msg.TrayOpenSettings) ||
		!appendTrayMenuItem(hMenu, trayActionRunSilently, msg.RunSilently) ||
		!appendTrayMenuItem(hMenu, trayActionExit, msg.TrayExit) {
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

func appendTrayMenuItem(hMenu win.HMENU, id uint32, text string) bool {
	label, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return false
	}
	position := win.GetMenuItemCount(hMenu)
	if position < 0 {
		return false
	}
	item := win.MENUITEMINFO{
		CbSize:     uint32(unsafe.Sizeof(win.MENUITEMINFO{})),
		FMask:      win.MIIM_FTYPE | win.MIIM_ID | win.MIIM_STRING,
		FType:      win.MFT_STRING,
		WID:        id,
		DwTypeData: label,
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
