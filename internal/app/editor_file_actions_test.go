package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

type codeTestSession struct {
	repository.Session
	root string
}

func (s codeTestSession) Snapshot(ctx context.Context) (repository.Snapshot, error) {
	snapshot, err := s.Session.Snapshot(ctx)
	snapshot.Root = s.root
	return snapshot, err
}

// useRealCodeWorkspace connects the existing Editor harness to a temporary
// working tree. File discovery is replaced only because these repositories do
// not need Git metadata; document reads, writes and structural operations are
// the real filesystem implementations.
func useRealCodeWorkspace(t *testing.T, h *harness, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		writeCodeTestFile(t, root, name, content)
	}
	originalList, originalRead, originalWrite := listCodeFiles, readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		listCodeFiles, readWorkingFile, writeWorkingFile = originalList, originalRead, originalWrite
	})
	listCodeFiles = func(root string) ([]string, error) {
		var paths []string
		err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			relative, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(relative))
			return nil
		})
		sort.Strings(paths)
		return paths, err
	}
	readWorkingFile = os.ReadFile
	writeWorkingFile = func(name string, data []byte) error {
		return os.WriteFile(name, data, 0o644)
	}
	h.app.snapshot.Root = root
	h.app.session = codeTestSession{Session: h.app.session, root: root}
	// File-operation tests exercise temporary workspaces, not external process
	// integration. Keep a locally installed gopls from retaining TempDir on Windows.
	h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Disabled: true}}
	openCodePageForTest(t, h, len(files))
	return root
}

func waitForCodeFileOperation(t *testing.T, h *harness) {
	t.Helper()
	h.waitFor(t, "the Editor file operation and refreshed file index", func() bool {
		return !h.app.code.fileOperationBusy && h.app.code.filesLoaded && !h.app.code.filesLoading
	})
	h.tester.Frame()
}

func openRealCodeDocument(t *testing.T, h *harness, name string) *codeDocument {
	t.Helper()
	h.app.openPinnedCodeFile(name, 0)
	return waitForCodeDocument(t, h, name)
}

func TestCodeTabMenuClosesTabsAndCopiesPaths(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"main.go":     "package main\n",
		"src/one.go":  "package src\n",
		"src/two.go":  "package src\n",
		"src/last.go": "package src\n",
	})
	for _, name := range []string{"main.go", "src/one.go", "src/two.go", "src/last.go"} {
		openRealCodeDocument(t, h, name)
	}
	h.tester.Frame()

	h.rightClick(t, "Editor tab src/two.go")
	h.requireMenuLabels(t, "Close Tab", "Close Other Tabs", "Close Tabs to the Right", "Close All Tabs", "Copy path", "Copy relative path")
	h.chooseMenuItem(t, "Copy relative path")
	if got := h.tester.Clipboard(); got != "src/two.go" {
		t.Fatalf("Copy relative path copied %q", got)
	}
	h.rightClick(t, "Editor tab src/two.go")
	h.chooseMenuItem(t, "Copy path")
	if got, want := h.tester.Clipboard(), filepath.Join(root, "src", "two.go"); got != want {
		t.Fatalf("Copy path copied %q, want %q", got, want)
	}

	h.rightClick(t, "Editor tab src/one.go")
	h.chooseMenuItem(t, "Close Tab")
	if got := strings.Join(h.app.code.tabs, ","); got != "main.go,src/two.go,src/last.go" {
		t.Fatalf("Close Tab left %q", got)
	}
	h.rightClick(t, "Editor tab main.go")
	h.chooseMenuItem(t, "Close Tabs to the Right")
	if got := strings.Join(h.app.code.tabs, ","); got != "main.go" {
		t.Fatalf("Close Tabs to the Right left %q", got)
	}

	for _, name := range []string{"src/one.go", "src/two.go", "src/last.go"} {
		openRealCodeDocument(t, h, name)
	}
	h.rightClick(t, "Editor tab src/two.go")
	h.chooseMenuItem(t, "Close Other Tabs")
	if got := strings.Join(h.app.code.tabs, ","); got != "src/two.go" || h.app.code.active != "src/two.go" {
		t.Fatalf("Close Other Tabs left tabs %q and active %q", got, h.app.code.active)
	}
	h.rightClick(t, "Editor tab src/two.go")
	h.chooseMenuItem(t, "Close All Tabs")
	if len(h.app.code.tabs) != 0 || len(h.app.code.documents) != 0 || h.app.code.active != "" || h.app.code.activeTab != 0 {
		t.Fatalf("Close All Tabs left tabs=%q documents=%d active=%q activeTab=%d",
			h.app.code.tabs, len(h.app.code.documents), h.app.code.active, h.app.code.activeTab)
	}
}

