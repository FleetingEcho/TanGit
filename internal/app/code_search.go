package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	workspaceSearchMaxFileSize     = 8 << 20
	workspaceSearchMaxText         = 240
	workspaceSearchMaxArgumentSize = 16 << 10
)

type workspaceSearchMatch struct {
	Path   string
	Line   int
	Column int
	Text   string
}

type workspaceSearchBackend uint8

const (
	workspaceSearchBackendNone workspaceSearchBackend = iota
	workspaceSearchBackendGo
	workspaceSearchBackendRipgrep
)

var workspaceSearchLookPath = exec.LookPath
var workspaceSearchReadFile = readWorkspaceSearchFile

// searchWorkspaceFiles searches paths in their supplied order. Ripgrep does
// the file scanning and Unicode-aware matching when it is available; the Go
// scanner keeps content search working on systems without it.
func searchWorkspaceFiles(ctx context.Context, root string, paths []string, query string, caseSensitive bool, limit int) []workspaceSearchMatch {
	matches, _, _ := searchWorkspaceFilesWithOptions(ctx, root, paths, workspaceSearchOptions{
		Query: query, CaseSensitive: caseSensitive,
	}, limit)
	return matches
}

func searchWorkspaceFilesWithOptions(ctx context.Context, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, workspaceSearchBackend, error) {
	if options.Query == "" || limit <= 0 || ctx.Err() != nil {
		return nil, workspaceSearchBackendNone, nil
	}
	if _, err := options.regularExpression(); err != nil {
		return nil, workspaceSearchBackendNone, err
	}
	if !options.PreferNative {
		if executable, err := workspaceSearchLookPath("rg"); err == nil {
			if matches, ok := searchWorkspaceFilesRipgrepWithOptions(ctx, executable, root, paths, options, limit); ok {
				return matches, workspaceSearchBackendRipgrep, nil
			}
		}
	}
	matches, err := searchWorkspaceFilesGoWithOptions(ctx, root, paths, options, limit)
	return matches, workspaceSearchBackendGo, err
}

func searchWorkspaceFilesGo(ctx context.Context, root string, paths []string, query string, caseSensitive bool, limit int) []workspaceSearchMatch {
	matches, _ := searchWorkspaceFilesGoWithOptions(ctx, root, paths, workspaceSearchOptions{
		Query: query, CaseSensitive: caseSensitive,
	}, limit)
	return matches
}

func searchWorkspaceFilesGoWithOptions(ctx context.Context, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, error) {
	if options.Query == "" || limit <= 0 || ctx.Err() != nil {
		return nil, nil
	}
	expression, err := options.regularExpression()
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil
	}
	resolvedRoot, err = filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return nil, nil
	}

	var matches []workspaceSearchMatch
	for _, candidate := range paths {
		if ctx.Err() != nil {
			return matches, nil
		}
		// Filter lazily so cancellation does not have to wait for a complete
		// repository-sized allocation before the first context check.
		if !options.pathIncluded(candidate) {
			continue
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		filename, ok := resolveWorkspaceSearchPath(resolvedRoot, cleaned)
		if !ok {
			continue
		}
		data, ok := workspaceSearchReadFile(ctx, filename)
		if !ok || ctx.Err() != nil || len(data) == 0 {
			continue
		}

		lines := bytes.Split(data, []byte{'\n'})
		for lineIndex, rawLine := range lines {
			// A final newline terminates the preceding line; it does not add an
			// extra empty document line for anchors such as ^ and $.
			if lineIndex == len(lines)-1 && len(rawLine) == 0 && len(data) > 0 {
				continue
			}
			if ctx.Err() != nil {
				return matches, nil
			}
			line := strings.TrimSuffix(string(rawLine), "\r")
			column, _, found := workspaceSearchLineRange(line, options, expression)
			if !found {
				continue
			}
			matches = append(matches, workspaceSearchMatch{
				Path:   cleaned,
				Line:   lineIndex + 1,
				Column: column + 1,
				Text:   workspaceSearchSnippet(line, column),
			})
			if len(matches) == limit {
				return matches, nil
			}
		}
	}
	return matches, nil
}

// Ripgrep encodes valid UTF-8 as text and arbitrary file names or contents as
// base64. Decoding into []byte handles the latter without a custom parser.
type workspaceSearchEncodedText struct {
	Text  string `json:"text"`
	Bytes []byte `json:"bytes"`
}

