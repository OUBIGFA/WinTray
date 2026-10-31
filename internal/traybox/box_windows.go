//go:build windows

package traybox

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
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
	bridge   *bridgeManager
	config   listenerConfig

	errorMu sync.Mutex
	onError func(error)
	errors  []error
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
	if logger == nil {
		logger = discardLogger{}
	}
	b := &Box{selfPath: selfPath, logger: logger}
	b.config = listenerConfig{className: brokerWindowClass, target: explorerTray, native: true,
		onError: b.reportError,
		newRecovery: func() (*recoveryClient, error) {
			return startRecovery(exec.Command(filepath.Join(filepath.Dir(selfPath), RecoveryExecutable), "--watch-tray"))
		},
	}
	return b
}

type discardLogger struct{}

func (discardLogger) Info(string) {}
func (discardLogger) Warn(string) {}

// SetErrorHandler receives background attachment/recovery failures. The
// handler must enqueue UI work, not block the broker. Failures before the UI
// exists are delivered when the handler is installed, rather than lost in logs.
func (b *Box) SetErrorHandler(handler func(error)) {
	b.errorMu.Lock()
	b.onError = handler
	pending := b.errors
	if handler != nil {
		b.errors = nil
	}
	b.errorMu.Unlock()
	if handler != nil && len(pending) != 0 {
		handler(errors.Join(pending...))
	}
}

func (b *Box) reportError(err error) {
	b.errorMu.Lock()
	handler := b.onError
	if handler == nil {
		b.errors = append(b.errors, err)
	}
	b.errorMu.Unlock()
	if handler != nil {
		handler(err)
	}
}

func (b *Box) HasSelection() bool { return len(b.paths) != 0 }

// RegisterHostedIcon associates a NotifyIcon owner with the program it hosts.
// Call before creating the icon; release after disposing it, before destroying
// the owner window, so reused window handles never inherit the association.
func (b *Box) RegisterHostedIcon(hwnd uintptr, exePath string, refresh func()) func() {
	owner := uint32(hwnd)
	b.hostedOwners.Store(owner, hostedIconOwner{canonicalIconPath(exePath), refresh})
	if b.bridge != nil {
		if err := b.bridge.updateHosted(); err != nil {
			b.reportError(err)
		}
	}
	return func() {
		b.hostedOwners.Delete(owner)
		if b.bridge != nil {
			if err := b.bridge.updateHosted(); err != nil {
				b.reportError(err)
			}
		}
	}
}

// SetPaths collects the icons of exactly these programs. Icons of programs
// no longer listed are shown in the notification area again. On failure the
// previous selection stays in effect.
func (b *Box) SetPaths(paths []string) error {
	var selected []string
	for _, p := range paths {
		p = canonicalIconPath(p)
		if p != "" && !strings.EqualFold(p, canonicalIconPath(b.selfPath)) && !Contains(selected, p) {
			selected = append(selected, p)
		}
	}
	switch {
	case len(selected) == 0:
		if b.listener != nil {
			stop := b.listener.stop
			if b.bridge != nil {
				stop = func() error { return b.bridge.apply(nil, b.listener.stop) }
			}
			if err := stop(); err != nil {
				return err
			}
			if b.bridge != nil {
				if err := b.bridge.close(); err != nil {
					b.logger.Warn(fmt.Sprintf("tray box: close native bridge: %v", err))
				}
				b.bridge = nil
			}
			b.listener = nil
			b.logger.Info("tray box: stopped")
		}
	case b.listener == nil:
		cfg := b.config
		cfg.hostedOwners = &b.hostedOwners
		// Start empty: no icon is hidden until the taskbar receiver has a
		// confirmed route and the recovery companion is ready.
		l, err := startListener(cfg, b.selfPath, nil, b.logger)
		if err != nil {
			return err
		}
		if cfg.native {
			b.bridge, err = newBridgeManager(b.selfPath, l)
			if err == nil {
				err = b.bridge.apply(selected, func() error { return l.updatePaths(selected) })
			}
		} else {
			err = l.updatePaths(selected)
		}
		if err != nil {
			if b.bridge != nil {
				err = errors.Join(err, b.bridge.close())
				b.bridge = nil
			}
			return errors.Join(err, l.stop())
		}
		b.listener = l
		b.logger.Info("tray box: started")
	default:
		change := func() error { return b.listener.updatePaths(selected) }
		if b.bridge != nil {
			change = func() error {
				return b.bridge.apply(selected, func() error { return b.listener.updatePaths(selected) })
			}
		}
		if err := change(); err != nil {
			return err
		}
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

// Close is terminal shutdown, unlike SetPaths(nil), which must roll back a
// failed deselection. Restoration errors are returned for logging, but cannot
// keep collecting or discard the companion's pending recovery snapshot.
func (b *Box) Close() error {
	var errs []error
	if b.listener != nil {
		errs = append(errs, b.listener.close())
		b.listener = nil
	}
	if b.bridge != nil {
		errs = append(errs, b.bridge.close())
		b.bridge = nil
	}
	b.paths = nil
	return errors.Join(errs...)
}
