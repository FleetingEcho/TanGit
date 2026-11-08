package app

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
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
	h.tester.Type("edit.go")
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
	readWorkingFile = func(string) ([]byte, error) {
		close(started)
		<-release
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
	for range 4 {
		h.updates.waitForUpdate(t)
	}
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
	if len(h.app.code.rows) != 3 {
		t.Fatalf("the large-tree filter left %d rows, want 3", len(h.app.code.rows))
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
