package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"lazygitapp/internal/repository"
)

// cancellationDiffSession models a repository read that observes cancellation.
// release remains available to cleanup every call when an assertion fails on an
// implementation that never cancels obsolete work.
type cancellationDiffSession struct {
	fakeSession
	started  chan string
	canceled chan string
	release  chan struct{}
}

func newCancellationDiffSession() *cancellationDiffSession {
	return &cancellationDiffSession{
		started:  make(chan string, 8),
		canceled: make(chan string, 8),
		release:  make(chan struct{}),
	}
}

func (s *cancellationDiffSession) wait(ctx context.Context, operation string) (repository.Diff, error) {
	s.started <- operation
	select {
	case <-ctx.Done():
		s.canceled <- operation
		return repository.Diff{}, ctx.Err()
	case <-s.release:
		return repository.Diff{NewText: operation + "\n", Kind: operation}, nil
	}
}

func (s *cancellationDiffSession) Diff(ctx context.Context, file repository.File, _ repository.DiffOptions) (repository.Diff, error) {
	return s.wait(ctx, "worktree:"+file.Path)
}

func (s *cancellationDiffSession) CommitFileDiff(ctx context.Context, _ repository.Commit, file repository.CommitFile, _ repository.DiffOptions) (repository.Diff, error) {
	return s.wait(ctx, "commit:"+file.Path)
}

func queuePerformancePublications(a *app) (*updates, <-chan bool) {
	updates := newUpdates()
	repainted := make(chan bool, 8)
	a.update = updates.update
	a.updateChanged = func(fn func() bool) {
		updates.update(func() { repainted <- fn() })
	}
	return updates, repainted
}

func awaitPerformancePublication(t *testing.T, updates *updates, repainted <-chan bool) bool {
	t.Helper()
	updates.waitForUpdate(t)
	return <-repainted
}

func expectDiffCancellation(t *testing.T, canceled <-chan string, want string) {
	t.Helper()
	select {
	case got := <-canceled:
		if got != want {
			t.Fatalf("canceled %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("%q was not canceled", want)
	}
}

func TestCommitScrollCancelsObsoleteDiffWithoutRepaint(t *testing.T) {
	session := newCancellationDiffSession()
	defer func() {
		select {
		case <-session.release:
		default:
			close(session.release)
		}
	}()
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionHistory
	updates, repainted := queuePerformancePublications(a)
	commit := repository.Commit{Hash: "commit"}

	a.requestCommitFileDiff(commit, repository.CommitFile{Path: "old.go"})
	if got := <-session.started; got != "commit:old.go" {
		t.Fatalf("first read = %q", got)
	}
	// Moving between file rows is what rapid keyboard/list scrolling does.
	a.requestCommitFileDiff(commit, repository.CommitFile{Path: "new.go"})
	if got := <-session.started; got != "commit:new.go" {
		t.Fatalf("replacement read = %q", got)
	}
	expectDiffCancellation(t, session.canceled, "commit:old.go")
	if awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("obsolete canceled commit diff repainted the visible list")
	}
	if a.commitDiffPath != "new.go" || !a.commitDiffLoading {
		t.Fatalf("cancellation damaged the replacement: path=%q loading=%v", a.commitDiffPath, a.commitDiffLoading)
	}

	close(session.release)
	if !awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("current diff did not repaint History after publication")
	}
	if a.commitDiffLoading || a.commitDiffState == nil || a.commitDiffState.New.String() != "commit:new.go\n" {
		t.Fatalf("replacement was not retained: loading=%v state=%#v", a.commitDiffLoading, a.commitDiffState)
	}
}

func TestWorktreeDiffReplacementCancelsObsoleteRead(t *testing.T) {
	session := newCancellationDiffSession()
	defer func() {
		select {
		case <-session.release:
		default:
			close(session.release)
		}
	}()
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionChanges
	a.snapshot.Files = []repository.File{changedFile("old.txt"), changedFile("new.txt")}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "old.txt", false)
	updates, repainted := queuePerformancePublications(a)

	a.requestFileDiff()
	if got := <-session.started; got != "worktree:old.txt" {
		t.Fatalf("first read = %q", got)
	}
	a.selectedFile = changeRowIndex(a.changeRows, "new.txt", false)
	a.requestFileDiff()
	if got := <-session.started; got != "worktree:new.txt" {
		t.Fatalf("replacement read = %q", got)
	}
	expectDiffCancellation(t, session.canceled, "worktree:old.txt")
	if awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("obsolete worktree diff repainted Changes")
	}

	close(session.release)
	if !awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("replacement worktree diff did not publish")
	}
	if a.diffState == nil || a.diffState.New.String() != "worktree:new.txt\n" {
		t.Fatalf("replacement worktree diff was not installed: %#v", a.diffState)
	}
}

type abaRead struct {
	kind string
	call int
	hash string
	path string
}

type abaCommitDetailSession struct {
	fakeSession
	started chan abaRead
	mu      sync.Mutex
	next    map[string]int
	files   map[int]chan []repository.CommitFile
	diffs   map[int]chan repository.Diff
}

func newABACommitDetailSession() *abaCommitDetailSession {
	return &abaCommitDetailSession{
		started: make(chan abaRead, 16), next: make(map[string]int),
		files: make(map[int]chan []repository.CommitFile), diffs: make(map[int]chan repository.Diff),
	}
}

func (s *abaCommitDetailSession) begin(kind, hash, path string) abaRead {
	s.mu.Lock()
	s.next[kind]++
	read := abaRead{kind: kind, call: s.next[kind], hash: hash, path: path}
	if kind == "files" {
		s.files[read.call] = make(chan []repository.CommitFile, 1)
	} else {
		s.diffs[read.call] = make(chan repository.Diff, 1)
	}
	s.mu.Unlock()
	s.started <- read
	return read
}

