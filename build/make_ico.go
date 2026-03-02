package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
)

type icoHeader struct {
	Reserved uint16
	Type     uint16
	Count    uint16
}

type icoDirEntry struct {
	Width       uint8
	Height      uint8
	ColorCount  uint8
	Reserved    uint8
	Planes      uint16
	BitCount    uint16
	BytesInRes  uint32
	ImageOffset uint32
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run build/make_ico.go <input.png> <output.ico>")
		os.Exit(2)
	}

	inPath := os.Args[1]
	outPath := os.Args[2]

	pngBytes, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read png failed: %v\n", err)
		os.Exit(1)
	}

	decoded, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "decode png failed: %v\n", err)
		os.Exit(1)
	}

	imgBounds := decoded.Bounds()
	imgConfig := image.Config{Width: imgBounds.Dx(), Height: imgBounds.Dy()}

	if imgConfig.Width <= 0 || imgConfig.Height <= 0 {
		fmt.Fprintf(os.Stderr, "png size is invalid: %dx%d\n", imgConfig.Width, imgConfig.Height)
		os.Exit(1)
	}

	target := 256
	if imgConfig.Width <= 256 && imgConfig.Height <= 256 {
		target = max(imgConfig.Width, imgConfig.Height)
	}

	if imgConfig.Width != target || imgConfig.Height != target {
		decoded = resizeNearest(decoded, target, target)
		var buffer bytes.Buffer
		if err = png.Encode(&buffer, decoded); err != nil {
			fmt.Fprintf(os.Stderr, "encode resized png failed: %v\n", err)
			os.Exit(1)
		}
		pngBytes = buffer.Bytes()
		imgConfig.Width = target
		imgConfig.Height = target
	}

	if err = writeICO(outPath, pngBytes, imgConfig.Width, imgConfig.Height); err != nil {
		fmt.Fprintf(os.Stderr, "write ico failed: %v\n", err)
		os.Exit(1)
	}
}

func writeICO(outPath string, pngBytes []byte, width int, height int) error {
	file, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer file.Close()

	header := icoHeader{Reserved: 0, Type: 1, Count: 1}
	entry := icoDirEntry{
		Width:       dimToICO(width),
		Height:      dimToICO(height),
		ColorCount:  0,
		Reserved:    0,
		Planes:      1,
		BitCount:    32,
		BytesInRes:  uint32(len(pngBytes)),
		ImageOffset: uint32(binary.Size(icoHeader{}) + binary.Size(icoDirEntry{})),
	}

	if err = binary.Write(file, binary.LittleEndian, header); err != nil {
		return err
	}
	if err = binary.Write(file, binary.LittleEndian, entry); err != nil {
		return err
	}

	_, err = io.Copy(file, bytes.NewReader(pngBytes))
	return err
}

func dimToICO(size int) uint8 {
	if size == 256 {
		return 0
	}
	return uint8(size)
}

func resizeNearest(src image.Image, width int, height int) image.Image {
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := srcBounds.Min.Y + (y*srcHeight)/height
		for x := 0; x < width; x++ {
			sx := srcBounds.Min.X + (x*srcWidth)/width
			dst.Set(x, y, color.NRGBAModel.Convert(src.At(sx, sy)))
		}
	}

	return dst
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
