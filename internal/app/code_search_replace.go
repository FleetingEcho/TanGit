package app

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"
)

type workspaceSearchReplacement struct {
	text  string
	edits []editor.Edit
	count int
}

// workspaceSearchLineAllRanges uses the canonical search matcher to return
// every non-overlapping match on a line. Search results and replacement use the
// same occurrence granularity, matching VS Code's result counts.
func workspaceSearchLineAllRanges(line string, options workspaceSearchOptions, expression *regexp.Regexp) [][]int {
	if options.Query == "" {
		return nil
	}
	if options.RegularExpr {
		if expression == nil {
			return nil
		}
		matches := expression.FindAllStringSubmatchIndex(line, -1)
		filtered := matches[:0]
		for _, match := range matches {
			if workspaceSearchWordBoundary(line, match[0], match[1], options.WholeWord) {
				filtered = append(filtered, match)
			}
		}
		return filtered
	}
	var matches [][]int
	for base := 0; base <= len(line); {
		start, end, found := workspaceSearchLineRange(line[base:], options, nil)
		if !found {
			break
		}
		start += base
		end += base
		matches = append(matches, []int{start, end})
		base = end
	}
	return matches
}

// planWorkspaceSearchReplacement constructs all edits against the original
// byte offsets. The same plan can update an editor buffer in one undo step or
// be serialized directly for a closed file.
func planWorkspaceSearchReplacement(content string, options workspaceSearchOptions, replacement string, preserveCase bool) (workspaceSearchReplacement, error) {
	expression, err := options.regularExpression()
	if err != nil {
		return workspaceSearchReplacement{}, err
	}
	if content == "" || options.Query == "" {
		return workspaceSearchReplacement{text: content}, nil
	}
	var output strings.Builder
	output.Grow(len(content))
	var edits []editor.Edit
	count := 0
	for lineStart := 0; lineStart < len(content); {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		next := len(content)
		if lineEnd < 0 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
			next = lineEnd + 1
		}
		textEnd := lineEnd
		if textEnd > lineStart && content[textEnd-1] == '\r' {
			textEnd--
		}
		line := content[lineStart:textEnd]
		matches := workspaceSearchLineAllRanges(line, options, expression)
		previous := 0
		for _, match := range matches {
			start, end := match[0], match[1]
			value := replacement
			if options.RegularExpr {
				value = string(expression.ExpandString(nil, replacement, line, match))
			}
			if preserveCase && !options.CaseSensitive {
				value = preserveWorkspaceSearchCase(line[start:end], value)
			}
			output.WriteString(line[previous:start])
			output.WriteString(value)
			edits = append(edits, editor.Edit{Start: lineStart + start, End: lineStart + end, Text: value})
			previous = end
			count++
		}
		output.WriteString(line[previous:])
		output.WriteString(content[textEnd:next])
		lineStart = next
	}
	if count == 0 {
		return workspaceSearchReplacement{text: content}, nil
	}
	return workspaceSearchReplacement{text: output.String(), edits: edits, count: count}, nil
}

// preserveWorkspaceSearchCase follows VS Code's useful common cases: lower,
// UPPER, and Capitalized matches carry their shape to the replacement. Mixed
// casing remains exactly what the user typed, including capture expansion.
func preserveWorkspaceSearchCase(matched, replacement string) string {
	first, firstSize := utf8.DecodeRuneInString(matched)
	letters := false
	upper, lower := true, true
	for _, r := range matched {
		if !unicode.IsLetter(r) {
			continue
		}
		letters = true
		upper = upper && unicode.IsUpper(r)
		lower = lower && unicode.IsLower(r)
	}
	if !letters {
		return replacement
	}
	if upper {
		return strings.ToUpper(replacement)
	}
	if lower {
		return strings.ToLower(replacement)
	}
	restLower := true
	for _, r := range matched[firstSize:] {
		if unicode.IsLetter(r) {
			restLower = restLower && unicode.IsLower(r)
		}
	}
	if unicode.IsUpper(first) && restLower {
		replacement = strings.ToLower(replacement)
		r, size := utf8.DecodeRuneInString(replacement)
		return string(unicode.ToUpper(r)) + replacement[size:]
	}
	return replacement
}