func TestCodeFileOperationDoesNotReindexAgainForItsQuietSnapshot(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{"old.txt": "old\n"})
	// Isolate the operation's explicit refresh from a real background snapshot;
	// the matching quiet snapshot is applied deterministically below.
	h.app.session = nil
	h.app.beginCodeFileOperation(codeFileCreateFile, "", "created.txt")
	waitForCodeFileOperation(t, h)
	token := h.app.code.filesToken

	changed := h.app.snapshot
	changed.Files = append([]repository.File(nil), changed.Files...)
	changed.Files = append(changed.Files, repository.File{Path: "created.txt", Status: "??", Added: true})
	was, now := signSnapshot(h.app.snapshot), signSnapshot(changed)
	if was.codeFiles == now.codeFiles {
		t.Fatal("the fixture does not model the created file's Git signature")
	}
	h.app.applyQuietSnapshot(changed, was, now)
	if h.app.code.filesToken != token || !h.app.code.filesLoaded || h.app.code.filesLoading {
		t.Fatalf("one creation reindexed twice: token %d->%d loaded=%v loading=%v",
			token, h.app.code.filesToken, h.app.code.filesLoaded, h.app.code.filesLoading)
	}

	external := changed
	external.Files = append([]repository.File(nil), changed.Files...)
	external.Files = append(external.Files, repository.File{Path: "external.txt", Status: "??", Added: true})
	was, now = signSnapshot(changed), signSnapshot(external)
	h.app.applyQuietSnapshot(external, was, now)
	if h.app.code.filesToken != token+1 || !h.app.code.filesLoading {
		t.Fatalf("a later external change was not reindexed: token %d->%d loading=%v",
			token, h.app.code.filesToken, h.app.code.filesLoading)
	}
	h.waitFor(t, "the later external file index", func() bool {
		return h.app.code.filesLoaded && !h.app.code.filesLoading
	})
}

