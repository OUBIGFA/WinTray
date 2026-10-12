//go:build windows

package traybox

import (
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/lxn/win"
)

// bridgeManager owns one receiving-end lease, irrespective of how many
// programs or sandbox children share a selected path. Discovery after a shell
// restart runs off the broker thread; a hung shell never blocks native input.
type bridgeManager struct {
	mu         sync.Mutex
	paths      []string
	target     shellEndpoint
	lease      *bridgeLease
	failed     error
	scanFailed bool
	closed     bool
	quit, done chan struct{}
	wake       <-chan struct{}
	report     func(error)
	active     func() bool
	scan       func() (shellEndpoint, error)
	attach     func(shellEndpoint) (*bridgeLease, error)
	refresh    func([]string)
	fault      func()
}

func newBridgeManager(selfPath string, l *listener) (*bridgeManager, error) {
	native, err := newNativeBridge(selfPath, l.hwnd)
	if err != nil {
		return nil, err
	}
	native.owners = l.cfg.hostedOwners
	m := &bridgeManager{quit: make(chan struct{}), done: make(chan struct{}), wake: l.bridgeRefresh,
		report: l.reportError, scan: currentShellEndpoint, attach: native.attach,
		fault:  func() { win.PostMessage(l.hwnd, msgBridgeFailed, 0, 0) },
		active: func() bool { l.mu.Lock(); defer l.mu.Unlock(); return !l.closing },
		refresh: func(paths []string) {
			postTaskbarCreated(paths, l.taskbarCreated)
			if l.cfg.hostedOwners != nil {
				l.cfg.hostedOwners.Range(func(_, value any) bool {
					owner := value.(hostedIconOwner)
					if Contains(paths, owner.path) && owner.refresh != nil {
						owner.refresh()
					}
					return true
				})
			}
		},
	}
	go m.run()
	return m, nil
}

func (m *bridgeManager) run() {
	defer close(m.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		refresh := false
		select {
		case <-m.quit:
			return
		case <-m.wake:
			refresh = true
		case <-ticker.C:
		}
		if err := m.poll(refresh); err != nil {
			m.report(err)
		}
	}
}

func (m *bridgeManager) cancel() error {
	if m.lease == nil {
		return nil
	}
	err := m.lease.cancel()
	m.lease = nil
	return err
}

// prepare installs dormantly. A failed/rejected selection can cancel without
// hiding any icons, and existing working selections stay routed on rollback.
func (m *bridgeManager) prepare(retry bool) (bool, error) {
	target, err := m.scan()
	if err != nil {
		if m.scanFailed && !retry {
			return false, nil
		}
		m.scanFailed = true
		return false, err
	}
	m.scanFailed = false
	if target == m.target && m.lease != nil {
		return false, nil
	}
	if target == m.target && m.failed != nil && !retry {
		return false, nil
	}
	if err := m.cancel(); err != nil {
		return false, err
	}
	m.target = target
	m.lease, m.failed = m.attach(target)
	return m.lease != nil, m.failed
}

func (m *bridgeManager) apply(paths []string, change func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("tray bridge is closed")
	}
	added := false
	if len(paths) != 0 {
		if !m.active() {
			return errors.New("tray recovery is unavailable")
		}
		var err error
		added, err = m.prepare(true)
		if err != nil {
			return err
		}
	}
	// Keep removals routed until their original visibility has been restored.
	// Additions are allowed at the receiver but not collected until change.
	if m.lease != nil && len(paths) != 0 {
		union := slices.Clone(m.paths)
		for _, path := range paths {
			if !Contains(union, path) {
				union = append(union, path)
			}
		}
		if err := m.lease.selectPaths(union); err != nil {
			if added {
				return errors.Join(err, m.cancel())
			}
			return err
		}
	}
	if err := change(); err != nil {
		if added {
			return errors.Join(err, m.cancel())
		}
		if m.lease != nil {
			return errors.Join(err, m.lease.selectPaths(m.paths))
		}
		return err
	}
	m.paths = slices.Clone(paths)
	var err error
	if len(paths) == 0 {
		err = m.cancel()
	} else {
		err = m.lease.selectPaths(paths)
		if err == nil && added {
			err = m.lease.activate()
		}
		if err != nil {
			err = m.fail(err)
		}
	}
	// Selection already committed; an activation failure is reported without
	// claiming rollback. Its dormant native receiver leaves original icons alone.
	if err != nil {
		m.report(err)
	}
	return nil
}

func (m *bridgeManager) poll(refresh bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.paths) == 0 || !m.active() {
		return nil
	}
	added, err := m.prepare(false)
	if err != nil {
		return err
	}
	if m.lease != nil && m.lease.alive != nil && !m.lease.alive() {
		return m.fail(errors.New("shell bridge stopped responding; collection released"))
	}
	if m.lease != nil && (added || refresh) {
		if err := m.lease.selectPaths(m.paths); err != nil {
			return m.fail(err)
		}
	}
	if added {
		if err := m.lease.activate(); err != nil {
			return m.fail(err)
		}
	}
	if m.lease != nil && (refresh || added) {
		m.refresh(m.paths)
	}
	return nil
}

// A lost receiver cannot retain a collection with no route for later updates.
func (m *bridgeManager) fail(cause error) error {
	m.failed = cause
	err := errors.Join(cause, m.cancel())
	if m.fault != nil {
		m.fault()
	}
	return err
}

// updateHosted is called before a hosted owner registers its first icon.
func (m *bridgeManager) updateHosted() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lease != nil {
		return m.lease.selectPaths(m.paths)
	}
	return nil
}

func (m *bridgeManager) close() error {
	m.mu.Lock()
	m.closed = true
	m.paths = nil
	err := m.cancel()
	close(m.quit)
	m.mu.Unlock()
	<-m.done
	return err
}
