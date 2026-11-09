package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func TestWorkspaceSearchExcludesConfiguredDirectoriesBeforeBothBackends(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		"source/main.go",
		"source/node_modules-file.txt",
		"source/node_modules/generated.js",
		"nested/dist/output.txt",
		"vendor/library.go",
	}
	for _, name := range paths {
		writeWorkspaceSearchTestFile(t, root, name, "needle\n")
	}
	options := workspaceSearchOptions{
		Query: "needle", CaseSensitive: true,
		ExcludedDirectories: defaultWorkspaceSearchExcludedDirectories(),
	}
	wantPaths := []string{"source/main.go", "source/node_modules-file.txt", "vendor/library.go"}
	if got := filterWorkspaceSearchPaths(paths, options); !slices.Equal(got, wantPaths) {
		t.Fatalf("filtered paths = %v, want %v", got, wantPaths)
	}

	assertMatches := func(backend string, matches []workspaceSearchMatch, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s search: %v", backend, err)
		}
		gotPaths := make([]string, len(matches))
		for index, match := range matches {
			gotPaths[index] = match.Path
		}
		if !slices.Equal(gotPaths, wantPaths) {
			t.Fatalf("%s paths = %v, want %v", backend, gotPaths, wantPaths)
		}
	}
	native, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, options, 20)
	assertMatches("native", native, err)
	if _, lookErr := exec.LookPath("rg"); lookErr == nil {
		matches, backend, searchErr := searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, 20)
		if backend != workspaceSearchBackendRipgrep {
			t.Fatalf("search backend = %v, want actual ripgrep", backend)
		}
		assertMatches("ripgrep", matches, searchErr)
	}

	// Clearing the setting is meaningful: no hidden defaults are put back.
	options.ExcludedDirectories = []string{}
	native, err = searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, options, 20)
	if err != nil || len(native) != len(paths) {
		t.Fatalf("empty exclusions native matches = %+v, err %v; want every file", native, err)
	}
	if _, lookErr := exec.LookPath("rg"); lookErr == nil {
		matches, _, searchErr := searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, 20)
		if searchErr != nil || len(matches) != len(paths) {
			t.Fatalf("empty exclusions ripgrep matches = %+v, err %v; want every file", matches, searchErr)
		}
	}
}

func TestWorkspaceSearchFallsBackWhenRipgrepIsUnavailable(t *testing.T) {
	originalLookPath := workspaceSearchLookPath
	t.Cleanup(func() { workspaceSearchLookPath = originalLookPath })
	workspaceSearchLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "source.txt", "before Needle after\n")
	want := []workspaceSearchMatch{{Path: "source.txt", Line: 1, Column: 8, Text: "before Needle after"}}
	matches, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, []string{"source.txt"}, workspaceSearchOptions{Query: "needle"}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if backend != workspaceSearchBackendGo {
		t.Fatalf("native fallback backend = %v, want Go", backend)
	}
	if !slices.Equal(matches, want) {
		t.Fatalf("native fallback matches = %+v, want %+v", matches, want)
	}
}

func TestWorkspaceSearchNativePreferenceDoesNotLookForRipgrep(t *testing.T) {
	originalLookPath := workspaceSearchLookPath
	t.Cleanup(func() { workspaceSearchLookPath = originalLookPath })
	lookedUp := false
	workspaceSearchLookPath = func(string) (string, error) {
		lookedUp = true
		return "rg", nil
	}
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "source.txt", "needle\n")
	matches, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, []string{"source.txt"}, workspaceSearchOptions{
		Query: "needle", PreferNative: true,
	}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if lookedUp || backend != workspaceSearchBackendGo || len(matches) != 1 {
		t.Fatalf("native-only search looked up rg = %v, backend = %v, matches = %v", lookedUp, backend, matches)
	}
}

func TestWorkspaceSearchRipgrepMatchesTheNativeScanner(t *testing.T) {
	executable, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep is not installed")
	}
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "first.txt", "Alpha needle needle\né Kelvin\nfinal ς\n")
	writeWorkspaceSearchTestFile(t, root, "second.txt", "short NEEDLE\r\nlast needle\n")
	writeWorkspaceSearchTestFile(t, root, "binary.dat", "needle before\n\x00needle after")
	paths := []string{"second.txt", "first.txt", "binary.dat", "missing.txt"}

	for _, test := range []struct {
		query         string
		caseSensitive bool
		limit         int
	}{
		{query: "needle", caseSensitive: true, limit: 20},
		{query: "NEEDLE", caseSensitive: false, limit: 20},
		{query: "k", caseSensitive: false, limit: 20},
		{query: "Σ", caseSensitive: false, limit: 20},
		{query: "needle", caseSensitive: false, limit: 2},
	} {
		ctx := context.Background()
		want := searchWorkspaceFilesGo(ctx, root, paths, test.query, test.caseSensitive, test.limit)
		got, ok := searchWorkspaceFilesRipgrep(ctx, executable, root, paths, test.query, test.caseSensitive, test.limit)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("ripgrep query %q (case sensitive %v, limit %d) = %+v, ok %v; native = %+v", test.query, test.caseSensitive, test.limit, got, ok, want)
		}
	}
}

