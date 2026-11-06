package app

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func stubCodeWorkspace(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	originalList, originalRead, originalWrite := listCodeFiles, readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		listCodeFiles, readWorkingFile, writeWorkingFile = originalList, originalRead, originalWrite
	})
	listCodeFiles = func(string) ([]string, error) {
		paths := make([]string, 0, len(files))
		for path := range files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return paths, nil
	}
	readWorkingFile = func(name string) ([]byte, error) {
		path := strings.TrimPrefix(filepath.ToSlash(name), "/work/project/")
		content, ok := files[path]
		if !ok {
			return nil, errors.New("file not found")
		}
		return []byte(content), nil
	}
	written := make(map[string]string)
	writeWorkingFile = func(name string, data []byte) error {
		path := strings.TrimPrefix(filepath.ToSlash(name), "/work/project/")
		written[path] = string(data)
		return nil
	}
	return written
}

func openCodePageForTest(t *testing.T, h *harness, fileCount int) {
	t.Helper()
	h.app.setWorkspaceTab(sectionCode)
	h.tester.Frame()
	h.waitFor(t, "the Code file index", func() bool {
		return h.app.code.filesLoaded && !h.app.code.filesLoading && len(h.app.code.files) == fileCount
	})
	h.tester.Frame()
}

func waitForCodeDocument(t *testing.T, h *harness, path string) *codeDocument {
	t.Helper()
	h.waitFor(t, "the Code document", func() bool {
		document := h.app.code.documents[path]
		return document != nil && !document.loading && document.state != nil
	})
	h.tester.Frame()
	return h.app.code.documents[path]
}

func TestCodePageOpensFiltersAndClosesPersistentTabs(t *testing.T) {
	written := stubCodeWorkspace(t, map[string]string{
		"main.go":              "package main\n",
		"internal/app/edit.go": "package app\n// editor\n",
	})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 2)
	if !h.tester.HasText("main.go") || !h.tester.HasText("internal") || h.tester.HasText("app") {
		t.Fatalf("the explorer does not start with collapsed directories: %q", h.texts())
	}

	h.tester.Key(ui.Cmd, ui.KeyP)
	if !h.tester.Focused("Quick open") {
		t.Fatal("Ctrl+P did not focus Quick open")
	}
	h.tester.Type("iaed")
	h.tester.Key(0, ui.KeyEnter)
	document := waitForCodeDocument(t, h, "internal/app/edit.go")
	if h.app.code.active != document.path || len(h.app.code.tabs) != 1 || h.app.code.preview != document.path {
		t.Fatalf("quick open selected %q with tabs %q", h.app.code.active, h.app.code.tabs)
	}
	document.state.Insert("/* kept */\n")
	h.tester.Frame()
	h.tester.Frame()
	if h.app.code.preview != "" {
		t.Fatalf("editing left %q as a temporary preview tab", h.app.code.preview)
	}
	if len(written) != 0 || !document.state.Buffer.Modified() {
		t.Fatal("typing saved the buffer implicitly")
	}
	h.tester.Key(ui.Cmd, ui.KeyS)
	if got := written[document.path]; !strings.Contains(got, "/* kept */") || document.state.Buffer.Modified() {
		t.Fatalf("explicit save failed: %q", got)
	}

	h.app.openCodeFile("main.go", 0)
	mainDocument := waitForCodeDocument(t, h, "main.go")
	if len(h.app.code.tabs) != 2 || h.app.code.active != "main.go" {
		t.Fatalf("opening a second file lost the tabs: %q", h.app.code.tabs)
	}
	h.app.openCodeFile(document.path, 0)
	h.tester.Frame()
	if h.app.code.documents[document.path] != document || h.app.activeCodeDocument().state != document.state {
		t.Fatal("switching tabs replaced the document's persistent editor state")
	}
	if !strings.Contains(document.state.Buffer.String(), "/* kept */") {
		t.Fatal("switching tabs lost the edited buffer")
	}

	h.app.closeCodeFile(document.path)
	h.tester.Frame()
	if len(h.app.code.tabs) != 1 || h.app.activeCodeDocument() != mainDocument {
		t.Fatalf("closing a tab chose the wrong remaining document: %+v", h.app.code)
	}
}

func TestCodePreviewTabIsReusedUntilDoubleClicked(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{
		"first.go":  "package first\n",
		"second.go": "package second\n",
		"third.go":  "package third\n",
	})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 3)

	if err := h.tester.Click("Open first.go"); err != nil {
		t.Fatal(err)
	}
	waitForCodeDocument(t, h, "first.go")
	if err := h.tester.Click("Open second.go"); err != nil {
		t.Fatal(err)
	}
	waitForCodeDocument(t, h, "second.go")
	if len(h.app.code.tabs) != 1 || h.app.code.tabs[0] != "second.go" || h.app.code.documents["first.go"] != nil {
		t.Fatalf("second preview did not replace the first: tabs=%q documents=%v", h.app.code.tabs, h.app.code.documents)
	}

	// A second quick click on the same row is its double-click and pins it.
	if err := h.tester.Click("Open second.go"); err != nil {
		t.Fatal(err)
	}
	if h.app.code.preview != "" {
		t.Fatalf("double-click left %q as the preview tab", h.app.code.preview)
	}
	if err := h.tester.Click("Open third.go"); err != nil {
		t.Fatal(err)
	}
	waitForCodeDocument(t, h, "third.go")
	if len(h.app.code.tabs) != 2 || h.app.code.tabs[0] != "second.go" || h.app.code.tabs[1] != "third.go" {
		t.Fatalf("opening after pinning produced tabs %q", h.app.code.tabs)
	}

	// The preview tab itself can also be double-clicked to keep it open.
	if err := h.tester.Click("Editor tab third.go"); err != nil {
		t.Fatal(err)
	}
	if err := h.tester.Click("Editor tab third.go"); err != nil {
		t.Fatal(err)
	}
	if h.app.code.preview != "" {
		t.Fatalf("double-clicking the tab left %q as the preview", h.app.code.preview)
	}
}

