package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type retryCommitSession struct {
	fakeSession
	mu      sync.Mutex
	attempt int
}

func (s *retryCommitSession) Commit(ctx context.Context, summary, body string, amend bool) error {
	s.mu.Lock()
	s.attempt++
	attempt := s.attempt
	s.mu.Unlock()
	s.calls.record("commit-attempt:" + summary + ":" + body)
	if attempt == 1 {
		return errors.New("pre-commit hook rejected; fix it and retry")
	}
	return s.fakeSession.Commit(ctx, summary, body, amend)
}

type retryCommitOpener struct{ session *retryCommitSession }

func (o retryCommitOpener) Init(context.Context, string) error { return nil }
func (o retryCommitOpener) Open(context.Context, string) (repository.Session, error) {
	return o.session, nil
}

func TestCommitFailurePreservesDraftAndRetryRerunsCommit(t *testing.T) {
	calls := &callRecorder{}
	session := &retryCommitSession{fakeSession: fakeSession{
		snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: sectionedFiles()}, calls: calls,
	}}
	a := newApp(retryCommitOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	a.commitSummary = "ship feature"
	a.commitBody = "keep this description"
	a.showCommitForm = true
	a.beginCommit()
	h.waitFor(t, "the failed commit", func() bool {
		return calls.has("commit-attempt:ship feature:keep this description") && !a.actionBusy && a.actionError != ""
	})
	if a.commitSummary != "ship feature" || a.commitBody != "keep this description" || !a.showCommitForm {
		t.Fatalf("failure damaged the draft: summary=%q body=%q form=%v", a.commitSummary, a.commitBody, a.showCommitForm)
	}
	a.beginCommit()
	h.waitFor(t, "the successful retry", func() bool { return calls.has("commit:ship feature") })
	h.settle(t)
	if a.commitSummary != "" || a.commitBody != "" || a.showCommitForm {
		t.Fatal("successful retry did not clear the committed draft")
	}
	session.mu.Lock()
	attempts := session.attempt
	session.mu.Unlock()
	if attempts != 2 {
		t.Fatalf("commit attempts = %d, want 2", attempts)
	}
}
