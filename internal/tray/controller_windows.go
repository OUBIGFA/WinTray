//go:build windows

package tray

import (
	"github.com/lxn/walk"
	"wintray/internal/i18n"
)

type Controller struct {
	notifyIcon *walk.NotifyIcon
	openAction *walk.Action
	exitAction *walk.Action
	language   string
}

func New(
	window *walk.MainWindow,
	showMainWindow func(),
	exitApp func(),
	language string,
) (*Controller, error) {
	ni, err := walk.NewNotifyIcon(window)
	if err != nil {
		return nil, err
	}

	c := &Controller{
		notifyIcon: ni,
		language:   language,
	}

	openAction := walk.NewAction()
	openAction.Triggered().Attach(func() {
		showMainWindow()
	})
	c.openAction = openAction
	ni.ContextMenu().Actions().Add(openAction)

	exitAction := walk.NewAction()
	exitAction.Triggered().Attach(func() {
		exitApp()
	})
	c.exitAction = exitAction
	ni.ContextMenu().Actions().Add(exitAction)

	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			showMainWindow()
		}
	})

	c.SetLanguage(language)
	if err = ni.SetVisible(true); err != nil {
		ni.Dispose()
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

func (c *Controller) Dispose() {
	if c == nil || c.notifyIcon == nil {
		return
	}
	_ = c.notifyIcon.SetVisible(false)
	c.notifyIcon.Dispose()
	c.notifyIcon = nil
}
