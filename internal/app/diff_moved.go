package app

import (
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// movedLine is one uniquely matched deletion/addition pair. File lines are
// one-based for the explanation; annotation lines are zero-based viewer rows.
type movedLine struct {
	From int
	To   int
}

type movedCandidate struct {
	text   string
	file   int
	viewer int
}

// movedLineAnnotations recognizes only unique, identical non-trivial lines.
// That deliberately conservative rule avoids claiming an edited line moved,
// and avoids marking every shifted line after an insertion as moved code.
func movedLineAnnotations(file editor.FilePatch) []editor.DiffAnnotation {
	var removed, added []movedCandidate
	oldViewer, newViewer := 0, 0
	for _, hunk := range file.Hunks {
		oldLine, newLine := hunk.OldStart, hunk.NewStart
		for _, line := range hunk.Lines {
			if line == "" || line[0] == '\\' {
				continue
			}
			text := line[1:]
			switch line[0] {
			case ' ':
				oldLine++
				newLine++
				oldViewer++
				newViewer++
			case '-':
				removed = append(removed, movedCandidate{text: text, file: oldLine, viewer: oldViewer})
				oldLine++
				oldViewer++
			case '+':
				added = append(added, movedCandidate{text: text, file: newLine, viewer: newViewer})
				newLine++
				newViewer++
			}
		}
	}
	removedUnique := uniqueMovedCandidates(removed)
	addedUnique := uniqueMovedCandidates(added)
	var annotations []editor.DiffAnnotation
	for text, old := range removedUnique {
		new, ok := addedUnique[text]
		if !ok {
			continue
		}
		pair := movedLine{From: old.file, To: new.file}
		annotations = append(annotations,
			editor.DiffAnnotation{Side: editor.DiffOld, Line: old.viewer, Height: 18, Key: pair},
			editor.DiffAnnotation{Side: editor.DiffNew, Line: new.viewer, Height: 18, Key: pair},
		)
	}
	sort.Slice(annotations, func(i, j int) bool {
		if annotations[i].Line == annotations[j].Line {
			return annotations[i].Side < annotations[j].Side
		}
		return annotations[i].Line < annotations[j].Line
	})
	return annotations
}

func uniqueMovedCandidates(lines []movedCandidate) map[string]movedCandidate {
	candidates := make(map[string]movedCandidate, len(lines))
	duplicates := map[string]bool{}
	for _, line := range lines {
		text := strings.TrimSpace(line.text)
		// Braces and other tiny lines are too common to make a useful move
		// explanation. Whitespace-only moves are not content moves either.
		if len(text) < 3 {
			continue
		}
		line.text = text
		if _, exists := candidates[text]; exists {
			duplicates[text] = true
			continue
		}
		candidates[text] = line
	}
	for text := range duplicates {
		delete(candidates, text)
	}
	return candidates
}

// renderMovedLine uses MyGo's under-line annotation area for a compact,
// keyboard-accessible explanation on both ends of a moved line.
func renderMovedLine(c *ui.Context, annotation editor.DiffAnnotation) {
	pair, ok := annotation.Key.(movedLine)
	if !ok {
		return
	}
	message := trf("Moved to line %d", pair.To)
	if annotation.Side == editor.DiffNew {
		message = trf("Moved from line %d", pair.From)
	}
	ui.Text(c, "↗ "+message).TextColor(c.Theme().Accent).FontSize(c.Theme().FontSize-1).
		Padding(0, 8).Tooltip(message)
}
