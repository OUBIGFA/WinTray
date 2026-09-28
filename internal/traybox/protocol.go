package traybox

import (
	"encoding/binary"
	"unicode/utf16"
)

// Shell_NotifyIcon hands each request to the window of class Shell_TrayWnd
// as WM_COPYDATA with dwData 1. The payload starts with a signature and the
// NIM_* message, followed by NOTIFYICONDATAW laid out with 32-bit handles so
// 32- and 64-bit programs share one format. Only the fields below are read;
// everything after them is copied unchanged.
const (
	copyDataNotifyIcon = 1
	copyDataIconRect   = 3

	trayDataSignature = 0x34753423

	offSignature = 0
	offMessage   = 4
	offHWnd      = 12
	offUID       = 16
	offFlags     = 20
	offCallback  = 24
	offIcon      = 28
	offTip       = 32
	tipChars     = 128
	offState     = 288
	offStateMask = 292
	offVersion   = 808
	offGUID      = 944
	trayDataSize = 964
)

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifState   = 0x08
	nifGUID    = 0x20

	nisHidden = 0x01
)

// trayData is the part of a Shell_NotifyIcon request the box works with.
type trayData struct {
	Message   uint32
	HWnd      uint32
	UID       uint32
	Flags     uint32
	Callback  uint32
	Icon      uint32
	Tip       string
	State     uint32
	StateMask uint32
	Version   uint32
	GUID      [16]byte
}

func parseTrayData(b []byte) (trayData, bool) {
	if len(b) < trayDataSize || le(b, offSignature) != trayDataSignature {
		return trayData{}, false
	}
	d := trayData{
		Message:   le(b, offMessage),
		HWnd:      le(b, offHWnd),
		UID:       le(b, offUID),
		Flags:     le(b, offFlags),
		Callback:  le(b, offCallback),
		Icon:      le(b, offIcon),
		State:     le(b, offState),
		StateMask: le(b, offStateMask),
		Version:   le(b, offVersion),
	}
	copy(d.GUID[:], b[offGUID:offGUID+16])
	if d.Flags&nifTip != 0 {
		tip := make([]uint16, 0, tipChars)
		for i := 0; i < tipChars; i++ {
			c := binary.LittleEndian.Uint16(b[offTip+2*i:])
			if c == 0 {
				break
			}
			tip = append(tip, c)
		}
		d.Tip = string(utf16.Decode(tip))
	}
	return d, true
}

func (d trayData) usesGUID() bool { return d.Flags&nifGUID != 0 && d.GUID != [16]byte{} }

// withHidden returns a copy of an NIM_ADD or state-changing NIM_MODIFY
// request that also hides the icon, so Explorer never shows it. Requests
// that leave the state alone keep the hidden state Explorer already has.
func withHidden(b []byte) []byte {
	d, ok := parseTrayData(b)
	if !ok || !(d.Message == nimAdd || d.Message == nimModify && d.Flags&nifState != 0) {
		return b
	}
	out := append([]byte(nil), b...)
	state, mask := uint32(0), uint32(0)
	if d.Flags&nifState != 0 {
		state, mask = d.State, d.StateMask
	}
	// Legacy callers may leave dwState/dwStateMask uninitialized when
	// NIF_STATE is absent. Adding that flag must not activate garbage bits.
	put(out, offFlags, d.Flags|nifState)
	put(out, offState, state|nisHidden)
	put(out, offStateMask, mask|nisHidden)
	return out
}

// stateRequest builds an NIM_MODIFY that only changes the hidden state of the
// icon a previous request of the same program described. Reusing that request
// keeps the layout Explorer expects on this version of Windows.
func stateRequest(template []byte, hidden bool) []byte {
	d, ok := parseTrayData(template)
	if !ok {
		return nil
	}
	// Preserve the opaque tail emitted by the installed shell32 (newer
	// Windows versions carry data beyond NOTIFYICONDATA). Only known,
	// unrequested fields are cleared; flags prevent replaying notifications.
	out := append([]byte(nil), template...)
	clear(out[offFlags:trayDataSize])
	put(out, offMessage, nimModify)
	put(out, offFlags, nifState|d.Flags&nifGUID)
	if hidden {
		put(out, offState, nisHidden)
	}
	put(out, offStateMask, nisHidden)
	copy(out[offGUID:offGUID+16], d.GUID[:])
	return out
}

func le(b []byte, off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }

func put(b []byte, off int, v uint32) { binary.LittleEndian.PutUint32(b[off:], v) }

// Shell_NotifyIconGetRect asks the Shell_TrayWnd window for an icon's
// position with dwData 3: a signature, which corner is wanted (1 top-left,
// 2 bottom-right), a size and reserved DWORD, then the icon identifier. This
// 40-byte wire structure is distinct from the NIM_* request layout above.
// The answer packs that corner's coordinates as MAKELONG(x, y).
const (
	rectCornerTopLeft     = 1
	rectCornerBottomRight = 2

	offRectCorner = 4
	offRectHWnd   = 16
	offRectUID    = 20
	offRectGUID   = 24
	rectQuerySize = 40
)

type rectQuery struct {
	Corner uint32
	ID     iconID
}

func parseRectQuery(b []byte) (rectQuery, bool) {
	if len(b) < rectQuerySize || le(b, offSignature) != trayDataSignature {
		return rectQuery{}, false
	}
	q := rectQuery{Corner: le(b, offRectCorner)}
	if q.Corner != rectCornerTopLeft && q.Corner != rectCornerBottomRight {
		return rectQuery{}, false
	}
	var guid [16]byte
	copy(guid[:], b[offRectGUID:offRectGUID+16])
	if guid != [16]byte{} {
		q.ID = iconID{guid: guid}
	} else {
		q.ID = iconID{hwnd: le(b, offRectHWnd), uid: le(b, offRectUID)}
	}
	return q, true
}

// rectAnswer packs one corner of a rectangle as Explorer answers it.
func rectAnswer(corner uint32, left, top, right, bottom int32) uintptr {
	x, y := left, top
	if corner == rectCornerBottomRight {
		x, y = right, bottom
	}
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}
