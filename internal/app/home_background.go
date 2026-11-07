package app

import (
	"bytes"
	"errors"
	"image"
	"io"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// maxHomeBackgroundSize keeps a picked photograph bounded before its much
// larger decoded pixels reach the UI. Ten mebibytes allows ordinary photos
// while rejecting unbounded files without relying on a filename extension.
const maxHomeBackgroundSize int64 = 10 << 20

// A small compressed file can otherwise expand to an enormous pixel buffer.
// Forty million pixels bounds an RGBA decode to about 160 MiB while allowing
// photographs from current phone cameras.
const maxHomeBackgroundPixels int64 = 40_000_000

// chooseHomeBackgroundPath and decodeHomeBackground are seams for tests and
// for the one bitmap decoder shared by uploads and persisted-image loading.
var chooseHomeBackgroundPath = func(opts mygo.OpenDialogOptions) ([]string, error) {
	return mygo.Dialog.Open(opts)
}

var decodeHomeBackground = ui.DecodeBitmap

// readHomeBackground validates the file's bytes, not just its extension. The
// extra byte beyond the limit distinguishes an exactly-10-MiB image from an
// oversized one even if the file changes while it is being read.
func readHomeBackground(path string) (*ui.Bitmap, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxHomeBackgroundSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxHomeBackgroundSize {
		return nil, errors.New("Choose an image no larger than 10 MiB")
	}
	// Read the dimensions before the decoder allocates its pixels. Dividing
	// rather than multiplying also keeps hostile, extreme dimensions from
	// overflowing the limit check itself.
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, errors.New("Choose a valid PNG, JPEG, GIF, WebP, or BMP image")
	}
	if int64(config.Width) > maxHomeBackgroundPixels/int64(config.Height) {
		return nil, errors.New("Choose an image with at most 40 million pixels")
	}
	bitmap, err := decodeHomeBackground(data)
	if err != nil {
		return nil, errors.New("Choose a valid PNG, JPEG, GIF, WebP, or BMP image")
	}
	return bitmap, nil
}

// browseHomeBackground asks for one image and decodes it away from the UI
// thread. A failed pick leaves both the old setting and its cached bitmap in
// place, and cancellation is deliberately quiet.
func (a *app) browseHomeBackground() {
	a.homeBackgroundSelectionSeq++
	selection := a.homeBackgroundSelectionSeq
	opts := mygo.OpenDialogOptions{
		Parent:      a.window,
		Title:       tr("Choose a Home background"),
		ButtonLabel: tr("Choose image"),
		Message:     tr("PNG, JPEG, GIF, WebP, or BMP, up to 10 MiB"),
		Filters: []mygo.FileFilter{{
			Name:       tr("Image files"),
			Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"},
		}},
	}
	go func() {
		defer a.recoverBackground()
		paths, err := chooseHomeBackgroundPath(opts)
		if err != nil {
			a.update(func() {
				if selection == a.homeBackgroundSelectionSeq {
					a.notify("error", "Could not set Home background", tr(err.Error()))
				}
			})
			return
		}
		if len(paths) == 0 || paths[0] == "" {
			return
		}
		path := paths[0]
		bitmap, err := readHomeBackground(path)
		a.update(func() {
			if selection != a.homeBackgroundSelectionSeq {
				return
			}
			if err != nil {
				a.notify("error", "Could not set Home background", tr(err.Error()))
				return
			}
			// Invalidate a persisted-image read that may still be in flight,
			// then install this decoded result for both Home and the preview.
			a.homeBackgroundGeneration++
			a.homeBackgroundPending = ""
			a.homeBackgroundFailed = ""
			a.homeBackgroundPath = path
			a.homeBackground = bitmap
			a.settings.HomeBackgroundPath = path
			a.saveSettings()
			a.notify("success", "Home background updated", path)
		})
	}()
}

// invalidateHomeBackgroundWork rejects every unfinished picker and decoder
// callback. Clearing the pending and failed markers lets a still-visible Home
// start a fresh load after a repository closes; an overdue callback cannot
// change that newer work because both generations have moved on.
func (a *app) invalidateHomeBackgroundWork() {
	a.homeBackgroundSelectionSeq++
	a.homeBackgroundGeneration++
	a.homeBackgroundPending = ""
	a.homeBackgroundFailed = ""
}

// removeHomeBackground also invalidates unfinished selection and load work,
// so a slow result cannot bring back an image the user just removed.
func (a *app) removeHomeBackground() {
	a.invalidateHomeBackgroundWork()
	a.settings.HomeBackgroundPath = ""
	a.homeBackground = nil
	a.homeBackgroundPath = ""
	a.saveSettings()
}

// syncHomeBackground lazily loads a path restored by either app or shell
// preferences. Only the settings path and decoded-path pair may be drawn;
// after a path change an old bitmap never waits on screen for its replacement.
func (a *app) syncHomeBackground() {
	path := a.settings.HomeBackgroundPath
	if path == "" {
		if a.homeBackground != nil || a.homeBackgroundPath != "" ||
			a.homeBackgroundPending != "" || a.homeBackgroundFailed != "" {
			a.homeBackgroundGeneration++
			a.homeBackground = nil
			a.homeBackgroundPath = ""
			a.homeBackgroundPending = ""
			a.homeBackgroundFailed = ""
		}
		return
	}
	if a.homeBackgroundPath == path && a.homeBackground != nil {
		return
	}
	a.homeBackground = nil
	a.homeBackgroundPath = ""
	if a.homeBackgroundPending == path || a.homeBackgroundFailed == path {
		return
	}
	a.homeBackgroundGeneration++
	generation := a.homeBackgroundGeneration
	a.homeBackgroundPending = path
	a.homeBackgroundFailed = ""
	go func() {
		defer a.recoverBackground()
		bitmap, err := readHomeBackground(path)
		a.update(func() {
			if generation != a.homeBackgroundGeneration || a.settings.HomeBackgroundPath != path {
				return
			}
			a.homeBackgroundPending = ""
			if err != nil {
				a.homeBackgroundFailed = path
				a.notify("error", "Could not load Home background", tr(err.Error()))
				return
			}
			a.homeBackgroundPath = path
			a.homeBackground = bitmap
		})
	}()
}

// homeBackgroundView paints below the launcher's normal-flow siblings. The
// translucent image lets the theme's radial glow and background remain part
// of the composition, preserving text contrast without hiding the picture.
func (a *app) homeBackgroundView(c *ui.Context) {
	if a.settings.HomeBackgroundDisabled || a.homeBackground == nil || a.homeBackgroundPath == "" ||
		a.homeBackgroundPath != a.settings.HomeBackgroundPath {
		return
	}
	ui.Image(c, a.homeBackground).
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Fit(ui.Cover).Opacity(float32(normalizeHomeBackgroundOpacity(a.settings.HomeBackgroundOpacity))).
		ZIndex(-1).PassThrough().
		Role(ui.RoleImage).Label(tr("Home background"))
}
