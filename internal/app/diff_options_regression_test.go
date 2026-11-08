package app

import (
	"context"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type recordedDiffRead struct {
	path    string
	options repository.DiffOptions
}

type diffOptionsRecordingSession struct {
	fakeSession
	mu    sync.Mutex
	reads []recordedDiffRead
}

func (s *diffOptionsRecordingSession) Diff(ctx context.Context, file repository.File, options repository.DiffOptions) (repository.Diff, error) {
	s.mu.Lock()
	s.reads = append(s.reads, recordedDiffRead{path: file.Path, options: options})
	s.mu.Unlock()
	return s.fakeSession.Diff(ctx, file, options)
}

func (s *diffOptionsRecordingSession) diffReads() []recordedDiffRead {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedDiffRead(nil), s.reads...)
}

func diffOptionsHarness(t *testing.T, session *diffOptionsRecordingSession, configure ...func(*app)) *harness {
	t.Helper()
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	for _, apply := range configure {
		apply(a)
	}
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = session.snapshot.Root
	openFixture(t, h.tester)
	h.settle(t)
	return h
}

func TestOrdinaryDiffOptionsReloadPatchesAndGateLineStaging(t *testing.T) {
	session := &diffOptionsRecordingSession{fakeSession: fakeSession{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files:  unstagedTrackedFile(),
	}}}
	h := diffOptionsHarness(t, session)
	h.waitFor(t, "the initial canonical diff", func() bool {
		return h.app.diffState != nil && h.app.diffPatch != ""
	})
	if !h.app.diffState.LineSelection || !h.app.lineSelectionAvailable() {
		t.Fatal("the initial canonical working-tree diff does not allow line staging")
	}

	reload := func(name string, want repository.DiffOptions, change func()) {
		t.Helper()
		previous := h.app.diffState
		readsBefore := len(session.diffReads())
		h.app.lineSelectMode = true
		change()
		if h.app.lineSelectMode {
			t.Errorf("%s left line-selection mode active while its patch was stale", name)
		}
		h.waitFor(t, name, func() bool {
			reads := session.diffReads()
			return len(reads) > readsBefore && h.app.diffState != nil && h.app.diffState != previous && !h.app.diffLoading
		})
		reads := session.diffReads()
		if got := reads[len(reads)-1]; got.path != "changed.txt" || got.options != want {
			t.Fatalf("%s read = %#v, want changed.txt with options %#v", name, got, want)
		}
		if h.app.diffState.ContextLines != want.ContextLines {
			t.Errorf("%s viewer context = %d, want %d", name, h.app.diffState.ContextLines, want.ContextLines)
		}
		wantLineSelection := canonicalDiffOptions(want)
		if h.app.diffState.LineSelection != wantLineSelection || h.app.lineSelectionAvailable() != wantLineSelection {
			t.Errorf("%s line staging state/availability = %v/%v, want %v",
				name, h.app.diffState.LineSelection, h.app.lineSelectionAvailable(), wantLineSelection)
		}
	}

	reload("ignore-whitespace reload", repository.DiffOptions{IgnoreWhitespace: true, ContextLines: 3},
		h.app.toggleDiffWhitespace)
	reload("canonical-whitespace reload", repository.DiffOptions{ContextLines: 3},
		h.app.toggleDiffWhitespace)
	reload("noncanonical-context reload", repository.DiffOptions{ContextLines: 5},
		h.app.cycleDiffContext)

	// Follow the real cycle through 10, 0, and 1 back to the canonical three.
	for _, wantContext := range []int{10, 0, 1, 3} {
		want := repository.DiffOptions{ContextLines: wantContext}
		reload("context-cycle reload", want, h.app.cycleDiffContext)
	}
}

func TestConflictReadAlwaysUsesCanonicalDiffOptions(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) {
		return []byte("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n"), nil
	}

	session := &diffOptionsRecordingSession{fakeSession: fakeSession{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files: []repository.File{{
			Path: "conflict.txt", Status: "UU", Tracked: true, Unstaged: true,
			HasMergeConflicts: true, HasInlineMergeConflicts: true,
		}},
	}}}
	noncanonical := repository.DiffOptions{IgnoreWhitespace: true, ContextLines: 10}
	h := diffOptionsHarness(t, session, func(a *app) { a.diffOptions = noncanonical })
	h.waitFor(t, "the conflict and its combined diff", func() bool {
		return h.app.conflictState != nil && h.app.conflictDiffPatch != "" && !h.app.diffLoading
	})

	reads := session.diffReads()
	if len(reads) != 1 {
		t.Fatalf("conflict diff reads = %#v, want exactly one", reads)
	}
	canonical := repository.DiffOptions{ContextLines: 3}
	if reads[0].path != "conflict.txt" || reads[0].options != canonical {
		t.Fatalf("conflict read = %#v, want canonical options %#v", reads[0], canonical)
	}
	if h.app.diffOptions != noncanonical {
		t.Fatalf("a conflict read rewrote the ordinary diff options: %#v", h.app.diffOptions)
	}
}
