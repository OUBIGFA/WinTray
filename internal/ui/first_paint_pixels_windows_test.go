//go:build windows

package ui

import "testing"

// A narrow strip can contain many antialiased text colors. The old whole-
// window color count accepted exactly the Windows 10 failure reported in #8.
func TestClientPixelCoverageRejectsNarrowRendering(t *testing.T) {
	const width, height = 400, 200
	for _, tc := range []struct {
		name       string
		paintLeft  bool
		paintRight bool
		wantPaint  bool
	}{
		{"blank", false, false, false},
		{"only left 10 pixels", true, false, false},
		{"only right side", false, true, false},
		{"both sides painted", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pixels := make([]uint32, width*height)
			for i := range pixels {
				pixels[i] = 0xFFFFFF
			}
			for y := 20; y < 40; y++ {
				for x := 0; x < 10; x++ {
					color := uint32(0x101010 * (x + 1))
					if tc.paintLeft {
						pixels[y*width+x] = color
					}
					if tc.paintRight {
						pixels[y*width+width/2+x] = color
					}
				}
			}
			if err := checkClientPixelCoverage(pixels, width); (err == nil) != tc.wantPaint {
				t.Fatalf("pixel coverage = %v, want painted=%t", err, tc.wantPaint)
			}
		})
	}
}
