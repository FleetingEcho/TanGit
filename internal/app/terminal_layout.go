package app

import "github.com/egoist/mygo/ui"

// terminalLayoutNode is either a shell leaf or a local split. Splitting turns
// only the active leaf into a node, retaining every other leaf and boundary.
type terminalLayoutNode struct {
	sessionID uint64
	below     bool
	percent   float32
	first     *terminalLayoutNode
	second    *terminalLayoutNode
}

// split replaces sessionID's leaf with two shells in the requested direction.
func (n *terminalLayoutNode) split(sessionID, newID uint64, below bool) bool {
	if n == nil {
		return false
	}
	if n.sessionID != 0 {
		if n.sessionID != sessionID {
			return false
		}
		n.first = &terminalLayoutNode{sessionID: sessionID}
		n.second = &terminalLayoutNode{sessionID: newID}
		n.sessionID = 0
		n.below = below
		n.percent = 50
		return true
	}
	return n.first.split(sessionID, newID, below) || n.second.split(sessionID, newID, below)
}

// removeTerminalLayout collapses a split when one of its shells closes.
func removeTerminalLayout(n *terminalLayoutNode, sessionID uint64) *terminalLayoutNode {
	if n == nil {
		return nil
	}
	if n.sessionID != 0 {
		if n.sessionID == sessionID {
			return nil
		}
		return n
	}
	n.first = removeTerminalLayout(n.first, sessionID)
	n.second = removeTerminalLayout(n.second, sessionID)
	if n.first == nil {
		return n.second
	}
	if n.second == nil {
		return n.first
	}
	return n
}

// orderTerminalLayoutLeaves applies a drag's insertion to the active tab's
// leaves without disturbing another tab or any existing split ratio.
func (a *app) orderTerminalLayoutLeaves() {
	group := a.activeTerminalGroup()
	var leaves []*terminalLayoutNode
	var collect func(*terminalLayoutNode)
	collect = func(n *terminalLayoutNode) {
		if n == nil {
			return
		}
		if n.sessionID != 0 {
			leaves = append(leaves, n)
			return
		}
		collect(n.first)
		collect(n.second)
	}
	collect(a.term.layout)
	ids := make([]uint64, 0, len(leaves))
	for _, session := range a.term.sessions {
		if session.groupID == group {
			ids = append(ids, session.id)
		}
	}
	if len(leaves) != len(ids) {
		return
	}
	for i, leaf := range leaves {
		leaf.sessionID = ids[i]
	}
	a.storeTerminalLayout()
}

// terminalSessionsView renders the persistent local split tree. Each new
// horizontal or vertical split subdivides the active card and nothing else.
func (a *app) terminalSessionsView(c *ui.Context) {
	a.terminalLayoutNodeView(c, a.term.layout, 0)
}

func (a *app) terminalLayoutNodeView(c *ui.Context, layout *terminalLayoutNode, node int) {
	if layout == nil {
		return
	}
	if layout.sessionID != 0 {
		if index, _ := a.terminalSessionByID(layout.sessionID); index >= 0 {
			a.terminalSessionView(c, index)
		}
		return
	}
	if layout.first == nil {
		a.terminalLayoutNodeView(c, layout.second, node)
		return
	}
	if layout.second == nil {
		a.terminalLayoutNodeView(c, layout.first, node)
		return
	}
	if layout.percent <= 0 || layout.percent >= 100 {
		layout.percent = 50
	}
	first := func() { a.terminalLayoutNodeView(c, layout.first, node*2+1) }
	second := func() { a.terminalLayoutNodeView(c, layout.second, node*2+2) }
	terminalSplitWithGrip(c, &layout.percent, layout.below, 50, node, first, second)
}

// terminalSplitWithGrip keeps MyGo's accessible splitter and percentages, but
// gives the seam the terminal's quiet presentation: its line stops short of
// the island corners and appears with an ellipsis only over the central grip.
// The transparent grip retains its pointer area while nothing is painted.
func terminalSplitWithGrip(c *ui.Context, value *float32, vertical bool, reset float32,
	node int, first, second func()) ui.Element {
	theme := c.Theme()
	host := ui.Box(c).Grow(1)
	if vertical {
		host.FillWidth()
	} else {
		host.FillHeight()
	}
	host.Children(func() {
		if vertical {
			ui.SplitVerticalPercent(c, value, first, second).Fill().SplitReset(reset)
		} else {
			ui.SplitPercent(c, value, first, second).Fill().SplitReset(reset)
		}

		bounds := host.Bounds()
		total := bounds.W
		if vertical {
			total = bounds.H
		}
		shown := min(max(*value, 0), 100)
		if total > 1 {
			shown = shown * (total - 1) / 100
		}
		if total > 0 {
			minPane := theme.Space(10)
			shown = min(max(shown, minPane), max(total-1-minPane, minPane))
		}

		// Keep the native divider clear of the rounded island corners.
		var centerMask ui.Element
		if vertical {
			ui.Box(c).Absolute().Top(shown).Left(0).Width(paneResizeLineInset).Height(1).
				Background(islandBackdrop(theme)).PassThrough()
			ui.Box(c).Absolute().Top(shown).Right(0).Width(paneResizeLineInset).Height(1).
				Background(islandBackdrop(theme)).PassThrough()
			centerMask = ui.Box(c).Absolute().Top(shown).Left(paneResizeLineInset).Right(paneResizeLineInset).Height(1).
				Background(islandBackdrop(theme)).PassThrough()
		} else {
			ui.Box(c).Absolute().Left(shown).Top(0).Height(paneResizeLineInset).Width(1).
				Background(islandBackdrop(theme)).PassThrough()
			ui.Box(c).Absolute().Left(shown).Bottom(0).Height(paneResizeLineInset).Width(1).
				Background(islandBackdrop(theme)).PassThrough()
			centerMask = ui.Box(c).Absolute().Left(shown).Top(paneResizeLineInset).Bottom(paneResizeLineInset).Width(1).
				Background(islandBackdrop(theme)).PassThrough()
		}

		grip := ui.Box(c).Absolute().Center().Radius(theme.Radius).
			Label(trf("%s %d", tr("Resize terminal split"), node+1)).Tooltip(tr("Drag to resize"))
		if vertical {
			grip.LeftPercent(50).Top(shown-paneResizeGripWidth/2).
				Size(paneResizeGripLength, paneResizeGripWidth).
				Translate(-paneResizeGripLength/2, 0).Cursor(ui.CursorResizeNS)
		} else {
			grip.Left(shown-paneResizeGripWidth/2).TopPercent(50).
				Size(paneResizeGripWidth, paneResizeGripLength).
				Translate(0, -paneResizeGripLength/2).Cursor(ui.CursorResizeEW)
		}
		dx, dy, held := grip.Dragged()
		hovered := grip.Hovered() || held
		if hovered {
			grip.Background(theme.SurfaceHover)
			centerMask.Opacity(0)
		} else {
			grip.Opacity(0)
		}
		grip.Children(func() {
			icon := ui.Icon(c, moreIcon).FontSize(14).TextColor(theme.Accent)
			if !vertical {
				icon.Rotate(90)
			}
		})

		if grip.DoubleClicked() {
			*value = reset
			return
		}
		delta := dx
		if vertical {
			delta = dy
		}
		if held && delta != 0 {
			next := shown + delta
			if total > 0 {
				minPane := theme.Space(10)
				next = min(max(next, minPane), max(total-1-minPane, minPane))
			}
			if total > 1 {
				*value = next * 100 / (total - 1)
			}
		}
	})
	return host
}