func (t workspaceSearchEncodedText) String() string {
	if t.Text != "" {
		return t.Text
	}
	return string(t.Bytes)
}

type workspaceSearchRipgrepMessage struct {
	Type string `json:"type"`
	Data struct {
		Path         workspaceSearchEncodedText `json:"path"`
		Lines        workspaceSearchEncodedText `json:"lines"`
		LineNumber   int                        `json:"line_number"`
		BinaryOffset *int64                     `json:"binary_offset"`
		Submatches   []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

func searchWorkspaceFilesRipgrep(ctx context.Context, executable, root string, paths []string, query string, caseSensitive bool, limit int) ([]workspaceSearchMatch, bool) {
	return searchWorkspaceFilesRipgrepWithOptions(ctx, executable, root, paths, workspaceSearchOptions{
		Query: query, CaseSensitive: caseSensitive,
	}, limit)
}

func searchWorkspaceFilesRipgrepWithOptions(ctx context.Context, executable, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, bool) {
	if options.Query == "" || limit <= 0 || ctx.Err() != nil {
		return nil, true
	}
	if _, err := options.regularExpression(); err != nil {
		return nil, false
	}
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, false
	}
	resolvedRoot, err = filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return nil, false
	}

	paths = filterWorkspaceSearchPaths(paths, options)
	filenames := make([]string, 0, len(paths))
	logicalPaths := make(map[string]string, len(paths))
	for _, candidate := range paths {
		if ctx.Err() != nil {
			return nil, true
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		// Do not stat or resolve every repository file before starting rg:
		// that serial filesystem walk dominated large searches. Only the much
		// smaller set of files that actually matches needs the full check.
		// Relative operands also keep command lines substantially shorter.
		filenames = append(filenames, cleaned)
		logicalPaths[cleaned] = cleaned
	}
	if len(filenames) == 0 {
		return nil, true
	}

	baseArgumentSize := workspaceSearchArgumentSize(workspaceSearchRipgrepArguments(options, limit, nil))
	var matches []workspaceSearchMatch
	for start := 0; start < len(filenames); {
		// Beyond the operating system's argument limit rg cannot start at all,
		// which used to send large repositories through the native scanner.
		// Ordered batches retain both ripgrep and the original result order.
		argumentSize, end := baseArgumentSize, start
		for end < len(filenames) {
			pathSize := len(filenames[end]) + 1
			if end > start && argumentSize+pathSize > workspaceSearchMaxArgumentSize {
				break
			}
			argumentSize += pathSize
			end++
		}
		batch, ok := searchWorkspaceFilesRipgrepBatch(ctx, executable, resolvedRoot, options, filenames[start:end], logicalPaths, limit-len(matches))
		matches = append(matches, batch...)
		if !ok {
			return nil, false
		}
		if ctx.Err() != nil || len(matches) == limit {
			return matches, true
		}
		start = end
	}
	return matches, true
}

func workspaceSearchRipgrepArguments(options workspaceSearchOptions, limit int, filenames []string) []string {
	caseOption := "--ignore-case"
	if options.CaseSensitive {
		caseOption = "--case-sensitive"
	}
	arguments := []string{"--no-config", "--json", "--threads", "1", caseOption}
	if options.WholeWord {
		arguments = append(arguments, "--word-regexp")
	}
	if !options.RegularExpr {
		arguments = append(arguments, "--fixed-strings")
	}
	arguments = append(arguments,
		"--max-count", strconv.Itoa(limit), "--max-filesize", strconv.Itoa(workspaceSearchMaxFileSize),
		"--encoding", "none", "--no-messages", "--regexp", options.Query, "--",
	)
	return append(arguments, filenames...)
}

func workspaceSearchArgumentSize(arguments []string) int {
	size := 0
	for _, argument := range arguments {
		size += len(argument) + 1
	}
	return size
}

func searchWorkspaceFilesRipgrepBatch(ctx context.Context, executable, resolvedRoot string, options workspaceSearchOptions, filenames []string, logicalPaths map[string]string, limit int) ([]workspaceSearchMatch, bool) {
	expression, expressionErr := options.regularExpression()
	if expressionErr != nil {
		return nil, false
	}
	arguments := workspaceSearchRipgrepArguments(options, limit, filenames)
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = resolvedRoot
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, false
	}
	if err = command.Start(); err != nil {
		return nil, false
	}

	decoder := json.NewDecoder(stdout)
	var matches []workspaceSearchMatch
	var matchFiles []string
	searchableFiles := make(map[string]bool)
	valid, limited, sawSummary := true, false, false
	for !limited {
		var message workspaceSearchRipgrepMessage
		if decodeErr := decoder.Decode(&message); decodeErr != nil {
			if decodeErr != io.EOF {
				valid = false
			}
			break
		}
		filename := filepath.ToSlash(message.Data.Path.String())
		logicalPath, knownPath := logicalPaths[filename]
		if message.Type == "summary" {
			sawSummary = true
		}
		switch message.Type {
		case "match":
			if !knownPath || message.Data.LineNumber <= 0 || len(message.Data.Submatches) == 0 {
				continue
			}
			searchable, checked := searchableFiles[filename]
			if !checked {
				resolvedFilename, resolveOK := resolveWorkspaceSearchPath(resolvedRoot, logicalPath)
				info, statErr := os.Stat(resolvedFilename)
				searchable = resolveOK && statErr == nil && info.Mode().IsRegular() && info.Size() <= workspaceSearchMaxFileSize
				searchableFiles[filename] = searchable
			}
			if !searchable {
				continue
			}
			line := strings.TrimSuffix(message.Data.Lines.String(), "\n")
			line = strings.TrimSuffix(line, "\r")
			// Ripgrep locates candidate lines, then the shared matcher chooses
			// the exact range. This keeps ASCII RE2 classes, Unicode whole-word
			// boundaries identical with native search.
			column, _, found := workspaceSearchLineRange(line, options, expression)
			if !found {
				continue
			}
			matches = append(matches, workspaceSearchMatch{
				Path:   logicalPath,
				Line:   message.Data.LineNumber,
				Column: column + 1,
				Text:   workspaceSearchSnippet(line, column),
			})
			matchFiles = append(matchFiles, filename)
		case "end":
			if !knownPath {
				continue
			}
			if message.Data.BinaryOffset != nil {
				keptMatches := matches[:0]
				keptFiles := matchFiles[:0]
				for index, matchFile := range matchFiles {
					if matchFile == filename {
						continue
					}
					keptMatches = append(keptMatches, matches[index])
					keptFiles = append(keptFiles, matchFile)
				}
				matches, matchFiles = keptMatches, keptFiles
			}
			// Wait for the end record before applying the global cap so a
			// binary file's earlier matches can still be discarded.
			limited = len(matches) >= limit
		}
		if !valid {
			break
		}
	}
	if limited || !valid {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return matches, true
	}
	if !valid {
		return nil, false
	}
	if limited {
		return matches[:limit], true
	}
	if waitErr != nil {
		// A missing indexed file makes rg finish with status 2 after writing a
		// complete summary. That is the native scanner's ordinary skip-missing
		// behavior, not a reason to repeat the entire repository search in Go.
		exitError, exited := waitErr.(*exec.ExitError)
		if !exited || (exitError.ExitCode() != 1 && (exitError.ExitCode() != 2 || !sawSummary)) {
			return nil, false
		}
	}
	return matches, true
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

// workspaceSearchContextReader stops a large file read between chunks after
// its search is cancelled. Regular-file reads remain synchronous and cheap.
type workspaceSearchContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r workspaceSearchContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func readWorkspaceSearchFile(ctx context.Context, filename string) ([]byte, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > workspaceSearchMaxFileSize {
		return nil, false
	}
	reader := workspaceSearchContextReader{ctx: ctx, r: file}
	data, err := io.ReadAll(io.LimitReader(reader, workspaceSearchMaxFileSize+1))
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
// long s and s), so the legacy literal helper retains its source-rune mapping.
func workspaceSearchRange(line, query string, caseSensitive bool) (int, int) {
	return workspaceSearchRangeWithOptions(line, workspaceSearchOptions{Query: query, CaseSensitive: caseSensitive})
}

func workspaceSearchRangeWithOptions(line string, options workspaceSearchOptions) (int, int) {
	expression, err := options.regularExpression()
	if err != nil {
		return -1, -1
	}
	start, end, ok := workspaceSearchLineRange(line, options, expression)
	if !ok {
		return -1, -1
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