func TestCodeExplorerMenuCreatesRenamesDeletesAndCopies(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"source/old.txt":       "old\n",
		"destination/keep.txt": "keep\n",
	})
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()

	h.rightClick(t, "Open source/old.txt")
	h.requireMenuLabels(t, "New File…", "New Folder…", "View File History…", "Rename…", "Delete…", "Copy path", "Copy relative path")
	h.chooseMenuItem(t, "Copy relative path")
	if got := h.tester.Clipboard(); got != "source/old.txt" {
		t.Fatalf("explorer copied relative path %q", got)
	}
	h.rightClick(t, "Open source/old.txt")
	h.chooseMenuItem(t, "Copy path")
	if got, want := h.tester.Clipboard(), filepath.Join(root, "source", "old.txt"); got != want {
		t.Fatalf("explorer copied absolute path %q, want %q", got, want)
	}
	h.rightClick(t, "Open source/old.txt")
	h.chooseMenuItem(t, "View File History…")
	h.waitFor(t, "the Editor file history", func() bool {
		return h.app.fileHistoryOpen && h.app.fileHistoryPath == "source/old.txt"
	})
	h.app.fileHistoryOpen = false
	h.tester.Frame()

	// A folder's New Folder action creates inside that folder, and the explicit
	// empty-directory row survives the next Git-file-index refresh.
	h.rightClick(t, "Collapse directory source")
	h.chooseMenuItem(t, "New Folder…")
	if got := h.app.code.fileFormDirectory; got != "source" {
		t.Fatalf("New Folder targets %q, want source", got)
	}
	h.tester.Type("empty")
	h.tester.Key(0, ui.KeyEnter)
	waitForCodeFileOperation(t, h)
	if info, err := os.Stat(filepath.Join(root, "source", "empty")); err != nil || !info.IsDir() {
		t.Fatalf("created directory: info=%v err=%v", info, err)
	}
	h.waitFor(t, "the empty directory row", func() bool {
		for _, row := range h.app.code.rows {
			if row.isDirectory() && row.path == "source/empty" {
				return true
			}
		}
		return false
	})
	if h.app.code.selectedRow < 0 || h.app.code.selectedRow >= len(h.app.code.rows) || h.app.code.rows[h.app.code.selectedRow].path != "source/empty" {
		t.Fatalf("the created empty folder was not revealed and selected: row=%d rows=%+v", h.app.code.selectedRow, h.app.code.rows)
	}

	// The toolbar uses the selected directory, not whichever document is open.
	h.app.selectCodeTreePath("source", true)
	h.tester.Frame()
	if err := h.tester.Click("New File…"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if got := h.app.code.fileFormDirectory; h.app.code.fileForm != codeFileFormNewFile || got != "source" {
		t.Fatalf("toolbar New File targets form=%v directory=%q", h.app.code.fileForm, got)
	}
	h.tester.Type("created.txt")
	if got := h.app.code.fileFormName; got != "created.txt" {
		t.Fatalf("New File input contains %q", got)
	}
	h.tester.Key(0, ui.KeyEnter)
	waitForCodeFileOperation(t, h)
	if _, err := os.Stat(filepath.Join(root, "source", "created.txt")); err != nil {
		t.Fatalf("New File did not create its target: form=%v name=%q error=%q busy=%v err=%v texts=%q",
			h.app.code.fileForm, h.app.code.fileFormName, h.app.code.fileFormError, h.app.code.fileOperationBusy, err, h.texts())
	}
	assertCodeFileContent(t, root, "source/created.txt", "")
	if _, ok := h.tester.Find("Open source/old.txt"); !ok {
		t.Fatalf("old file is not visible after creation: form=%v rows=%+v texts=%q", h.app.code.fileForm, h.app.code.rows, h.texts())
	}
	h.rightClick(t, "Open source/old.txt")
	h.chooseMenuItem(t, "Rename…")
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("renamed.txt")
	h.tester.Key(0, ui.KeyEnter)
	waitForCodeFileOperation(t, h)
	assertCodeFileMissing(t, root, "source/old.txt")
	assertCodeFileContent(t, root, "source/renamed.txt", "old\n")

	h.rightClick(t, "Open source/renamed.txt")
	h.chooseMenuItem(t, "Delete…")
	if h.app.confirm == nil {
		t.Fatal("Delete opened no confirmation")
	}
	if err := h.tester.Click("Delete"); err != nil {
		t.Fatal(err)
	}
	waitForCodeFileOperation(t, h)
	assertCodeFileMissing(t, root, "source/renamed.txt")
}

