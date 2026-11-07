package app

import (
	"strings"
	"unicode/utf8"

	"lazygitapp/internal/repository"
)

const (
	blameHashWidth       = 7
	blameAuthorMaxWidth  = 18
	blameDateDisplaySize = 10
)

// formatBlame keeps the selectable, searchable read-only editor, but spends
// its width on source rather than repeating a full hash, timestamp, timezone,
// and duplicate source-line number on every row. Contiguous lines from one
// commit share one metadata label and a vertical continuation mark.
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
	metadataWidth := blameHashWidth + 2 + authorWidth + 2 + blameDateDisplaySize + 2
	continuation := strings.Repeat(" ", metadataWidth) + "│ "

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
			output.WriteString("  ")
			output.WriteString(line.Date)
			output.WriteString(strings.Repeat(" ", max(0, blameDateDisplaySize-utf8.RuneCountInString(line.Date))))
			output.WriteString("  │ ")
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
