package app

import (
	"strings"
	"unicode/utf8"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const (
	blameHashWidth       = 7
	blameAuthorMaxWidth  = 18
	blameDateDisplaySize = 10
)

// formatBlame keeps one selectable, searchable editor row per source line.
// Contiguous lines from one commit share a compact metadata label; their
// continuation rows leave that column blank rather than drawing a grid beside
// the source.
func formatBlame(lines []repository.BlameLine) string {
	if len(lines) == 0 {
		return ""
	}
	authorWidth := 0
	for _, line := range lines {
		width := utf8.RuneCountInString(line.Author)
		if width > blameAuthorMaxWidth {
			width = blameAuthorMaxWidth
		}
		if width > authorWidth {
			authorWidth = width
		}
	}
	metadataWidth := blameHashWidth + 2 + authorWidth + 3 + blameDateDisplaySize + 2
	continuation := strings.Repeat(" ", metadataWidth)

	var output strings.Builder
	previousHash := ""
	for index, line := range lines {
		if index > 0 {
			output.WriteByte('\n')
		}
		if line.Hash == previousHash {
			output.WriteString(continuation)
		} else {
			output.WriteString(shortBlameHash(line.Hash))
			output.WriteString("  ")
			output.WriteString(padBlameAuthor(line.Author, authorWidth))
			output.WriteString(" · ")
			output.WriteString(line.Date)
			output.WriteString(strings.Repeat(" ", max(0, blameDateDisplaySize-utf8.RuneCountInString(line.Date))))
			output.WriteString("  ")
		}
		output.WriteString(line.Text)
		previousHash = line.Hash
	}
	return output.String()
}

func shortBlameHash(hash string) string {
	if len(hash) > blameHashWidth {
		return hash[:blameHashWidth]
	}
	return hash + strings.Repeat(" ", blameHashWidth-len(hash))
}

func padBlameAuthor(author string, width int) string {
	runes := []rune(author)
	if len(runes) > blameAuthorMaxWidth {
		runes = append(runes[:blameAuthorMaxWidth-1], '…')
	}
	return string(runes) + strings.Repeat(" ", max(0, width-len(runes)))
}

// closeBlame puts the file's diff back where the blame report was. Bumping the
// generation also drops a blame still being read, so it cannot reappear.
func (a *app) closeBlame() {
	a.blameGeneration++
	a.blameState = nil
	a.blameLoading = false
	a.blameError = ""
}

// followBlame keeps an open blame on the file the list has chosen. Choosing
// another file reads that file's blame in place of the old one, so the report
// never describes a file other than the one selected; a file that cannot be
// blamed closes it.
func (a *app) followBlame() {
	open := a.blameState != nil || a.blameLoading || a.blameError != ""
	change, ok := a.selectedChange()
	if !open || !ok || a.blamePath == change.file.Path {
		return
	}
	if change.file.Deleted || change.file.HasMergeConflicts {
		a.closeBlame()
		return
	}
	a.beginBlame()
}
