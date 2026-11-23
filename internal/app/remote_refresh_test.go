package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
)

func TestRemoteRefreshIgnoresHostStatAndReloadsSameStatusDiff(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "a.txt")
	if err := os.WriteFile(filename, []byte("first local version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := repository.Snapshot{Root: root, Branch: "main", Files: []repository.File{changedFile("a.txt")}}
	beforeLocal := signSnapshotForWorkspace(snapshot, false)
	beforeRemote := signSnapshotForWorkspace(snapshot, true)
	// A same-Git-status edit substantially changes the host size/mtime. The SSH
	// signature must not acquire either fact, even though this exact remote path
	// also exists and is readable on the host.
	if err := os.WriteFile(filename, []byte("a much longer second local version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterLocal := signSnapshotForWorkspace(snapshot, false)
	afterRemote := signSnapshotForWorkspace(snapshot, true)
	if beforeLocal.files == afterLocal.files {
		t.Fatal("test did not change the local size/mtime signature")
	}
	if beforeRemote != afterRemote {
		t.Fatalf("remote signature read host metadata:\n%q\n%q", beforeRemote, afterRemote)
	}

	opener := &liveOpener{snapshot: snapshot}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, root)
	a.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: root}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "a.txt", false)
	if _, ok := a.selectedChange(); !ok {
		t.Fatal("test did not select the remote change")
	}
	if signSnapshotForWorkspace(a.snapshot, true) != signSnapshotForWorkspace(opener.current(), true) {
		t.Fatal("test did not arrange equal remote signatures")
	}
	generation := a.diffGeneration
	a.requestQuietRefresh()
	if !a.refreshBusy {
		t.Fatal("same-status remote quiet refresh did not start")
	}
	drainRefresh(t, updated, 1)
	if a.diffGeneration == generation {
		t.Fatalf("equal Git status left the remote diff stale at generation %d", generation)
	}
	// Finish the replacement read so no background work outlives the test.
	for a.cancelDiff != nil {
		updated.waitForUpdate(t)
	}
	local, err := os.ReadFile(filename)
	if err != nil || string(local) != "a much longer second local version\n" {
		t.Fatalf("remote refresh altered the unrelated host file: %q %v", local, err)
	}
}

func TestQuietConflictRefreshRepaintsOnlyChangedContent(t *testing.T) {
	const original = "before\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\n"
	for _, test := range []struct {
		name          string
		content       string
		wantRepaint   bool
		wantSameState bool
	}{
		{name: "unchanged", content: original, wantSameState: true},
		{name: "changed", content: original + "after\n", wantRepaint: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			file := repository.File{
				Path: "a.txt", Status: "UU", Tracked: true, Unstaged: true,
				HasMergeConflicts: true, HasInlineMergeConflicts: true,
			}
			stages := repository.ConflictStages{
				Base:     repository.ConflictStage{Present: true, Content: "base\n"},
				Current:  repository.ConflictStage{Present: true, Content: "ours\n"},
				Incoming: repository.ConflictStage{Present: true, Content: "theirs\n"},
			}
			a := newApp(fakeOpener{})
			a.session = fakeSession{conflictStages: stages, patch: "combined patch"}
			a.snapshot = repository.Snapshot{Root: root, Branch: "main", Files: []repository.File{file}}
			a.changeRows = a.changeRowsFrom(a.snapshot.Files)
			a.selectedFile = changeRowIndex(a.changeRows, file.Path, false)
			a.diffKey = "conflict\x00" + file.Path
			a.conflictState = newConflictState(file.Path, original, 0)
			a.conflictView = &editor.State{Buffer: editor.NewBuffer(original)}
			a.conflictStages = stages
			a.conflictDiffState = &editor.DiffState{}
			a.conflictDiffPatch = "combined patch"
			state, view, review := a.conflictState, a.conflictView, a.conflictDiffState

			publications := make(chan bool, 2)
			a.update = func(fn func()) { fn(); publications <- true }
			a.updateChanged = func(fn func() bool) { publications <- fn() }
			repaintAtRequest := a.refreshRemoteQuietDetails()
			var repaintAtCompletion bool
			select {
			case repaintAtCompletion = <-publications:
			case <-time.After(2 * time.Second):
				t.Fatal("quiet conflict read did not publish")
			}
			repainted := repaintAtRequest || repaintAtCompletion
			if repainted != test.wantRepaint {
				t.Fatalf("conflict refresh repaint = %v, want %v (request=%v completion=%v)",
					repainted, test.wantRepaint, repaintAtRequest, repaintAtCompletion)
			}
			sameState := a.conflictState == state && a.conflictView == view && a.conflictDiffState == review
			if sameState != test.wantSameState {
				t.Fatalf("conflict view preservation = %v, want %v", sameState, test.wantSameState)
			}
			if a.diffLoading || a.cancelDiff != nil {
				t.Fatal("completed conflict refresh left a loading operation behind")
			}
		})
	}
}

