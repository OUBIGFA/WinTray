//go:build windows

package traybox

import (
	"errors"
	"image"
	"unsafe"

	"github.com/lxn/win"
)

// maxIconSide bounds the image kept for one icon; tray icons are far smaller.
const maxIconSide = 256

// iconImage renders an icon handle into an image the box can keep after the
// program destroys or replaces the handle. Drawing it once on black and once
// on white recovers its transparency whatever kind of icon it is: alpha,
// mask-based or monochrome.
func iconImage(icon win.HICON) (*image.NRGBA, error) {
	w, h, err := iconSize(icon)
	if err != nil {
		return nil, err
	}
	black, err := renderIcon(icon, w, h, 0x00)
	if err != nil {
		return nil, err
	}
	white, err := renderIcon(icon, w, h, 0xFF)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h*4; i += 4 {
		// Blue, green, red; the alpha channel of a GDI drawing is unreliable.
		diff := (int(white[i]) - int(black[i]) + int(white[i+1]) - int(black[i+1]) + int(white[i+2]) - int(black[i+2])) / 3
		a := min(max(255-diff, 0), 255)
		if a == 0 {
			continue
		}
		img.Pix[i+0] = unpremultiply(black[i+2], a)
		img.Pix[i+1] = unpremultiply(black[i+1], a)
		img.Pix[i+2] = unpremultiply(black[i+0], a)
		img.Pix[i+3] = byte(a)
	}
	return img, nil
}

func unpremultiply(c byte, a int) byte { return byte(min(int(c)*255/a, 255)) }

func iconSize(icon win.HICON) (int, int, error) {
	var info win.ICONINFO
	if !win.GetIconInfo(icon, &info) {
		return 0, 0, errors.New("GetIconInfo failed")
	}
	defer func() {
		if info.HbmColor != 0 {
			win.DeleteObject(win.HGDIOBJ(info.HbmColor))
		}
		if info.HbmMask != 0 {
			win.DeleteObject(win.HGDIOBJ(info.HbmMask))
		}
	}()
	var bm win.BITMAP
	source := info.HbmColor
	if source == 0 {
		source = info.HbmMask
	}
	if win.GetObject(win.HGDIOBJ(source), unsafe.Sizeof(bm), unsafe.Pointer(&bm)) == 0 {
		return 0, 0, errors.New("GetObject failed for the icon bitmap")
	}
	w, h := int(bm.BmWidth), int(bm.BmHeight)
	if info.HbmColor == 0 {
		// A monochrome icon stacks its AND and XOR masks.
		h /= 2
	}
	if w <= 0 || h <= 0 {
		return 0, 0, errors.New("empty icon")
	}
	return min(w, maxIconSide), min(h, maxIconSide), nil
}

// renderIcon draws the icon on an opaque background and returns its BGRA rows.
func renderIcon(icon win.HICON, w, h int, background byte) ([]byte, error) {
	dc := win.CreateCompatibleDC(0)
	if dc == 0 {
		return nil, errors.New("CreateCompatibleDC failed")
	}
	defer win.DeleteDC(dc)
	bmp, bits, err := newDIB(w, h)
	if err != nil {
		return nil, err
	}
	defer win.DeleteObject(win.HGDIOBJ(bmp))
	old := win.SelectObject(dc, win.HGDIOBJ(bmp))
	defer win.SelectObject(dc, old)
	for i := range bits {
		bits[i] = background
	}
	if !win.DrawIconEx(dc, 0, 0, icon, int32(w), int32(h), 0, 0, win.DI_NORMAL) {
		return nil, errors.New("DrawIconEx failed")
	}
	win.GdiFlush()
	return append([]byte(nil), bits...), nil
}

// newDIB creates a top-down 32-bit bitmap and returns its pixel memory.
func newDIB(w, h int) (win.HBITMAP, []byte, error) {
	header := win.BITMAPINFOHEADER{
		BiPlanes:      1,
		BiBitCount:    32,
		BiWidth:       int32(w),
		BiHeight:      -int32(h),
		BiCompression: win.BI_RGB,
	}
	header.BiSize = uint32(unsafe.Sizeof(header))
	var bits unsafe.Pointer
	bmp := win.CreateDIBSection(0, &header, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bmp == 0 || bits == nil {
		return 0, nil, errors.New("CreateDIBSection failed")
	}
	return bmp, unsafe.Slice((*byte)(bits), w*h*4), nil
}

// MenuBitmap scales an icon image to a size×size 32-bit bitmap with
// premultiplied alpha, which menus draw with transparency. The caller owns
// the bitmap and deletes it with win.DeleteObject.
func MenuBitmap(img *image.NRGBA, size int) (win.HBITMAP, error) {
	if img == nil || img.Bounds().Empty() || size <= 0 {
		return 0, errors.New("no icon image")
	}
	hbmp, pixels, err := newDIB(size, size)
	if err != nil {
		return 0, err
	}
	b := img.Bounds()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r, g, bl, a := average(img, b.Min.X+x*b.Dx()/size, b.Min.Y+y*b.Dy()/size,
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
