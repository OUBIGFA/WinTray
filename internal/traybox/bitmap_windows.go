//go:build windows

package traybox

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"unsafe"

	"github.com/lxn/win"
)

// MenuBitmap turns an icon snapshot into a size×size 32-bit bitmap with
// premultiplied alpha, which menus draw with transparency. The caller owns
// the bitmap and deletes it with win.DeleteObject.
func MenuBitmap(snapshot []byte, size int) (win.HBITMAP, error) {
	if len(snapshot) == 0 || size <= 0 {
		return 0, errors.New("no icon snapshot")
	}
	src, err := png.Decode(bytes.NewReader(snapshot))
	if err != nil {
		return 0, err
	}
	rgba := image.NewNRGBA(src.Bounds())
	draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Src)

	header := win.BITMAPINFOHEADER{
		BiPlanes:      1,
		BiBitCount:    32,
		BiWidth:       int32(size),
		BiHeight:      -int32(size), // top-down rows
		BiCompression: win.BI_RGB,
	}
	header.BiSize = uint32(unsafe.Sizeof(header))
	var bits unsafe.Pointer
	hbmp := win.CreateDIBSection(0, &header, win.DIB_RGB_COLORS, &bits, 0, 0)
	if hbmp == 0 || bits == nil {
		return 0, errors.New("CreateDIBSection failed")
	}
	pixels := unsafe.Slice((*byte)(bits), size*size*4)
	b := rgba.Bounds()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r, g, bl, a := average(rgba, b.Min.X+x*b.Dx()/size, b.Min.Y+y*b.Dy()/size,
				b.Min.X+(x+1)*b.Dx()/size, b.Min.Y+(y+1)*b.Dy()/size)
			i := (y*size + x) * 4
			pixels[i+0] = byte(bl * a / 255)
			pixels[i+1] = byte(g * a / 255)
			pixels[i+2] = byte(r * a / 255)
			pixels[i+3] = byte(a)
		}
	}
	return hbmp, nil
}

// average box-filters the source pixels in [x0,x1)×[y0,y1), weighting color
// by alpha so transparent pixels do not darken the edges.
func average(img *image.NRGBA, x0, y0, x1, y1 int) (r, g, b, a int) {
	x1, y1 = max(x1, x0+1), max(y1, y0+1)
	var sr, sg, sb, sa, n int
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := img.NRGBAAt(x, y)
			sr += int(c.R) * int(c.A)
			sg += int(c.G) * int(c.A)
			sb += int(c.B) * int(c.A)
			sa += int(c.A)
			n++
		}
	}
	if sa == 0 {
		return 0, 0, 0, 0
	}
	return sr / sa, sg / sa, sb / sa, sa / n
}
