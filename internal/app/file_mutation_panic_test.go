package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func openStageMutationHarness(t *testing.T, session repository.Session) (*app, *updates, *harness) {
	t.Helper()
	a := newApp(fixtureOpener{session: session})
	a.path = "/work/project"
	a.workspaceTab = sectionChanges
	updated := newUpdates()
	a.update = updated.update
	a.beginOpen()
	updated.waitForUpdate(t)
	return a, updated, &harness{app: a, updates: updated, tester: ui.NewTester(a.view, 960, 640)}
}

type panicStageSession struct{ *delayedStageSnapshotSession }

func (s *panicStageSession) Stage(context.Context, repository.File) error {
	panic("intentional mutation-worker failure")
}

func TestFileMutationFeedbackStopsOnPanic(t *testing.T) {
	conflicted := repository.File{Path: "conflict.txt", Status: "UU", HasMergeConflicts: true, Tracked: true, Unstaged: true}
	base := repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{conflicted}}
	details := repository.Snapshot{Branches: []repository.Branch{{Name: "main", Head: true}}}
	session := &panicStageSession{&delayedStageSnapshotSession{
		fakeSession:        fakeSession{snapshot: base.WithDetails(details)},
		core:               base,
		details:            details,
		openDetailsStart:   make(chan struct{}),
		openDetailsRelease: closedStageGate(),
	}}
	a, _, h := openStageMutationHarness(t, session)
	a.beginStageFile(conflicted, true)
	h.waitFor(t, "the panic to be contained without a stale mutation phase", func() bool {
		return a.fileMutation.phase == fileMutationIdle && !a.actionBusy && a.actionError != ""
	})
	if a.actionError == "" {
		t.Fatal("worker panic was not reported")
	}
}
