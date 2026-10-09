package traybox

import (
	"bytes"
	"testing"
)

func TestHiddenRegistrationDoesNotActivateUninitializedLegacyState(t *testing.T) {
	raw := trayRequest{message: nimAdd, hwnd: 1, uid: 2, flags: nifMessage | nifIcon | nifTip,
		state: 0xaabbccdd, mask: 0x11223344}.bytes()
	original := append([]byte(nil), raw...)
	d, ok := parseTrayData(withHidden(raw))
	if !ok || d.Flags != nifMessage|nifIcon|nifTip|nifState || d.State != nisHidden || d.StateMask != nisHidden {
		t.Fatalf("unused legacy state became active: %+v", d)
	}
	if !bytes.Equal(raw, original) {
		t.Fatal("modified caller-owned data")
	}
}

func TestRectAnswerReturnsNativePositionAndSize(t *testing.T) {
	for _, bounds := range [][4]int32{{3126, 1392, 3158, 1440}, {-120, -60, -88, -12}} {
		position := rectAnswer(1, bounds[0], bounds[1], bounds[2], bounds[3])
		size := rectAnswer(2, bounds[0], bounds[1], bounds[2], bounds[3])
		if int16(position) != int16(bounds[0]) || int16(position>>16) != int16(bounds[1]) ||
			int16(size) != int16(bounds[2]-bounds[0]) || int16(size>>16) != int16(bounds[3]-bounds[1]) {
			t.Fatalf("bounds=%v produced position=0x%X size=0x%X; want origin and extent, not two corners", bounds, position, size)
		}
	}
}

func TestStateRequestPreservesUnknownShellExtensionBytes(t *testing.T) {
	raw := trayRequest{message: nimAdd, hwnd: 17, uid: 4, flags: nifTip | nifMessage | nifIcon, tip: "native"}.bytes()
	for i := trayDataSize; i < len(raw); i++ {
		raw[i] = byte(i%251 + 1)
	}
	before := append([]byte(nil), raw...)
	for _, hidden := range []bool{true, false} {
		request := stateRequest(raw, hidden)
		if !bytes.Equal(request[trayDataSize:], before[trayDataSize:]) {
			t.Fatal("state-only request corrupted shell extension bytes")
		}
		if !bytes.Equal(raw, before) {
			t.Fatal("state-only request mutated original registration")
		}
		d, ok := parseTrayData(request)
		if !ok || d.Flags != nifState || d.HWnd != 17 || d.UID != 4 || d.Callback != 0 || d.Icon != 0 {
			t.Fatalf("invalid state-only request: %+v", d)
		}
	}
}