func TestWorkspaceSearchRipgrepAcceptsLargePathLists(t *testing.T) {
	executable, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep is not installed")
	}
	paths := make([]string, 40_000)
	for index := range paths {
		paths[index] = fmt.Sprintf("a/long/repository/path/with/many/components/file-%05d.go", index)
	}
	root := t.TempDir()
	want := make([]workspaceSearchMatch, 0, 3)
	for _, index := range []int{0, len(paths) / 2, len(paths) - 1} {
		writeWorkspaceSearchTestFile(t, root, paths[index], "needle\n")
		want = append(want, workspaceSearchMatch{Path: paths[index], Line: 1, Column: 1, Text: "needle"})
	}
	matches, ok := searchWorkspaceFilesRipgrep(context.Background(), executable, root, paths, "needle", true, 500)
	if !ok {
		t.Fatal("ripgrep could not accept the large repository file list")
	}
	if !slices.Equal(matches, want) {
		t.Fatalf("matches across path batches = %+v, want %+v", matches, want)
	}
	limited, ok := searchWorkspaceFilesRipgrep(context.Background(), executable, root, paths, "needle", true, 2)
	if !ok || !slices.Equal(limited, want[:2]) {
		t.Fatalf("two-match limit across path batches = %+v, ok %v; want %+v", limited, ok, want[:2])
	}
}

func TestWorkspaceSearchOptionsMatchAcrossRipgrepAndNative(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is not installed")
	}
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "profiles/web/first.go", "needle Needle needlefish\nprefixNeedle suffix\nra_playbook(\"one\") RA_PLAYBOOK\n")
	writeWorkspaceSearchTestFile(t, root, "profiles/web/second.txt", "needle\nra_other\n")
	writeWorkspaceSearchTestFile(t, root, "profiles/other/skip.go", "needle\nra_playbook\n")

	tests := []workspaceSearchOptions{
		{Query: "needle", WholeWord: true},
		{Query: "Needle", CaseSensitive: true, Include: "./profiles/web", Exclude: "**/second.txt"},
		{Query: "needle", Include: "*.go", Exclude: "profiles/other"},
		{Query: "ra_(playbook|other)", RegularExpr: true, Include: "**/web/**"},
		{Query: "RA_[A-Z]+", RegularExpr: true, CaseSensitive: true, Include: "**/*.go"},
	}
	paths := []string{"profiles/web/second.txt", "profiles/other/skip.go", "profiles/web/first.go"}
	for _, options := range tests {
		want, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, options, 50)
		if err != nil {
			t.Fatalf("native options %+v: %v", options, err)
		}
		got, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, 50)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("options %+v = %+v, err %v; native = %+v", options, got, err, want)
		}
		if backend != workspaceSearchBackendRipgrep {
			t.Errorf("options %+v backend = %v, want ripgrep", options, backend)
		}
		if len(got) == 0 {
			t.Errorf("options %+v fixture returned no matches", options)
		}
	}

	_, _, err := searchWorkspaceFilesWithOptions(context.Background(), root, []string{"profiles/web/first.go"}, workspaceSearchOptions{
		Query: "([unclosed", RegularExpr: true,
	}, 50)
	if err == nil {
		t.Fatal("an invalid regular expression was not reported")
	}
}

func TestWorkspaceSearchUnicodeRegexAndWholeWordParity(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "unicode.txt", "écat cat\nκόσμε cafe\n")
	options := []workspaceSearchOptions{
		{Query: "cat", WholeWord: true, CaseSensitive: true},
		{Query: `\w+`, RegularExpr: true, CaseSensitive: true},
		{Query: `[[:word:]]+`, RegularExpr: true, CaseSensitive: true},
	}
	var wholeWord []workspaceSearchMatch
	for _, option := range options {
		native, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, []string{"unicode.txt"}, option, 20)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := searchWorkspaceFilesWithOptions(context.Background(), root, []string{"unicode.txt"}, option, 20)
		if err != nil || !slices.Equal(got, native) {
			t.Errorf("Unicode options %+v = %+v, err %v; native = %+v", option, got, err, native)
		}
		if len(native) == 0 {
			t.Fatalf("Unicode options %+v returned no fixture matches", option)
		}
		if option.Query == "cat" {
			wholeWord = native
		}
	}

	if len(wholeWord) != 1 || wholeWord[0].Line != 1 || wholeWord[0].Column != 7 {
		t.Fatalf("Unicode whole-word match = %+v, want only the standalone cat at byte column 7", wholeWord)
	}
}

func TestWorkspaceSearchZeroWidthAnchorsMatchNativeAndRipgrep(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "anchors.txt", "alpha\r\n\r\nbeta\n")
	writeWorkspaceSearchTestFile(t, root, "empty.txt", "")
	paths := []string{"anchors.txt", "empty.txt"}
	for _, query := range []string{"^", "$"} {
		options := workspaceSearchOptions{Query: query, RegularExpr: true, CaseSensitive: true}
		native, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, options, 20)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, 20)
		if err != nil || !slices.Equal(got, native) {
			t.Errorf("anchor %q = %+v, err %v; native = %+v", query, got, err, native)
		}
		if len(native) != 3 {
			t.Errorf("anchor %q returned %d matches, want three real document lines: %+v", query, len(native), native)
		}
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
	if err := os.Symlink(outsideRoot, filepath.Join(root, "linked-directory")); err != nil {
		t.Fatal(err)
	}

	paths := []string{
		outside,
		filepath.ToSlash(relativeOutside),
		"sub/../../outside.txt",
		"linked.txt",
		"linked-directory/outside.txt",
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
