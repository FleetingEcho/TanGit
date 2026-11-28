package app

import "github.com/egoist/mygo/ui"

// richTip is the content of a tooltip that needs more than one plain sentence:
// a title with its shortcut as a key badge, an explanation, and identifiers such
// as hashes or paths in badges of their own. Short single-sentence tooltips keep
// using Element.Tooltip, whose look matches the rest of the toolkit.
type richTip struct {
	Title    string
	Body     string
	Shortcut string
	// Mono holds the identifiers (hashes, refs, paths, commands), one badge each.
	Mono []string
	// Font is the family of the Mono badges; empty keeps the default.
	Font string
}

// richTooltip shows tip beside anchor when the pointer rests on it. It must run
// after anchor is built, in the same frame, as TooltipBase requires.
func richTooltip(c *ui.Context, anchor ui.Element, tip richTip) {
	theme := c.Theme()
	if tip.Title != "" {
		anchor.Description(tip.Title)
	}
	ui.TooltipBase(c, anchor, func(t ui.Element) {
		t.Padding(8, 10).Radius(theme.Radius+2).Background(theme.Surface).Border(1, theme.Border).
			Shadow(0, 2, 10, 0, ui.RGBA(0, 0, 0, 0.25)).Margin(0, 0, 6, 0).MaxWidth(440).
			Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					if tip.Title != "" || tip.Shortcut != "" {
						ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
							if tip.Title != "" {
								ui.Text(c, tip.Title).Bold().TextColor(theme.Text).Grow(1)
							}
							if tip.Shortcut != "" {
								pickerKeyHint(c, tip.Shortcut)
							}
						})
					}
					if tip.Body != "" {
						ui.Text(c, tip.Body).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					}
					if len(tip.Mono) > 0 {
						ui.Column(c).Gap(4).Children(func() {
							for _, value := range tip.Mono {
								ui.Box(c).Padding(2, 7).Radius(theme.Radius).
									Background(theme.Accent.Alpha(0.12)).Border(1, theme.Accent.Alpha(0.3)).Children(func() {
									text := ui.Text(c, value).FontSize(theme.FontSize - 1).TextColor(theme.Text)
									if tip.Font != "" {
										text.Font(tip.Font)
									}
								})
							}
						})
					}
				})
			})
	})
}
