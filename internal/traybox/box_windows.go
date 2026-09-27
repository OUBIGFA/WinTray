//go:build windows

package traybox

import "strings"

// Box collects the tray icons of the programs selected in WinTray's list.
// Its methods run on the UI thread.
type Box struct {
	selfPath string
	logger   Logger
	paths    []string
	listener *listener
	config   listenerConfig
}

func NewBox(selfPath string, logger Logger) *Box {
	return &Box{
		selfPath: selfPath,
		logger:   logger,
		config:   listenerConfig{className: trayWindowClass, target: explorerTray, raise: true},
	}
}

func (b *Box) HasSelection() bool { return len(b.paths) != 0 }

// SetPaths collects the icons of exactly these programs. Icons of programs
// no longer listed are shown in the notification area again. On failure the
// previous selection stays in effect.
func (b *Box) SetPaths(paths []string) error {
	var selected []string
	for _, p := range paths {
		if p != "" && !strings.EqualFold(p, b.selfPath) && !Contains(selected, p) {
			selected = append(selected, p)
		}
	}
	switch {
	case len(selected) == 0:
		if b.listener != nil {
			if err := b.listener.stop(); err != nil {
				return err
			}
			b.listener = nil
			b.logger.Info("tray box: stopped")
		}
	case b.listener == nil:
		l, err := startListener(b.config, b.selfPath, selected, b.logger)
		if err != nil {
			return err
		}
		b.listener = l
		b.logger.Info("tray box: started")
	default:
		b.listener.setPaths(selected)
	}
	b.paths = selected
	return nil
}

// Icons lists the collected icons that are currently shown.
func (b *Box) Icons() []Icon {
	if b.listener == nil {
		return nil
	}
	return b.listener.snapshot()
}

// Close shows every collected icon in the notification area again.
func (b *Box) Close() error {
	return b.SetPaths(nil)
}