func TestCodeWorkspaceSearchOpensTheMatchedLine(t *testing.T) {
	files := map[string]string{
		"main.go":              "package main\n// Needle here\n",
		"internal/app/edit.go": "package app\n// first\n// needle again\n",
	}
	stubCodeWorkspace(t, files)
	root := t.TempDir()
	for path, content := range files {
		name := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	readWorkingFile = os.ReadFile
	h := menuTestApp(t)
	h.app.snapshot.Root = root
	openCodePageForTest(t, h, 2)
	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	if !h.tester.Focused("Search in files") {
		t.Fatal("Ctrl+Shift+F did not focus workspace search")
	}
	h.tester.Type("needle")
	h.tester.Key(0, ui.KeyEnter)
	h.waitFor(t, "workspace results", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 2
	})
	h.tester.Frame()
	if h.app.code.searchSelected != 0 {
		t.Fatalf("the first result is not selected: %d", h.app.code.searchSelected)
	}
	if got := len(h.app.code.searchRows); got != 4 {
		t.Fatalf("two file groups and two matches produced %d rows, want 4", got)
	}
	if err := h.tester.Click("Collapse file results internal/app/edit.go"); err != nil {
		t.Fatalf("collapse a file group: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if got := len(h.app.code.searchRows); got != 3 {
		t.Fatalf("collapsing one file left %d rows, want 3", got)
	}
	if err := h.tester.Click("Expand file results internal/app/edit.go"); err != nil {
		t.Fatalf("expand a file group: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()

	h.press(t, ui.KeyF3)
	waitForCodeDocument(t, h, "main.go")
	if err := h.tester.Click("Open internal/app/edit.go:3:4: // needle again"); err != nil {
		t.Fatalf("click a search match: %v; texts=%q", err, h.texts())
	}
	document := waitForCodeDocument(t, h, "internal/app/edit.go")
	if got := document.state.CursorPos().Line; got != 2 {
		t.Fatalf("search opened line %d, want zero-based line 2", got)
	}
	if got := document.state.CursorPos().Col; got != 3 {
		t.Fatalf("search opened byte column %d, want zero-based column 3", got)
	}
	if h.app.code.active != "internal/app/edit.go" {
		t.Fatalf("search opened %q", h.app.code.active)
	}
}

func TestCodeSearchEmptyStateOffersExternalWorkspace(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"nested/workspace/main.go": "package main\n"})
	if !h.app.openCodeWorkspaceFolder("nested/workspace") {
		t.Fatal("the workspace folder was rejected")
	}
	h.app.code.sidebar = codeSidebarSearch
	opened := stubEditor(t, nil)
	h.tester.Frame()

	for _, text := range []string{
		"Search project files",
		"Large repository? Search may be slow. Use an external editor instead.",
	} {
		if !h.tester.HasText(text) {
			t.Fatalf("the concise search guidance is missing %q: %q", text, h.texts())
		}
	}
	if err := h.tester.Click("External"); err != nil {
		t.Fatalf("the search empty state has no External action: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	h.requireMenuLabels(t, "VS Code", "Zed", "Customize editor…")
	h.chooseMenuItem(t, "Zed")
	want := []launchedEditor{{command: editorPresets[2].Command, path: filepath.Join(root, "nested", "workspace")}}
	if !slices.Equal(*opened, want) {
		t.Fatalf("search External opened %+v, want %+v", *opened, want)
	}
}

func TestCodeSearchBadgeReportsTheActualBackend(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is not installed")
	}
	originalLookPath := workspaceSearchLookPath
	t.Cleanup(func() { workspaceSearchLookPath = originalLookPath })

	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.app.code.sidebar = codeSidebarSearch
	h.app.code.searchQuery = "needle"
	h.app.beginCodeSearch()
	h.waitFor(t, "the ripgrep search", func() bool { return !h.app.code.searchRunning })
	h.tester.Frame()
	if h.app.code.searchBackend != workspaceSearchBackendRipgrep {
		t.Fatalf("search backend = %v, want ripgrep", h.app.code.searchBackend)
	}
	if !h.tester.HasText("ripgrep") {
		t.Fatalf("ripgrep badge is missing: %q", h.texts())
	}
	if _, found := h.tester.Find("Search powered by ripgrep"); !found {
		t.Fatal("the ripgrep badge has no accessible label")
	}

	workspaceSearchLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	h.app.beginCodeSearch()
	if h.app.code.searchBackend != workspaceSearchBackendNone {
		t.Fatal("a new search retained the previous search's backend badge")
	}
	h.waitFor(t, "the native fallback search", func() bool { return !h.app.code.searchRunning })
	h.tester.Frame()
	if h.app.code.searchBackend != workspaceSearchBackendGo {
		t.Fatalf("fallback backend = %v, want Go", h.app.code.searchBackend)
	}
	if h.tester.HasText("ripgrep") {
		t.Fatal("the native fallback incorrectly displays the ripgrep badge")
	}

	h.app.code.searchQuery = ""
	h.app.beginCodeSearch()
	h.tester.Frame()
	if h.app.code.searchBackend != workspaceSearchBackendNone || h.tester.HasText("ripgrep") {
		t.Fatal("clearing the query retained the backend badge")
	}
}

func TestCodeSearchDebouncesTypingAndClearsOptionsWhenEmptied(t *testing.T) {
	originalSchedule := scheduleCodeSearch
	t.Cleanup(func() { scheduleCodeSearch = originalSchedule })
	var timers []func()
	var delays []time.Duration
	scheduleCodeSearch = func(delay time.Duration, run func()) {
		delays = append(delays, delay)
		timers = append(timers, run)
	}

	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"first.txt":  "needle\n",
		"second.txt": "Needle\n",
	})
	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tester.Type("wrong")
	h.tester.Frame()
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("needle")
	h.tester.Frame()
	if len(timers) != 2 || len(delays) != 2 || delays[0] != codeSearchDebounceDelay || delays[1] != codeSearchDebounceDelay {
		t.Fatalf("debounce scheduling = %d timers with delays %v", len(timers), delays)
	}
	if h.app.code.searchSubmitted != "" || h.app.code.searchRunning {
		t.Fatal("typing started a search before the debounce elapsed")
	}

	// A callback from superseded input must not search the newer query early.
	timers[0]()
	h.updates.waitForUpdate(t)
	if h.app.code.searchSubmitted != "" || h.app.code.searchRunning {
		t.Fatal("a stale debounce callback started a search")
	}
	timers[1]()
	h.updates.waitForUpdate(t)
	h.waitFor(t, "the debounced repository search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 2
	})

	h.app.code.searchCaseSensitive = true
	h.app.code.searchWholeWord = true
	h.app.code.searchRegularExpr = true
	h.app.code.searchInclude = "*.go"
	h.app.code.searchExclude = "vendor"
	h.app.code.searchFiltersOpen = true
	if err := h.tester.Click("Search in files"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Key(0, ui.KeyBackspace)
	h.tester.Frame()
	if h.app.code.searchQuery != "" || h.app.code.searchSubmitted != "" || h.app.code.searchRunning || len(h.app.code.searchResults) != 0 {
		t.Fatal("clearing the query did not immediately clear the submitted search")
	}
	if h.app.code.searchCaseSensitive || h.app.code.searchWholeWord || h.app.code.searchRegularExpr || h.app.code.searchFiltersOpen ||
		h.app.code.searchInclude != "" || h.app.code.searchExclude != "" {
		t.Fatal("clearing the query left search options highlighted or active")
	}
}

