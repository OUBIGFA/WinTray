//go:build windows

package traybox

import (
	"strings"
	"sync"
)

// Box collects the tray icons of the programs selected in WinTray's list.
// Its methods run on the UI thread.
type Box struct {
	selfPath string
	logger   Logger
	paths    []string
	listener *listener
	config   listenerConfig
	// Hosted windows belong to WinTray, but their icons represent another
	// executable. Keep identities across listener stop/start cycles.
	hostedOwners sync.Map // uint32 HWND -> hostedIconOwner
}

type hostedIconOwner struct {
	path string
	// refresh queues a current native icon update on its owning UI thread.
	refresh func()
}

func NewBox(selfPath string, logger Logger) *Box {
	return &Box{
		selfPath: selfPath,
		logger:   logger,
		config:   listenerConfig{className: trayWindowClass, target: explorerTray, raise: true},
	}
}

func (b *Box) HasSelection() bool { return len(b.paths) != 0 }

// RegisterHostedIcon associates a NotifyIcon owner with the program it hosts.
// Call before creating the icon; release after disposing it, before destroying
// the owner window, so reused window handles never inherit the association.
func (b *Box) RegisterHostedIcon(hwnd uintptr, exePath string, refresh func()) func() {
	owner := uint32(hwnd)
	b.hostedOwners.Store(owner, hostedIconOwner{canonicalIconPath(exePath), refresh})
	return func() { b.hostedOwners.Delete(owner) }
}

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
		cfg := b.config
		cfg.hostedOwners = &b.hostedOwners
		l, err := startListener(cfg, b.selfPath, selected, b.logger)
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
