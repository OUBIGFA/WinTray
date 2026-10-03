//go:build windows

package branding

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"
	"sync"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// github.png is generated from assets/github.svg by build/svg2png.py.
//
//go:embed assets/github.png
var githubPNG []byte

var (
	appIconOnce sync.Once
	appIcon     *walk.Icon
	appIconErr  error

	githubIconOnce sync.Once
	githubIcon     *walk.Icon
	githubIconErr  error
)

func AppIcon() (*walk.Icon, error) {
	appIconOnce.Do(func() {
		// The linked resource contains the same logo. Native loading chooses
		// the window/tray size at each DPI instead of keeping an 800px bitmap
		// and decoding hundreds of thousands of pixels during every startup.
		// Group icon 1 is supplied by rsrc_windows_amd64.syso.
		appIcon, appIconErr = walk.NewIconFromResourceIdWithSize(1, walk.Size{
			Width:  int(win.GetSystemMetricsForDpi(win.SM_CXICON, 96)),
			Height: int(win.GetSystemMetricsForDpi(win.SM_CYICON, 96)),
		})
	})

	return appIcon, appIconErr
}

// GitHubIcon returns the GitHub mark sized for a toolbar-style button. The
// bitmap is authored at twice the logical size, so it stays sharp on high-DPI
// displays.
func GitHubIcon() (*walk.Icon, error) {
	githubIconOnce.Do(func() {
		githubIcon, githubIconErr = decodeIcon(githubPNG, 192)
	})

	return githubIcon, githubIconErr
}

func decodeIcon(data []byte, dpi int) (*walk.Icon, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return walk.NewIconFromImageForDPI(img, dpi)
}
