package agentassist

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type sourceLine struct {
	text       string
	start, end int
}

func splitSourceLines(text string) []sourceLine {
	if text == "" {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	lines := make([]sourceLine, len(parts))
	offset := 0
	for i, part := range parts {
		lines[i] = sourceLine{text: part, start: offset, end: offset + len(part)}
		offset += len(part)
	}
	return lines
}

func withoutLineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}

type conflictMarker int

const (
	markerNone conflictMarker = iota
	markerCurrent
	markerBase
	markerSeparate
	markerIncoming
)

func conflictMarkerKind(line string, size int) (conflictMarker, string) {
	line = withoutLineEnding(line)
	for _, candidate := range []struct {
		prefix string
		kind   conflictMarker
	}{
		{"<", markerCurrent},
		{"|", markerBase},
		{"=", markerSeparate},
		{">", markerIncoming},
	} {
		marker := strings.Repeat(candidate.prefix, size)
		if !strings.HasPrefix(line, marker) {
			continue
		}
		remainder := line[size:]
		if remainder != "" && remainder[0] != ' ' && remainder[0] != '\t' {
			return markerNone, ""
		}
		return candidate.kind, strings.TrimSpace(remainder)
	}
	return markerNone, ""
}

type conflictParseState int

const (
	conflictCurrent conflictParseState = iota
	conflictBase
	conflictIncoming
)

type conflictBuilder struct {
	startLine, startOffset  int
	state                   conflictParseState
	currentLabel            string
	baseLabel               string
	hasBase                 bool
	current, base, incoming strings.Builder
}

func parseConflictRegions(text string, markerSize int, artifact Artifact) ([]ConflictRegion, error) {
	if markerSize == 0 {
		markerSize = 7
	}
	lines := splitSourceLines(text)
	regions := make([]ConflictRegion, 0)
	var open *conflictBuilder
	for i, line := range lines {
		kind, label := conflictMarkerKind(line.text, markerSize)
		if open == nil {
			if kind == markerCurrent {
				open = &conflictBuilder{startLine: i + 1, startOffset: line.start, currentLabel: label}
			}
			continue
		}
		if kind == markerCurrent {
			return nil, protocolError("nested or unterminated conflict region beginning at line %d", open.startLine)
		}
		switch open.state {
		case conflictCurrent:
			switch kind {
			case markerBase:
				open.hasBase, open.baseLabel, open.state = true, label, conflictBase
			case markerSeparate:
				open.state = conflictIncoming
			case markerIncoming:
				return nil, protocolError("conflict region at line %d has no separator", open.startLine)
			default:
				open.current.WriteString(line.text)
			}
		case conflictBase:
			switch kind {
			case markerSeparate:
				open.state = conflictIncoming
			case markerBase:
				return nil, protocolError("conflict region at line %d has duplicate base markers", open.startLine)
			case markerIncoming:
				return nil, protocolError("conflict region at line %d has no separator after base", open.startLine)
			default:
				open.base.WriteString(line.text)
			}
		case conflictIncoming:
			if kind == markerIncoming {
				endOffset := line.end
				raw := text[open.startOffset:endOffset]
				region := ConflictRegion{
					StartLine: open.startLine, EndLine: i + 1,
					CurrentLabel: open.currentLabel, BaseLabel: open.baseLabel, IncomingLabel: label,
					HasBase: open.hasBase, Current: open.current.String(), Base: open.base.String(), Incoming: open.incoming.String(),
				}
				region.ID = identityDigest("region", artifact.ID, artifact.Path, strconv.Itoa(region.StartLine), strconv.Itoa(region.EndLine), raw)
				regions = append(regions, region)
				open = nil
				continue
			}
			if kind == markerBase || kind == markerSeparate {
				return nil, protocolError("conflict region at line %d has misplaced incoming content marker", open.startLine)
			}
			open.incoming.WriteString(line.text)
		}
	}
	if open != nil {
		return nil, protocolError("unterminated conflict region beginning at line %d", open.startLine)
	}
	return regions, nil
}

var unifiedHunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?:.*)$`)

type parsedDiffHunk struct {
	hunk DiffHunk
	raw  string
}

func parseUnifiedDiff(text string, artifact Artifact) ([]DiffFile, error) {
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return nil, binaryError("diff contains binary bytes")
	}
	lines := splitSourceLines(text)
	for _, line := range lines {
		plain := withoutLineEnding(line.text)
		if plain == "GIT binary patch" || (strings.HasPrefix(plain, "Binary files ") && strings.HasSuffix(plain, " differ")) {
			return nil, binaryError("binary patches cannot be inspected or grouped")
		}
	}
	files := make([]DiffFile, 0)
	for i := 0; i < len(lines); {
		if !isDiffFileStart(lines, i) {
			i++
			continue
		}
		file, next, err := parseDiffFile(lines, i, len(files), artifact)
		if err != nil {
			return nil, err
		}
		for _, previous := range files {
			if previous.Path == file.Path {
				return nil, duplicateError("diff file path %q", file.Path)
			}
		}
		files = append(files, file)
		i = next
	}
	if len(files) == 0 && strings.TrimSpace(text) != "" {
		return nil, protocolError("source contains no unified-diff file headers")
	}
	return files, nil
}

func isDiffFileStart(lines []sourceLine, index int) bool {
	plain := withoutLineEnding(lines[index].text)
	if strings.HasPrefix(plain, "diff --git ") {
		return true
	}
	return strings.HasPrefix(plain, "--- ") && index+1 < len(lines) && strings.HasPrefix(withoutLineEnding(lines[index+1].text), "+++ ")
}

func parseDiffFile(lines []sourceLine, start, ordinal int, artifact Artifact) (DiffFile, int, error) {
	file := DiffFile{Hunks: make([]DiffHunk, 0)}
	var parsed []parsedDiffHunk
	headerSeen := false
	gitHeader := strings.HasPrefix(withoutLineEnding(lines[start].text), "diff --git ")
	i := start
	for i < len(lines) {
		if i > start && strings.HasPrefix(withoutLineEnding(lines[i].text), "diff --git ") {
			break
		}
		if i > start && !gitHeader && isDiffFileStart(lines, i) {
			break
		}
		plain := withoutLineEnding(lines[i].text)
		if strings.HasPrefix(plain, "@@ ") {
			hunk, raw, next, err := parseDiffHunk(lines, i)
			if err != nil {
				return DiffFile{}, 0, fmt.Errorf("file %d: %w", ordinal, err)
			}
			parsed = append(parsed, parsedDiffHunk{hunk: hunk, raw: raw})
			i = next
			continue
		}
		if strings.HasPrefix(plain, "--- ") && i+1 < len(lines) && strings.HasPrefix(withoutLineEnding(lines[i+1].text), "+++ ") {
			if headerSeen {
				return DiffFile{}, 0, protocolError("file %d has duplicate ---/+++ headers", ordinal)
			}
			oldPath, err := parseDiffPath(strings.TrimPrefix(plain, "--- "), true)
			if err != nil {
				return DiffFile{}, 0, fmt.Errorf("file %d old path: %w", ordinal, err)
			}
			newPath, err := parseDiffPath(strings.TrimPrefix(withoutLineEnding(lines[i+1].text), "+++ "), false)
			if err != nil {
				return DiffFile{}, 0, fmt.Errorf("file %d new path: %w", ordinal, err)
			}
			file.OldPath, file.Path = oldPath, newPath
			if file.Path == "" {
				file.Path = oldPath
				file.OldPath = ""
			}
			headerSeen = true
			i += 2
			continue
		}
		i++
	}
	if file.Path == "" && gitHeader {
		oldPath, newPath, err := parseDiffGitPaths(withoutLineEnding(lines[start].text))
		if err == nil {
			file.OldPath, file.Path = oldPath, newPath
			if file.Path == "" {
				file.Path = oldPath
				file.OldPath = ""
			}
		}
	}
	if file.Path == "" {
		return DiffFile{}, 0, protocolError("file %d has no valid ---/+++ path header", ordinal)
	}
	end := i
	raw := joinSourceLines(lines[start:end])
	file.ID = identityDigest("file", artifact.ID, strconv.Itoa(ordinal), file.OldPath, file.Path, raw)
	file.Hunks = make([]DiffHunk, len(parsed))
	for hunkIndex, item := range parsed {
		hunk := item.hunk
		hunk.ID = identityDigest("hunk", file.ID, strconv.Itoa(hunkIndex), item.raw)
		for lineIndex := range hunk.Lines {
			line := &hunk.Lines[lineIndex]
			line.ID = identityDigest("line", hunk.ID, strconv.Itoa(lineIndex), string(line.Kind), strconv.Itoa(line.OldLine), strconv.Itoa(line.NewLine), line.Text)
		}
		file.Hunks[hunkIndex] = hunk
	}
	return file, end, nil
}

func joinSourceLines(lines []sourceLine) string {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line.text)
	}
	return builder.String()
}

func parseDiffHunk(lines []sourceLine, start int) (DiffHunk, string, int, error) {
	header := withoutLineEnding(lines[start].text)
	matches := unifiedHunkHeader.FindStringSubmatch(header)
	if matches == nil {
		return DiffHunk{}, "", 0, protocolError("malformed hunk header %q", header)
	}
	oldStart, oldCount, newStart, newCount, err := hunkCoordinates(matches)
	if err != nil {
		return DiffHunk{}, "", 0, err
	}
	hunk := DiffHunk{Header: header, OldStart: oldStart, OldLines: oldCount, NewStart: newStart, NewLines: newCount, Lines: make([]DiffLine, 0)}
	oldLine, newLine, oldSeen, newSeen := oldStart, newStart, 0, 0
	i := start + 1
	for i < len(lines) {
		plain := withoutLineEnding(lines[i].text)
		if oldSeen == oldCount && newSeen == newCount && !strings.HasPrefix(plain, `\`) {
			break
		}
		if strings.HasPrefix(plain, "diff --git ") || strings.HasPrefix(plain, "@@ ") {
			return DiffHunk{}, "", 0, protocolError("hunk %q ended before its declared line counts", header)
		}
		if plain == "" {
			return DiffHunk{}, "", 0, protocolError("hunk %q contains an unprefixed empty line", header)
		}
		line := DiffLine{}
		prefix := plain[:1]
		line.Text = plain[1:]
		switch prefix {
		case " ":
			line.Kind, line.OldLine, line.NewLine = LineContext, oldLine, newLine
			oldLine, newLine, oldSeen, newSeen = oldLine+1, newLine+1, oldSeen+1, newSeen+1
		case "+":
			line.Kind, line.NewLine = LineAdd, newLine
			newLine, newSeen = newLine+1, newSeen+1
		case "-":
			line.Kind, line.OldLine = LineDelete, oldLine
			oldLine, oldSeen = oldLine+1, oldSeen+1
		case `\`:
			line.Kind, line.Text = LineMetadata, plain
		default:
			return DiffHunk{}, "", 0, protocolError("hunk %q contains invalid line %q", header, plain)
		}
		if oldSeen > oldCount || newSeen > newCount {
			return DiffHunk{}, "", 0, protocolError("hunk %q exceeds its declared line counts", header)
		}
		hunk.Lines = append(hunk.Lines, line)
		i++
	}
	if oldSeen != oldCount || newSeen != newCount {
		return DiffHunk{}, "", 0, protocolError("hunk %q has old/new counts %d/%d, expected %d/%d", header, oldSeen, newSeen, oldCount, newCount)
	}
	return hunk, joinSourceLines(lines[start:i]), i, nil
}

func hunkCoordinates(matches []string) (int, int, int, int, error) {
	values := [4]int{}
	for i, raw := range []string{matches[1], matches[2], matches[3], matches[4]} {
		if raw == "" && (i == 1 || i == 3) {
			values[i] = 1
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return 0, 0, 0, 0, protocolError("invalid hunk coordinate %q", raw)
		}
		values[i] = value
	}
	return values[0], values[1], values[2], values[3], nil
}

func parseDiffPath(raw string, old bool) (string, error) {
	raw = strings.TrimSpace(strings.SplitN(raw, "\t", 2)[0])
	if raw == "/dev/null" {
		return "", nil
	}
	var value string
	if strings.HasPrefix(raw, `"`) {
		end := quotedPrefixLength(raw)
		if end == 0 {
			return "", protocolError("unterminated quoted path")
		}
		unquoted, err := strconv.Unquote(raw[:end])
		if err != nil {
			return "", protocolError("invalid quoted path: %v", err)
		}
		value = unquoted
	} else {
		value = raw
	}
	if old {
		value = strings.TrimPrefix(value, "a/")
	} else {
		value = strings.TrimPrefix(value, "b/")
	}
	if err := validateSourcePath(value); err != nil {
		return "", err
	}
	return value, nil
}