func TestCodeSearchEditingCancelsActiveWorkAndStartsLatestQuery(t *testing.T) {
	originalRun, originalSchedule := runCodeSearch, scheduleCodeSearch
	t.Cleanup(func() {
		runCodeSearch = originalRun
		scheduleCodeSearch = originalSchedule
	})
	started := make(chan string, 4)
	cancelled := make(chan string, 4)
	runCodeSearch = func(ctx context.Context, _ string, _ []string, options workspaceSearchOptions, _ int) ([]workspaceSearchMatch, workspaceSearchBackend, error) {
		started <- options.Query
		if strings.HasPrefix(options.Query, "old") {
			<-ctx.Done()
			cancelled <- options.Query
			return nil, workspaceSearchBackendNone, ctx.Err()
		}
		return []workspaceSearchMatch{{Path: "fresh.txt", Line: 1, Column: 1, Text: "fresh"}}, workspaceSearchBackendGo, nil
	}
	var timers []func()
	scheduleCodeSearch = func(_ time.Duration, run func()) {
		timers = append(timers, run)
	}
	waitSignal := func(signals <-chan string, want, action string) {
		t.Helper()
		select {
		case got := <-signals:
			if got != want {
				t.Fatalf("%s signal = %q, want %q", action, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s %q", action, want)
		}
	}

	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{"fresh.txt": "fresh\n"})
	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tester.Type("old")
	h.tester.Frame()
	timers[0]()
	h.updates.waitForUpdate(t)
	waitSignal(started, "old", "starting")

	if err := h.tester.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	if h.app.code.searchRunning || h.app.code.searchQuery != "" || h.app.code.searchSubmitted != "" || h.tester.HasText("Searching files…") {
		t.Fatalf("Clear left the old search active: running=%v query=%q submitted=%q texts=%q",
			h.app.code.searchRunning, h.app.code.searchQuery, h.app.code.searchSubmitted, h.texts())
	}
	waitSignal(cancelled, "old", "cancellation")
	h.updates.waitForUpdate(t)

	h.tester.Type("old-again")
	h.tester.Frame()
	timers[1]()
	h.updates.waitForUpdate(t)
	waitSignal(started, "old-again", "starting")
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("fresh")
	h.tester.Frame()
	if h.app.code.searchRunning || len(timers) != 3 {
		t.Fatalf("replacement text did not immediately stop the old search and schedule the new one: running=%v timers=%d",
			h.app.code.searchRunning, len(timers))
	}
	waitSignal(cancelled, "old-again", "cancellation")
	h.updates.waitForUpdate(t)
	timers[2]()
	h.updates.waitForUpdate(t)
	waitSignal(started, "fresh", "starting")
	h.waitFor(t, "the replacement query", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 1 && h.app.code.searchSubmitted == "fresh"
	})
}

