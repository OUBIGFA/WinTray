//go:build windows

package branding

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"
	"sync"

	"github.com/lxn/walk"
)

//go:embed assets/logo.png
var logoPNG []byte

var (
	appIconOnce sync.Once
	appIcon     *walk.Icon
	appIconErr  error
)

func AppIcon() (*walk.Icon, error) {
	appIconOnce.Do(func() {
		img, _, err := image.Decode(bytes.NewReader(logoPNG))
		if err != nil {
			appIconErr = err
			return
		}

		appIcon, appIconErr = walk.NewIconFromImageForDPI(img, 96)
	})

	return appIcon, appIconErr
}
