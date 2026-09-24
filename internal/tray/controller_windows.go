//go:build windows

package tray

import (
	"github.com/lxn/walk"
	"wintray/internal/branding"
	"wintray/internal/i18n"
)

type Controller struct {
	notifyIcon    *walk.NotifyIcon
	openAction    *walk.Action
	exitAction    *walk.Action
	language      string
	exitRequested bool
}

func New(
	window *walk.MainWindow,
	showMainWindow func(),
	exitApp func(),
	language string,
	visible bool,
) (*Controller, error) {
	ni, err := walk.NewNotifyIcon(window)
	if err != nil {
		return nil, err
	}

	c := &Controller{
		notifyIcon: ni,
		language:   language,
	}

	if appIcon, iconErr := branding.AppIcon(); iconErr == nil && appIcon != nil {
		if err = ni.SetIcon(appIcon); err != nil {
			disposeNotifyIcon(ni)
			return nil, err
		}
	}

	openAction := walk.NewAction()
	openAction.Triggered().Attach(func() {
		if c.exitRequested {
			return
		}
		showMainWindow()
	})
	c.openAction = openAction
	ni.ContextMenu().Actions().Add(openAction)

	exitAction := walk.NewAction()
	exitAction.Triggered().Attach(func() {
		if c.exitRequested {
			return
		}
		// Mark the controller before invoking the application callback. The
		// callback closes the main window asynchronously, so queued tray
		// activation messages must not reopen it during that interval.
		c.exitRequested = true
		exitApp()
	})
	c.exitAction = exitAction
	ni.ContextMenu().Actions().Add(exitAction)

	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton && !c.exitRequested {
			showMainWindow()
		}
	})

	c.SetLanguage(language)
	if err = ni.SetVisible(visible); err != nil {
		disposeNotifyIcon(ni)
		return nil, err
	}

	return c, nil
}

func (c *Controller) SetLanguage(language string) {
	if c == nil || c.notifyIcon == nil {
		return
	}
	c.language = language
	msg := i18n.For(language)
	_ = c.notifyIcon.SetToolTip(msg.TrayToolTip)
	if c.openAction != nil {
		c.openAction.SetText(msg.TrayOpenSettings)
	}
	if c.exitAction != nil {
		c.exitAction.SetText(msg.TrayExit)
	}
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
