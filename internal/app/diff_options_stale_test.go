package app

import (
	"context"
	"testing"

	"lazygitapp/internal/repository"
)

// staleOptionsSession holds the first canonical read until its replacement is
// already on screen. The late answer must not overwrite the user's new options.
type staleOptionsSession struct {
	fakeSession
	commitGate chan struct{}
	stashGate  chan struct{}
}

func (s *staleOptionsSession) CommitFileDiff(_ context.Context, _ repository.Commit, _ repository.CommitFile, options repository.DiffOptions) (repository.Diff, error) {
	if !options.IgnoreWhitespace {
		<-s.commitGate
		return repository.Diff{NewText: "stale\n", Kind: "Commit"}, nil
	}
	return repository.Diff{NewText: "current\n", Kind: "Commit"}, nil
}

func (s *staleOptionsSession) StashFileDiff(_ context.Context, _ int, _ repository.CommitFile, options repository.DiffOptions) (repository.Diff, error) {
	if !options.IgnoreWhitespace {
		<-s.stashGate
		return repository.Diff{NewText: "stale\n", Kind: "Stash"}, nil
	}
	return repository.Diff{NewText: "current\n", Kind: "Stash"}, nil
}

func TestLateOrdinaryDiffCannotReplaceNewerOptions(t *testing.T) {
	for _, test := range []struct {
		name string
		read func(*app)
		text func(*app) string
		gate func(*staleOptionsSession) chan struct{}
	}{
		{
			name: "commit file",
			read: func(a *app) {
				a.requestCommitFileDiff(repository.Commit{Hash: "commit"}, repository.CommitFile{Path: "file.txt"})
			},
			text: func(a *app) string { return a.commitDiffState.New.String() },
			gate: func(s *staleOptionsSession) chan struct{} { return s.commitGate },
		},
		{
			name: "stash file",
			read: func(a *app) {
				a.requestStashFileDiff(repository.Stash{Hash: "stash"}, repository.CommitFile{Path: "file.txt"})
			},
			text: func(a *app) string { return a.stashDiffState.New.String() },
			gate: func(s *staleOptionsSession) chan struct{} { return s.stashGate },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &staleOptionsSession{commitGate: make(chan struct{}), stashGate: make(chan struct{})}
			t.Cleanup(func() {
				select {
				case <-test.gate(session):
				default:
					close(test.gate(session))
				}
			})
			updates := newUpdates()
			a := newApp(fakeOpener{})
			a.session = session
			a.update = updates.update
			a.diffOptions = repository.DiffOptions{ContextLines: 3}

			test.read(a) // canonical request, held in the fake session
			a.toggleDiffWhitespace()
			test.read(a) // replacement returns immediately
			updates.waitForUpdate(t)
			if got := test.text(a); got != "current\n" {
				t.Fatalf("replacement text = %q, want current", got)
			}

			close(test.gate(session))
			updates.waitForUpdate(t)
			if got := test.text(a); got != "current\n" {
				t.Fatalf("late canonical response replaced the new diff with %q", got)
			}
		})
	}
}
