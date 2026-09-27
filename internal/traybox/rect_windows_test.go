//go:build windows

package traybox

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

// This wire layout is independent of the parser constants. Shell32's rect
// request is not the NOTIFYICONDATA layout: it includes a reserved DWORD.
func rectWire(owner, uid, corner uint32, guid [16]byte) []byte {
	b := make([]byte, 40)
	put(b, 0, 0x34753423)
	put(b, 4, corner)
	put(b, 8, 32)
	put(b, 16, owner)
	put(b, 20, uid)
	copy(b[24:], guid[:])
	return b
}

func queryListenerRect(l *listener, b []byte) uintptr {
	cds := copyDataStruct{DwData: copyDataIconRect, CbData: uint32(len(b)), LpData: uintptr(unsafe.Pointer(&b[0]))}
	result := win.SendMessage(l.hwnd, win.WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds)))
	runtime.KeepAlive(b)
	return result
}

func TestCollectedIconPositionUsesShell32WireLayout(t *testing.T) {
	for i, guid := range [][16]byte{{}, {1, 2, 3, 4, 5}} {
		t.Run([]string{"HWND and uID", "GUID"}[i], func(t *testing.T) {
			f, l := selectedListener(t)
			flags := uint32(nifMessage)
			if guid != [16]byte{} {
				flags |= nifGUID
			}
			owner := uint32(f.program)
			send(l, trayRequest{message: nimAdd, hwnd: owner, uid: 37, flags: flags, callback: testCallback, guid: guid})
			f.result.Store(0) // Explorer cannot place a NIS_HIDDEN icon.
			a := queryListenerRect(l, rectWire(owner, 37, 1, guid))
			b := queryListenerRect(l, rectWire(owner, 37, 2, guid))
			left, top := int16(a), int16(a>>16)
			right, bottom := int16(b), int16(b>>16)
			if right <= left || bottom <= top {
				t.Errorf("collected icon has no usable rectangle: (%d,%d)-(%d,%d), guid=%x", left, top, right, bottom, guid)
			}
			// A query for another icon must not invent a position.
			if got := queryListenerRect(l, rectWire(owner, 99, 1, [16]byte{})); got != 0 {
				t.Errorf("unknown icon returned a fabricated position: %#x", got)
			}
			f.result.Store(1)
			if err := l.stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
