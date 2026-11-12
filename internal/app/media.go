package app

import (
	"bytes"
	"context"
	"image"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

const maxMediaReviewPixels int64 = 40_000_000

type mediaPreview struct {
	present     bool
	unavailable string
	problem     string
	bitmap      *ui.Bitmap
	width       int
	height      int
	// data lets quiet reloads compare pictures after decoding. DecodeBitmap
	// already retains these same bytes for restoring released GPU pixels.
	data []byte
}

type mediaReview struct {
	old mediaPreview
	new mediaPreview
}

func sameMediaReview(a, b *mediaReview) bool {
	if a == nil || b == nil {
		return a == b
	}
	return sameMediaPreview(a.old, b.old) && sameMediaPreview(a.new, b.new)
}

func sameMediaPreview(a, b mediaPreview) bool {
	return a.present == b.present && a.unavailable == b.unavailable && a.problem == b.problem &&
		a.width == b.width && a.height == b.height && bytes.Equal(a.data, b.data)
}

func isMediaPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

// loadMediaReview is called from the same worker as its textual diff, so the
// caller's existing hash/token checks reject both stale text and stale images.
func loadMediaReview(ctx context.Context, session repository.Session, request repository.MediaRequest) (*mediaReview, error) {
	pair, err := session.Media(ctx, request)
	if err != nil {
		return nil, err
	}
	return &mediaReview{old: decodeMediaPreview(pair.Old), new: decodeMediaPreview(pair.New)}, nil
}

func decodeMediaPreview(blob repository.MediaBlob) mediaPreview {
	preview := mediaPreview{present: blob.Present, unavailable: blob.Unavailable}
	if !blob.Present || blob.Unavailable != "" {
		return preview
	}
	return decodeMediaData(blob.Data)
}

// decodeMediaData validates and decodes image bytes already read by an editor.
func decodeMediaData(data []byte) mediaPreview {
	preview := mediaPreview{present: true, data: data}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		preview.problem = "This side is not a supported image"
		return preview
	}
	if int64(config.Width) > maxMediaReviewPixels/int64(config.Height) {
		preview.problem = "Image has more than 40 million pixels"
		return preview
	}
	preview.bitmap, err = ui.DecodeBitmap(data)
	if err != nil {
		preview.problem = "This side is not a supported image"
		return preview
	}
	preview.width, preview.height = config.Width, config.Height
	return preview
}

// mediaReviewView draws corresponding old/new versions at the same size. A
// missing side is meaningful review state, not an error or an empty bitmap.
func mediaReviewView(c *ui.Context, media *mediaReview, path string) {
	if media == nil {
		return
	}
	theme := c.Theme()
	ui.Row(c).Grow(1).Gap(12).Children(func() {
		mediaSideView(c, theme, "Before", path, media.old)
		mediaSideView(c, theme, "After", path, media.new)
	})
}

func mediaSideView(c *ui.Context, theme *ui.Theme, label, path string, preview mediaPreview) {
	ui.Column(c).Grow(1).Fill().Gap(6).Padding(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, tr(label)).Bold().Grow(1)
			if preview.width > 0 {
				ui.Textf(c, "%d × %d", preview.width, preview.height).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		})
		switch {
		case !preview.present:
			mediaMessage(c, theme, "No file on this side", "")
		case preview.unavailable != "":
			mediaMessage(c, theme, "Preview unavailable", preview.unavailable)
		case preview.problem != "" || preview.bitmap == nil:
			problem := preview.problem
			if problem == "" {
				problem = "This side could not be decoded"
			}
			mediaMessage(c, theme, "Preview unavailable", problem)
		default:
			ui.Image(c, preview.bitmap).Grow(1).Fill().Fit(ui.Contain).
				Role(ui.RoleImage).Label(trf("%s image: %s", tr(label), path))
		}
	})
}

func mediaMessage(c *ui.Context, theme *ui.Theme, title, detail string) {
	ui.Column(c).Grow(1).Fill().Center().Gap(4).Children(func() {
		ui.Text(c, tr(title)).TextColor(theme.TextMuted)
		if detail != "" {
			ui.Text(c, tr(detail)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).TextAlign(ui.Center)
		}
	})
}

// binaryFileView shows a decodable image without offering an editor that
// could corrupt its bytes. Other binary formats keep the compact explanation.
func binaryFileView(c *ui.Context, path string, preview *mediaPreview, controls func()) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Fill().Center().Gap(8).Padding(8).Children(func() {
		if preview != nil && preview.bitmap != nil {
			ui.Image(c, preview.bitmap).Grow(1).Fill().Fit(ui.Contain).
				Role(ui.RoleImage).Label(path)
			if preview.width > 0 {
				ui.Textf(c, "%d × %d", preview.width, preview.height).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		}
		ui.Text(c, tr("This file is binary and cannot be edited here.")).
			TextColor(theme.TextMuted).TextAlign(ui.Center)
		if controls != nil {
			controls()
		}
	})
}

// noTextualDiffOrMedia keeps the established fallback for unsupported binary
// files while allowing every image pane and fullscreen view to share previews.
func noTextualDiffOrMedia(c *ui.Context, media *mediaReview, path string) {
	if media != nil {
		mediaReviewView(c, media, path)
		return
	}
	ui.Column(c).Grow(1).Center().Children(func() {
		ui.Text(c, tr("No textual diff")).TextColor(c.Theme().TextMuted)
	})
}
