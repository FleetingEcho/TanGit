package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	workspaceSearchMaxFileSize = 8 << 20
	workspaceSearchMaxText     = 240
)

type workspaceSearchMatch struct {
	Path   string
	Line   int
	Column int
	Text   string
}

// searchWorkspaceFiles searches paths in their supplied order. Its caller can
// run it asynchronously and discard results from stale searches.
func searchWorkspaceFiles(ctx context.Context, root string, paths []string, query string, caseSensitive bool, limit int) []workspaceSearchMatch {
	if query == "" || limit <= 0 || ctx.Err() != nil {
		return nil
	}

	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	resolvedRoot, err = filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return nil
	}

	queryASCII := workspaceSearchASCII(query)
	foldedQuery := query
	if !caseSensitive {
		if queryASCII {
			foldedQuery = strings.ToLower(query)
		} else {
			foldedQuery = foldWorkspaceSearchText(query).text
		}
	}

	var matches []workspaceSearchMatch
	for _, candidate := range paths {
		if ctx.Err() != nil {
			return matches
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		filename, ok := resolveWorkspaceSearchPath(resolvedRoot, cleaned)
		if !ok {
			continue
		}
		data, ok := readWorkspaceSearchFile(filename)
		if !ok || ctx.Err() != nil {
			continue
		}

		for lineIndex, rawLine := range bytes.Split(data, []byte{'\n'}) {
			if ctx.Err() != nil {
				return matches
			}
			line := strings.TrimSuffix(string(rawLine), "\r")
			column := workspaceSearchIndex(line, query, foldedQuery, queryASCII, caseSensitive)
			if column < 0 {
				continue
			}
			matches = append(matches, workspaceSearchMatch{
				Path:   cleaned,
				Line:   lineIndex + 1,
				Column: column + 1,
				Text:   workspaceSearchSnippet(line, column),
			})
			if len(matches) == limit {
				return matches
			}
		}
	}
	return matches
}

func cleanWorkspaceSearchPath(name string) (string, bool) {
	if name == "" || strings.ContainsRune(name, '\x00') || path.IsAbs(name) || filepath.IsAbs(name) {
		return "", false
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", false
	}
	return cleaned, true
}

func resolveWorkspaceSearchPath(root, name string) (string, bool) {
	filename, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, filename)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return filename, true
}

func readWorkspaceSearchFile(filename string) ([]byte, bool) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > workspaceSearchMaxFileSize {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(file, workspaceSearchMaxFileSize+1))
	if err != nil || len(data) > workspaceSearchMaxFileSize || bytes.IndexByte(data, 0) >= 0 {
		return nil, false
	}
	return data, true
}

func workspaceSearchIndex(line, query, foldedQuery string, queryASCII, caseSensitive bool) int {
	if caseSensitive {
		return strings.Index(line, query)
	}
	if queryASCII && workspaceSearchASCII(line) {
		return strings.Index(strings.ToLower(line), foldedQuery)
	}
	if !utf8.ValidString(line) || !utf8.ValidString(query) {
		return strings.Index(lowerWorkspaceSearchASCII(line), lowerWorkspaceSearchASCII(query))
	}
	foldedLine := foldWorkspaceSearchText(line)
	index := strings.Index(foldedLine.text, foldedQuery)
	if index < 0 {
		return -1
	}
	foldedRune := utf8.RuneCountInString(foldedLine.text[:index])
	return foldedLine.sourceOffsets[foldedRune]
}

// workspaceSearchRange returns byte offsets for the source text that produced
// the match. Unicode case folding can change UTF-8 byte lengths (for example,
// long s and s), so the end advances by source runes rather than len(query).
func workspaceSearchRange(line, query string, caseSensitive bool) (int, int) {
	if query == "" {
		return -1, -1
	}
	queryASCII := workspaceSearchASCII(query)
	foldedQuery := query
	if !caseSensitive {
		if queryASCII {
			foldedQuery = strings.ToLower(query)
		} else {
			foldedQuery = foldWorkspaceSearchText(query).text
		}
	}
	start := workspaceSearchIndex(line, query, foldedQuery, queryASCII, caseSensitive)
	if start < 0 {
		return -1, -1
	}
	end := start
	for range utf8.RuneCountInString(query) {
		if end >= len(line) {
			break
		}
		_, size := utf8.DecodeRuneInString(line[end:])
		end += size
	}
	return start, end
}

func workspaceSearchASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func lowerWorkspaceSearchASCII(value string) string {
	lowered := []byte(value)
	for index, char := range lowered {
		if char >= 'A' && char <= 'Z' {
			lowered[index] = char + ('a' - 'A')
		}
	}
	return string(lowered)
}

type workspaceSearchFoldedText struct {
	text          string
	sourceOffsets []int
}

// foldWorkspaceSearchText uses the same simple Unicode folding classes as
// strings.EqualFold and retains source byte offsets for UI columns.
func foldWorkspaceSearchText(value string) workspaceSearchFoldedText {
	var folded strings.Builder
	folded.Grow(len(value))
	offsets := make([]int, 0, utf8.RuneCountInString(value))
	for offset, char := range value {
		offsets = append(offsets, offset)
		canonical := char
		for next := unicode.SimpleFold(char); next != char; next = unicode.SimpleFold(next) {
			if next < canonical {
				canonical = next
			}
		}
		folded.WriteRune(unicode.ToLower(canonical))
	}
	return workspaceSearchFoldedText{text: folded.String(), sourceOffsets: offsets}
}

func workspaceSearchSnippet(line string, matchByte int) string {
	total := utf8.RuneCountInString(line)
	if total <= workspaceSearchMaxText {
		return line
	}
	matchRune := utf8.RuneCountInString(line[:matchByte])
	start := 0
	if matchRune > workspaceSearchMaxText*3/4 {
		start = matchRune - workspaceSearchMaxText/2
	}
	if start+workspaceSearchMaxText > total {
		start = total - workspaceSearchMaxText
	}
	end := start + workspaceSearchMaxText
	runes := []rune(line)
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < total {
		snippet += "…"
	}
	return snippet
}
