package app

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// workspaceSearchOptions is captured when a search starts, so editing another
// field cannot change the meaning or highlighting of results already shown.
type workspaceSearchOptions struct {
	Query         string
	CaseSensitive bool
	WholeWord     bool
	RegularExpr   bool
	Include       string
	Exclude       string
	// Folder is the literal repository-relative scope chosen by Search in
	// folder. Include remains the editable glob text shown in the search panel.
	Folder string
	// ExcludedDirectories is an immutable, normalized snapshot of the user's
	// configured folder names, excluded before either backend scans any file.
	ExcludedDirectories []string
}

func (options workspaceSearchOptions) regularExpression() (*regexp.Regexp, error) {
	if !options.RegularExpr {
		return nil, nil
	}
	pattern := options.Query
	if !options.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile("(?m)" + pattern)
}

// workspaceSearchLineRange returns the first match on one line. Literal
// matching keeps the repository search's Unicode-aware byte columns.
func workspaceSearchLineRange(line string, options workspaceSearchOptions, expression *regexp.Regexp) (int, int, bool) {
	if options.Query == "" {
		return 0, 0, false
	}
	if options.RegularExpr {
		if expression == nil {
			return 0, 0, false
		}
		for _, match := range expression.FindAllStringSubmatchIndex(line, -1) {
			if !workspaceSearchWordBoundary(line, match[0], match[1], options.WholeWord) {
				continue
			}
			return match[0], match[1], true
		}
		return 0, 0, false
	}

	queryASCII := workspaceSearchASCII(options.Query)
	foldedQuery := options.Query
	if !options.CaseSensitive {
		if queryASCII {
			foldedQuery = strings.ToLower(options.Query)
		} else {
			foldedQuery = foldWorkspaceSearchText(options.Query).text
		}
	}
	for base := 0; base <= len(line); {
		column := workspaceSearchIndex(line[base:], options.Query, foldedQuery, queryASCII, options.CaseSensitive)
		if column < 0 {
			break
		}
		start := base + column
		end := start
		for range utf8.RuneCountInString(options.Query) {
			if end >= len(line) {
				break
			}
			_, size := utf8.DecodeRuneInString(line[end:])
			end += size
		}
		if workspaceSearchWordBoundary(line, start, end, options.WholeWord) {
			return start, end, true
		}
		if start >= len(line) {
			break
		}
		_, size := utf8.DecodeRuneInString(line[start:])
		base = start + size
	}
	return 0, 0, false
}

func workspaceSearchWordBoundary(text string, start, end int, required bool) bool {
	if !required {
		return true
	}
	return (start == 0 || !workspaceSearchWordRuneBefore(text, start)) &&
		(end == len(text) || !workspaceSearchWordRuneAt(text, end))
}

func workspaceSearchWordRuneBefore(text string, offset int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:offset])
	return workspaceSearchWordRune(r)
}

func workspaceSearchWordRuneAt(text string, offset int) bool {
	r, _ := utf8.DecodeRuneInString(text[offset:])
	return workspaceSearchWordRune(r)
}

func workspaceSearchWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// These are dependency mirrors and generated output, not repository source.
// Git tracks some of them in large repositories, so gitignore-aware discovery
// alone does not keep their contents out of ripgrep's expensive file scan.
var workspaceSearchDefaultExcludedDirectories = []string{
	".git", ".hg", ".svn",
	"node_modules", "bower_components",
	"dist", "build", ".build", "target", "coverage",
	".next", ".nuxt", ".nx", ".turbo",
	".cache", ".parcel-cache", ".gradle",
}

func defaultWorkspaceSearchExcludedDirectories() []string {
	return normalizeWorkspaceSearchExcludedDirectories(workspaceSearchDefaultExcludedDirectories)
}

// normalizeWorkspaceSearchExcludedDirectories accepts folder names only, and
// returns a sorted, duplicate-free, non-nil slice. In particular, an empty
// configured list stays empty instead of silently restoring the defaults.
func normalizeWorkspaceSearchExcludedDirectories(directories []string) []string {
	normalized := make([]string, 0, len(directories))
	seen := make(map[string]bool, len(directories))
	for _, directory := range directories {
		directory = strings.TrimSpace(directory)
		if directory == "" || directory == "." || directory == ".." ||
			strings.ContainsAny(directory, `/\\`) || seen[directory] {
			continue
		}
		seen[directory] = true
		normalized = append(normalized, directory)
	}
	sort.Strings(normalized)
	return normalized
}

