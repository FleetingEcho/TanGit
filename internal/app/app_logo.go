package app

import (
	_ "embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

// appLogoPNG is the full-color application icon, shown where the app names
// itself. The single-color appIcon stays for places that take the theme's color.
//
//go:embed assets/app-logo.png
var appLogoPNG []byte

var (
	appLogoOnce   sync.Once
	appLogoBitmap *ui.Bitmap
)

// appLogo draws the application icon at size DIPs, or the single-color mark if
// the image cannot be decoded.
func appLogo(c *ui.Context, size float32) {
	appLogoOnce.Do(func() {
		if bitmap, err := ui.DecodeBitmap(appLogoPNG); err == nil {
			appLogoBitmap = bitmap
		}
	})
	if appLogoBitmap == nil {
		ui.Icon(c, appIcon).FontSize(size * 0.55).TextColor(c.Theme().Accent)
		return
	}
	ui.Image(c, appLogoBitmap).Size(size, size).Fit(ui.Contain).Label(appName)
}
