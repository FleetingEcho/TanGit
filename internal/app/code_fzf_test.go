package app

import (
	"fmt"
	"testing"
)

func TestRankWorkspaceSearchMatchesUsesFzfRelevanceWithinFileGroups(t *testing.T) {
	matches := []workspaceSearchMatch{
		{Path: "z-prefix.txt", Line: 1, Column: 8, Text: "prefix-needle suffix"},
		{Path: "a-start.txt", Line: 9, Column: 14, Text: "prefix-prefix-needle"},
		{Path: "a-start.txt", Line: 2, Column: 1, Text: "needle suffix"},
	}
	ranked := rankWorkspaceSearchMatches(matches, workspaceSearchOptions{Query: "needle", CaseSensitive: true})
	if len(ranked) != len(matches) {
		t.Fatalf("fzf ranking dropped matches: %+v", ranked)
	}
	if ranked[0].Path != "a-start.txt" || ranked[0].Line != 2 || ranked[1].Path != "a-start.txt" || ranked[2].Path != "z-prefix.txt" {
		t.Fatalf("fzf relevance order = %+v", ranked)
	}
}

func TestRankWorkspaceSearchMatchesPreservesRegexAndZeroWidthResults(t *testing.T) {
	for _, options := range []workspaceSearchOptions{
		{Query: "ra_(playbook|other)", RegularExpr: true, CaseSensitive: true},
		{Query: "^", RegularExpr: true, CaseSensitive: true},
	} {
		matches := []workspaceSearchMatch{
			{Path: "second.txt", Line: 2, Column: 1, Text: "ra_other"},
			{Path: "first.txt", Line: 1, Column: 8, Text: "prefix ra_playbook"},
		}
		ranked := rankWorkspaceSearchMatches(matches, options)
		if len(ranked) != len(matches) {
			t.Fatalf("query %q dropped matches: %+v", options.Query, ranked)
		}
		for index := 1; index < len(ranked); index++ {
			if ranked[index-1].Path == ranked[index].Path && ranked[index].Path != ranked[0].Path {
				t.Fatalf("query %q split a file group: %+v", options.Query, ranked)
			}
		}
	}
}

func BenchmarkFilterCodePathsFzf(b *testing.B) {
	paths := make([]string, 135319)
	for index := range paths {
		paths[index] = fmt.Sprintf("profiles/module-%05d/nested/repository/file.go", index)
	}
	paths[len(paths)-1] = "profiles/web/extended/ra_playbook.go"
	b.ResetTimer()
	for range b.N {
		if got := filterCodePaths(paths, "raplaybook"); len(got) == 0 {
			b.Fatal("large-repository query returned no matches")
		}
	}
}
