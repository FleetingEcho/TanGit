package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Code sidebar shows whether the repository is indexed, and a click on an
// unindexed one starts the index.
func TestCodeSearchIndexButtonShowsStateAndBuilds(t *testing.T) {
	h := menuTestApp(t)
	h.app.snapshot.Root = t.TempDir()
	h.app.setWorkspaceTab(sectionCode)
	h.app.pathCache = t.TempDir()
	h.tester.SetSize(1100, 800)
	h.tester.Frame()
	if _, ok := h.tester.Find("Code search is not indexed"); !ok {
		t.Fatal("an unindexed repository does not say so")
	}
	h.app.code.searchIndex.status = codeSearchIndexReady
	h.app.code.searchIndex.documents = 12
	h.tester.Frame()
	if _, ok := h.tester.Find("Code search index is ready"); !ok {
		t.Fatal("an indexed repository does not say so")
	}
	h.app.code.searchIndex.status = codeSearchIndexUnavailable
	h.tester.Frame()
	if _, ok := h.tester.Find("Code search index is too large"); !ok {
		t.Fatal("a too-large repository does not say so")
	}

	// Clicking while there is no index asks to build one.
	h.app.code.searchIndex.status = codeSearchIndexMissing
	h.app.code.filesLoaded = false
	h.app.startCodeSearchIndex()
	if !h.app.code.filesLoading && !h.app.code.filesLoaded {
		t.Fatal("starting the index did not read the file list first")
	}
}

func TestTruncatedPlainFolderSkipsCodeSearchIndex(t *testing.T) {
	h := menuTestApp(t)
	a := h.app
	a.session = nil
	a.snapshot.Root = t.TempDir()
	a.pathCache = t.TempDir()
	previous := listPlainCodeFiles
	t.Cleanup(func() { listPlainCodeFiles = previous })
	listPlainCodeFiles = func(string) (plainFileList, error) {
		return plainFileList{Paths: []string{"first.txt"}, Truncated: true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.code.searchIndex = codeSearchIndexState{status: codeSearchIndexChecking, busy: true, cancel: cancel}
	a.setWorkspaceTab(sectionCode)
	a.ensureCodeFiles()
	h.waitFor(t, "the truncated file list", func() bool { return a.code.filesLoaded })
	a.startCodeSearchIndex()
	a.invalidateCodeSearchIndex()
	h.tester.Frame()
	state := a.code.searchIndex
	if !a.code.filesTruncated || ctx.Err() == nil || state.busy || state.requested || state.rebuildScheduled || state.refreshScheduled {
		t.Fatalf("the truncated listing left indexing active: truncated=%v, state=%+v", a.code.filesTruncated, state)
	}
	if _, found := h.tester.Find("Building the code search index"); found {
		t.Fatal("the truncated listing still appears to be indexing")
	}
}

func TestCodeSearchIndexDoubleClickStopsAndRestarts(t *testing.T) {
	if !codeSearchIndexingSupported() {
		t.Skip("Zoekt is unavailable on this platform")
	}
	h := menuTestApp(t)
	a := h.app
	a.snapshot.Root = t.TempDir()
	a.pathCache = t.TempDir()
	if err := os.WriteFile(filepath.Join(a.snapshot.Root, "main.txt"), []byte("searchable content"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.code.replaceFiles([]string{"main.txt"})
	a.code.filesLoaded = true
	t.Cleanup(a.resetCodeSearchIndex)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.code.searchIndex = codeSearchIndexState{status: codeSearchIndexChecking, busy: true, cancel: cancel}
	a.setWorkspaceTab(sectionCode)
	h.tester.Frame()
	doubleClick := func(label string) {
		t.Helper()
		if err := h.tester.Click(label); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		if err := h.tester.Click(label); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
	}
	doubleClick("Building the code search index")
	if !a.code.searchIndexPaused || ctx.Err() == nil {
		t.Fatal("double-click did not stop the active index build")
	}
	a.resumeCodeSearchIndex()
	a.invalidateCodeSearchIndex()
	if state := a.code.searchIndex; state.busy || state.refreshScheduled || state.rebuildScheduled {
		t.Fatalf("background refresh restarted a paused index: %+v", state)
	}
	h.tester.Advance(time.Second)
	doubleClick("Code search indexing paused")
	if a.code.searchIndexPaused {
		t.Fatal("double-click did not resume indexing")
	}
	h.waitFor(t, "the restarted index", func() bool { return a.codeSearchIndexDirectory() != "" })
}

// Opening a local repository reads its file list in the background, a moment
// later, so that the search index starts; with Zoekt off for it, nothing starts.
func TestOpeningARepositoryStartsTheSearchIndex(t *testing.T) {
	var scheduled []func()
	previous := scheduleCodeSearchIndexOnOpen
	scheduleCodeSearchIndexOnOpen = func(_ time.Duration, run func()) { scheduled = append(scheduled, run) }
	t.Cleanup(func() { scheduleCodeSearchIndexOnOpen = previous })

	h := menuTestApp(t)
	root := t.TempDir()
	h.app.snapshot.Root = root
	h.app.code.filesLoaded, h.app.code.filesLoading = false, false
	scheduled = nil
	h.app.startCodeSearchIndexOnOpen()
	if len(scheduled) != 1 || h.app.code.filesLoading {
		t.Fatalf("the file read must wait for the delay: %d scheduled, loading %v", len(scheduled), h.app.code.filesLoading)
	}
	scheduled[0]()
	h.tester.Frame()
	h.waitFor(t, "the file list read for the index", func() bool { return h.app.code.filesLoaded || h.app.code.filesLoading })

	off := menuTestApp(t)
	off.app.snapshot.Root = root
	off.app.settings.ZoektDisabledRepositories = []string{root}
	scheduled = nil
	off.app.startCodeSearchIndexOnOpen()
	if len(scheduled) != 0 {
		t.Fatal("a repository with Zoekt turned off was prepared for indexing")
	}
}
