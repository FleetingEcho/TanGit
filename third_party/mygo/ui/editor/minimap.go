package editor

import (
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/internal/l10n"
	"github.com/egoist/mygo/ui"
)

// Minimap shows a compact, syntax-colored overview of st's buffer. Rows sample
// the document as far as its outline allows, and clicking one reveals its line.
// Give it a width; its content follows the editor's theme and visible range.
func Minimap(c *ui.Context, st *State) ui.Element {
	if st == nil {
		return ui.Column(c)
	}
	st.ensure()
	if st.Buffer == nil {
		st.Buffer = NewBuffer("")
	}
	// Reuse the editor's current lexer, semantic tokens, and resolved theme.
	// Keeping these details behind Minimap prevents its palette from diverging
	// from the full-size editor when an app supplies a custom theme.
	st.ed.init(st.Buffer, st.options(), c.Theme())
	st.ed.view.semantic = st.SemanticTokens
	buffer := st.Buffer
	lineCount := buffer.LineCount()
	if lineCount <= 0 {
		return ui.Column(c)
	}
	rows := min(76, lineCount)
	lastSample := (rows - 1) * lineCount / rows
	st.ed.view.advanceSyntax(lastSample+1, 250*time.Microsecond)
	highlighter := st.ed.view.followHighlighter()
	theme := st.ed.theme
	first, last := st.VisibleLines()
	var scratch []Token
	return ui.Column(c).Padding(4, 5).Gap(1).AlignItems(ui.Stretch).Clip().Children(func() {
		for row := range rows {
			line := row * lineCount / rows
			text := buffer.Line(line)
			var tokens []Token
			if highlighter != nil {
				tokens, _ = highlighter.Tokens(line, text, scratch[:0])
				scratch = tokens
			}
			lineStart := buffer.Offset(Pos{Line: line})
			runs := minimapRuns(text, tokens, st.ed.view.semantic, lineStart)
			button := ui.ButtonBase(c).FillWidth().Justify(ui.Start).Height(5).Padding(0).
				Label(l10n.In(c.Locale()).GoToLine() + " " + strconv.Itoa(line+1)).
				HandleInput(func(ev ui.InputEvent) bool {
					return st.minimapInput(ev, line, rows)
				})
			button.Children(func() {
				for _, run := range runs {
					ui.Text(c, run.text).Font("monospace").FontSize(2).SingleLine().
						TextColor(theme.TokenColor(run.kind))
				}
			})
			if line >= first && line <= last {
				button.Background(theme.Selection.Alpha(0.35))
			}
			if button.Clicked() && !st.minimapDrag.moved {
				st.ScrollTo(line)
			}
		}
	})
}

// minimapRun is one part of a Minimap line with the editor's token kind. Text
// keeps spaces and tabs so indentation and the separation between tokens remain.
type minimapRun struct {
	text string
	kind TokenKind
}

func minimapRuns(text string, tokens []Token, semantic []SemanticToken, lineStart int) []minimapRun {
	points := map[int]struct{}{0: {}, len(text): {}}
	for _, token := range tokens {
		if token.End > 0 && token.End < len(text) {
			points[token.End] = struct{}{}
		}
	}
	type overlap struct{ start, end, index int }
	overlaps := make([]overlap, 0)
	for i, token := range semantic {
		start := max(0, token.Start-lineStart)
		end := min(len(text), token.End-lineStart)
		if start >= end {
			continue
		}
		points[start], points[end] = struct{}{}, struct{}{}
		overlaps = append(overlaps, overlap{start, end, i})
	}
	boundaries := make([]int, 0, len(points))
	for point := range points {
		boundaries = append(boundaries, point)
	}
	slices.Sort(boundaries)
	runs := make([]minimapRun, 0, len(boundaries)-1)
	for i := 0; i+1 < len(boundaries); i++ {
		start, end := boundaries[i], boundaries[i+1]
		if start >= end {
			continue
		}
		kind := TokenText
		for _, token := range tokens {
			if start < token.End {
				kind = token.Kind
				break
			}
		}
		for _, overlap := range overlaps {
			if start >= overlap.start && end <= overlap.end {
				kind = semantic[overlap.index].Kind
			}
		}
		if len(runs) > 0 && runs[len(runs)-1].kind == kind {
			runs[len(runs)-1].text += text[start:end]
			continue
		}
		runs = append(runs, minimapRun{text: text[start:end], kind: kind})
	}
	return runs
}

const minimapRowStep = 6

type minimapDragState struct {
	active, moved bool
	downY         float32
	downLine      int
}

func (st *State) minimapInput(ev ui.InputEvent, line, rows int) bool {
	if st == nil || st.Buffer == nil {
		return false
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		st.minimapDrag = minimapDragState{active: true, downY: ev.Y, downLine: line}
		return true
	case ui.InputPointerMove:
		drag := &st.minimapDrag
		if !drag.active {
			return false
		}
		delta := ev.Y - drag.downY
		if !drag.moved && math.Abs(float64(delta)) < 2 {
			return true
		}
		drag.moved = true
		lineCount := st.Buffer.LineCount()
		offset := int(math.Round(float64(delta) * float64(lineCount) / float64(rows*minimapRowStep)))
		target := max(0, min(lineCount-1, drag.downLine+offset))
		st.ScrollTo(target)
		return true
	case ui.InputPointerUp:
		active := st.minimapDrag.active
		st.minimapDrag.active = false
		return active
	case ui.InputScroll:
		st.SetScrollY(st.ScrollY() + float64(ev.DY))
		return true
	}
	return false
}
