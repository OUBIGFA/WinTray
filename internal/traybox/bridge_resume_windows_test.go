//go:build windows

package traybox

import (
	"github.com/lxn/win"
	"testing"
)

func TestObservationRetryDoesNotOverwriteNewerState(t *testing.T) {
	_, l := selectedListener(t)
	owner := uint32(l.hwnd)
	id := iconID{hwnd: owner, uid: 7}
	l.record("selected", trayData{HWnd: owner, UID: 7, Message: nimAdd}, trayRequest{hwnd: owner, uid: 7}.bytes(), nil)
	batch := make([]byte, 52)
	put(batch, 0, 4)
	put(batch, 8, nimModify)
	put(batch, 12, owner)
	put(batch, 16, 7)
	put(batch, 20, nifState)
	put(batch, 24, nisHidden)
	put(batch, 28, nisHidden)
	if l.observeNativeBatch(batch) != 1 {
		t.Fatal("batch rejected")
	}
	l.icons[id].hidden = false // a newer native registration arrived after resume
	if l.observeNativeBatch(batch) != 1 || l.icons[id].hidden {
		t.Fatal("late acknowledgement retry overwrote new state")
	}
	// Receiver timed out after our first ACK and appended a later deletion.
	// Retried prefix must be ignored, but its new suffix must be applied.
	batch = append(batch, make([]byte, 44)...)
	put(batch, 52, nimDelete)
	put(batch, 56, owner)
	put(batch, 60, 7)
	if l.observeNativeBatch(batch) != 1 || l.icons[id] != nil {
		t.Fatal("enlarged retry lost its new deletion")
	}
}

func TestNewShellEpochResetsPauseSequence(t *testing.T) {
	_, l := selectedListener(t)
	// Send the public message through the owner thread; a new Explorer may
	// start again at token 1 while the host retains the same broker HWND.
	batch := make([]byte, 52)
	put(batch, 0, 800)
	if l.observeNativeBatch(batch) != 1 {
		t.Fatal("old shell sequence rejected")
	}
	win.SendMessage(l.hwnd, msgBridgeEpoch, 0, 0)
	put(batch, 0, 1)
	if l.observeNativeBatch(batch) != 1 || l.observedToken != 1 {
		t.Fatal("new shell sequence rejected")
	}
}

func TestObservedNativeStateDoesNotReviveHiddenOrDeletedIcons(t *testing.T) {
	_, l := selectedListener(t)
	owner := uint32(l.hwnd)
	for _, uid := range []uint32{1, 2, 3} {
		l.record("selected", trayData{HWnd: owner, UID: uid, Message: nimAdd}, trayRequest{hwnd: owner, uid: uid}.bytes(), nil)
	}
	data := make([]byte, 88)
	put(data, 0, nimModify)
	put(data, 4, owner)
	put(data, 8, 1)
	put(data, 12, nifState)
	put(data, 16, nisHidden)
	put(data, 20, nisHidden)
	put(data, 44, nimDelete)
	put(data, 48, owner)
	put(data, 52, 2)
	if l.observeNative(data) != 1 {
		t.Fatal("native observations rejected")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.icons[iconID{hwnd: owner, uid: 1}].hidden {
		t.Fatal("owner-hidden icon kept stale visible state")
	}
	if l.icons[iconID{hwnd: owner, uid: 2}] != nil {
		t.Fatal("owner-deleted icon kept recovery record")
	}
	if l.icons[iconID{hwnd: owner, uid: 3}] == nil {
		t.Fatal("untouched collection was lost")
	}
}
