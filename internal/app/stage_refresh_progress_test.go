package app

import (
	"context"
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type delayedStageSnapshotSession struct {
	fakeSession
	core                 repository.Snapshot
	details              repository.Snapshot
	coreRelease          chan struct{}
	openDetailsStart     chan struct{}
	openDetailsRelease   chan struct{}
	actionDetailsStart   chan struct{}
	actionDetailsRelease chan struct{}
	detailCalls          int
	fullReads            int
}

func (s *delayedStageSnapshotSession) Stage(context.Context, repository.File) error { return nil }

func (s *delayedStageSnapshotSession) CoreSnapshot(context.Context) (repository.Snapshot, error) {
	if s.coreRelease != nil {
		<-s.coreRelease
	}
	return s.core, nil
}

func (s *delayedStageSnapshotSession) DetailsSnapshot(context.Context) (repository.Snapshot, error) {
	s.detailCalls++
	if s.detailCalls == 1 {
		close(s.openDetailsStart)
		<-s.openDetailsRelease
	} else {
		select {
		case <-s.actionDetailsStart:
		default:
			close(s.actionDetailsStart)
		}
		<-s.actionDetailsRelease
	}
	return s.details, nil
}

// Snapshot models the old serial full-read. Progressive updates must not use
// it, while non-progressive sessions keep this exact fallback.
func (s *delayedStageSnapshotSession) Snapshot(ctx context.Context) (repository.Snapshot, error) {
	s.fullReads++
	core, err := s.CoreSnapshot(ctx)
	if err != nil {
		return repository.Snapshot{}, err
	}
	details, err := s.DetailsSnapshot(ctx)
	if err != nil {
		return repository.Snapshot{}, err
	}
	return core.WithDetails(details), nil
}

func TestMarkResolvedPublishesCoreBeforeAncillaryDetails(t *testing.T) {
	conflicted := repository.File{Path: "conflict.txt", Status: "UU", HasMergeConflicts: true, Tracked: true, Unstaged: true}
	resolved := conflicted
	resolved.Status = "M "
	resolved.Staged = true
	resolved.Unstaged = false
	resolved.HasMergeConflicts = false
	base := repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{conflicted}}
	core := repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{resolved}}
	details := repository.Snapshot{
		Commits:  []repository.Commit{{Hash: "detail-commit", Subject: "history"}},
		Branches: []repository.Branch{{Name: "main", Head: true}},
		Tags:     []repository.Tag{{Name: "v1.0.0"}},
	}
	session := &delayedStageSnapshotSession{
		fakeSession:          fakeSession{snapshot: core.WithDetails(details)},
		core:                 base,
		details:              details,
		openDetailsStart:     make(chan struct{}),
		openDetailsRelease:   make(chan struct{}),
		actionDetailsStart:   make(chan struct{}),
		actionDetailsRelease: make(chan struct{}),
	}
	for _, channel := range []chan struct{}{session.openDetailsRelease, session.actionDetailsRelease} {
		channel := channel
		t.Cleanup(func() {
			select {
			case <-channel:
			default:
				close(channel)
			}
		})
	}
	a := newApp(fixtureOpener{session: session})
	a.path = base.Root
	updated := newUpdates()
	a.update = updated.update
	a.workspaceTab = sectionChanges
	a.beginOpen()
	updated.waitForUpdate(t)
	<-session.openDetailsStart
	close(session.openDetailsRelease)
	updated.waitForUpdate(t)
	h := &harness{app: a, updates: updated, tester: ui.NewTester(a.view, 960, 640)}
	if a.state != stateReady {
		t.Fatalf("progressive open did not complete: state=%v", a.state)
	}

	session.core = core
	session.coreRelease = make(chan struct{})
	a.beginStageFile(conflicted, true)
	h.waitFor(t, "the accepted stage and file-list refresh", func() bool {
		return a.fileMutation.phase == fileMutationUpdating
	})
	h.tester.Frame()
	if !h.tester.HasText("Updating file list…") || !h.tester.HasText("Working…") {
		t.Fatalf("accepted update is not visibly busy: %q", h.texts())
	}
	close(session.coreRelease)
	h.waitFor(t, "ancillary details to begin while the core is already visible", func() bool {
		select {
		case <-session.actionDetailsStart:
			return true
		default:
			return false
		}
	})
	updated.runPending()
	if len(a.snapshot.Files) != 1 || !a.snapshot.Files[0].Staged || a.snapshot.Files[0].HasMergeConflicts {
		t.Fatalf("file update waits for blocked ancillary details: %+v", a.snapshot.Files)
	}
	if session.fullReads != 0 {
		t.Fatalf("mark resolved still uses the legacy full snapshot: %d calls", session.fullReads)
	}
	h.tester.Frame()
	if !h.tester.HasText("Staged Changes") {
		t.Fatalf("resolved file is not present in the staged section: %q", h.texts())
	}
	if !a.repositoryDetailsLoading {
		t.Fatal("deferred repository details do not expose their loading state")
	}

	close(session.actionDetailsRelease)
	h.waitFor(t, "ancillary details to merge without replacing the core", func() bool {
		return !a.repositoryDetailsLoading
	})
	if len(a.snapshot.Tags) != 1 || a.snapshot.Tags[0].Name != "v1.0.0" ||
		len(a.snapshot.Files) != 1 || !a.snapshot.Files[0].Staged || a.snapshot.Files[0].HasMergeConflicts {
		t.Fatalf("details overwrote the accepted core state: %+v", a.snapshot)
	}
	if a.fileMutation.phase != fileMutationIdle {
		t.Fatalf("accepted file mutation did not settle: phase=%v", a.fileMutation.phase)
	}
}

