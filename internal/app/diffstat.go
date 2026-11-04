package app

import "github.com/egoist/mygo/ui"

// fileDiffStatCounts shows the exact number of added and deleted lines on a
// changed-file row. Zero counts stay visible so every row reports both sides
// of its change, including untracked and binary files whose loader has no
// textual line count.
func fileDiffStatCounts(c *ui.Context, path string, added, deleted int) {
	theme := c.Theme()
	label := trf("Diff statistics for %s: %d additions, %d deletions", path, added, deleted)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Label(label).Tooltip(label).Children(func() {
		ui.Textf(c, "+%d", added).TextColor(theme.Success).FontSize(theme.FontSize - 1)
		ui.Textf(c, "−%d", deleted).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
	})
}
