package app

import (
	"testing"

	"lazygitapp/internal/repository"
)

// graphRowText draws a row as characters, for readable assertions: the dot,
// the verticals, and the corners where a line turns.
func graphRowText(row graphRow) string {
	n := max(row.width, 1)
	up := make([]bool, n)
	down := make([]bool, n)
	left := make([]bool, n)
	right := make([]bool, n)
	for _, pipe := range row.pipes {
		if pipe.to == "" {
			// A root commit: nothing runs below it.
			continue
		}
		if pipe.from == graphStartHash && pipe.fromPos == pipe.toPos {
			// Nothing runs above the graph's first commit.
			continue
		}
		switch pipe.kind {
		case graphPipeTerminates:
			up[pipe.fromPos] = true
		case graphPipeStarts:
			down[pipe.toPos] = true
		case graphPipeContinues:
			up[pipe.fromPos] = true
			down[pipe.toPos] = true
		}
		if pipe.fromPos < pipe.toPos {
			right[pipe.fromPos] = true
			left[pipe.toPos] = true
		} else if pipe.fromPos > pipe.toPos {
			left[pipe.fromPos] = true
			right[pipe.toPos] = true
		}
	}
	glyph := func(i int) rune {
		switch {
		case i == row.pos:
			if row.merge {
				return '◎'
			}
			return '●'
		case up[i] && down[i]:
			return '│'
		case up[i] && right[i]:
			return '╰'
		case up[i] && left[i]:
			return '╯'
		case down[i] && right[i]:
			return '╭'
		case down[i] && left[i]:
			return '╮'
		case left[i] && right[i]:
			return '─'
		case up[i]:
			return '╵'
		case down[i]:
			return '╷'
		}
		return ' '
	}
	out := make([]rune, n)
	for i := range out {
		out[i] = glyph(i)
	}
	return string(out)
}

func TestGraphLayoutDrawsLinearHistory(t *testing.T) {
	rows := graphLayout([]repository.Commit{
		{Hash: "c3", Parents: []string{"c2"}},
		{Hash: "c2", Parents: []string{"c1"}},
		{Hash: "c1"},
	})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, row := range rows {
		if row.pos != 0 {
			t.Errorf("row %d sits in lane %d, want 0", i, row.pos)
		}
		if got := graphRowText(row); got != "●" {
			t.Errorf("row %d draws %q, want %q", i, got, "●")
		}
	}
	if lanes := graphLaneCount(rows); lanes != 1 {
		t.Errorf("lanes = %d, want 1", lanes)
	}
}

func TestGraphLayoutForksAndMerges(t *testing.T) {
	rows := graphLayout([]repository.Commit{
		// A merge whose second parent is not on screen yet forks a lane.
		{Hash: "merge", Parents: []string{"a", "b"}},
		{Hash: "a", Parents: []string{"base"}},
		{Hash: "b", Parents: []string{"base"}},
		// Both lanes wait for base now, so it joins them back together.
		{Hash: "base"},
	})
	want := []string{"◎╮", "●│", "│●", "●╯"}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		if got := graphRowText(row); got != want[i] {
			t.Errorf("row %d draws %q, want %q", i, got, want[i])
		}
	}
	if lanes := graphLaneCount(rows); lanes != 2 {
		t.Errorf("lanes = %d, want 2", lanes)
	}
}

// A lane that ends closes up: the lines right of it move left on the next row
// instead of leaving the graph as wide as it once was.
func TestGraphLayoutCompactsFreedLanes(t *testing.T) {
	rows := graphLayout([]repository.Commit{
		{Hash: "merge", Parents: []string{"main", "side"}},
		{Hash: "side", Parents: []string{"base"}},
		{Hash: "main", Parents: []string{"base"}},
		{Hash: "base"},
	})
	for _, row := range rows {
		if row.width > 2 {
			t.Errorf("%q is %d lanes wide, want at most 2", graphRowText(row), row.width)
		}
	}
}

func TestGraphLayoutWithoutCommits(t *testing.T) {
	if rows := graphLayout(nil); len(rows) != 0 {
		t.Fatalf("got %d rows for no commits", len(rows))
	}
	if lanes := graphLaneCount(nil); lanes != 1 {
		t.Fatalf("lanes = %d without commits, want 1", lanes)
	}
}

// The graph's column fits its lanes: one lane is never narrower than a lane,
// and even a graph with more branches than the bound allows stays compact
// instead of pushing the commit's text off the row.
func TestGraphGutterFitsItsLanes(t *testing.T) {
	if got, want := graphGutterWidth(1), float32(graphLanePitch+2*graphLanePad); got != want {
		t.Errorf("one lane: gutter = %v, want %v", got, want)
	}
	if got, want := graphGutterWidth(0), float32(graphLanePitch+2*graphLanePad); got != want {
		t.Errorf("no lanes: gutter = %v, want one lane's %v", got, want)
	}
	if got, want := graphGutterWidth(10), float32(10*graphLanePitch+2*graphLanePad); got != want {
		t.Errorf("ten lanes: gutter = %v, want %v", got, want)
	}
	if got := graphGutterWidth(40); got != graphGutterMax {
		t.Errorf("forty lanes: gutter = %v, want the bound %v", got, graphGutterMax)
	}
	if got, want := graphPitch(10), float32(graphLanePitch); got != want {
		t.Errorf("ten lanes: pitch = %v, want %v", got, want)
	}
	if got, want := graphPitch(40), float32(graphGutterMax-2*graphLanePad)/40; got != want {
		t.Errorf("forty lanes: pitch = %v, want the compressed %v", got, want)
	}
}

// A row's text starts beside the lanes that row uses: a commit on one lane
// keeps its text near that lane, while a row that fans out into several lanes
// makes room for all of them.
func TestGraphRowInsetFollowsTheRow(t *testing.T) {
	if got, want := graphRowInset(1, 1), float32(graphLanePad+graphLanePitch); got != want {
		t.Errorf("one lane of one: inset = %v, want %v", got, want)
	}
	narrow, wide := graphRowInset(1, 8), graphRowInset(8, 8)
	if !(narrow < wide) {
		t.Errorf("one lane's inset %v is not left of eight lanes' %v", narrow, wide)
	}
	if got, want := wide, float32(graphLanePad+8*graphLanePitch); got != want {
		t.Errorf("eight lanes of eight: inset = %v, want %v", got, want)
	}
	// The busiest row still fits its lanes, and no row is inset past what the
	// bound allows.
	if got, want := wide, float32(graphGutterMax); got > want {
		t.Errorf("eight lanes' inset %v exceeds the bound %v", got, want)
	}
	if got := graphRowInset(40, 40); got > float32(graphGutterMax) {
		t.Errorf("forty lanes' inset %v exceeds the bound %v", got, graphGutterMax)
	}
}
