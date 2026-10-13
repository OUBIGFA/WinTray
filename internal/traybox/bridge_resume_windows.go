//go:build windows

package traybox

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/lxn/win"
)

func bridgeWord(b []byte) uint64 {
	return uint64(binary.LittleEndian.Uint32(b)) | uint64(binary.LittleEndian.Uint32(b[4:]))<<32
}

// Each journal batch is acknowledged once even when a slow SendMessageTimeout
// caused the shell to retry it. Late duplicate batches cannot revive old state.
func (l *listener) observeNativeBatch(data []byte) uintptr {
	token, offset := le(data, 0), le(data, 4)
	if token != l.observedToken {
		if l.observedToken != 0 && int32(token-l.observedToken) <= 0 {
			return 1
		}
		l.observedToken, l.observedOffset = token, 0
	}
	count := uint32((len(data) - 8) / 44)
	if offset > l.observedOffset {
		return 0
	}
	// A retry can include newer observations appended after the first send.
	// Skip only the acknowledged prefix, not the whole enlarged batch.
	skip := l.observedOffset - offset
	if skip >= count {
		return 1
	}
	l.observeNative(data[8+skip*44:])
	l.observedOffset = offset + count
	return 1
}

// observeNative applies only state/deletion metadata from packets that reached
// Explorer while Go was paused. Do not replay native payloads or retain a stale
// recovery entry that could resurrect an icon its owner has hidden/deleted.
func (l *listener) observeNative(data []byte) uintptr {
	l.mu.Lock()
	defer l.mu.Unlock()
	for len(data) >= 44 {
		d := trayData{Message: le(data, 0), HWnd: le(data, 4), UID: le(data, 8), Flags: le(data, 12), State: le(data, 16), StateMask: le(data, 20), Version: le(data, 24)}
		copy(d.GUID[:], data[28:44])
		data = data[44:]
		id := idOf(d)
		t := l.icons[id]
		if t == nil {
			continue
		} // native ADD will be collected on refresh
		l.seq++
		t.revision = l.seq
		switch d.Message {
		case nimDelete:
			delete(l.icons, id)
		case nimAdd, nimModify:
			if d.Message == nimAdd {
				t.hidden = false
			}
			if d.Flags&nifState != 0 && d.StateMask&nisHidden != 0 {
				t.hidden = d.State&nisHidden != 0
			}
		case nimSetVersion:
			t.version = d.Version
		}
	}
	return 1
}

// Resume only after the broker has unwound the slow request. The native lease
// never changes, so no reinjection or user action is needed. A bounded native
// handshake rejects stale notifications from an earlier pause.
func (l *listener) resumeBridge() {
	if l.resumePending == 0 {
		return
	}
	l.mu.Lock()
	closing := l.closing
	l.mu.Unlock()
	if closing {
		return
	}
	token := l.resumePending
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], token)
	cds := copyDataStruct{bridgeResumeData, 8, uintptr(unsafe.Pointer(&data[0]))}
	var result uintptr
	var pin runtime.Pinner
	pin.Pin(&data[0])
	pin.Pin(&cds)
	pin.Pin(&result)
	defer pin.Unpin()
	tray := l.cfg.target(l.hwnd)
	ok, _, _ := procSendMessageTimeoutW.Call(uintptr(tray), win.WM_COPYDATA, uintptr(l.hwnd), uintptr(unsafe.Pointer(&cds)), smtoAbortIfHung, 500, uintptr(unsafe.Pointer(&result)))
	if ok == 0 || result == 0 {
		return
	} // receiver timer repeats the bounded handshake
	if token == l.resumedToken { // duplicate notification, already refreshed
		l.resumePending = 0
		return
	}
	// Even if the prior handshake completed after our wait timed out, the
	// reply (2) confirms it is no longer paused. Still refresh exactly once.
	l.resumedToken = token
	l.logger.Warn(fmt.Sprintf("tray box: delayed request recovered (token=%d error=%d); refreshing selected icons", token, l.resumeError))
	l.resumePending = 0
	l.mu.Lock()
	// The receiver delivered native visibility/deletion state during the
	// handshake. Refresh image/callback fields from the owners themselves.
	for path := range l.paths {
		l.pending[path] = true
	}
	l.mu.Unlock()
	win.PostMessage(l.hwnd, msgSync, 0, 0)
}