func TestCodeFolderSearchUsesLiteralScopeAndRetainsRename(t *testing.T) {
	h := menuTestApp(t)
	folder := "roles[old]*?"
	useRealCodeWorkspace(t, h, map[string]string{
		folder + "/inside.txt":  "needle\n",
		"rolesXabc/outside.txt": "needle\n",
	})
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()
	h.app.code.searchQuery = "needle"

	h.rightClick(t, "Collapse directory "+folder)
	h.requireMenuLabels(t, "Open folder as workspace", "Rename…")
	h.chooseMenuItem(t, "Open folder as workspace")
	if h.app.code.searchInclude != "" || h.app.code.searchFolder != folder {
		t.Fatalf("folder search scope include=%q literal=%q, want an independent %q", h.app.code.searchInclude, h.app.code.searchFolder, folder)
	}
	h.waitFor(t, "the literal folder search", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 1
	})
	if got := h.app.code.searchResults[0].Path; got != folder+"/inside.txt" {
		t.Fatalf("folder search returned %q", got)
	}

	// The visible scope is editable, so a deep folder can be changed without
	// turning a literal folder name into an include glob.
	h.tester.Frame()
	if err := h.tester.Click("Workspace folder"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("rolesXabc")
	h.tester.Key(0, ui.KeyEnter)
	h.waitFor(t, "search in the edited folder", func() bool {
		return !h.app.code.searchRunning && len(h.app.code.searchResults) == 1 && h.app.code.searchResults[0].Path == "rolesXabc/outside.txt"
	})
}

func TestCodeExplorerDragMovesFileAndPreservesOpenDocument(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"source/note.txt":      "original\n",
		"destination/keep.txt": "keep\n",
	})
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()

	document := openRealCodeDocument(t, h, "source/note.txt")
	document.state.Insert("// edited before drag\n")
	h.tester.Frame()

	source, ok := h.tester.Find("Open source/note.txt")
	if !ok {
		t.Fatal("the source file row is missing")
	}
	target, ok := h.tester.Find("Collapse directory destination")
	if !ok {
		t.Fatal("the destination folder row is missing")
	}
	h.tester.Press(source.X+source.W/2, source.Y+source.H/2)
	h.tester.Move(target.X+target.W/2, target.Y+target.H/2)
	h.tester.Frame()
	h.tester.Release(target.X+target.W/2, target.Y+target.H/2)
	h.tester.Frame()
	h.waitFor(t, "the dragged file", func() bool {
		_, err := os.Stat(filepath.Join(root, "destination", "note.txt"))
		return err == nil && !h.app.code.fileOperationBusy && h.app.code.filesLoaded
	})
	assertCodeFileMissing(t, root, "source/note.txt")
	assertCodeFileContent(t, root, "destination/note.txt", "original\n")
	if !document.state.Buffer.Modified() {
		t.Fatal("moving implicitly saved the dirty buffer")
	}
	moved := h.app.code.documents["destination/note.txt"]
	if moved != document || document.path != "destination/note.txt" {
		t.Fatalf("drag replaced or orphaned the open document: moved=%p original=%p path=%q", moved, document, document.path)
	}
	if h.app.code.documents["source/note.txt"] != nil || h.app.code.active != "destination/note.txt" {
		t.Fatalf("drag left old document state: active=%q documents=%v", h.app.code.active, h.app.code.documents)
	}
	if len(h.app.code.tabs) != 1 || h.app.code.tabs[0] != "destination/note.txt" {
		t.Fatalf("drag left tabs %q", h.app.code.tabs)
	}
	if got := document.state.Buffer.String(); got != "// edited before drag\noriginal\n" {
		t.Fatalf("the moved editor buffer changed to %q", got)
	}
}

