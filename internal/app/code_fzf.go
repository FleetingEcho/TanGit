package app

import (
	"sort"
	"strings"
	"sync"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

const (
	fzfSlab16Size = 100 * 1024
	fzfSlab32Size = 2048
)

// fzf's scoring scheme is process-global. Keep scheme switches and their
// scoring calls together so Quick open and content ranking can safely run on
// different workers without racing in the upstream algorithm.
var fzfSchemeMutex sync.Mutex

func withFzfScheme(scheme string, run func(*util.Slab)) {
	fzfSchemeMutex.Lock()
	defer fzfSchemeMutex.Unlock()
	algo.Init(scheme)
	run(util.MakeSlab(fzfSlab16Size, fzfSlab32Size))
}

func fzfPattern(query string, caseSensitive bool) ([]rune, bool) {
	normalize := true
	if !caseSensitive {
		query = strings.ToLower(query)
	}
	pattern := []rune(query)
	normalized := algo.NormalizeRunes(pattern)
	if string(pattern) != string(normalized) {
		normalize = false
	} else {
		pattern = normalized
	}
	return pattern, normalize
}

func filterCodePathsFzf(paths []string, query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return append([]string(nil), paths...)
	}
	pattern, normalize := fzfPattern(query, false)
	type scoredPath struct {
		path  string
		score int
	}
	matches := make([]scoredPath, 0, len(paths))
	withFzfScheme("path", func(slab *util.Slab) {
		for _, candidate := range paths {
			chars := util.ToChars([]byte(candidate))
			result, _ := algo.FuzzyMatchV2(false, normalize, true, &chars, pattern, false, slab)
			if result.Start >= 0 {
				matches = append(matches, scoredPath{path: candidate, score: result.Score})
			}
		}
	})
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].path < matches[j].path
	})
	filtered := make([]string, len(matches))
	for index, match := range matches {
		filtered[index] = match.path
	}
	return filtered
}

// rankWorkspaceSearchMatches applies fzf's mature relevance scoring only after
// ripgrep or the native scanner has established the exact result set. This
// keeps case, word, regex, and highlight semantics identical while
// presenting the most relevant lines first. Groups stay intact for the UI.
func rankWorkspaceSearchMatches(matches []workspaceSearchMatch, options workspaceSearchOptions) []workspaceSearchMatch {
	if len(matches) < 2 {
		return matches
	}
	type scoredMatch struct {
		match workspaceSearchMatch
		score int
	}
	type matchGroup struct {
		path    string
		matches []scoredMatch
		best    int
	}
	groups := make([]matchGroup, 0)
	for _, match := range matches {
		if len(groups) == 0 || groups[len(groups)-1].path != match.Path {
			groups = append(groups, matchGroup{path: match.Path})
		}
		group := &groups[len(groups)-1]
		group.matches = append(group.matches, scoredMatch{match: match})
	}

	withFzfScheme("default", func(slab *util.Slab) {
		for groupIndex := range groups {
			group := &groups[groupIndex]
			group.best = -1
			for matchIndex := range group.matches {
				scored := &group.matches[matchIndex]
				query := options.Query
				caseSensitive := options.CaseSensitive
				if options.RegularExpr {
					// Regex syntax is not literal fzf input. Score the text selected by
					// the shared matcher instead, including zero-width expressions.
					start, end := workspaceSearchRangeWithOptions(scored.match.Text, options)
					if start < 0 || end < start {
						continue
					}
					query = scored.match.Text[start:end]
					caseSensitive = true
				}
				if query == "" {
					scored.score = 0
				} else {
					pattern, normalize := fzfPattern(query, caseSensitive)
					chars := util.ToChars([]byte(scored.match.Text))
					result, _ := algo.ExactMatchNaive(caseSensitive, normalize, true, &chars, pattern, false, slab)
					scored.score = result.Score
				}
				if scored.score > group.best {
					group.best = scored.score
				}
			}
			sort.SliceStable(group.matches, func(i, j int) bool {
				return group.matches[i].score > group.matches[j].score
			})
		}
	})

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].best != groups[j].best {
			return groups[i].best > groups[j].best
		}
		return groups[i].path < groups[j].path
	})
	ranked := make([]workspaceSearchMatch, 0, len(matches))
	for _, group := range groups {
		for _, match := range group.matches {
			ranked = append(ranked, match.match)
		}
	}
	return ranked
}
