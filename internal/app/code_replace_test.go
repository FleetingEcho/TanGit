package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func prepareCodeSearchReplacement(t *testing.T, h *harness, query, replacement string) {
	t.Helper()
	h.app.code.searchQuery = query
	h.app.beginCodeSearch()
	h.waitFor(t, "results before replacement", func() bool {
		return !h.app.code.searchRunning && h.app.code.searchSubmitted == query
	})
	h.app.code.searchReplaceQuery = replacement
}

func readCodeReplacementTestFile(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCodeSearchReplaceAllUpdatesCleanBuffersAndClosedFiles(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"closed.txt": "Needle needle\n",
		"open.txt":   "Needle needle\n",
	})
	document := openRealCodeDocument(t, h, "open.txt")
	prepareCodeSearchReplacement(t, h, "needle", "other")
	h.app.code.searchPreserveCase = true
	h.app.startCodeSearchReplaceAll()
	h.waitFor(t, "clean editor and closed-file replacement", func() bool {
		return !h.app.code.searchReplaceRunning && h.app.code.searchReplaceStatus == "4 replacements in 2 files"
	})
	want := "Other other\n"
	if got := readCodeReplacementTestFile(t, root, "closed.txt"); got != want {
		t.Fatalf("closed file = %q, want %q", got, want)
	}
	if got := readCodeReplacementTestFile(t, root, "open.txt"); got != want {
		t.Fatalf("open file on disk = %q, want %q", got, want)
	}
	if got := document.state.Buffer.String(); got != want || document.state.Buffer.Modified() {
		t.Fatalf("open buffer = %q modified=%v, replacement error %q", got, document.state.Buffer.Modified(), h.app.code.searchReplaceError)
	}
	if !document.state.Buffer.Undo() || document.state.Buffer.String() != "Needle needle\n" || !document.state.Buffer.Modified() {
		t.Fatal("Replace All did not make one undoable, explicitly saveable editor edit")
	}
	if got := readCodeReplacementTestFile(t, root, "open.txt"); got != want {
		t.Fatal("undo changed the disk behind the user's back")
	}
}

func TestCodeSearchReplaceAllRefusesInScopeDirtyBuffersBeforeWrites(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"dirty.txt": "needle dirty\n",
		"other.txt": "needle other\n",
	})
	document := openRealCodeDocument(t, h, "dirty.txt")
	document.state.Insert("unsaved ")
	prepareCodeSearchReplacement(t, h, "needle", "changed")
	h.app.requestCodeSearchReplaceAll()
	if h.app.confirm != nil || h.app.code.searchReplaceRunning {
		t.Fatal("dirty preflight allowed replacement to reach confirmation or disk work")
	}
	if !strings.Contains(h.app.code.searchReplaceError, "dirty.txt") {
		t.Fatalf("dirty preflight error does not name the protection reason: %q", h.app.code.searchReplaceError)
	}
	if got := readCodeReplacementTestFile(t, root, "other.txt"); got != "needle other\n" {
		t.Fatalf("another file was partially replaced: %q", got)
	}
	if got := document.state.Buffer.String(); got != "unsaved needle dirty\n" {
		t.Fatalf("unsaved buffer changed: %q", got)
	}
}

func TestCodeSearchReplaceAllAllowsOutOfScopeDirtyBuffers(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"dirty.txt":  "needle dirty\n",
		"scoped.txt": "needle scoped\n",
	})
	document := openRealCodeDocument(t, h, "dirty.txt")
	document.state.Insert("unsaved ")
	prepareCodeSearchReplacement(t, h, "needle", "changed")
	h.app.code.searchInclude = "*.go"
	// The submitted options are the immutable search scope, so resubmit after
	// editing the include filter rather than changing replacement behavior.
	h.app.beginCodeSearch()
	h.waitFor(t, "filtered search", func() bool { return !h.app.code.searchRunning })
	h.app.code.searchReplaceQuery = "changed"
	h.app.code.searchInclude = "scoped.txt"
	h.app.beginCodeSearch()
	h.waitFor(t, "scoped search", func() bool { return !h.app.code.searchRunning })
	h.app.startCodeSearchReplaceAll()
	h.waitFor(t, "out-of-scope replacement", func() bool {
		return !h.app.code.searchReplaceRunning && h.app.code.searchReplaceStatus == "1 replacements in 1 files"
	})
	if got := readCodeReplacementTestFile(t, root, "dirty.txt"); got != "needle dirty\n" {
		t.Fatal("the out-of-scope dirty document caused an unnecessary disk write")
	}
	if got := readCodeReplacementTestFile(t, root, "scoped.txt"); got != "changed scoped\n" {
		t.Fatalf("in-scope file was not replaced: %q", got)
	}
	if got := document.state.Buffer.String(); got != "unsaved needle dirty\n" || !document.state.Buffer.Modified() {
		t.Fatal("out-of-scope replacement discarded unsaved text")
	}
}

