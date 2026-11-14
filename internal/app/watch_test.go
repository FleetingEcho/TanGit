package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lazygitapp/internal/repository"
)

// watchingLiveOpener adds filesystem notifications to the mutable repository
// used by the quiet-refresh tests without requiring OS events in every UI test.
type watchingLiveOpener struct {
	*liveOpener
	events    chan struct{}
	cancelled chan struct{}
}

func (o watchingLiveOpener) Open(ctx context.Context, path string) (repository.Session, error) {
	session, err := o.liveOpener.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	return watchingLiveSession{
		liveSession: session.(liveSession), events: o.events, cancelled: o.cancelled,
	}, nil
}

type watchingLiveSession struct {
	liveSession
	events    chan struct{}
	cancelled chan struct{}
}

func (s watchingLiveSession) Watch(ctx context.Context) (<-chan struct{}, error) {
	go func() {
		<-ctx.Done()
		close(s.cancelled)
	}()
	return s.events, nil
}

// An event on the visible repository bypasses the timer. An event on a hidden
// page does no Git work in the background; switching to that page still reads
// it immediately, as before filesystem watching was added.
func TestRepositoryEventsRefreshOnlyTheVisibleFocusedPage(t *testing.T) {
	opener := watchingLiveOpener{
		liveOpener: &liveOpener{snapshot: repository.Snapshot{
			Root: "/work/repo", Branch: "main", Files: []repository.File{changedFile("a.txt")},
		}},
		events: make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	repositoryTab := s.activeTab()
	t.Cleanup(a.shutdown)

	opener.set(repository.Snapshot{
		Root: "/work/repo", Branch: "main",
		Files: []repository.File{changedFile("a.txt"), changedFile("external.txt")},
	})
	opener.events <- struct{}{}
	for a.snapshot.ChangeCount() != 2 {
		updated.waitForUpdate(t)
	}
	for a.refreshBusy || a.refreshPending {
		updated.waitForUpdate(t)
	}

	home := s.newTab("")
	s.tabs = append(s.tabs, home)
	s.active = len(s.tabs) - 1
	opener.set(repository.Snapshot{
		Root: "/work/repo", Branch: "main", Files: []repository.File{changedFile("hidden.txt")},
	})
	opener.events <- struct{}{}
	drainRefresh(t, updated, 1)
	if len(a.snapshot.Files) != 2 {
		t.Fatalf("a background page read its repository: %+v", a.snapshot.Files)
	}

	s.activate(s.indexOf(repositoryTab))
	for len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "hidden.txt" {
		updated.waitForUpdate(t)
	}
	for a.refreshBusy || a.refreshPending {
		updated.waitForUpdate(t)
	}

	s.handleWindowBlurred()
	opener.events <- struct{}{}
	drainRefresh(t, updated, 1)
	if a.refreshBusy {
		t.Fatal("a blurred window started a filesystem-triggered read")
	}
}

func TestLinkedWorktreeCodeAndCommitChangesRefreshTheVisiblePage(t *testing.T) {
	parent := t.TempDir()
	main := filepath.Join(parent, "main")
	if err := os.Mkdir(main, 0o700); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, main, "init", "--initial-branch=main")
	runGitIn(t, main, "config", "user.name", "Worktree Watch Test")
	runGitIn(t, main, "config", "user.email", "worktree-watch@example.com")
	code := "package example\n\nconst Value = 1\n"
	if err := os.WriteFile(filepath.Join(main, "code.go"), []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, main, "add", "code.go")
	runGitIn(t, main, "commit", "-m", "initial")
	linked := filepath.Join(parent, "feature")
	runGitIn(t, main, "worktree", "add", "-b", "feature", linked)

	s, _, updated := newLiveShell(t, repository.Opener{})
	a := openLiveRepo(t, s, updated, linked)
	t.Cleanup(s.shutdown)
	for a.repositoryDetailsLoading {
		updated.waitForUpdate(t)
	}
	if !a.openedLinkedWorktree() {
		t.Fatalf("opened %s without recognizing its linked-worktree identity: %+v", linked, a.snapshot.Worktrees)
	}

	if err := os.WriteFile(filepath.Join(linked, "code.go"), []byte(code+"const ExternalEdit = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "code.go" {
		updated.waitForUpdate(t)
	}
	oldHead := a.snapshot.HeadHash
	runGitIn(t, linked, "add", "code.go")
	runGitIn(t, linked, "commit", "-m", "external worktree update")
	for a.snapshot.HeadHash == oldHead || len(a.snapshot.Files) != 0 {
		updated.waitForUpdate(t)
	}
}

// A filesystem notification only schedules a Git read; it must not repaint
// before that read has found something visible to change.
func TestRepositoryEventDoesNotRepaintBeforeRefresh(t *testing.T) {
	opener := watchingLiveOpener{
		liveOpener: &liveOpener{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"}},
		events:     make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	t.Cleanup(a.shutdown)

	notified := false
	a.externalRefresh = func() { notified = true }
	repaint := true
	a.updateChanged = func(fn func() bool) {
		a.update(func() { repaint = fn() })
	}
	opener.events <- struct{}{}
	drainRefresh(t, updated, 1)
	if !notified {
		t.Fatal("the filesystem event did not schedule a refresh")
	}
	if repaint {
		t.Fatal("the filesystem event repainted before its refresh completed")
	}
}

type blockingWatchOpener struct {
	*liveOpener
	started   chan struct{}
	cancelled chan struct{}
}

func (o blockingWatchOpener) Open(ctx context.Context, path string) (repository.Session, error) {
	session, err := o.liveOpener.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	return blockingWatchSession{
		liveSession: session.(liveSession), started: o.started, cancelled: o.cancelled,
	}, nil
}

type blockingWatchSession struct {
	liveSession
	started   chan struct{}
	cancelled chan struct{}
}

func (s blockingWatchSession) Watch(ctx context.Context) (<-chan struct{}, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	return nil, ctx.Err()
}

func TestShutdownCancelsRepositoryWatch(t *testing.T) {
	opener := watchingLiveOpener{
		liveOpener: &liveOpener{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"}},
		events:     make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	a.shutdown()
	select {
	case <-opener.cancelled:
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown left the repository watcher running")
	}
}

// Installing watchers is background work too: closing a page while the
// initial directory tree is being prepared cancels it instead of trapping the
// UI update that installed the repository.
func TestShutdownCancelsRepositoryWatchDuringSetup(t *testing.T) {
	opener := blockingWatchOpener{
		liveOpener: &liveOpener{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"}},
		started:    make(chan struct{}),
		cancelled:  make(chan struct{}),
	}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	select {
	case <-opener.started:
	case <-time.After(4 * time.Second):
		t.Fatal("repository watch setup did not start")
	}
	a.shutdown()
	select {
	case <-opener.cancelled:
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown did not interrupt repository watch setup")
	}
}