func TestCodeFolderMoveRekeysNestedDocumentsAndDirectories(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"tree/nested/open.txt": "open\n",
		"archive/keep.txt":     "keep\n",
	})
	document := openRealCodeDocument(t, h, "tree/nested/open.txt")
	document.state.Insert("edited\n")
	if err := os.Mkdir(filepath.Join(root, "tree", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.app.code.extraDirectories = map[string]bool{"tree/empty": true}
	h.app.fileTreeCollapsed = make(map[string]bool)
	h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, "tree/nested")] = true
	h.app.syncCodeTree()

	h.app.code.pendingMoveSource = "tree"
	h.app.code.pendingMoveTarget = "archive"
	h.app.applyPendingCodeActions()
	waitForCodeFileOperation(t, h)
	h.waitFor(t, "the moved folder index", func() bool { return h.app.code.filesLoaded })
	assertCodeFileMissing(t, root, "tree/nested/open.txt")
	assertCodeFileContent(t, root, "archive/tree/nested/open.txt", "open\n")
	if !document.state.Buffer.Modified() || document.state.Buffer.String() != "edited\nopen\n" {
		t.Fatal("moving lost or implicitly saved unsaved changes")
	}
	if moved := h.app.code.documents["archive/tree/nested/open.txt"]; moved != document {
		t.Fatalf("nested open document was not rekeyed: %p != %p", moved, document)
	}
	if !h.app.code.extraDirectories["archive/tree/empty"] || h.app.code.extraDirectories["tree/empty"] {
		t.Fatalf("empty directories were not moved: %v", h.app.code.extraDirectories)
	}
	if !h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, "archive/tree/nested")] {
		t.Fatal("nested folder collapse state did not follow the move")
	}
}

func TestCodeExplorerRowsDoNotShowPathTooltips(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{"nested/tooltip.txt": "content\n"})
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()

	row, found := h.tester.Find("Open nested/tooltip.txt")
	if !found {
		t.Fatal("the explorer file row is missing")
	}
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Advance(700 * time.Millisecond)
	h.tester.Frame()
	for _, text := range h.tester.Texts() {
		if text == "nested/tooltip.txt" {
			t.Fatalf("hovering an explorer file still shows its redundant path tooltip: %q", h.texts())
		}
	}
}

func TestCodeCloseKeepsUnsavedBuffersAndWaitsForFileOperations(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{"note.txt": "original\n"})
	document := openRealCodeDocument(t, h, "note.txt")
	document.state.Insert("unsaved\n")
	realWrite := writeWorkingFile
	writeWorkingFile = func(string, []byte) error { return errors.New("disk full") }
	h.app.closeCodeFile("note.txt")
	h.app.acceptConfirm()
	if h.app.code.documents["note.txt"] != document || document.saveError == "" || !document.state.Buffer.Modified() {
		t.Fatalf("a failed close discarded its buffer: document=%p error=%q", h.app.code.documents["note.txt"], document.saveError)
	}
	writeWorkingFile = realWrite
	h.app.code.fileOperationBusy = true
	h.app.closeCodeFile("note.txt")
	if h.app.code.documents["note.txt"] != document {
		t.Fatal("a structural file operation allowed an editor tab to close")
	}
	h.app.code.fileOperationBusy = false
	h.app.closeCodeFile("note.txt")
	h.app.acceptConfirm()
	if len(h.app.code.tabs) != 0 || len(h.app.code.documents) != 0 {
		t.Fatal("the tab did not close after its save succeeded")
	}
}

func TestCodeDeleteDiscardsWithoutRecreatingLateEdits(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"source/note.txt": "original\n"})
	document := openRealCodeDocument(t, h, "source/note.txt")
	if err := performCodeFileOperation(root, codeFileDelete, "source/note.txt", "source/note.txt"); err != nil {
		t.Fatal(err)
	}
	// Model an edit arriving after the asynchronous delete started. Completion
	// must discard this document rather than close it through the autosave path.
	h.app.code.fileOperationBusy = true
	document.state.Insert("edited while deleting\n")
	h.app.completeCodeFileOperation(codeFileDelete, "source/note.txt", "source/note.txt", false)
	h.app.code.fileOperationBusy = false
	h.tester.Frame()
	if h.app.code.documents["source/note.txt"] != nil || len(h.app.code.tabs) != 0 {
		t.Fatal("the deleted document stayed open")
	}
	assertCodeFileMissing(t, root, "source/note.txt")
	if !h.app.code.extraDirectories["source"] {
		t.Fatal("the real parent folder disappeared after its last file was deleted")
	}
}

