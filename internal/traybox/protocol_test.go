package traybox

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// trayRequest builds a Shell_NotifyIcon payload the way shell32 sends it,
// including the reserved tail observed on Windows 11.
type trayRequest struct {
	message, hwnd, uid, flags, callback, icon, state, mask, version uint32
	tip                                                             string
	guid                                                            [16]byte
}

func (r trayRequest) bytes() []byte {
	b := make([]byte, 1484)
	put(b, offSignature, trayDataSignature)
	put(b, offMessage, r.message)
	put(b, 8, 956)
	put(b, offHWnd, r.hwnd)
	put(b, offUID, r.uid)
	put(b, offFlags, r.flags)
	put(b, offCallback, r.callback)
	put(b, offIcon, r.icon)
	for i, c := range utf16.Encode([]rune(r.tip)) {
		binary.LittleEndian.PutUint16(b[offTip+2*i:], c)
	}
	put(b, offState, r.state)
	put(b, offStateMask, r.mask)
	put(b, offVersion, r.version)
	copy(b[offGUID:], r.guid[:])
	return b
}

func TestParseTrayDataReadsTheFieldsTheBoxUses(t *testing.T) {
	raw := trayRequest{message: nimAdd, hwnd: 0x206BC, uid: 3, flags: nifMessage | nifIcon | nifTip,
		callback: 0x8001, icon: 0x230E89, tip: "QQ：在线", version: 4}.bytes()
	d, ok := parseTrayData(raw)
	if !ok {
		t.Fatal("valid request rejected")
	}
	if d.Message != nimAdd || d.HWnd != 0x206BC || d.UID != 3 || d.Callback != 0x8001 || d.Icon != 0x230E89 || d.Tip != "QQ：在线" || d.Version != 4 {
		t.Fatalf("parsed %+v", d)
	}
	if idOf(d) != (iconID{hwnd: 0x206BC, uid: 3}) {
		t.Fatalf("id = %+v", idOf(d))
	}
	// Without NIF_TIP the tooltip buffer is not meaningful.
	raw = trayRequest{message: nimModify, flags: nifIcon, tip: "stale"}.bytes()
	if d, _ := parseTrayData(raw); d.Tip != "" {
		t.Fatalf("tooltip read without NIF_TIP: %q", d.Tip)
	}
	for name, bad := range map[string][]byte{
		"short":     raw[:trayDataSize-1],
		"signature": append([]byte{0, 0, 0, 0}, raw[4:]...),
	} {
		if _, ok := parseTrayData(bad); ok {
			t.Errorf("%s request accepted", name)
		}
	}
}

func TestGUIDIconsAreIdentifiedByGUID(t *testing.T) {
	guid := [16]byte{1, 2, 3}
	d, _ := parseTrayData(trayRequest{hwnd: 5, uid: 1, flags: nifGUID, guid: guid}.bytes())
	if idOf(d) != (iconID{guid: guid}) {
		t.Fatalf("id = %+v", idOf(d))
	}
	// A GUID present without NIF_GUID does not identify the icon.
	d, _ = parseTrayData(trayRequest{hwnd: 5, uid: 1, guid: guid}.bytes())
	if idOf(d) != (iconID{hwnd: 5, uid: 1}) {
		t.Fatalf("id = %+v", idOf(d))
	}
}

func TestWithHiddenHidesAddedIconsAndStateChanges(t *testing.T) {
	cases := []struct {
		name      string
		req       trayRequest
		changed   bool
		wantState uint32
	}{
		{"add", trayRequest{message: nimAdd, flags: nifIcon | nifMessage}, true, nisHidden},
		{"add keeps other state bits", trayRequest{message: nimAdd, flags: nifState, state: 0x02, mask: 0x02}, true, 0x02 | nisHidden},
		{"program shows its icon", trayRequest{message: nimModify, flags: nifState, mask: nisHidden}, true, nisHidden},
		{"icon update keeps state", trayRequest{message: nimModify, flags: nifIcon | nifTip}, false, 0},
		{"delete", trayRequest{message: nimDelete}, false, 0},
		{"version", trayRequest{message: nimSetVersion, version: 4}, false, 0},
	}
	for _, c := range cases {
		raw := c.req.bytes()
		original := append([]byte(nil), raw...)
		out := withHidden(raw)
		if string(raw) != string(original) {
			t.Fatalf("%s: input modified", c.name)
		}
		d, _ := parseTrayData(out)
		if !c.changed {
			if string(out) != string(raw) {
				t.Errorf("%s: request changed", c.name)
			}
			continue
		}
		if d.Flags&nifState == 0 || d.State != c.wantState || d.StateMask&nisHidden == 0 {
			t.Errorf("%s: flags=%#x state=%#x mask=%#x", c.name, d.Flags, d.State, d.StateMask)
		}
		if d.Flags&^nifState != c.req.flags&^nifState || len(out) != len(raw) {
			t.Errorf("%s: other fields changed", c.name)
		}
	}
}

func TestStateRequestOnlyChangesTheHiddenState(t *testing.T) {
	guid := [16]byte{9, 8, 7}
	template := trayRequest{message: nimAdd, hwnd: 0x10, uid: 7, flags: nifMessage | nifIcon | nifTip | nifGUID,
		callback: 0x8007, icon: 0x99, tip: "tip", version: 4, guid: guid}.bytes()
	for _, hidden := range []bool{true, false} {
		out := stateRequest(template, hidden)
		d, ok := parseTrayData(out)
		if !ok || len(out) != len(template) {
			t.Fatalf("hidden=%t: invalid request", hidden)
		}
		want := uint32(0)
		if hidden {
			want = nisHidden
		}
		if d.Message != nimModify || d.Flags != nifState|nifGUID || d.State != want || d.StateMask != nisHidden {
			t.Errorf("hidden=%t: %+v", hidden, d)
		}
		if d.HWnd != 0x10 || d.UID != 7 || d.GUID != guid || le(out, 8) != 956 {
			t.Errorf("hidden=%t: icon identity lost: %+v", hidden, d)
		}
		if d.Icon != 0 || d.Callback != 0 || d.Tip != "" {
			t.Errorf("hidden=%t: carries more than the state: %+v", hidden, d)
		}
	}
	if stateRequest([]byte("junk"), true) != nil {
		t.Error("invalid template produced a request")
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		icon Icon
		want string
	}{
		{Icon{ExePath: `C:\x\PinToDesk.exe`, Tooltip: " PTD 1.0.4 \nsecond line"}, "PTD 1.0.4"},
		{Icon{ExePath: `C:\x\karing.exe`}, "karing"},
		{Icon{ExePath: `C:\x\Cherry Studio.exe`, Tooltip: "  "}, "Cherry Studio"},
	}
	for _, c := range cases {
		if got := c.icon.DisplayName(); got != c.want {
			t.Errorf("%q: got %q want %q", c.icon.ExePath, got, c.want)
		}
	}
	if !Contains([]string{`C:\Apps\QQ.exe`}, `c:\apps\qq.EXE`) || Contains(nil, `C:\a.exe`) {
		t.Error("Contains must compare paths case-insensitively")
	}
}
