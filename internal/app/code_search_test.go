package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func writeWorkspaceSearchTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSearchCaseBehaviorAndByteColumns(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "source.txt", "Alpha needle needle\nalpha Needle\né Kelvin\nfinal ς\n")

	caseSensitive := searchWorkspaceFiles(context.Background(), root, []string{"source.txt"}, "needle", true, 20)
	wantCaseSensitive := []workspaceSearchMatch{{Path: "source.txt", Line: 1, Column: 7, Text: "Alpha needle needle"}}
	if !slices.Equal(caseSensitive, wantCaseSensitive) {
		t.Fatalf("case-sensitive matches = %+v, want %+v", caseSensitive, wantCaseSensitive)
	}

	caseInsensitive := searchWorkspaceFiles(context.Background(), root, []string{"source.txt"}, "NEEDLE", false, 20)
	wantCaseInsensitive := []workspaceSearchMatch{
		{Path: "source.txt", Line: 1, Column: 7, Text: "Alpha needle needle"},
		{Path: "source.txt", Line: 2, Column: 7, Text: "alpha Needle"},
	}
	if !slices.Equal(caseInsensitive, wantCaseInsensitive) {
		t.Fatalf("case-insensitive matches = %+v, want one per matching line: %+v", caseInsensitive, wantCaseInsensitive)
	}

	for _, test := range []struct {
		query string
		line  int
		col   int
		text  string
	}{
		{query: "k", line: 3, col: 4, text: "é Kelvin"},
		{query: "Σ", line: 4, col: 7, text: "final ς"},
	} {
		matches := searchWorkspaceFiles(context.Background(), root, []string{"source.txt"}, test.query, false, 20)
		want := []workspaceSearchMatch{{Path: "source.txt", Line: test.line, Column: test.col, Text: test.text}}
		if !slices.Equal(matches, want) {
			t.Errorf("Unicode case-folding query %q = %+v, want byte columns %+v", test.query, matches, want)
		}
	}
}

func TestWorkspaceSearchOrderingMultipleLinesAndLimit(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "first.txt", "nothing\n  needle and needle\nneedle last\n")
	writeWorkspaceSearchTestFile(t, root, "second.txt", "needle second\nnone\nxx needle\n")
	paths := []string{"second.txt", "./first.txt"}

	matches := searchWorkspaceFiles(context.Background(), root, paths, "needle", true, 20)
	want := []workspaceSearchMatch{
		{Path: "second.txt", Line: 1, Column: 1, Text: "needle second"},
		{Path: "second.txt", Line: 3, Column: 4, Text: "xx needle"},
		{Path: "first.txt", Line: 2, Column: 3, Text: "  needle and needle"},
		{Path: "first.txt", Line: 3, Column: 1, Text: "needle last"},
	}
	if !slices.Equal(matches, want) {
		t.Fatalf("matches = %+v, want stable path and line order %+v", matches, want)
	}

	limited := searchWorkspaceFiles(context.Background(), root, paths, "needle", true, 2)
	if !slices.Equal(limited, want[:2]) {
		t.Fatalf("limited matches = %+v, want first two %+v", limited, want[:2])
	}
}

func TestWorkspaceSearchSkipsBinaryMissingNonregularAndOversizeFiles(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "binary.dat", "needle before\n\x00needle after")
	writeWorkspaceSearchTestFile(t, root, "oversize.txt", "needle\n"+strings.Repeat("b", workspaceSearchMaxFileSize))
	exactContent := "needle\n" + strings.Repeat("a", workspaceSearchMaxFileSize-len("needle\n"))
	writeWorkspaceSearchTestFile(t, root, "exact.txt", exactContent)
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths := []string{"missing.txt", "binary.dat", "oversize.txt", "directory", "exact.txt"}
	matches := searchWorkspaceFiles(context.Background(), root, paths, "needle", true, 20)
	want := []workspaceSearchMatch{{Path: "exact.txt", Line: 1, Column: 1, Text: "needle"}}
	if !slices.Equal(matches, want) {
		t.Fatalf("matches after skipped files = %+v, want only the exact-size text file %+v", matches, want)
	}
}

func TestWorkspaceSearchRejectsPathsEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outsideRoot := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "inside.txt", "safe needle\n")
	writeWorkspaceSearchTestFile(t, root, "sub/placeholder.txt", "placeholder\n")
	outside := filepath.Join(outsideRoot, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	relativeOutside, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.txt")); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		outside,
		filepath.ToSlash(relativeOutside),
		"sub/../../outside.txt",
		"linked.txt",
		"sub/../inside.txt",
	}
	matches := searchWorkspaceFiles(context.Background(), root, paths, "needle", true, 20)
	want := []workspaceSearchMatch{{Path: "inside.txt", Line: 1, Column: 6, Text: "safe needle"}}
	if !slices.Equal(matches, want) {
		t.Fatalf("escaping path search = %+v, want only the contained cleaned path %+v", matches, want)
	}
}

func TestWorkspaceSearchEmptyQueryLimitsAndCancellation(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "source.txt", "needle\n")
	for _, test := range []struct {
		name  string
		query string
		limit int
	}{
		{name: "empty query", query: "", limit: 10},
		{name: "zero limit", query: "needle", limit: 0},
		{name: "negative limit", query: "needle", limit: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if matches := searchWorkspaceFiles(context.Background(), root, []string{"source.txt"}, test.query, true, test.limit); matches != nil {
				t.Fatalf("matches = %+v, want nil", matches)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if matches := searchWorkspaceFiles(ctx, root, []string{"source.txt"}, "needle", true, 10); matches != nil {
		t.Fatalf("pre-cancelled search = %+v, want nil", matches)
	}
}

func TestWorkspaceSearchCRLFAndLongLineSnippet(t *testing.T) {
	root := t.TempDir()
	longLine := strings.Repeat("x", 300) + "needle" + strings.Repeat("y", 300)
	writeWorkspaceSearchTestFile(t, root, "windows.txt", longLine+"\r\nshort needle\r\n")

	matches := searchWorkspaceFiles(context.Background(), root, []string{"windows.txt"}, "needle", true, 10)
	if len(matches) != 2 {
		t.Fatalf("CRLF search returned %+v, want two matches", matches)
	}
	if matches[0].Line != 1 || matches[0].Column != 301 || !strings.Contains(matches[0].Text, "needle") {
		t.Errorf("long-line match did not retain its match and byte column: %+v", matches[0])
	}
	if utf8.RuneCountInString(matches[0].Text) > workspaceSearchMaxText+2 {
		t.Errorf("snippet contains %d runes, want around %d: %q", utf8.RuneCountInString(matches[0].Text), workspaceSearchMaxText, matches[0].Text)
	}
	wantSecond := workspaceSearchMatch{Path: "windows.txt", Line: 2, Column: 7, Text: "short needle"}
	if matches[1] != wantSecond {
		t.Errorf("CRLF match = %+v, want no carriage return %+v", matches[1], wantSecond)
	}
}