func TestCodeOperationInvalidatesStaleFileIndex(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"source/old.txt": "old\n"})
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	listCodeFiles = func(string) ([]string, error) {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			<-releaseFirst
			return []string{"source/old.txt"}, nil
		case 2:
			close(secondStarted)
			return []string{"source/new.txt"}, nil
		default:
			return nil, errors.New("unexpected extra file-index request")
		}
	}
	h.app.requestCodeFiles()
	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the stale file-index request did not start")
	}
	h.app.beginCodeFileOperation(codeFileRename, "source/old.txt", "source/new.txt")
	h.waitFor(t, "the replacement file-index request", func() bool {
		select {
		case <-secondStarted:
			return true
		default:
			return false
		}
	})
	if calls.Load() != 2 {
		t.Fatalf("the operation started %d file-index requests, want 2", calls.Load())
	}
	waitForCodeFileOperation(t, h)
	close(releaseFirst)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	assertCodeFileMissing(t, root, "source/old.txt")
	if got := strings.Join(h.app.code.files, ","); got != "source/new.txt" {
		t.Fatalf("a stale file-index read replaced the new paths with %q", got)
	}
}

func TestCodeMoveRejectsAReadFromTheOldPath(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"source/old.txt": "current\n"})
	oldReadStarted := make(chan struct{})
	releaseOldRead := make(chan struct{})
	readWorkingFile = func(name string) ([]byte, error) {
		if filepath.Base(name) == "old.txt" {
			close(oldReadStarted)
			<-releaseOldRead
			return []byte("stale\n"), nil
		}
		return os.ReadFile(name)
	}
	h.app.openCodeFile("source/old.txt", 0)
	select {
	case <-oldReadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the old document read did not start")
	}
	h.app.beginCodeFileOperation(codeFileRename, "source/old.txt", "source/new.txt")
	waitForCodeFileOperation(t, h)
	h.waitFor(t, "the renamed document read", func() bool {
		document := h.app.code.documents["source/new.txt"]
		return document != nil && document.state != nil && !document.loading
	})
	close(releaseOldRead)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	document := h.app.code.documents["source/new.txt"]
	if document == nil || document.state.Buffer.String() != "current\n" {
		t.Fatalf("the old-path read replaced the renamed buffer: %+v", document)
	}
	if h.app.code.documents["source/old.txt"] != nil {
		t.Fatal("the old document path returned after its stale read finished")
	}
	assertCodeFileContent(t, root, "source/new.txt", "current\n")
}

func TestCodeMovePreservesChangesEditorAfterSaveFailure(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"source/note.txt": "original\n"})
	state := &editor.State{Buffer: editor.NewBuffer("original\n")}
	state.Insert("unsaved\n")
	buffer := state.Buffer
	h.app.fileEditing = true
	h.app.fileEditKey = "source/note.txt"
	h.app.fileEditState = state
	writeWorkingFile = func(string, []byte) error { return errors.New("disk full") }
	h.app.beginCodeFileOperation(codeFileRename, "source/note.txt", "source/renamed.txt")
	if !h.app.fileEditing || h.app.fileEditState == nil || h.app.fileEditState.Buffer != buffer || h.app.fileEditSaveError == "" {
		t.Fatal("a failed Changes-editor save was discarded before a rename")
	}
	if !buffer.Modified() || h.app.code.fileOperationBusy {
		t.Fatal("the rename ran despite its Changes-editor save failure")
	}
	assertCodeFileContent(t, root, "source/note.txt", "original\n")
	assertCodeFileMissing(t, root, "source/renamed.txt")
}