func quotedPrefixLength(raw string) int {
	escaped := false
	for i := 1; i < len(raw); i++ {
		if escaped {
			escaped = false
			continue
		}
		if raw[i] == '\\' {
			escaped = true
			continue
		}
		if raw[i] == '"' {
			return i + 1
		}
	}
	return 0
}

// parseDiffGitPaths covers ordinary and C-quoted mode-only Git headers. Text
// patches still take their authoritative paths from ---/+++ headers.
func parseDiffGitPaths(line string) (string, string, error) {
	words, err := splitGitHeaderWords(strings.TrimPrefix(line, "diff --git "))
	if err != nil || len(words) != 2 {
		return "", "", protocolError("invalid diff --git paths")
	}
	oldPath, err := parseDiffPath(words[0], true)
	if err != nil {
		return "", "", err
	}
	newPath, err := parseDiffPath(words[1], false)
	if err != nil {
		return "", "", err
	}
	return oldPath, newPath, nil
}

func splitGitHeaderWords(raw string) ([]string, error) {
	words := make([]string, 0, 2)
	for len(strings.TrimSpace(raw)) != 0 {
		raw = strings.TrimLeft(raw, " \t")
		if raw[0] == '"' {
			end := quotedPrefixLength(raw)
			if end == 0 {
				return nil, protocolError("unterminated quoted diff header")
			}
			word, err := strconv.Unquote(raw[:end])
			if err != nil {
				return nil, err
			}
			words = append(words, word)
			raw = raw[end:]
			continue
		}
		end := strings.IndexAny(raw, " \t")
		if end < 0 {
			words = append(words, raw)
			break
		}
		words = append(words, raw[:end])
		raw = raw[end:]
	}
	return words, nil
}
