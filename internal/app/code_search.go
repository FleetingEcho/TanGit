package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
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
	workspaceSearchBackendZoekt
)

var workspaceSearchLookPath = exec.LookPath
var workspaceSearchReadFile = readWorkspaceSearchFile

// Scoped searches can opt into generated files by naming a path or glob.
// Discover those files separately from the Git-filtered Explorer and index.
func scopedCodeSearchPaths(ctx context.Context, root string, paths []string, options workspaceSearchOptions, client remoteWorkspaceClient) []string {
	if options.File == "" && options.Folder == "" && len(workspaceSearchGlobs(options.Include)) == 0 {
		return paths
	}
	args := []string{"ls-files", "-z", "--others", "--ignored", "--exclude-standard"}
	var output []byte
	var err error
	if client == nil {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = root
		output, err = command.Output()
	} else {
		result, runErr := client.Run(ctx, root, "git", args...)
		output, err = result.Output, runErr
	}
	if err != nil {
		return paths
	}
	seen := make(map[string]bool, len(paths))
	for _, name := range paths {
		seen[name] = true
	}
	for name := range strings.SplitSeq(string(output), "\x00") {
		if ctx.Err() != nil {
			return paths
		}
		if validCodeRelativePath(name) && !seen[name] && options.pathIncluded(name) {
			paths = append(paths, name)
			seen[name] = true
		}
	}
	return paths
}