func TestCodeSearchClearCancelsNativeFileScan(t *testing.T) {
	originalLookPath, originalRead := workspaceSearchLookPath, workspaceSearchReadFile
	t.Cleanup(func() {
		workspaceSearchLookPath = originalLookPath
		workspaceSearchReadFile = originalRead
	})
	workspaceSearchLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	started := make(chan struct{})
	readPaths := make(chan string, 4)
	workspaceSearchReadFile = func(ctx context.Context, filename string) ([]byte, bool) {
		readPaths <- filename
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return nil, false
	}

	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"first.txt":  "needle\n",
		"second.txt": "needle\n",
	})
	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tester.Type("needle")
	h.tester.Key(0, ui.KeyEnter)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the native search did not reach its first file")
	}

	if err := h.tester.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if len(readPaths) != 1 {
		t.Fatalf("clearing the query allowed the native scan to read %d files", len(readPaths))
	}
	if h.app.code.searchRunning || h.app.code.searchCancel != nil || h.app.code.searchSubmitted != "" || len(h.app.code.searchResults) != 0 {
		t.Fatalf("the cancelled native search retained work or results: running=%v cancel=%v submitted=%q results=%d",
			h.app.code.searchRunning, h.app.code.searchCancel != nil, h.app.code.searchSubmitted, len(h.app.code.searchResults))
	}
}

func TestCodeSearchOptionButtonsResubmitWithImmutableOptions(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"first.txt":  "needle needlefish\n",
		"second.txt": "Needle\n",
	})
	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tester.Type("needle")
	h.tester.Key(0, ui.KeyEnter)
	h.waitFor(t, "the initial case-insensitive search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 2
	})

	if err := h.tester.Click("Match case"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the case-sensitive search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 1
	})
	if !h.app.code.searchCaseSensitive || !h.app.code.searchSubmittedOptions.CaseSensitive {
		t.Fatal("Match case did not become part of the submitted search options")
	}
	if err := h.tester.Click("Match whole word"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the whole-word search", func() bool { return !h.app.code.searchRunning })
	if !h.app.code.searchWholeWord || !h.app.code.searchSubmittedOptions.WholeWord {
		t.Fatal("Match whole word did not become part of the submitted search options")
	}
	if err := h.tester.Click("Use regular expression"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the regular-expression search", func() bool { return !h.app.code.searchRunning })
	if !h.app.code.searchRegularExpr || !h.app.code.searchSubmittedOptions.RegularExpr {
		t.Fatal("Use regular expression did not become part of the submitted search options")
	}

	theme := &ui.Theme{Warning: ui.RGB(250, 204, 21)}
	spans := codeSearchMatchHighlightSpans("before ra_playbook after", workspaceSearchOptions{
		Query: "ra_(playbook|p)", RegularExpr: true, CaseSensitive: true,
	}, theme)
	if len(spans) != 3 || spans[1].Text != "ra_playbook" || spans[1].Background.A == 0 {
		t.Fatalf("variable-length regex highlighting = %+v", spans)
	}
}

func TestCodeSearchCompactScopeAndDisclosedFilters(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"root.txt":              "needle\n",
		"nested/deep/inside.go": "// needle\n",
		"nested/other/skip.go":  "// needle\n",
	})
	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tester.Type("needle")
	scope, scopeOK := h.tester.Find("Workspace folder")
	tabs, tabsOK := h.tester.Find("Code sidebar")
	if !scopeOK || !tabsOK || scope.Y+scope.H > tabs.Y {
		t.Fatalf("the folder selector is not above Files / Search: scope=%+v tabs=%+v", scope, tabs)
	}
	if !slices.Contains(h.texts(), root) {
		t.Fatalf("the root scope does not show the absolute repository path %q: %q", root, h.texts())
	}
	query, queryOK := h.tester.Find("Search in files")
	matchCase, matchCaseOK := h.tester.Find("Match case")
	filters, filtersOK := h.tester.Find("File filters")
	run, runOK := h.tester.Find("Run search")
	if !queryOK || !matchCaseOK || !filtersOK || !runOK {
		t.Fatal("the compact search row is missing a control")
	}
	for name, box := range map[string]ui.Rect{"Match case": matchCase, "File filters": filters, "Run search": run} {
		if box.Y >= query.Y+query.H || box.Y+box.H <= query.Y {
			t.Fatalf("%s is on a separate row from the query: query=%+v control=%+v", name, query, box)
		}
	}
	if matchCase.W > 24 || matchCase.H > 24 || run.W > 26 || run.H > 26 {
		t.Fatalf("search buttons did not shrink: match=%+v run=%+v", matchCase, run)
	}
	if _, found := h.tester.Find("Files to include"); found {
		t.Fatal("advanced file filters consume space before they are requested")
	}
	// The icon-only Search action has the same accessible name and behavior as
	// the former wide text button.
	if err := h.tester.Click("Run search"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the initial repository search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 3
	})
	h.tester.Frame()
	if err := h.tester.Click("Workspace folder"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("nested/deep/")
	h.tester.Frame()
	if h.app.code.searchFolder != "" || len(h.app.code.searchResults) != 3 {
		t.Fatalf("an unsubmitted folder draft changed the workspace: folder=%q results=%d",
			h.app.code.searchFolder, len(h.app.code.searchResults))
	}
	h.tester.Key(0, ui.KeyEnter)
	h.waitFor(t, "the deep-folder search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 1
	})
	if got := h.app.code.searchFolder; got != "nested/deep" {
		t.Fatalf("normalized search folder = %q", got)
	}
	if got := h.app.code.searchResults[0].Path; got != "nested/deep/inside.go" {
		t.Fatalf("deep-folder search returned %q", got)
	}
	if h.app.code.searchSubmittedOptions.Include != "" {
		t.Fatal("the independent folder path leaked into the include glob")
	}
	if got := h.app.code.visiblePaths; !slices.Equal(got, []string{"nested/deep/inside.go"}) {
		t.Fatalf("the Explorer did not adopt the opened workspace folder: %q", got)
	}
	for _, row := range h.app.code.rows {
		if row.path == "nested" || row.path == "root.txt" || row.path == "nested/other" || row.path == "nested/other/skip.go" {
			t.Fatalf("the Explorer still shows an ancestor or outside path: %+v", row)
		}
	}
	h.tester.Frame()
	if err := h.tester.Click("Open entire repository"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the restored repository search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 3
	})
	h.tester.Frame()
	if err := h.tester.Click("File filters"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, found := h.tester.Find("Files to include"); !found {
		t.Fatal("the filter disclosure did not reveal Files to include")
	}
	if _, found := h.tester.Find("Replace All"); found {
		t.Fatal("the Editor still exposes Replace All")
	}

	h.tester.Click("Workspace folder")
	h.tester.Type("../outside")
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if h.app.code.searchRunning || h.app.code.searchError == "" || h.app.code.searchFolder != "" || len(h.app.code.searchResults) != 3 {
		t.Fatalf("an outside-folder draft was not rejected: running=%v error=%q folder=%q results=%d",
			h.app.code.searchRunning, h.app.code.searchError, h.app.code.searchFolder, len(h.app.code.searchResults))
	}
}