func workspaceSearchDirectoryExcluded(name string, directories []string) bool {
	segments := strings.Split(name, "/")
	// The final segment is a file name, not one of its parent folders.
	for _, segment := range segments[:len(segments)-1] {
		index := sort.SearchStrings(directories, segment)
		if index < len(directories) && directories[index] == segment {
			return true
		}
	}
	return false
}

func filterWorkspaceSearchPaths(paths []string, options workspaceSearchOptions) []string {
	filtered := make([]string, 0, len(paths))
	for _, candidate := range paths {
		if options.pathIncluded(candidate) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func (options workspaceSearchOptions) pathIncluded(name string) bool {
	name = path.Clean(filepathToSlash(name))
	if workspaceSearchDirectoryExcluded(name, options.ExcludedDirectories) {
		return false
	}
	folder := path.Clean(filepathToSlash(strings.TrimSpace(options.Folder)))
	if folder != "." && folder != "" && !strings.HasPrefix(name, folder+"/") {
		return false
	}
	include := options.Include
	if folder != "." && folder != "" && path.Clean(filepathToSlash(strings.TrimSpace(include))) == folder {
		include = ""
	}
	return workspaceSearchPathIncluded(name, include, options.Exclude)
}

func workspaceSearchPathIncluded(name, include, exclude string) bool {
	name = path.Clean(filepathToSlash(name))
	if patterns := workspaceSearchGlobs(include); len(patterns) > 0 && !workspaceSearchAnyGlob(patterns, name) {
		return false
	}
	patterns := workspaceSearchGlobs(exclude)
	return len(patterns) == 0 || !workspaceSearchAnyGlob(patterns, name)
}

func filepathToSlash(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

func workspaceSearchGlobs(value string) []string {
	var patterns []string
	for _, pattern := range strings.Split(value, ",") {
		pattern = strings.TrimSpace(filepathToSlash(pattern))
		pattern = strings.TrimPrefix(pattern, "./")
		pattern = strings.TrimSuffix(pattern, "/")
		if pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

func workspaceSearchAnyGlob(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if workspaceSearchGlob(pattern, name) {
			return true
		}
	}
	return false
}

// workspaceSearchGlob covers the search panel's path, folder, *, ?, **, and
// character-class patterns. A pattern without a slash can match any segment,
// as VS Code's files-to-include field does for names such as *.go.
func workspaceSearchGlob(pattern, name string) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		if path.Base(name) == pattern && !strings.Contains(pattern, "/") {
			return true
		}
		return name == pattern || strings.HasPrefix(name, strings.TrimSuffix(pattern, "/")+"/")
	}
	if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
		for _, segment := range strings.Split(name, "/") {
			if matched, err := path.Match(pattern, segment); err == nil && matched {
				return true
			}
		}
		return false
	}
	if !strings.Contains(pattern, "**") {
		matched, err := path.Match(pattern, name)
		return err == nil && matched
	}
	expression := workspaceSearchGlobExpression(pattern)
	matched, err := regexp.MatchString(expression, name)
	return err == nil && matched
}

func workspaceSearchGlobExpression(pattern string) string {
	var result strings.Builder
	result.WriteString("^")
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				for index+1 < len(pattern) && pattern[index+1] == '*' {
					index++
				}
				if index+1 < len(pattern) && pattern[index+1] == '/' {
					result.WriteString("(?:.*/)?")
					index += 2
					continue
				}
				result.WriteString(".*")
				index++
				continue
			}
			result.WriteString("[^/]*")
			index++
		case '?':
			result.WriteString("[^/]")
			index++
		case '[':
			end := strings.IndexByte(pattern[index+1:], ']')
			if end < 0 {
				result.WriteString("\\[")
				index++
				continue
			}
			end += index + 1
			class := pattern[index+1 : end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			result.WriteString("[" + class + "]")
			index = end + 1
		default:
			result.WriteString(regexp.QuoteMeta(pattern[index : index+1]))
			index++
		}
	}
	result.WriteString("$")
	return result.String()
}