type stageFailureSession struct{ *delayedStageSnapshotSession }

func (s *stageFailureSession) Stage(context.Context, repository.File) error {
	return errors.New("stage failed for this test")
}

func closedStageGate() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func TestFileMutationFeedbackStopsOnFailureAndClose(t *testing.T) {
	for _, closeRepository := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "repository close"}[closeRepository], func(t *testing.T) {
			conflicted := repository.File{Path: "conflict.txt", Status: "UU", HasMergeConflicts: true, Tracked: true, Unstaged: true}
			base := repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{conflicted}}
			details := repository.Snapshot{Branches: []repository.Branch{{Name: "main", Head: true}}}
			session := &delayedStageSnapshotSession{
				fakeSession:          fakeSession{snapshot: base.WithDetails(details)},
				core:                 base,
				details:              details,
				openDetailsStart:     make(chan struct{}),
				openDetailsRelease:   closedStageGate(),
				actionDetailsStart:   make(chan struct{}),
				actionDetailsRelease: make(chan struct{}),
			}
			var subject repository.Session = session
			if !closeRepository {
				subject = &stageFailureSession{session}
			}
			a, _, h := openStageMutationHarness(t, subject)
			if closeRepository {
				session.actionDetailsRelease = make(chan struct{})
				t.Cleanup(func() {
					select {
					case <-session.actionDetailsRelease:
					default:
						close(session.actionDetailsRelease)
					}
				})
			}
			session.coreRelease = make(chan struct{})

			a.beginStageFile(conflicted, true)
			h.waitFor(t, "the accepted mutation to become work", func() bool {
				return a.fileMutation.phase == fileMutationUpdating
			})
			if closeRepository {
				a.shutdown()
				if a.fileMutation.phase != fileMutationIdle {
					t.Fatalf("closing a repository left mutation feedback active")
				}
				close(session.actionDetailsRelease)
			}
			close(session.coreRelease)
			h.waitFor(t, "the mutation to settle or be canceled", func() bool {
				return a.fileMutation.phase == fileMutationIdle && !a.actionBusy
			})
			if a.fileMutation.phase != fileMutationIdle {
				t.Fatalf("finished mutation did not settle: phase=%v err=%q", a.fileMutation.phase, a.actionError)
			}
			if !closeRepository && a.actionError == "" {
				t.Fatal("the failed stage did not report its error")
			}
		})
	}
}