// searchWorkspaceFiles searches paths in their supplied order. An explicitly
// built Zoekt index supplies fresh candidate files when available; ripgrep and
// the Go scanner retain cold-search and no-index behavior.
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
		if options.IndexDirectory != "" {
			if matches, ok := searchWorkspaceFilesZoekt(ctx, root, paths, options, limit); ok {
				return matches, workspaceSearchBackendZoekt, nil
			}
			if ctx.Err() != nil {
				return nil, workspaceSearchBackendNone, nil
			}
		}
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
			for _, match := range workspaceSearchLineAllRanges(line, options, expression) {
				column := match[0]
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
			// Return each match on the line, as VS Code does, while retaining
			// the shared literal/regex and Unicode word-boundary semantics.
			for _, match := range workspaceSearchLineAllRanges(line, options, expression) {
				column := match[0]
				matches = append(matches, workspaceSearchMatch{
					Path:   logicalPath,
					Line:   message.Data.LineNumber,
					Column: column + 1,
					Text:   workspaceSearchSnippet(line, column),
				})
				matchFiles = append(matchFiles, filename)
			}
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

// codeSearchRow is either a collapsible file heading (match == -1) or one
// match within that file, so the virtualized list can retain keyboard support.
type codeSearchRow struct {
	path    string
	match   int
	matches int
}

func (r codeSearchRow) isFile() bool { return r.match < 0 }

// buildCodeSearchRows is the allocation-heavy search projection. It is pure
// so a completed search worker can build even a very large result set before
// publishing one immutable slice to the UI thread.
func buildCodeSearchRows(matches []workspaceSearchMatch, collapsed map[string]bool) []codeSearchRow {
	var rows []codeSearchRow
	for first := 0; first < len(matches); {
		path := matches[first].Path
		end := first + 1
		for end < len(matches) && matches[end].Path == path {
			end++
		}
		rows = append(rows, codeSearchRow{path: path, match: -1, matches: end - first})
		if !collapsed[path] {
			for match := first; match < end; match++ {
				rows = append(rows, codeSearchRow{path: path, match: match})
			}
		}
		first = end
	}
	return rows
}

// rebuildCodeSearchRows groups consecutive results from the same sorted file.
// The selected match is kept where possible; collapsing its file selects the
// file heading rather than leaving the list pointing at a hidden result.
func (a *app) rebuildCodeSearchRows() {
	selectedFile := ""
	if a.code.searchSelectedRow >= 0 && a.code.searchSelectedRow < len(a.code.searchRows) {
		row := a.code.searchRows[a.code.searchSelectedRow]
		if row.isFile() {
			selectedFile = row.path
		}
	}
	a.code.searchRows = buildCodeSearchRows(a.code.searchResults, a.code.searchCollapsed)
	a.syncCodeSearchSelection(selectedFile)
}

func (a *app) syncCodeSearchSelection(selectedFile string) {
	if len(a.code.searchRows) == 0 {
		a.code.searchSelected, a.code.searchSelectedRow = -1, -1
		return
	}
	if a.code.searchSelected < 0 || a.code.searchSelected >= len(a.code.searchResults) {
		a.code.searchSelected = 0
	}
	a.code.searchSelectedRow = -1
	for row, item := range a.code.searchRows {
		if selectedFile != "" && item.isFile() && item.path == selectedFile {
			a.code.searchSelectedRow = row
			break
		}
		if selectedFile == "" && item.match == a.code.searchSelected {
			a.code.searchSelectedRow = row
			break
		}
	}
	if a.code.searchSelectedRow < 0 {
		path := a.code.searchResults[a.code.searchSelected].Path
		for row, item := range a.code.searchRows {
			if item.isFile() && item.path == path {
				a.code.searchSelectedRow = row
				break
			}
		}
	}
}

func (a *app) selectCodeSearchRow(row int) {
	if row < 0 || row >= len(a.code.searchRows) {
		return
	}
	a.code.searchSelectedRow = row
	if match := a.code.searchRows[row].match; match >= 0 {
		a.code.searchSelected = match
	}
}

func (a *app) toggleCodeSearchFile(path string) {
	if a.code.searchCollapsed == nil {
		a.code.searchCollapsed = make(map[string]bool)
	}
	a.code.searchCollapsed[path] = !a.code.searchCollapsed[path]
	a.code.searchCollapsedSeq++
	a.rebuildCodeSearchRows()
}

func (a *app) revealCodeSearchMatch(match int) {
	if match < 0 || match >= len(a.code.searchResults) {
		return
	}
	if a.code.searchCollapsed[a.code.searchResults[match].Path] {
		a.code.searchCollapsedSeq++
	}
	delete(a.code.searchCollapsed, a.code.searchResults[match].Path)
	a.code.searchSelected = match
	a.code.searchSelectedRow = -1
	a.rebuildCodeSearchRows()
	for row, item := range a.code.searchRows {
		if item.match == match {
			a.code.searchSelectedRow = row
			a.code.searchList.ScrollIntoView(row)
			return
		}
	}
}

// codeSearchQueryChanged debounces reactive searches. Clearing the field is
// immediate: it cancels stale work, old results, and option highlights at once.
func (a *app) codeSearchQueryChanged() {
	if strings.TrimSpace(a.code.searchQuery) == "" {
		a.beginCodeSearch()
		return
	}
	// Every edit retires the previous search immediately. Only the replacement
	// search is debounced; an old spinner or result callback must not survive
	// while the user is typing a different query.
	if a.code.searchCancel != nil {
		a.code.searchCancel()
		a.code.searchCancel = nil
	}
	a.code.searchToken++
	a.code.searchRunning = false
	a.code.searchLoadingMore = false
	a.code.searchHasMore = false
	a.code.searchAfterFiles = false
	a.code.searchDebounceSeq++
	sequence := a.code.searchDebounceSeq
	scheduleCodeSearch(codeSearchDebounceDelay, func() {
		a.updateChanged(func() bool {
			if sequence != a.code.searchDebounceSeq {
				return false
			}
			a.beginCodeSearch()
			return a.workspaceTab == sectionCode
		})
	})
}

func (a *app) beginCodeSearch() {
	// Query changes do not make repository contents dirty. Search itself
	// verifies the index before and after consulting it; saves and filesystem
	// changes invalidate it explicitly without another background stat pass.
	a.ensureCodeSearchIndex()
	a.code.searchHasMore = false
	a.code.searchLoadingMore = false
	a.startCodeSearch(codeSearchPageSize, false)
}

func (a *app) startCodeSearch(limit int, loadingMore bool) {
	a.code.searchDebounceSeq++
	query := strings.TrimSpace(a.code.searchQuery)
	if a.code.filesLoading && query != "" {
		a.code.searchAfterFiles = true
		return
	}
	a.code.searchAfterFiles = false
	if a.code.searchCancel != nil {
		a.code.searchCancel()
		a.code.searchCancel = nil
	}
	a.code.searchToken++
	token := a.code.searchToken
	a.code.searchSubmitted = query
	folder, validFolder := normalizedCodeSearchFolder(a.code.searchFolder)
	if !validFolder {
		a.code.searchError = tr("Search folder must stay inside the repository")
		a.code.searchSubmittedOptions = workspaceSearchOptions{}
		a.code.searchResults, a.code.searchRows = nil, nil
		a.code.searchBackend = workspaceSearchBackendNone
		a.code.searchRunning = false
		a.code.searchLoadingMore = false
		a.code.searchHasMore = false
		return
	}
	a.code.searchFolder = folder
	a.code.searchFolderDraft = folder
	if a.code.searchScopeFolder != "" && a.code.searchInclude != a.code.searchScopeFolder {
		a.code.searchScopeFolder = ""
	}
	if a.code.searchScopeFile != "" && a.code.searchInclude != a.code.searchScopeFile {
		a.code.searchScopeFile = ""
	}
	searchScopeFolder := a.code.searchScopeFolder
	if searchScopeFolder == "" {
		searchScopeFolder = folder
	}
	a.code.searchSubmittedOptions = workspaceSearchOptions{
		Query:               query,
		CaseSensitive:       a.code.searchCaseSensitive,
		WholeWord:           a.code.searchWholeWord,
		RegularExpr:         a.code.searchRegularExpr,
		PreferNative:        a.code.searchNativeOnly,
		IndexDirectory:      a.codeSearchIndexDirectory(),
		Include:             a.code.searchInclude,
		Exclude:             a.code.searchExclude,
		Folder:              searchScopeFolder,
		File:                a.code.searchScopeFile,
		ExcludedDirectories: a.searchExclusions(),
	}
	a.code.searchError = ""
	if !loadingMore {
		a.code.searchResults, a.code.searchRows = nil, nil
		a.code.searchBackend = workspaceSearchBackendNone
		if a.code.searchCollapsed != nil {
			a.code.searchCollapsedSeq++
		}
		a.code.searchCollapsed = nil
		a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	}
	if query == "" || a.snapshot.Root == "" {
		if query == "" {
			a.code.searchCaseSensitive = false
			a.code.searchWholeWord = false
			a.code.searchRegularExpr = false
			a.code.searchInclude, a.code.searchExclude = "", ""
			a.code.searchScopeFolder, a.code.searchScopeFile = "", ""
			a.code.searchFiltersOpen = false
			// The already captured empty options remain the single source of
			// truth for highlighting; no cleared option can leak into a later run.
			a.code.searchSubmittedOptions = workspaceSearchOptions{}
		}
		a.code.searchRunning = false
		a.code.searchLoadingMore = false
		a.code.searchHasMore = false
		return
	}
	root := a.snapshot.Root
	paths := append([]string(nil), a.code.files...)
	options := a.code.searchSubmittedOptions
	ctx, cancel := context.WithCancel(a.repositoryWork.Context())
	a.code.searchCancel = cancel
	a.code.searchRunning = true
	a.code.searchLoadingMore = loadingMore
	remote := a.location.IsRemote()
	var remoteClient remoteWorkspaceClient
	var remoteErr error
	if remote {
		remoteClient, remoteErr = a.remoteWorkspaceClient()
	}
	collapsed := maps.Clone(a.code.searchCollapsed)
	collapsedRevision := a.code.searchCollapsedSeq
	go func() {
		defer a.recoverBackground()
		var matches []workspaceSearchMatch
		var backend workspaceSearchBackend
		var err error
		if remote {
			err = remoteErr
			if err == nil {
				paths = scopedCodeSearchPaths(ctx, root, paths, options, remoteClient)
				matches, backend, err = searchRemoteWorkspaceFiles(ctx, remoteClient, root, paths, options, limit)
			}
		} else {
			listed := len(paths)
			paths = scopedCodeSearchPaths(ctx, root, paths, options, nil)
			if len(paths) != listed {
				// The index covers the Explorer list. Scan the expanded scope directly.
				options.IndexDirectory = ""
			}
			matches, backend, err = runCodeSearch(ctx, root, paths, options, limit)
		}
		if ctx.Err() == nil {
			matches = rankWorkspaceSearchMatches(matches, options)
		}
		rows := buildCodeSearchRows(matches, collapsed)
		a.updateChanged(func() bool {
			if token != a.code.searchToken {
				return false
			}
			a.code.searchRunning = false
			a.code.searchLoadingMore = false
			a.code.searchCancel = nil
			a.code.searchError = safeError(err)
			a.code.searchResults = matches
			a.code.searchBackend = backend
			a.code.searchHasMore = err == nil && len(matches) == limit
			if !loadingMore {
				a.code.searchSelected = 0
			}
			selectedFile := ""
			if a.code.searchSelectedRow >= 0 && a.code.searchSelectedRow < len(a.code.searchRows) &&
				a.code.searchRows[a.code.searchSelectedRow].isFile() {
				selectedFile = a.code.searchRows[a.code.searchSelectedRow].path
			}
			if collapsedRevision != a.code.searchCollapsedSeq {
				// The user folded a file while this read was running. Preserve
				// that newer UI action rather than repainting the worker's stale
				// snapshot with the opposite expansion state.
				rows = buildCodeSearchRows(matches, a.code.searchCollapsed)
			}
			a.code.searchRows = rows
			a.syncCodeSearchSelection(selectedFile)
			if !loadingMore && a.code.searchHasMore {
				// Publish the first useful page, then complete the query in one
				// second scan. Repeatedly enlarging a cap would rescan every
				// earlier match once per page and leave the count incomplete
				// until the user reached the end of the result list.
				a.startCodeSearch(codeSearchAllResults, true)
			}
			return a.workspaceTab == sectionCode
		})
	}()
}

func (a *app) openCodeSearchMatch(match workspaceSearchMatch) {
	a.openCodeSearchMatchMode(match, false)
}

func (a *app) openCodeSearchMatchMode(match workspaceSearchMatch, pinned bool) {
	a.openCodeLocationMode(match.Path, match.Line, match.Column-1, pinned)
	document := a.code.documents[match.Path]
	if document == nil {
		return
	}
	// Keep the submitted options with the pending location: asynchronous file
	// loading must select this result even if another search starts meanwhile.
	document.pendingLine = match.Line
	document.pendingColumn = max(0, match.Column-1)
	document.pendingUTF16 = false
	document.pendingSelection = true
	document.pendingSearchOptions = a.code.searchSubmittedOptions
	a.revealCodeLine(document)
}

func (a *app) navigateCodeSearch(direction int) {
	match, ok := wrappedSearchRow(a.code.searchSelected, len(a.code.searchResults), direction)
	if !ok {
		return
	}
	a.revealCodeSearchMatch(match)
	a.openCodeSearchMatch(a.code.searchResults[match])
}