func TestCodeRepositoryRootCreatesAndAcceptsDrops(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"source/note.txt": "note\n"})
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()

	openRealCodeDocument(t, h, "source/note.txt")
	h.rightClick(t, "Repository root")
	h.chooseMenuItem(t, "New File…")
	if got := h.app.code.fileFormDirectory; got != "" {
		t.Fatalf("root New File targets %q", got)
	}
	h.tester.Type("created.txt")
	h.tester.Key(0, ui.KeyEnter)
	waitForCodeFileOperation(t, h)
	assertCodeFileContent(t, root, "created.txt", "")

	source, ok := h.tester.Find("Open source/note.txt")
	if !ok {
		t.Fatal("the root-drag source row is missing")
	}
	target, ok := h.tester.Find("Repository root")
	if !ok {
		t.Fatal("the repository-root drop target is missing")
	}
	h.tester.Press(source.X+source.W/2, source.Y+source.H/2)
	h.tester.Move(target.X+target.W/2, target.Y+target.H/2)
	h.tester.Frame()
	h.tester.Release(target.X+target.W/2, target.Y+target.H/2)
	h.tester.Frame()
	waitForCodeFileOperation(t, h)
	assertCodeFileMissing(t, root, "source/note.txt")
	assertCodeFileContent(t, root, "note.txt", "note\n")
	if h.app.code.documents["note.txt"] == nil {
		t.Fatal("the open document did not follow its move to the repository root")
	}
}

func TestCodeNestedWorkspaceRootCreatesAndAcceptsDrops(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"nested/deep/sub/note.txt": "note\n",
		"nested/other/skip.txt":    "skip\n",
		"root.txt":                 "root\n",
	})
	if !h.app.openCodeWorkspaceFolder("nested/deep") {
		t.Fatal("the nested workspace was rejected")
	}
	h.app.setAllFileTreeDirectories(codeFileTreeScope, h.app.code.files, false)
	h.app.syncCodeTree()
	h.tester.Frame()

	h.rightClick(t, "Workspace root nested/deep")
	h.chooseMenuItem(t, "New File…")
	if got := h.app.code.fileFormDirectory; got != "nested/deep" {
		t.Fatalf("workspace-root New File targets %q", got)
	}
	h.app.closeCodeFileForm()
	h.tester.Frame()

	source, ok := h.tester.Find("Open nested/deep/sub/note.txt")
	if !ok {
		t.Fatal("the nested workspace source row is missing")
	}
	target, ok := h.tester.Find("Workspace root nested/deep")
	if !ok {
		t.Fatal("the nested workspace drop target is missing")
	}
	h.tester.Press(source.X+source.W/2, source.Y+source.H/2)
	h.tester.Move(target.X+target.W/2, target.Y+target.H/2)
	h.tester.Frame()
	h.tester.Release(target.X+target.W/2, target.Y+target.H/2)
	h.tester.Frame()
	waitForCodeFileOperation(t, h)
	assertCodeFileMissing(t, root, "nested/deep/sub/note.txt")
	assertCodeFileContent(t, root, "nested/deep/note.txt", "note\n")
}

func TestCodeEmptyDirectoriesFilterAndPrune(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeCodeTestFile(t, root, "not-a-directory", "file")
	a := newApp(fakeOpener{})
	a.snapshot.Root = root
	a.code.extraDirectories = map[string]bool{"keep": true, "missing": true, "not-a-directory": true}
	a.pruneCodeDirectories(root)
	if len(a.code.extraDirectories) != 1 || !a.code.extraDirectories["keep"] {
		t.Fatalf("stale empty directories were not pruned: %v", a.code.extraDirectories)
	}
	a.code.filter = "keep"
	a.syncCodeTree()
	if len(a.code.rows) != 1 || a.code.rows[0].path != "keep" {
		t.Fatalf("a matching empty directory is not visible: %+v", a.code.rows)
	}
	a.code.filter = "unrelated"
	a.syncCodeTree()
	if len(a.code.rows) != 0 {
		t.Fatalf("an empty directory ignored the explorer filter: %+v", a.code.rows)
	}
}
