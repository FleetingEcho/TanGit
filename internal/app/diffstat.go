package app

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// commitDiffStatBlocks keeps a fixed six-square footprint for every commit.
// Mixed changes always show both colors, even for a very small minority.
func commitDiffStatBlocks(c *ui.Context, hash string, added, deleted int) {
	theme := c.Theme()
	green := 0
	switch {
	case added > 0 && deleted > 0:
		green = min(5, max(1, int(math.Round(6*float64(added)/(float64(added)+float64(deleted))))))
	case added > 0:
		green = 6
	}
	size := max(float32(6), theme.FontSize/2)
	label := trf("Diff statistics for %s: %d additions, %d deletions", hash, added, deleted)
	row := ui.Row(c).Gap(2).Shrink(0).AlignItems(ui.Center).Label(label).Description(label)
	row.Children(func() {
		for i := 0; i < 6; i++ {
			color := theme.Danger
			if added == 0 && deleted == 0 {
				color = theme.TextMuted.Alpha(0.35)
			} else if i < green {
				color = theme.Success
			}
			block := ui.Box(c).Size(size, size).Shrink(0).Radius(1).Background(color).Description(label)
			commitDiffStatTooltip(c, block, added, deleted)
		}
	})
	commitDiffStatTooltip(c, row, added, deleted)
}

func commitDiffStatTooltip(c *ui.Context, anchor ui.Element, added, deleted int) {
	theme := c.Theme()
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		tip.Padding(6, 9).Radius(5).Background(theme.Surface).Border(1, theme.Border).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2)).Margin(0, 0, 6, 0).
			Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Textf(c, "+%d", added).TextColor(theme.Success).SingleLine()
					ui.Textf(c, "−%d", deleted).TextColor(theme.Danger).SingleLine()
				})
			})
	})
}

// fileDiffStatCounts shows the exact number of added and deleted lines on a
// changed-file row. Zero counts stay visible so every row reports both sides
// of its change, including untracked and binary files whose loader has no
// textual line count.
func fileDiffStatCounts(c *ui.Context, path string, added, deleted int) {
	theme := c.Theme()
	label := trf("Diff statistics for %s: %d additions, %d deletions", path, added, deleted)
	ui.Row(c).Gap(6).Shrink(0).AlignItems(ui.Center).Label(label).Tooltip(label).Children(func() {
		ui.Textf(c, "+%d", added).TextColor(theme.Success).FontSize(theme.FontSize - 1)
		ui.Textf(c, "−%d", deleted).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
	})
}