// Resolving a hunk changes Git's combined diff but not the conflict's
// identity. A quiet refresh must accept that new patch without replacing the
// state that owns the original markers and undo stack.
func TestQuietConflictRefreshPreservesUndoHistory(t *testing.T) {
	original := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\nc\n<<<<<<< HEAD\n5\n=======\n6\n>>>>>>> t\n"
	state := newConflictState("a.txt", original, 0)
	state.resolve(conflictCurrent)
	state.resolve(conflictCurrent)
	if len(state.regions) != 1 || len(state.undo) != 2 {
		t.Fatalf("test setup has %d regions and %d undo entries, want 1 and 2", len(state.regions), len(state.undo))
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(state.text()), 0o644); err != nil {
		t.Fatal(err)
	}
	file := repository.File{Path: "a.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true, HasInlineMergeConflicts: true}
	stages := repository.ConflictStages{
		Base:     repository.ConflictStage{Present: true, Content: "base\n"},
		Current:  repository.ConflictStage{Present: true, Content: "current\n"},
		Incoming: repository.ConflictStage{Present: true, Content: "incoming\n"},
	}
	a := newApp(fakeOpener{})
	a.session = fakeSession{conflictStages: stages, patch: "updated combined patch"}
	a.snapshot = repository.Snapshot{Root: root, Branch: "main", Files: []repository.File{file}}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, file.Path, false)
	a.diffKey = "conflict\x00" + file.Path
	a.conflictState = state
	a.conflictView = &editor.State{Buffer: editor.NewBuffer(state.text())}
	a.conflictStages = stages
	a.conflictDiffState = &editor.DiffState{}
	a.conflictDiffPatch = "old combined patch"
	view, review := a.conflictView, a.conflictDiffState
	originalLines := slices.Clone(state.originalLines)
	undoLen := len(state.undo)

	publications := make(chan bool, 2)
	a.update = func(fn func()) { fn(); publications <- true }
	a.updateChanged = func(fn func() bool) { publications <- fn() }
	a.refreshRemoteQuietDetails()
	select {
	case <-publications:
	case <-time.After(2 * time.Second):
		t.Fatal("quiet conflict read did not publish")
	}
	if a.conflictState != state || a.conflictView != view || a.conflictDiffState != review {
		t.Fatal("quiet patch refresh replaced the conflict state or its viewers")
	}
	if len(state.undo) != undoLen || !slices.Equal(state.originalLines, originalLines) {
		t.Fatalf("quiet patch refresh lost history: undo=%d/%d original=%v", len(state.undo), undoLen, slices.Equal(state.originalLines, originalLines))
	}
	if a.conflictDiffPatch != "updated combined patch" {
		t.Fatalf("quiet refresh kept stale patch %q", a.conflictDiffPatch)
	}

	a.undoConflictResolution()
	if len(state.regions) != 2 {
		t.Fatalf("Undo after refresh has %d conflicts, want 2", len(state.regions))
	}
	if got := state.text(); got != original {
		// This is not the expected final text: two undos should be possible.
		a.undoConflictResolution()
		if got = state.text(); got != original {
			t.Fatalf("two Undo operations after refresh produced %q, want the original three conflicts", got)
		}
	}
}

