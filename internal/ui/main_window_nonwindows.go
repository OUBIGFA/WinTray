//go:build !windows

package ui

import "wintray/internal/config"

type Callbacks struct {
	OnSave           func(config.Settings)
	OnOpenLogs       func()
	OnCleanupRestore func()
	OnLaunchNow      func(config.ManagedAppEntry)
	OnCheckUpdate    func()
	OnOpenRepository func()
	OnExit           func()
}

type MainWindow struct{}

func NewMainWindow(_ config.Settings, _ Callbacks) (*MainWindow, error) { return &MainWindow{}, nil }
func (w *MainWindow) ShowMainWindow()                                   {}
func (w *MainWindow) HideMainWindow()                                   {}
func (w *MainWindow) Run() int                                          { return 0 }
func (w *MainWindow) RequestExplicitClose()                             {}
func (w *MainWindow) SetLaunchNowBusy(_ bool)                           {}
func (w *MainWindow) SetCheckUpdateBusy(_ bool)                         {}
func (w *MainWindow) Confirm(_, _ string) bool                          { return false }
func (w *MainWindow) Native() any                                       { return nil }
func (w *MainWindow) Settings() config.Settings                         { return config.DefaultSettings() }
