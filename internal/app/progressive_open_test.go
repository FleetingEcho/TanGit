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

type closeTrackingSession struct {
	fakeSession
	closeCalls int
}

func (s *closeTrackingSession) Close() error {
	s.closeCalls++
	return nil
}

func TestShutdownClosesAnInstalledSessionOnlyOnce(t *testing.T) {
	session := &closeTrackingSession{fakeSession: fakeSession{snapshot: repository.Snapshot{
		Root: "/work/closable", Branch: "main",
	}}}
	a := newApp(fixtureOpener{session: session})
	a.path = session.snapshot.Root
	updated := newUpdates()
	a.update = updated.update
	a.beginOpen()
	updated.waitForUpdate(t)
	if a.session != session || a.state != stateReady {
		t.Fatalf("closable session was not installed: state=%v", a.state)
	}

	a.shutdown()
	a.shutdown()
	if session.closeCalls != 1 {
		t.Fatalf("Close called %d times, want once", session.closeCalls)
	}
}

type blockedCoreCloseSession struct {
	closeTrackingSession
	started chan struct{}
	release chan struct{}
}

func (s *blockedCoreCloseSession) CoreSnapshot(context.Context) (repository.Snapshot, error) {
	close(s.started)
	<-s.release
	return s.snapshot, nil
}

func (s *blockedCoreCloseSession) DetailsSnapshot(context.Context) (repository.Snapshot, error) {
	return repository.Snapshot{}, nil
}

func TestOpenClosesASessionWhoseCoreArrivesAfterShutdown(t *testing.T) {
	session := &blockedCoreCloseSession{
		closeTrackingSession: closeTrackingSession{fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root: "/work/late", Branch: "main",
		}}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	a := newApp(fixtureOpener{session: session})
	a.path = session.snapshot.Root
	updated := newUpdates()
	a.update = updated.update
	a.beginOpen()
	<-session.started
	a.shutdown()
	close(session.release)
	updated.waitForUpdate(t)

	if session.closeCalls != 1 {
		t.Fatalf("late core session Close called %d times, want once", session.closeCalls)
	}
	if a.session != nil || a.state != stateEmpty {
		t.Fatalf("late core reopened a closed workspace: state=%v session=%T", a.state, a.session)
	}
}

type panickingSnapshotCloseSession struct{ closeTrackingSession }

func (s *panickingSnapshotCloseSession) Snapshot(context.Context) (repository.Snapshot, error) {
	panic("snapshot failed")
}

type panickingProgressiveCloseSession struct {
	closeTrackingSession
	panicCore    bool
	panicDetails bool
}

func (s *panickingProgressiveCloseSession) CoreSnapshot(context.Context) (repository.Snapshot, error) {
	if s.panicCore {
		panic("core failed")
	}
	return s.snapshot, nil
}

func (s *panickingProgressiveCloseSession) DetailsSnapshot(context.Context) (repository.Snapshot, error) {
	if s.panicDetails {
		panic("details failed")
	}
	return repository.Snapshot{}, nil
}

func TestPreInstallPanicsCloseTheirSessions(t *testing.T) {
	for _, name := range []string{"snapshot", "core"} {
		t.Run(name, func(t *testing.T) {
			var session repository.Session
			var closer *closeTrackingSession
			if name == "snapshot" {
				owned := &panickingSnapshotCloseSession{}
				owned.snapshot.Root = "/work/panicking"
				closer = &owned.closeTrackingSession
				session = owned
			} else {
				owned := &panickingProgressiveCloseSession{panicCore: true}
				owned.snapshot.Root = "/work/panicking"
				closer = &owned.closeTrackingSession
				session = owned
			}
			a := newApp(fixtureOpener{session: session})
			a.path = "/work/panicking"
			updated := newUpdates()
			a.update = updated.update
			a.beginOpen()
			updated.waitForUpdate(t)
			if closer.closeCalls != 1 {
				t.Fatalf("Close called %d times after panic, want once", closer.closeCalls)
			}
			if a.state != stateError || a.session != nil {
				t.Fatalf("panicking open installed a session: state=%v session=%T", a.state, a.session)
			}
		})
	}
}

func TestInstalledDetailsPanicLeavesShutdownAsOwner(t *testing.T) {
	session := &panickingProgressiveCloseSession{panicDetails: true}
	session.snapshot = repository.Snapshot{Root: "/work/details", Branch: "main"}
	a := newApp(fixtureOpener{session: session})
	a.path = session.snapshot.Root
	updated := newUpdates()
	a.update = updated.update
	a.beginOpen()
	updated.waitForUpdate(t) // Install the core.
	updated.waitForUpdate(t) // Recover the details panic.
	if session.closeCalls != 0 {
		t.Fatalf("details panic closed the installed session %d times", session.closeCalls)
	}
	if a.session != session || a.state != stateReady {
		t.Fatalf("details panic discarded the installed core: state=%v", a.state)
	}
	a.shutdown()
	if session.closeCalls != 1 {
		t.Fatalf("shutdown Close calls = %d, want one", session.closeCalls)
	}
}

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

func TestProgressiveOpenDefaultsHistoryAndGraphToCurrentBranch(t *testing.T) {
	a, updated, session := progressiveOpenFixture(t, nil)
	close(session.detailsRelease)
	updated.waitForUpdate(t) // Install branch and commit details, then start both filtered reads.

	// The core snapshot deliberately has no branches. Defaults must be applied
	// again now, rather than being abandoned with that branchless core.
	for _, target := range []int{sectionHistory, sectionGraph} {
		query := a.commitQueryFor(target)
		if !query.defaulted || query.applied.Branch != "main" || query.draft.Branch != "main" {
			t.Errorf("target %d default = defaulted:%v applied:%+v draft:%+v, want current branch main",
				target, query.defaulted, query.applied, query.draft)
		}
		if query.limit != repository.SnapshotCommitLimit {
			t.Errorf("target %d limit = %d, want %d", target, query.limit, repository.SnapshotCommitLimit)
		}
	}
	updated.waitForUpdate(t)
	updated.waitForUpdate(t)
	for _, target := range []int{sectionHistory, sectionGraph} {
		query := a.commitQueryFor(target)
		if query.loading || query.err != "" || len(query.matches) != 1 {
			t.Errorf("target %d did not finish its current-branch read: %+v", target, query)
		}
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
