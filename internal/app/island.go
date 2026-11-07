package app

import "github.com/egoist/mygo/ui"

// The interface as islands: every pane is a rounded card floating a gap
// off its neighbours over the window's darker backdrop, the way VS Code
// and other modern shells draw their workspaces. The panes keep the
// theme's own Background face, so every control sits on the face it was
// designed for; the depth comes from the gap, the corner and the shadow.

const islandGap float32 = 5

// islandCard dresses a pane as an island: the card's face and outline,
// a corner a step rounder than the controls', a soft shadow lifting it
// off the backdrop, and half the gap as the margin around it so two
// islands meet at exactly islandGap of open window.
//
// A pane that fills its island sizes itself with Grow(1), not Fill: Fill
// is 100% of the pane's room and the island's margin rides outside that,
// which overflows the split's clip and shaves the island's far edges.
// Grow leaves the margins their room, and the column's stretch gives the
// width the same way.
func islandCard(c *ui.Context, pane ui.Element) ui.Element {
	theme := c.Theme()
	return pane.Background(theme.Background).
		Border(1, theme.Border).
		Radius(theme.Radius+4).
		Shadow(0, 3, 8, 0, islandShadow(theme)).
		Margin(islandGap / 2)
}

// islandShadow is the shadow under an island: heavier in a dark theme,
// where a light shadow only muddies the cards' edges.
func islandShadow(theme *ui.Theme) ui.Color {
	if theme.Dark {
		return ui.RGBA(0, 0, 0, 0.5)
	}
	return ui.RGBA(0, 0, 0, 0.16)
}

// islandBackdrop is the wash the islands float over, the window's own
// background pushed a step darker so the cards read as lifted. It is
// opaque: surfaces that float over the workspace, like the file history
// drawer, wear it as their own face.
func islandBackdrop(theme *ui.Theme) ui.Color {
	if theme.Dark {
		return theme.Background.Mix(black, 0.5)
	}
	return theme.Background.Mix(black, 0.08)
}

var black = ui.RGBA(0, 0, 0, 1)