func TestCodeCheckoutRefreshesFilesDocumentsAndSearchWithoutDiscardingEdits(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"main.go":  "package main // old branch\n",
		"dirty.go": "package dirty // needle old\n",
		"old.go":   "package old\n",
	})
	clean := openRealCodeDocument(t, h, "main.go")
	dirty := openRealCodeDocument(t, h, "dirty.go")
	dirty.state.Insert("// unsaved editor text\n")
	oldTabs := append([]string(nil), h.app.code.tabs...)
	h.app.code.searchQuery = "needle"
	h.app.beginCodeSearch()
	h.waitFor(t, "the old-branch search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 1
	})

	writeCodeTestFile(t, root, "main.go", "package main // new branch\n")
	writeCodeTestFile(t, root, "feature.go", "package feature // needle new\n")
	if err := os.Remove(filepath.Join(root, "old.go")); err != nil {
		t.Fatal(err)
	}
	oldSnapshot := h.app.snapshot
	newSnapshot := oldSnapshot
	newSnapshot.Branch, newSnapshot.Head = "feature", "new-head"
	h.app.snapshot = newSnapshot
	if !h.app.refreshCodeAfterCheckout(oldSnapshot, newSnapshot) {
		t.Fatal("the branch change did not trigger Code reconciliation")
	}
	h.waitFor(t, "the new branch's complete Editor state", func() bool {
		return h.app.code.filesLoaded && !h.app.code.filesLoading && len(h.app.code.files) == 3 &&
			clean.state != nil && strings.Contains(clean.state.Buffer.String(), "new branch") &&
			!h.app.code.searchRunning && len(h.app.code.searchResults) == 2
	})
	searchedPaths := []string{h.app.code.searchResults[0].Path, h.app.code.searchResults[1].Path}
	slices.Sort(searchedPaths)
	if !slices.Equal(searchedPaths, []string{"dirty.go", "feature.go"}) {
		t.Fatalf("search used stale paths or contents: %q", searchedPaths)
	}
	if got := dirty.state.Buffer.String(); !strings.Contains(got, "unsaved editor text") || !dirty.state.Buffer.Modified() {
		t.Fatalf("checkout changed the dirty buffer: %q", got)
	}
	if dirty.branchNotice == "" {
		t.Fatal("the retained dirty buffer has no previous-branch warning")
	}
	if h.app.code.active != "dirty.go" || !slices.Equal(h.app.code.tabs, oldTabs) {
		t.Fatalf("checkout changed active tab or tabs: active=%q tabs=%q, want %q", h.app.code.active, h.app.code.tabs, oldTabs)
	}
	h.app.saveCodeDocument(dirty)
	if dirty.branchNotice != "" || dirty.state.Buffer.Modified() {
		t.Fatal("explicitly saving retained edits did not accept them on the new branch")
	}
}