func TestCodeSearchReplaceAllLeavesLateEditorEditsAuthoritative(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"open.txt": "needle\n"})
	document := openRealCodeDocument(t, h, "open.txt")
	options := workspaceSearchOptions{Query: "needle", CaseSensitive: true}
	changes, err := planWorkspaceSearchReplacement(document.state.Buffer.String(), options, "changed", false)
	if err != nil {
		t.Fatal(err)
	}
	planned := codeSearchReplaceOpenDocument{
		document: document, state: document.state, generation: document.generation,
		version: document.state.Buffer.Version(), changes: changes,
	}
	document.state.Insert("newer user edit ")
	outcome := codeSearchReplaceOutcome{
		open: []codeSearchReplaceOpenDocument{planned}, pending: make(map[*codeDocument]int),
		root: root, generation: h.app.generation,
	}
	h.app.code.searchReplaceRunning = true
	h.app.code.searchReplaceToken++
	h.app.applyCodeSearchReplaceOpenDocuments(context.Background(), h.app.code.searchReplaceToken, false, outcome)
	if got := document.state.Buffer.String(); got != "newer user edit needle\n" {
		t.Fatalf("replacement overwrote a newer editor version: %q", got)
	}
	if h.app.code.searchReplaceError == "" {
		t.Fatal("late editor edit was silently skipped without a partial-failure report")
	}
	if got := readCodeReplacementTestFile(t, root, "open.txt"); got != "needle\n" {
		t.Fatalf("late buffer edit leaked to disk: %q", got)
	}
}

func TestCodeSearchReplaceAllReportsClosedFileWriteFailure(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"closed.txt": "needle\n"})
	prepareCodeSearchReplacement(t, h, "needle", "changed")
	originalWrite := writeWorkingFile
	writeWorkingFile = func(string, []byte) error { return errors.New("disk full") }
	t.Cleanup(func() { writeWorkingFile = originalWrite })
	h.app.startCodeSearchReplaceAll()
	h.waitFor(t, "replacement write failure", func() bool { return !h.app.code.searchReplaceRunning })
	if h.app.code.searchReplaceError == "" || h.app.code.searchReplaceStatus != "No matches to replace" {
		t.Fatalf("write failure was not reported honestly: status=%q error=%q", h.app.code.searchReplaceStatus, h.app.code.searchReplaceError)
	}
	if got := readCodeReplacementTestFile(t, root, "closed.txt"); got != "needle\n" {
		t.Fatalf("failed write changed content: %q", got)
	}
}

func TestCodeSearchReplaceAllConfirmationIsRequiredAndCancellable(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle needle\n"})
	prepareCodeSearchReplacement(t, h, "needle", "changed")
	h.tester.Key(ui.Cmd | ui.Shift, ui.KeyH)
	h.tester.Frame()

	if err := h.tester.Click("Replace All"); err != nil {
		t.Fatalf("open Replace All confirmation: %v", err)
	}
	h.tester.Frame()
	request := h.app.confirm
	if request == nil {
		t.Fatal("Replace All wrote without its confirmation")
	}
	if request.title != "Replace All" || request.message != "Replace all matches in the selected files?" ||
		request.confirmLabel != "Replace All" || !request.destructive {
		t.Fatalf("Replace All confirmation = %+v", request)
	}
	if !h.tester.HasText("This changes files on disk. Open files with unsaved changes must be saved first.") {
		t.Fatalf("confirmation omits the disk-write warning: %q", h.texts())
	}
	if h.app.code.searchReplaceRunning || readCodeReplacementTestFile(t, root, "source.txt") != "needle needle\n" {
		t.Fatal("asking for confirmation already changed the file")
	}

	// Escape cancels the modal without starting any disk work.
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.confirm != nil || h.app.code.searchReplaceRunning {
		t.Fatal("cancelling the confirmation did not abandon replacement")
	}
	if got := readCodeReplacementTestFile(t, root, "source.txt"); got != "needle needle\n" {
		t.Fatalf("cancelled replacement changed the file: %q", got)
	}

	if err := h.tester.Click("Replace All"); err != nil {
		t.Fatalf("reopen Replace All confirmation: %v", err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	h.waitFor(t, "confirmed replacement", func() bool {
		return !h.app.code.searchReplaceRunning && h.app.code.searchReplaceStatus == "2 replacements in 1 files"
	})
	if got := readCodeReplacementTestFile(t, root, "source.txt"); got != "changed changed\n" {
		t.Fatalf("confirmed file = %q, want %q", got, "changed changed\n")
	}
	if h.app.code.searchReplaceError != "" {
		t.Fatalf("confirmed replacement reported an error: %q", h.app.code.searchReplaceError)
	}
}
