package app

import (
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
