//go:build windows

package tray

import (
	"fmt"
	"os/exec"

	"github.com/lxn/walk"
	"wintray/internal/i18n"
)

type Controller struct {
	notifyIcon    *walk.NotifyIcon
	openAction    *walk.Action
	logsAction    *walk.Action
	exitAction    *walk.Action
	openLogPath   func() (string, error)
	reportOpenErr func(string)
	language      string
}

func New(
	window *walk.MainWindow,
	showMainWindow func(),
	exitApp func(),
	openLogPath func() (string, error),
	reportOpenErr func(string),
	language string,
) (*Controller, error) {
	ni, err := walk.NewNotifyIcon(window)
	if err != nil {
		return nil, err
	}

	c := &Controller{
		notifyIcon:    ni,
		openLogPath:   openLogPath,
		reportOpenErr: reportOpenErr,
		language:      language,
	}

	openAction := walk.NewAction()
	openAction.Triggered().Attach(func() {
		showMainWindow()
	})
	c.openAction = openAction
	ni.ContextMenu().Actions().Add(openAction)

	logsAction := walk.NewAction()
	logsAction.Triggered().Attach(func() {
		if c.openLogPath == nil {
			return
		}
		path, openErr := c.openLogPath()
		if openErr != nil {
			if c.reportOpenErr != nil {
				c.reportOpenErr(openErr.Error())
			}
			return
		}
		if err := exec.Command("explorer", "/select,", path).Start(); err != nil && c.reportOpenErr != nil {
			c.reportOpenErr(err.Error())
		}
	})
	c.logsAction = logsAction
	ni.ContextMenu().Actions().Add(logsAction)

	ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())

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
	if c.logsAction != nil {
		c.logsAction.SetText(msg.TrayOpenLogs)
	}
	if c.exitAction != nil {
		c.exitAction.SetText(msg.TrayExit)
	}
}

func (c *Controller) ReportOpenLogsError(err error) {
	if c == nil || err == nil || c.reportOpenErr == nil {
		return
	}
	msg := i18n.For(c.language)
	c.reportOpenErr(fmt.Sprintf("%s: %v", msg.StatusOpenLogsFailed, err))
}

func (c *Controller) Dispose() {
	if c == nil || c.notifyIcon == nil {
		return
	}
	_ = c.notifyIcon.SetVisible(false)
	c.notifyIcon.Dispose()
	c.notifyIcon = nil
}