func TestCodeClearingFileFilterRevealsTheOpenDocument(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"CM_VERSION":             "other\n",
		"nested/deep/edge.tsx":   "export const edge = true\n",
		"nested/deep/other.tsx":  "export const other = true\n",
		"nested/deep/hidden.tsx": "export const hidden = true\n",
	})
	document := openRealCodeDocument(t, h, "nested/deep/edge.tsx")
	if document == nil {
		t.Fatal("the active document did not open")
	}

	// Leave another file selected and the active document's folders folded, as
	// happens after browsing a large repository before using Quick open.
	for row, item := range h.app.code.rows {
		if item.path == "CM_VERSION" {
			h.app.selectCodeRow(row)
			break
		}
	}
	for _, directory := range []string{"nested", "nested/deep"} {
		h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, directory)] = true
	}
	h.app.syncCodeTree()
	h.tester.Frame()

	h.tester.Key(ui.Cmd, ui.KeyP)
	h.tester.Type("CM_VERSION")
	h.tester.Frame()
	if h.app.code.appliedFilter != "CM_VERSION" || h.app.selectedCodePath() != "CM_VERSION" {
		t.Fatalf("filter did not select its visible file: filter=%q selected=%q", h.app.code.appliedFilter, h.app.selectedCodePath())
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Key(0, ui.KeyBackspace)
	h.tester.Frame()

	if h.app.code.filter != "" || h.app.code.appliedFilter != "" {
		t.Fatalf("clear left filter=%q applied=%q", h.app.code.filter, h.app.code.appliedFilter)
	}
	if got := h.app.selectedCodePath(); got != "nested/deep/edge.tsx" {
		t.Fatalf("clearing Quick open selected %q, want the open document", got)
	}
	for _, directory := range []string{"nested", "nested/deep"} {
		if h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, directory)] {
			t.Fatalf("clearing Quick open left %q collapsed", directory)
		}
	}
	if h.app.code.selectedRow < 0 || h.app.code.selectedRow >= len(h.app.code.rows) ||
		h.app.code.rows[h.app.code.selectedRow].path != document.path {
		t.Fatal("the open document is not the selected row that paints the active background")
	}
	access, ok := h.tester.Access("Open nested/deep/edge.tsx")
	if !ok || access.Offscreen {
		t.Fatalf("the selected open document was not revealed: ok=%v access=%+v", ok, access)
	}
	if !h.tester.Focused("Quick open") {
		t.Fatal("revealing the open document stole focus from the empty filter")
	}
}

