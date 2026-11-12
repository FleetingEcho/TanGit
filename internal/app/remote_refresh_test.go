package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
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
