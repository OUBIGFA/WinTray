//go:build windows

package shellapp

import (
	"errors"
	"image"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

type imageFactoryVtbl struct {
	unknownVtbl
	GetImage uintptr
}

type imageFactory struct{ vtbl *imageFactoryVtbl }

var iidImageFactory = windows.GUID{Data1: 0xbcc18b79, Data2: 0xba16, Data3: 0x442f, Data4: [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b}}

func Icon(path string, size int) (*image.RGBA, error) {
	if size < 1 || size > 256 {
		return nil, errors.New("application icon size must be between 1 and 256 pixels")
	}
	leave, err := enterCOM()
	if err != nil {
		return nil, err
	}
	defer leave()
	item, err := openShellItem(path)
	if err != nil {
		return nil, err
	}
	defer release(unsafe.Pointer(item))
	return iconFromItem(item, size)
}

func iconFromItem(item *shellItem, size int) (*image.RGBA, error) {
	var factory *imageFactory
	result, _, _ := syscall.SyscallN(item.vtbl.QueryInterface, uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(&iidImageFactory)), uintptr(unsafe.Pointer(&factory)))
	if err := hresult(result, "get application icon factory"); err != nil {
		return nil, err
	}
	defer release(unsafe.Pointer(factory))
	var bitmap win.HBITMAP
	const iconOnly = 0x4
	if unsafe.Sizeof(uintptr(0)) == 8 {
		dimensions := uint64(uint32(size)) | uint64(uint32(size))<<32
		result, _, _ = syscall.SyscallN(factory.vtbl.GetImage, uintptr(unsafe.Pointer(factory)), uintptr(dimensions), iconOnly, uintptr(unsafe.Pointer(&bitmap)))
	} else {
		result, _, _ = syscall.SyscallN(factory.vtbl.GetImage, uintptr(unsafe.Pointer(factory)), uintptr(size), uintptr(size), iconOnly, uintptr(unsafe.Pointer(&bitmap)))
	}
	if err := hresult(result, "read application icon"); err != nil {
		return nil, err
	}
	defer win.DeleteObject(win.HGDIOBJ(bitmap))
	var dimensions win.BITMAP
	if win.GetObject(win.HGDIOBJ(bitmap), unsafe.Sizeof(dimensions), unsafe.Pointer(&dimensions)) == 0 {
		return nil, errors.New("read application icon dimensions")
	}
	width, height := int(dimensions.BmWidth), int(dimensions.BmHeight)
	if width <= 0 || height <= 0 || width > 256 || height > 256 {
		return nil, errors.New("application icon has invalid dimensions")
	}
	icon := image.NewRGBA(image.Rect(0, 0, width, height))
	info := win.BITMAPINFO{BmiHeader: win.BITMAPINFOHEADER{
		BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(width), BiHeight: -int32(height),
		BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB, BiSizeImage: uint32(len(icon.Pix)),
	}}
	dc := win.GetDC(0)
	if dc == 0 {
		return nil, errors.New("create application icon drawing context")
	}
	defer win.ReleaseDC(0, dc)
	if win.GetDIBits(dc, bitmap, 0, uint32(height), &icon.Pix[0], &info, win.DIB_RGB_COLORS) != int32(height) {
		return nil, errors.New("read application icon pixels")
	}
	for offset := 0; offset < len(icon.Pix); offset += 4 {
		icon.Pix[offset], icon.Pix[offset+2] = icon.Pix[offset+2], icon.Pix[offset]
	}
	return icon, nil
}