func TestCodeMarkdownPreviewSplitsAndTracksUnsavedEdits(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"guide.md": "# Original\n\nA **live preview**.\n",
		"main.go":  "package main\n",
	})
	document := openRealCodeDocument(t, h, "guide.md")
	if err := h.tester.Click("Toggle Markdown preview"); err != nil {
		t.Fatalf("Markdown has no preview toggle: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()

	source, sourceOK := h.tester.Find("Code editor")
	preview, previewOK := h.tester.Find("Markdown preview")
	if !sourceOK || !previewOK || source.W <= 0 || preview.W <= 0 || source.X+source.W > preview.X {
		t.Fatalf("source and preview are not side by side: source=%+v/%v preview=%+v/%v",
			source, sourceOK, preview, previewOK)
	}
	if !h.tester.HasText("A live preview.") {
		t.Fatalf("the Markdown preview is not rendered: %q", h.texts())
	}

	document.state.Buffer.Set("# Updated\n\nUnsaved **rendered edit**.\n")
	h.tester.Frame()
	if !h.tester.HasText("Unsaved rendered edit.") || !document.state.Buffer.Modified() {
		t.Fatalf("the preview did not track the unsaved buffer: modified=%v texts=%q",
			document.state.Buffer.Modified(), h.texts())
	}
	if err := h.tester.Click("Toggle Markdown preview"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, found := h.tester.Find("Markdown preview"); found {
		t.Fatal("the preview remained visible after its toggle was switched off")
	}

	openRealCodeDocument(t, h, "main.go")
	if _, found := h.tester.Find("Toggle Markdown preview"); found {
		t.Fatal("a non-Markdown document has a Markdown preview toggle")
	}
}

func TestCodeLanguageServersUseAnIconWithAHoverStatusList(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"src/edge.tsx":   "export const edge = true\n",
		"src/styles.css": ":root { color: green; }\n",
	})
	// Install neither processes nor prompts in this view test; the two running
	// connections below model servers that already exist in the background.
	for _, id := range []string{"go", "typescript", "css", "tailwind", "eslint", "oxlint"} {
		h.app.settings.LanguageServers[id] = languageServerSettings{Disabled: true}
	}
	openRealCodeDocument(t, h, "src/edge.tsx")
	openRealCodeDocument(t, h, "src/styles.css")
	openRealCodeDocument(t, h, "src/edge.tsx")
	for _, id := range []string{"typescript", "css"} {
		h.app.settings.LanguageServers[id] = languageServerSettings{}
		profile, _ := languageServerProfileByID(id)
		h.app.additionalLanguageServers[id] = &managedLanguageServer{
			profile: profile, connection: newFakeLanguageServerConnection(), documents: make(map[string]uint64),
		}
	}
	h.tester.Frame()

	if h.tester.HasText("TypeScript Language Server") || h.tester.HasText("VS Code CSS Language Server") {
		t.Fatalf("language-server names still occupy the editor toolbar: %q", h.texts())
	}
	button, ok := h.tester.Find("Language servers")
	if !ok {
		t.Fatalf("the compact language-server status button is missing: %q", h.texts())
	}
	h.tester.Move(button.X+button.W/2, button.Y+button.H/2)
	h.tester.Frame()
	for _, label := range []string{"TypeScript Language Server", "VS Code CSS Language Server", "Running"} {
		if !h.tester.HasText(label) {
			t.Errorf("the hover panel does not show %q: %q", label, h.texts())
		}
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.tester.HasText("TypeScript Language Server") || h.app.code.languageServersOpen {
		t.Fatal("Escape did not close the language-server status panel")
	}
}

func TestCodeSearchHighlightSpansMarkTheMatchedSourceText(t *testing.T) {
	theme := &ui.Theme{Warning: ui.RGB(250, 204, 21)}
	spans := codeSearchHighlightSpans("before Needle after", "needle", false, theme)
	if len(spans) != 3 || spans[0].Text != "before " || spans[1].Text != "Needle" || spans[2].Text != " after" {
		t.Fatalf("unexpected highlighted spans: %+v", spans)
	}
	if spans[1].Background.A == 0 {
		t.Fatal("the matched text has no background highlight")
	}

	// Unicode simple folding may change a match's UTF-8 byte length. Highlight
	// the full source rune, not a prefix calculated from the query's bytes.
	spans = codeSearchHighlightSpans("a ſ b", "s", false, theme)
	if len(spans) != 3 || spans[1].Text != "ſ" {
		t.Fatalf("Unicode case folding highlighted the wrong bytes: %+v", spans)
	}
	if spans = codeSearchHighlightSpans("Needle", "needle", true, theme); len(spans) != 1 || spans[0].Background.A != 0 {
		t.Fatalf("a case-sensitive mismatch was highlighted: %+v", spans)
	}
}

func TestBuiltInEditorRevealsAFileAfterTheCodeIndexLoads(t *testing.T) {
	files := map[string]string{
		"main.go":              "package main\n",
		"internal/app/edit.go": "package app\n",
	}
	stubCodeWorkspace(t, files)
	started := make(chan struct{})
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	listCodeFiles = func(string) ([]string, error) {
		close(started)
		<-release
		return []string{"internal/app/edit.go", "main.go"}, nil
	}
	h := menuTestApp(t)
	h.app.setSectionVisible(sectionCode, false)
	h.app.code.filter = "does-not-match"
	h.app.workspacePaneCollapsed[sectionCode] = true
	h.app.fileTreeCollapsed = make(map[string]bool)
	h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, "internal")] = true
	h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, "internal/app")] = true

	h.app.openBuiltInEditor("/work/project/internal/app/edit.go", 0)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the Code file index did not start")
	}
	if h.app.code.pendingReveal != "internal/app/edit.go" {
		t.Fatalf("pending reveal = %q", h.app.code.pendingReveal)
	}
	close(release)
	released = true
	h.waitFor(t, "the delayed explorer reveal", func() bool {
		return h.app.code.filesLoaded && h.app.code.pendingReveal == "" &&
			h.app.selectedCodePath() == "internal/app/edit.go"
	})
	if h.app.workspaceTab != sectionCode || !h.app.sectionVisible(sectionCode) {
		t.Fatal("Built-in did not make Code the active, visible section")
	}
	if h.app.code.filter != "" || h.app.workspacePaneCollapsed[sectionCode] {
		t.Fatal("the old explorer filter or collapsed pane still hides the file")
	}
	for _, directory := range []string{"internal", "internal/app"} {
		if h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, directory)] {
			t.Fatalf("parent %q stayed collapsed", directory)
		}
	}
}

func TestCodePageRetainsDocumentsAcrossSectionsAndRejectsStaleReads(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{"main.go": "package main\n"})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	document := waitForCodeDocument(t, h, "main.go")
	h.app.setWorkspaceTab(sectionChanges)
	h.tester.Frame()
	h.app.setWorkspaceTab(sectionCode)
	h.tester.Frame()
	if h.app.code.documents["main.go"] != document || h.app.activeCodeDocument() != document {
		t.Fatal("switching workspace sections discarded the Code document")
	}

	originalRead := readWorkingFile
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	readWorkingFile = func(string) ([]byte, error) {
		close(started)
		<-release
		close(finished)
		return []byte("stale"), nil
	}
	document.state = nil
	h.app.requestCodeDocument(document)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the replacement read did not start")
	}
	h.app.resetCode()
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the released stale read did not finish")
	}
	// Run the stale read's completion itself, rather than depending on an
	// arbitrary number of unrelated background updates.
	h.updates.waitForUpdate(t)
	if len(h.app.code.documents) != 0 || h.app.code.active != "" {
		t.Fatal("a read from the old repository repopulated reset Code state")
	}
	readWorkingFile = originalRead
}

