package app

import (
	"cmp"
	"slices"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// graphStartHash marks the pipe that comes from above the first commit: it
// has no commit of its own, so there is no line above the graph's first row.
const graphStartHash = "START"

// graphPipeKind is how a pipe runs through one row of the graph.
type graphPipeKind uint8

const (
	// graphPipeTerminates rises from the row's top edge to the commit's line.
	graphPipeTerminates graphPipeKind = iota
	// graphPipeStarts falls from the commit's line to the row's bottom edge.
	graphPipeStarts
	// graphPipeContinues runs from the top edge to the bottom edge, moving
	// left or right where it crosses the commit's line.
	graphPipeContinues
)

// graphPipe is one line of the commit graph through one row. It runs from
// fromPos at the top edge, along the row's centre line, to toPos at the bottom
// edge, according to its kind. colour follows the branch from the commit that
// opened it, so a line keeps its colour down its whole length.
type graphPipe struct {
	fromPos int
	toPos   int
	from    string
	to      string
	kind    graphPipeKind
	color   int
}

// graphRow is the graph beside one commit: the pipes to paint, the lane the
// commit's dot sits in, and how wide the graph is in this row.
type graphRow struct {
	pipes []graphPipe
	pos   int
	merge bool
	color int
	width int
}

// graphLayout assigns every commit a lane and draws the pipes between them,
// the way Git's graph does: a commit takes the lane waiting for it, or a new
// lane at the right, and lanes that free up are filled from the left by the
// lines still running, so the graph stays as narrow as it can.
//
// The commits must arrive newest first in topological order, as Git's history
// does, so a parent is always seen after its children.
func graphLayout(commits []repository.Commit) []graphRow {
	if len(commits) == 0 {
		return nil
	}
	pipes := []graphPipe{{
		fromPos: 0,
		toPos:   0,
		from:    graphStartHash,
		to:      commits[0].Hash,
		kind:    graphPipeStarts,
		color:   0,
	}}
	rows := make([]graphRow, 0, len(commits))
	for _, commit := range commits {
		pipes = graphNextPipes(pipes, commit)
		rows = append(rows, graphRowOf(pipes))
	}
	return rows
}

// graphNextPipes runs the lanes through commit's row and returns the pipes
// that leave it towards the commits below.
func graphNextPipes(prev []graphPipe, commit repository.Commit) []graphPipe {
	// A pipe that terminated in the row above, or a root commit's pipe into
	// nothing, says nothing about this row.
	current := make([]graphPipe, 0, len(prev))
	for _, pipe := range prev {
		if pipe.kind == graphPipeTerminates || pipe.to == "" {
			continue
		}
		current = append(current, pipe)
	}

	maxPos := -1
	for _, pipe := range current {
		maxPos = max(maxPos, pipe.toPos)
	}
	// A commit no line was waiting for takes a new lane at the far right;
	// one a line was waiting for takes that line's lane and colour.
	pos, color := maxPos+1, maxPos+1
	for _, pipe := range current {
		if pipe.to == commit.Hash {
			pos, color = pipe.toPos, pipe.color
			break
		}
	}

	// taken spots are those a line ends on, traversed ones those a line runs
	// through. A new pipe may not end on a spot a continuing pipe already
	// runs through, or it would read as that pipe's.
	taken := map[int]bool{}
	traversed := map[int]bool{}
	continuing := map[int]bool{}
	for _, pipe := range current {
		if pipe.to != commit.Hash {
			continuing[pipe.toPos] = true
		}
	}
	freeForContinuing := func() int {
		for i := 0; ; i++ {
			if !traversed[i] {
				return i
			}
		}
	}
	freeForNew := func() int {
		for i := 0; ; i++ {
			if !taken[i] && !continuing[i] {
				return i
			}
		}
	}
	traverse := func(from, to int) {
		left, right := from, to
		if left > right {
			left, right = right, left
		}
		for i := left; i <= right; i++ {
			traversed[i] = true
		}
		taken[to] = true
	}

	parent := ""
	if len(commit.Parents) > 0 {
		parent = commit.Parents[0]
	}
	pipes := make([]graphPipe, 0, len(current)+len(commit.Parents))
	pipes = append(pipes, graphPipe{
		fromPos: pos,
		toPos:   pos,
		from:    commit.Hash,
		to:      parent,
		kind:    graphPipeStarts,
		color:   color,
	})

	// Lines waiting for this commit end here, joined to its dot; lines on a
	// lane left of the dot continue on that lane.
	for _, pipe := range current {
		switch {
		case pipe.to == commit.Hash:
			pipes = append(pipes, graphPipe{
				fromPos: pipe.toPos,
				toPos:   pos,
				from:    pipe.from,
				to:      pipe.to,
				kind:    graphPipeTerminates,
				color:   pipe.color,
			})
			traverse(pipe.toPos, pos)
		case pipe.toPos < pos:
			to := freeForContinuing()
			pipes = append(pipes, graphPipe{
				fromPos: pipe.toPos,
				toPos:   to,
				from:    pipe.from,
				to:      pipe.to,
				kind:    graphPipeContinues,
				color:   pipe.color,
			})
			traverse(pipe.toPos, to)
		}
	}

	// Every parent past the first opens a lane of its own.
	for i, parent := range commit.Parents {
		if i == 0 {
			continue
		}
		to := freeForNew()
		pipes = append(pipes, graphPipe{
			fromPos: pos,
			toPos:   to,
			from:    commit.Hash,
			to:      parent,
			kind:    graphPipeStarts,
			color:   to,
		})
		taken[to] = true
	}

	// Lines right of the dot move left into the free lanes, so the graph
	// closes up as branches end instead of keeping the width it once had.
	for _, pipe := range current {
		if pipe.to == commit.Hash || pipe.toPos <= pos {
			continue
		}
		last := pipe.toPos
		for i := pipe.toPos; i > pos; i-- {
			if taken[i] || traversed[i] {
				break
			}
			last = i
		}
		pipes = append(pipes, graphPipe{
			fromPos: pipe.toPos,
			toPos:   last,
			from:    pipe.from,
			to:      pipe.to,
			kind:    graphPipeContinues,
			color:   pipe.color,
		})
		traverse(pipe.toPos, last)
	}

	// Left to right, so a line ending on a lane paints before one passing
	// through it.
	slices.SortFunc(pipes, func(a, b graphPipe) int {
		if a.toPos != b.toPos {
			return cmp.Compare(a.toPos, b.toPos)
		}
		return cmp.Compare(a.kind, b.kind)
	})
	return pipes
}

// graphRowOf reads the row's dot and width from its pipes.
func graphRowOf(pipes []graphPipe) graphRow {
	row := graphRow{width: 1}
	starts := 0
	for _, pipe := range pipes {
		switch pipe.kind {
		case graphPipeStarts:
			if starts == 0 {
				row.color = pipe.color
			}
			starts++
			row.pos = pipe.fromPos
		case graphPipeTerminates:
			row.pos = pipe.toPos
		}
		row.width = max(row.width, pipe.fromPos+1, pipe.toPos+1)
	}
	row.merge = starts > 1
	row.pipes = pipes
	return row
}

// graphLaneCount is the widest row's lane count, which every row pads to so
// the commit columns line up.
func graphLaneCount(rows []graphRow) int {
	width := 1
	for _, row := range rows {
		width = max(width, row.width)
	}
	return width
}

// The graph's geometry: lanes sit graphLanePitch apart, far enough that the
// halo around one lane leaves the next alone, and the gutter is never wider
// than graphGutterMax, so a graph with more branches than that compresses its
// lanes instead of pushing the commit's text off the row.
const (
	graphLanePitch = 18
	graphLanePad   = 10
	graphGutterMax = 420
)

// graphGutterWidth is the width the lanes take, in DIPs: room for every lane
// at a comfortable pitch, bounded so a very busy graph compresses instead of
// widening without limit.
func graphGutterWidth(lanes int) float32 {
	lanes = max(1, lanes)
	return min(float32(lanes)*graphLanePitch+2*graphLanePad, float32(graphGutterMax))
}

// graphPitch is the distance between neighbouring lanes, in DIPs: the pitch
// above, or what the bound leaves when the graph is too busy for it. Every
// row shares it, so a lane keeps its column down the whole list.
func graphPitch(lanes int) float32 {
	lanes = max(1, lanes)
	return (graphGutterWidth(lanes) - 2*graphLanePad) / float32(lanes)
}

// graphRowInset is where one row's text starts, in DIPs from the row's
// content edge: just past the lanes that row actually uses. A row with a
// single lane puts its commit's text beside that lane instead of at the
// widest row's column, which would leave the row's own graph marooned in an
// empty gutter.
func graphRowInset(rowLanes, lanes int) float32 {
	return graphLanePad + float32(max(1, rowLanes))*graphPitch(lanes)
}

// drawGraphRow paints the row's lanes over the list row r, in the style of
// VS Code's commit graph: two-pixel lines over a wider halo of the row's own
// background, wide cubic transitions between lanes, and a disc ringed by the
// background. Lines run from the row's top edge to its bottom edge, so
// consecutive rows connect, and the horizontal runs meet at the commit's dot
// on the row's centre.
func drawGraphRow(p *ui.Painter, r ui.Rect, row graphRow, lanes int, selected, hovered bool, theme *ui.Theme) {
	// rowPadX mirrors the row's horizontal padding: the lanes start where
	// the row's content does. bend is how much of the row a lane transition
	// takes: VS Code sweeps 80% of it, so a branch leaves its lane in a
	// long S rather than a short elbow.
	const (
		rowPadX = 6
		bend    = 0.8
	)
	lanes = max(1, lanes)
	pitch := graphPitch(lanes)
	if pitch <= 0 {
		return
	}
	originX := r.X + rowPadX + graphLanePad
	laneX := func(lane int) float32 {
		return originX + (float32(lane)+0.5)*pitch
	}
	top, bottom := r.Y, r.Y+r.H
	midY := r.Y + r.H/2
	lineWidth := float32(2)
	if selected {
		lineWidth = 2.25
	}
	colorOf := func(pipe graphPipe) ui.Color {
		if selected {
			return theme.AccentText
		}
		return graphLaneColor(pipe.color)
	}
	// The lanes are drawn over the row's background, so clearing a wider
	// stroke of it under each line separates crossing lanes and keeps the
	// commit's disc from touching the lines that join it, as the graph in
	// VS Code does.
	bg := theme.Background
	switch {
	case selected:
		bg = theme.Accent
	case hovered:
		bg = theme.SurfaceHover
	}
	path := &ui.Path{}
	for _, pipe := range row.pipes {
		if pipe.from == graphStartHash && pipe.fromPos == pipe.toPos {
			continue
		}
		if pipe.kind == graphPipeStarts && pipe.to == "" {
			// A root commit: nothing runs below it.
			continue
		}
		lane := laneX(pipe.fromPos)
		to := laneX(pipe.toPos)
		// Transitions bend through most of the row, so a branch sweeps into
		// its lane rather than cornering on the commit's line.
		// Butt caps, as SVG's default and as VS Code draws its graph: a
		// round cap reaches half a stroke past the row's edge, so the wide
		// shadow under one row's line paints over the line the row above it
		// drew. Reset restores the round cap, so set it for each pipe.
		path.Reset().Cap(ui.ButtCap)
		switch pipe.kind {
		case graphPipeTerminates:
			path.MoveTo(lane, top)
			if pipe.fromPos == pipe.toPos {
				path.LineTo(to, midY)
				break
			}
			d := (midY - top) * bend
			path.CubeTo(lane, top+d, to, midY-d, to, midY)
		case graphPipeStarts:
			if pipe.fromPos == pipe.toPos {
				path.MoveTo(lane, midY).LineTo(to, bottom)
				break
			}
			d := (bottom - midY) * bend
			path.MoveTo(lane, midY).CubeTo(lane, midY+d, to, bottom-d, to, bottom)
		case graphPipeContinues:
			if pipe.fromPos == pipe.toPos {
				path.MoveTo(lane, top).LineTo(to, bottom)
				break
			}
			d := (bottom - top) * bend
			path.MoveTo(lane, top).CubeTo(lane, top+d, to, bottom-d, to, bottom)
		}
		p.StrokePath(path, 4, bg.Alpha(0.75))
		p.StrokePath(path, lineWidth, colorOf(pipe))
	}
	// The dot sits on top of the lines it joins, ringed by the background so
	// a lane passing behind it does not touch the disc.
	radius := min(4.5, max(3, pitch*0.28))
	centerX, centerY := laneX(row.pos), midY
	color := graphLaneColor(row.color)
	if selected {
		color = theme.AccentText
	}
	disc := ui.Rect{X: centerX - radius, Y: centerY - radius, W: 2 * radius, H: 2 * radius}
	p.Fill(disc, color, radius)
	p.Stroke(disc, bg.Alpha(0.75), radius, 1)
}

// graphLaneColor is the colour of a lane: a fixed cycle of mid-tone hues, so
// a lane reads the same in both themes and crossing lines stay tellable
// apart on either background.
func graphLaneColor(lane int) ui.Color {
	palette := [...]ui.Color{
		ui.Hex("#4f7cf7"), // blue
		ui.Hex("#d9822b"), // orange
		ui.Hex("#8b5cf6"), // violet
		ui.Hex("#2f9e6e"), // green
		ui.Hex("#d9468a"), // pink
		ui.Hex("#0f9bb0"), // teal
		ui.Hex("#dc4c4c"), // red
		ui.Hex("#8a7b2a"), // olive
	}
	return palette[paletteIndex(lane, len(palette))]
}

// paletteIndex wraps a lane number onto a palette of n colours. A lane is never
// meant to be negative, but wrapping the negative side too keeps a stray
// value drawing in a palette colour instead of panicking.
func paletteIndex(lane, n int) int {
	if n <= 0 {
		return 0
	}
	lane %= n
	if lane < 0 {
		lane += n
	}
	return lane
}
