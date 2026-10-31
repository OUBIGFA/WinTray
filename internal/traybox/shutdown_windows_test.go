//go:build windows

package traybox

import (
	"os"
	"sync/atomic"
	"testing"

	"github.com/lxn/win"
)

func TestBoxCloseIsTerminalWhenExplorerRejectsRestoration(t *testing.T) {
	f, l := selectedListener(t)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 4, flags: nifTip, tip: "exit fixture"})
	icon := l.snapshot()[0]
	box := &Box{listener: l, paths: []string{icon.ExePath}, logger: quietLogger{}, config: l.cfg}
	f.result.Store(0)
	defer f.result.Store(1)
	if err := box.Close(); err == nil {
		t.Fatal("restoration failure was not reported for logging")
	}
	if box.HasSelection() || box.listener != nil || activeListener.Load() != nil || isWindow(l.hwnd) {
		t.Fatal("terminal close retained a live collector after failed restoration")
	}
	if err := box.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
}

// Explorer rejects every request made while the broker is alive. Closing
// must still release it and preserve the companion's acknowledged snapshot.
func TestRejectedShutdownHandsRecoveryToCompanion(t *testing.T) {
	f := startFakeShell(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, l, _ := startCrashCollector(t, f.target, self)
	send(l, trayRequest{message: nimAdd, hwnd: uint32(f.program), uid: 91, flags: nifTip, tip: "exit recovery"})
	var rejected, restored atomic.Int32
	f.setReply(func(d trayData) uintptr {
		if isShown(d) {
			if isWindow(l.hwnd) {
				rejected.Add(1)
				return 0
			}
			restored.Add(1)
		}
		return 1
	})
	win.PostMessage(l.hwnd, win.WM_CLOSE, 0, 0)
	waitFor(t, "broker closes despite rejected restoration", func() bool { return !isWindow(l.hwnd) })
	waitFor(t, "companion restores after broker closes", func() bool { return restored.Load() > 0 })
	if rejected.Load() == 0 {
		t.Fatal("fixture did not reject the in-process restoration")
	}
}