func (s *abaCommitDetailSession) finishCleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.files {
		select {
		case ch <- nil:
		default:
		}
	}
	for _, ch := range s.diffs {
		select {
		case ch <- repository.Diff{}:
		default:
		}
	}
}

func (s *abaCommitDetailSession) fileResults(call int) chan []repository.CommitFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[call]
}

func (s *abaCommitDetailSession) diffResults(call int) chan repository.Diff {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diffs[call]
}

// CommitFiles deliberately ignores cancellation: token/publication behavior
// must remain safe even after a real process has already completed its read.
func (s *abaCommitDetailSession) CommitFiles(_ context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	read := s.begin("files", commit.Hash, "")
	return <-s.fileResults(read.call), nil
}

func (s *abaCommitDetailSession) CommitFileDiff(_ context.Context, commit repository.Commit, file repository.CommitFile, _ repository.DiffOptions) (repository.Diff, error) {
	read := s.begin("diff", commit.Hash, file.Path)
	return <-s.diffResults(read.call), nil
}

func nextABARead(t *testing.T, session *abaCommitDetailSession, kind string, call int) abaRead {
	t.Helper()
	select {
	case read := <-session.started:
		if read.kind != kind || read.call != call {
			t.Fatalf("read = %#v, want kind=%q call=%d", read, kind, call)
		}
		return read
	case <-time.After(time.Second):
		t.Fatalf("no %s read %d started", kind, call)
		return abaRead{}
	}
}

func TestCommitFilesABACompletionDoesNotRepaintOrReplaceCurrentRead(t *testing.T) {
	session := newABACommitDetailSession()
	defer session.finishCleanup()
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionHistory
	updates, repainted := queuePerformancePublications(a)

	a.requestCommitDetails(repository.Commit{Hash: "A"})
	nextABARead(t, session, "files", 1)
	a.requestCommitDetails(repository.Commit{Hash: "B"})
	nextABARead(t, session, "files", 2)
	a.requestCommitDetails(repository.Commit{Hash: "A"})
	nextABARead(t, session, "files", 3)

	session.fileResults(1) <- []repository.CommitFile{{Path: "stale-A.txt"}}
	if awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("stale A result repainted after A-B-A selection")
	}
	if len(a.commitFiles) != 0 || !a.commitFilesLoading {
		t.Fatalf("stale A result replaced the current read: files=%#v loading=%v", a.commitFiles, a.commitFilesLoading)
	}
	session.fileResults(3) <- []repository.CommitFile{{Path: "current-A.txt"}}
	if !awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("current A result did not publish")
	}
	if len(a.commitFiles) != 1 || a.commitFiles[0].Path != "current-A.txt" {
		t.Fatalf("current A files = %#v", a.commitFiles)
	}
}

func TestCommitDiffABACompletionDoesNotRepaintOrReplaceCurrentRead(t *testing.T) {
	session := newABACommitDetailSession()
	defer session.finishCleanup()
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionHistory
	updates, repainted := queuePerformancePublications(a)
	file := repository.CommitFile{Path: "same.go"}
	readA := repository.Commit{Hash: "A", Parents: []string{"parent-A"}}
	readB := repository.Commit{Hash: "B", Parents: []string{"parent-B"}}

	a.requestCommitFileDiff(readA, file)
	nextABARead(t, session, "diff", 1)
	a.requestCommitFileDiff(readB, file)
	nextABARead(t, session, "diff", 2)
	a.requestCommitFileDiff(readA, file)
	nextABARead(t, session, "diff", 3)

	session.diffResults(1) <- repository.Diff{NewText: "stale A\n", Kind: "Commit"}
	if awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("stale A diff repainted after A-B-A selection")
	}
	if a.commitDiffState != nil || !a.commitDiffLoading {
		t.Fatalf("stale A diff replaced the current state: state=%p loading=%v", a.commitDiffState, a.commitDiffLoading)
	}
	session.diffResults(3) <- repository.Diff{NewText: "current A\n", Kind: "Commit"}
	if !awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("current A diff did not publish")
	}
	if a.commitDiffState == nil || a.commitDiffState.New.String() != "current A\n" {
		t.Fatalf("current A diff = %#v", a.commitDiffState)
	}
}

func TestQuietFileDiffSamePatchKeepsStateAndBytesWithoutRepaint(t *testing.T) {
	const patch = "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-first\n+second\n"
	session := &controlledRemoteDiffSession{Session: fakeSession{}, response: repository.Diff{Kind: "Unstaged", Patch: patch}}
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionChanges
	a.snapshot.Files = []repository.File{changedFile("a.txt")}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "a.txt", false)
	updates, repainted := queuePerformancePublications(a)

	a.requestFileDiff()
	if !awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("initial worktree diff did not publish")
	}
	state := a.diffState
	if state == nil || a.diffPatch != patch {
		t.Fatalf("test could not load initial patch %q", a.diffPatch)
	}
	started, release := session.respond(repository.Diff{Kind: "Unstaged", Patch: patch})
	a.requestQuietFileDiff()
	<-started
	close(release)
	if awaitPerformancePublication(t, updates, repainted) {
		t.Fatal("identical quiet patch requested a repaint")
	}
	if a.diffState != state || a.diffPatch != patch || a.diffKind != "Unstaged" || a.diffLoading {
		t.Fatalf("identical quiet patch changed displayed data: state=%p/%p bytes=%d loading=%v",
			a.diffState, state, len(a.diffPatch), a.diffLoading)
	}
	if got := a.diffState.Hunks(); len(got) != 1 || got[0].NewStart != 0 || got[0].NewEnd != 1 {
		t.Fatalf("quiet refresh changed diff state: %#v", got)
	}
}