func TestRemoteQuietRefreshDoesNotRepaintUnchangedDiff(t *testing.T) {
	const (
		firstPatch  = "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-first\n+second\n"
		secondPatch = "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-first\n+third\n"
	)
	root := t.TempDir()
	snapshot := repository.Snapshot{Root: root, Branch: "main", Files: []repository.File{changedFile("a.txt")}}
	opener := &liveOpener{snapshot: snapshot}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, root)
	a.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: root}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "a.txt", false)
	session := &controlledRemoteDiffSession{Session: a.session, response: repository.Diff{Kind: "Unstaged", Patch: firstPatch}}
	a.session = session
	a.updateChanged = func(fn func() bool) {
		updated.update(func() { s.updateAppChanged(a, fn) })
	}
	invalidations := 0
	s.invalidate = func() { invalidations++ }
	a.beginDiff()
	for a.cancelDiff != nil {
		updated.waitForUpdate(t)
	}
	if a.diffState == nil || a.diffPatch != firstPatch {
		t.Fatal("test did not load the initial diff")
	}

	for _, test := range []struct {
		name              string
		response          repository.Diff
		wantInvalidations int
		wantNewDiff       bool
	}{
		{name: "unchanged", response: repository.Diff{Kind: "Unstaged", Patch: firstPatch}},
		{name: "changed", response: repository.Diff{Kind: "Unstaged", Patch: secondPatch}, wantInvalidations: 1, wantNewDiff: true},
		{name: "patchless", response: repository.Diff{Kind: "Untracked", NewText: "first"}, wantInvalidations: 1, wantNewDiff: true},
		{name: "unchanged patchless", response: repository.Diff{Kind: "Untracked", NewText: "first"}},
		{name: "changed patchless", response: repository.Diff{Kind: "Untracked", NewText: "second"}, wantInvalidations: 1, wantNewDiff: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			displayed := a.diffState
			started, release := session.respond(test.response)
			invalidations = 0
			a.requestQuietRefresh()
			drainRefresh(t, updated, 1)
			<-started
			preserved := a.diffState == displayed && !a.diffLoading
			duringInvalidations := invalidations
			close(release)
			updated.waitForUpdate(t)
			if !preserved {
				t.Fatal("the quiet refresh cleared the visible diff or showed a loading state")
			}
			if duringInvalidations != 0 {
				t.Fatalf("starting the quiet read caused %d invalidations, want 0", duringInvalidations)
			}
			if invalidations != test.wantInvalidations {
				t.Fatalf("the completed quiet read caused %d invalidations, want %d", invalidations, test.wantInvalidations)
			}
			if (a.diffState != displayed) != test.wantNewDiff {
				t.Fatalf("diff replacement = %v, want %v", a.diffState != displayed, test.wantNewDiff)
			}
			if a.diffPatch != test.response.Patch {
				t.Fatalf("the displayed patch is %q, want %q", a.diffPatch, test.response.Patch)
			}
			if test.response.Patch == "" && (a.diffState.Old.String() != test.response.OldText || a.diffState.New.String() != test.response.NewText) {
				t.Fatalf("the displayed patchless texts are %q and %q, want %q and %q",
					a.diffState.Old.String(), a.diffState.New.String(), test.response.OldText, test.response.NewText)
			}
		})
	}
}

type controlledRemoteDiffSession struct {
	repository.Session
	mu       sync.Mutex
	response repository.Diff
	started  chan struct{}
	release  chan struct{}
}

func (s *controlledRemoteDiffSession) respond(response repository.Diff) (<-chan struct{}, chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.response = response
	s.started = make(chan struct{})
	s.release = make(chan struct{})
	return s.started, s.release
}

func (s *controlledRemoteDiffSession) Diff(context.Context, repository.File, repository.DiffOptions) (repository.Diff, error) {
	s.mu.Lock()
	response, started, release := s.response, s.started, s.release
	s.mu.Unlock()
	if started != nil {
		close(started)
		<-release
	}
	return response, nil
}
