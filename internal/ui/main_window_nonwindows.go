//go:build !windows

package ui

import "wintray/internal/config"

type Callbacks struct {
	OnSave     func(config.Settings)
	OnOpenLogs func()
	OnExit     func()
}

type MainWindow struct{}

func NewMainWindow(_ config.Settings, _ Callbacks) (*MainWindow, error) { return &MainWindow{}, nil }
func (w *MainWindow) ShowMainWindow()                                   {}
func (w *MainWindow) HideMainWindow()                                   {}
func (w *MainWindow) Run() int                                          { return 0 }
func (w *MainWindow) RequestExplicitClose()                             {}
func (w *MainWindow) Native() any                                       { return nil }
func (w *MainWindow) Settings() config.Settings                         { return config.DefaultSettings() }
