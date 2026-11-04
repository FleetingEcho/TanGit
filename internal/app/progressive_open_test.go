package app

import (
	"context"
	"errors"
	"testing"

	"lazygitapp/internal/repository"
)

type fixtureOpener struct{ session repository.Session }

func (o fixtureOpener) Open(context.Context, string) (repository.Session, error) {
	return o.session, nil
}
func (fixtureOpener) Init(context.Context, string) error { return nil }

type blockedProgressiveSession struct {
	fakeSession
	core           repository.Snapshot
	details        repository.Snapshot
	detailsErr     error
	detailsStart   chan struct{}
	detailsRelease chan struct{}
}

func (s *blockedProgressiveSession) CoreSnapshot(context.Context) (repository.Snapshot, error) {
	return s.core, nil
}

func (s *blockedProgressiveSession) DetailsSnapshot(context.Context) (repository.Snapshot, error) {
	close(s.detailsStart)
	<-s.detailsRelease
	return s.details, s.detailsErr
}

func progressiveOpenFixture(t *testing.T, detailsErr error) (*app, *updates, *blockedProgressiveSession) {
	t.Helper()
	session := &blockedProgressiveSession{
		core: repository.Snapshot{
			Root:   "/work/progressive",
			Branch: "main",
			Files:  []repository.File{{Path: "core.txt", Status: " M", Unstaged: true, Tracked: true}},
		},
		details: repository.Snapshot{
			Commits:  []repository.Commit{{Hash: "detail-commit", Subject: "Deferred history"}},
			Branches: []repository.Branch{{Name: "main", DisplayName: "main", Head: true}},
			Tags:     []repository.Tag{{Name: "v1.0.0"}},
		},
		detailsErr:     detailsErr,
		detailsStart:   make(chan struct{}),
		detailsRelease: make(chan struct{}),
	}
	session.fakeSession.snapshot = session.core.WithDetails(session.details)
	a := newApp(fixtureOpener{session: session})
	a.path = session.core.Root
	updated := newUpdates()
	a.update = updated.update
	a.beginOpen()
	updated.waitForUpdate(t)
	<-session.detailsStart
	return a, updated, session
}

func TestProgressiveOpenShowsCoreBeforeDetailsFinish(t *testing.T) {
	a, updated, session := progressiveOpenFixture(t, nil)
	if a.state != stateReady || !a.repositoryDetailsLoading {
		t.Fatalf("core did not become ready: state=%v loading=%v", a.state, a.repositoryDetailsLoading)
	}
	if len(a.snapshot.Commits) != 0 || len(a.snapshot.Files) != 1 {
		t.Fatalf("unexpected core snapshot: %+v", a.snapshot)
	}

	// Details attach to the UI's live core, not to a copy captured before the
	// deferred read. A working-tree change made meanwhile survives the merge.
	a.snapshot.Files = []repository.File{{Path: "live.txt", Status: "??", Unstaged: true, Added: true}}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "live.txt", false)
	close(session.detailsRelease)
	updated.waitForUpdate(t)

	if a.repositoryDetailsLoading || len(a.snapshot.Commits) != 1 || len(a.snapshot.Tags) != 1 {
		t.Fatalf("details were not installed: %+v", a.snapshot)
	}
	if len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "live.txt" {
		t.Fatalf("deferred details overwrote the live core: %+v", a.snapshot.Files)
	}
	selected, ok := a.selectedChange()
	if !ok || selected.file.Path != "live.txt" {
		t.Fatalf("the working-tree selection was not preserved: %+v ok=%v", selected, ok)
	}
}

func TestProgressiveOpenRejectsDetailsSupersededByAnAction(t *testing.T) {
	a, updated, session := progressiveOpenFixture(t, nil)
	// An action's complete snapshot supersedes the deferred read even before
	// its own completion handler clears the loading flag.
	a.snapshot = repository.Snapshot{
		Root:    "/work/progressive",
		Commits: []repository.Commit{{Hash: "newer-action", Subject: "Current"}},
	}
	a.snapshotSeq++
	close(session.detailsRelease)
	updated.waitForUpdate(t)

	if a.repositoryDetailsLoading {
		t.Fatal("stale deferred work left the repository loading")
	}
	if len(a.snapshot.Commits) != 1 || a.snapshot.Commits[0].Hash != "newer-action" {
		t.Fatalf("stale details replaced the action snapshot: %+v", a.snapshot)
	}
	if len(a.snapshot.Tags) != 0 {
		t.Fatalf("stale details were partially applied: %+v", a.snapshot.Tags)
	}
}

// A filesystem event during the deferred read is not discarded with the
// quiet-refresh guard. The details finish first, then one full read picks up
// the external working-tree change without replacing the newer details.
func TestProgressiveOpenQueuesAnExternalChangeDuringDetails(t *testing.T) {
	a, updated, session := progressiveOpenFixture(t, nil)
	session.fakeSession.snapshot.Files = []repository.File{changedFile("external.txt")}
	a.requestQuietRefresh()
	if !a.refreshPending {
		t.Fatal("an external change during deferred details was discarded")
	}
	close(session.detailsRelease)
	for a.repositoryDetailsLoading || a.refreshBusy || a.refreshPending {
		updated.waitForUpdate(t)
	}
	if len(a.snapshot.Commits) != 1 || a.snapshot.Commits[0].Hash != "detail-commit" {
		t.Fatalf("the follow-up refresh lost the deferred history: %+v", a.snapshot.Commits)
	}
	if len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "external.txt" {
		t.Fatalf("the follow-up refresh missed the external file: %+v", a.snapshot.Files)
	}
}

func TestProgressiveOpenDetailsFailureKeepsCoreUsable(t *testing.T) {
	a, updated, session := progressiveOpenFixture(t, errors.New("tags unavailable"))
	close(session.detailsRelease)
	updated.waitForUpdate(t)
	if a.state != stateReady || a.repositoryDetailsLoading || len(a.snapshot.Files) != 1 {
		t.Fatalf("a details failure discarded the core: state=%v loading=%v snapshot=%+v",
			a.state, a.repositoryDetailsLoading, a.snapshot)
	}
	if a.actionError == "" {
		t.Fatal("a details failure was silently presented as empty repository data")
	}
}
