package traybox

import "testing"

func TestToggleAddsAndRemovesCaseInsensitively(t *testing.T) {
	paths, boxed := Toggle(nil, `C:\Apps\Listary.exe`)
	if !boxed || len(paths) != 1 {
		t.Fatalf("add: got %v boxed=%t", paths, boxed)
	}
	paths, boxed = Toggle(append(paths, `D:\QuickLook\QuickLook.exe`), `c:\apps\listary.EXE`)
	if boxed || len(paths) != 1 || paths[0] != `D:\QuickLook\QuickLook.exe` {
		t.Fatalf("remove: got %v boxed=%t", paths, boxed)
	}
	if !Contains(paths, `d:\quicklook\quicklook.exe`) || Contains(paths, `C:\Apps\Listary.exe`) {
		t.Fatalf("contains mismatch for %v", paths)
	}
}

func TestToggleDoesNotModifyInput(t *testing.T) {
	in := []string{`C:\a.exe`, `C:\b.exe`}
	_, _ = Toggle(in, `C:\a.exe`)
	if in[0] != `C:\a.exe` || in[1] != `C:\b.exe` {
		t.Fatalf("input changed: %v", in)
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
}
