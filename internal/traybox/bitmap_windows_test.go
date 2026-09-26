//go:build windows

package traybox

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"unsafe"

	"github.com/lxn/win"
)

func TestMenuBitmapPremultipliesAndScales(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if x < 16 {
				src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 128}) // half-transparent red
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	hbmp, err := MenuBitmap(buf.Bytes(), 16)
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

func TestMenuBitmapRejectsMissingSnapshot(t *testing.T) {
	if _, err := MenuBitmap(nil, 16); err == nil {
		t.Fatal("expected error")
	}
	if _, err := MenuBitmap([]byte("not a png"), 16); err == nil {
		t.Fatal("expected error")
	}
}
