//go:build windows

package app

import (
	"sync"

	"wintray/internal/tray"
)

// pendingHostedWindows owns recovery until a hidden program has been handed to
// the UI-thread tray host. Launch workers may finish after the UI loop stops,
// so their results must remain recoverable without running a queued callback.
type pendingHostedWindows struct {
	mu      sync.Mutex
	windows map[uint32]tray.HostedWindow
}

func (p *pendingHostedWindows) remember(w tray.HostedWindow) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.windows == nil {
		p.windows = make(map[uint32]tray.HostedWindow)
	}
	p.windows[w.ProcessID] = w
}

func (p *pendingHostedWindows) forget(w tray.HostedWindow) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.windows, w.ProcessID)
}

// restoreAll is called after launch workers have stopped. The callback must
// restore only this program's window, without activating it.
func (p *pendingHostedWindows) restoreAll(restore func(tray.HostedWindow)) {
	p.mu.Lock()
	pending := p.windows
	p.windows = nil
	p.mu.Unlock()
	for _, w := range pending {
		restore(w)
	}
}
