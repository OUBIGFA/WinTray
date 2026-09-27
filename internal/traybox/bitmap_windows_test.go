//go:build windows

package traybox

import (
	"image"
	"image/color"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

// newTestIcon builds a size×size icon. With alpha, color holds straight BGRA
// pixels; without, transparency comes from the mask (1 = transparent).
func newTestIcon(t *testing.T, size int, pixel func(x, y int) (bgra [4]byte, transparent bool), alpha bool) win.HICON {
	t.Helper()
	colorBmp, bits, err := newDIB(size, size)
	if err != nil {
		t.Fatal(err)
	}
	defer win.DeleteObject(win.HGDIOBJ(colorBmp))
	stride := (size + 15) / 16 * 2 // monochrome rows are WORD-aligned
	mask := make([]byte, stride*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			p, transparent := pixel(x, y)
			if !alpha {
				p[3] = 0
				if transparent {
					p = [4]byte{}
					mask[y*stride+x/8] |= 0x80 >> (x % 8)
				}
			}
			copy(bits[(y*size+x)*4:], p[:])
		}
	}
	maskBmp := win.CreateBitmap(int32(size), int32(size), 1, 1, unsafe.Pointer(&mask[0]))
	if maskBmp == 0 {
		t.Fatal("CreateBitmap failed")
	}
	defer win.DeleteObject(win.HGDIOBJ(maskBmp))
	icon := win.CreateIconIndirect(&win.ICONINFO{FIcon: 1, HbmMask: maskBmp, HbmColor: colorBmp})
	if icon == 0 {
		t.Fatal("CreateIconIndirect failed")
	}
	t.Cleanup(func() { win.DestroyIcon(icon) })
	return icon
}

func near(a, b uint8) bool { d := int(a) - int(b); return d >= -3 && d <= 3 }

func TestIconImageKeepsColorsAndAlpha(t *testing.T) {
	// Left half opaque red, right half half-transparent blue.
	icon := newTestIcon(t, 16, func(x, y int) ([4]byte, bool) {
		if x < 8 {
			return [4]byte{0, 0, 255, 255}, false
		}
		return [4]byte{255, 0, 0, 128}, false
	}, true)
	img, err := iconImage(icon)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 16, 16) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	if c := img.NRGBAAt(2, 5); c.R < 250 || c.G > 5 || c.B > 5 || c.A < 250 {
		t.Errorf("opaque red became %v", c)
	}
	if c := img.NRGBAAt(12, 5); c.B < 245 || c.R > 10 || c.A < 124 || c.A > 132 {
		t.Errorf("half-transparent blue became %v", c)
	}
}

func TestIconImageUsesTheMaskOfIconsWithoutAlpha(t *testing.T) {
	icon := newTestIcon(t, 16, func(x, y int) ([4]byte, bool) {
		return [4]byte{0, 200, 0, 0}, (x+y)%2 == 0
	}, false)
	img, err := iconImage(icon)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []image.Point{{0, 0}, {1, 1}, {5, 3}} {
		if c := img.NRGBAAt(p.X, p.Y); c.A != 0 {
			t.Errorf("masked pixel %v = %v, want transparent", p, c)
		}
	}
	for _, p := range []image.Point{{1, 0}, {0, 1}, {6, 3}} {
		if c := img.NRGBAAt(p.X, p.Y); c.A != 255 || !near(c.G, 200) || c.R != 0 || c.B != 0 {
			t.Errorf("opaque pixel %v = %v, want solid green", p, c)
		}
	}
}

func TestIconImageRejectsInvalidHandles(t *testing.T) {
	if _, err := iconImage(0); err == nil {
		t.Fatal("expected an error for a null icon")
	}
}

func TestMenuBitmapPremultipliesAndScales(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 16; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 128}) // half-transparent red
		}
	}
	hbmp, err := MenuBitmap(src, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer win.DeleteObject(win.HGDIOBJ(hbmp))

	var dib win.DIBSECTION
	if win.GetObject(win.HGDIOBJ(hbmp), unsafe.Sizeof(dib), unsafe.Pointer(&dib)) == 0 {
		t.Fatal("GetObject failed")
	}
	if dib.DsBm.BmWidth != 16 || dib.DsBm.BmHeight != 16 || dib.DsBm.BmBitsPixel != 32 {
		t.Fatalf("bitmap %dx%d@%d", dib.DsBm.BmWidth, dib.DsBm.BmHeight, dib.DsBm.BmBitsPixel)
	}
	pixels := unsafe.Slice((*byte)(dib.DsBm.BmBits), 16*16*4)
	left, right := pixels[0:4], pixels[15*4:16*4]
	// BGRA, premultiplied: red 255 at alpha 128 becomes 128.
	if left[2] != 128 || left[3] != 128 || left[0] != 0 {
		t.Errorf("left pixel %v", left)
	}
	if right[3] != 0 || right[2] != 0 {
		t.Errorf("right pixel %v", right)
	}
}

func TestMenuBitmapRejectsMissingImage(t *testing.T) {
	if _, err := MenuBitmap(nil, 16); err == nil {
		t.Fatal("expected error")
	}
	if _, err := MenuBitmap(image.NewNRGBA(image.Rectangle{}), 16); err == nil {
		t.Fatal("expected error for an empty image")
	}
}