func TestCodeLargeTreeIsReusedAcrossStableFrames(t *testing.T) {
	files := make(map[string]string, 1500)
	for directory := range 50 {
		for file := range 30 {
			files[fmt.Sprintf("src/dir%02d/file%03d.go", directory, file)] = "package src\n"
		}
	}
	stubCodeWorkspace(t, files)
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1500)
	if len(h.app.code.rows) != 1 {
		t.Fatalf("initial folders are not collapsed: %d rows", len(h.app.code.rows))
	}
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()
	if len(h.app.code.rows) != 1551 {
		t.Fatalf("expanded tree has %d rows", len(h.app.code.rows))
	}
	rows := h.app.code.rows
	for range 3 {
		h.tester.Frame()
	}
	if len(h.app.code.rows) != len(rows) || &h.app.code.rows[0] != &rows[0] {
		t.Fatal("stable frames rebuilt and re-sorted the large explorer tree")
	}

	if err := h.tester.Click("Collapse directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if len(h.app.code.rows) != 1 || h.app.code.rows[0].path != "src" {
		t.Fatalf("folding did not rebuild the large tree: %+v", h.app.code.rows)
	}
	if err := h.tester.Click("Expand directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if len(h.app.code.rows) != 1551 {
		t.Fatalf("expanding restored %d rows, want 1,551", len(h.app.code.rows))
	}
	rows = h.app.code.rows
	h.app.code.filter = "dir49/file029"
	h.tester.Frame()
	if len(h.app.code.rows) != 1 || h.app.code.rows[0].path != "src/dir49/file029.go" {
		t.Fatalf("Quick open did not show the fzf-ranked file directly: %+v", h.app.code.rows)
	}
	if &h.app.code.rows[0] == &rows[0] {
		t.Fatal("a filter change reused stale large-tree rows")
	}
}

func TestCodePageRetainsTwentyIndependentTabBuffers(t *testing.T) {
	files := make(map[string]string, 20)
	for index := range 20 {
		files[fmt.Sprintf("src/file%02d.go", index)] = "package src\n"
	}
	stubCodeWorkspace(t, files)
	h := menuTestApp(t)
	openCodePageForTest(t, h, 20)
	documents := make(map[string]*codeDocument, 20)
	for index := range 20 {
		name := fmt.Sprintf("src/file%02d.go", index)
		h.app.openCodeFile(name, 0)
		documents[name] = waitForCodeDocument(t, h, name)
		documents[name].state.Insert(fmt.Sprintf("// independent buffer %02d\n", index))
	}
	h.tester.Frame()
	if len(h.app.code.tabs) != 20 || len(h.app.code.documents) != 20 {
		t.Fatalf("20 open files produced tabs=%d documents=%d", len(h.app.code.tabs), len(h.app.code.documents))
	}
	for index := range 20 {
		name := fmt.Sprintf("src/file%02d.go", index)
		document := h.app.code.documents[name]
		if document != documents[name] || document.state != documents[name].state {
			t.Fatalf("tab %q replaced its persistent editor state", name)
		}
		if want := fmt.Sprintf("// independent buffer %02d\n", index); !strings.Contains(document.state.Buffer.String(), want) {
			t.Fatalf("tab %q contains %q, missing %q", name, document.state.Buffer.String(), want)
		}
	}
	h.app.openCodeFile("src/file07.go", 0)
	h.tester.Frame()
	if h.app.activeCodeDocument() != documents["src/file07.go"] {
		t.Fatal("switching among 20 tabs did not retain the selected document")
	}
}

func BenchmarkBuildCodeTree1500Files(b *testing.B) {
	paths := make([]string, 0, 1500)
	for directory := range 50 {
		for file := range 30 {
			paths = append(paths, fmt.Sprintf("src/dir%02d/file%03d.go", directory, file))
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = buildFileTreeRowsWithDirectories(paths, nil, true, nil, codeFileTreeScope)
	}
}

func TestCodePageReportsBinaryAndUnreadableFiles(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{
		"picture.png": string(testPNG(t, 9, 6, color.RGBA{B: 255, A: 255})),
		"blob.bin":    "before\x00after",
		"broken.go":   "package broken\n",
	})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 3)
	h.app.openCodeFile("picture.png", 0)
	h.waitFor(t, "the image preview", func() bool {
		document := h.app.code.documents["picture.png"]
		return document != nil && !document.loading && document.imagePreview != nil
	})
	h.tester.Frame()
	if !h.tester.HasText("9 × 6") || !h.tester.HasText("This file is binary and cannot be edited here.") {
		t.Fatalf("the image preview is missing its dimensions or read-only explanation: %q", h.texts())
	}

	h.app.openCodeFile("blob.bin", 0)
	h.waitFor(t, "the binary check", func() bool {
		document := h.app.code.documents["blob.bin"]
		return document != nil && !document.loading && document.binary
	})
	h.tester.Frame()
	if !h.tester.HasText("This file is binary") {
		t.Fatalf("the binary file has no explanation: %q", h.texts())
	}

	originalRead := readWorkingFile
	readWorkingFile = func(string) ([]byte, error) { return nil, errors.New("permission denied") }
	h.app.openCodeFile("broken.go", 0)
	h.waitFor(t, "the failed read", func() bool {
		document := h.app.code.documents["broken.go"]
		return document != nil && !document.loading && document.loadError != ""
	})
	h.tester.Frame()
	if !h.tester.HasText("The file could not be read.") || !h.tester.HasText("permission denied") {
		t.Fatalf("the read error is not shown: %q", h.texts())
	}
	readWorkingFile = originalRead
}
