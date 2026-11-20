package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type blockingReviewSession struct {
	fakeSession
	started  chan struct{}
	canceled chan struct{}
}

func (s *blockingReviewSession) ReviewedFiles(ctx context.Context, _ []repository.File) (map[string]bool, error) {
	close(s.started)
	<-ctx.Done()
	close(s.canceled)
	return nil, ctx.Err()
}

func TestClosingRepositoryCancelsReviewRead(t *testing.T) {
	a := newApp(fakeOpener{})
	updates := newUpdates()
	a.update = updates.update
	session := &blockingReviewSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: "/work/project"}},
		started:     make(chan struct{}),
		canceled:    make(chan struct{}),
	}
	a.installOpenedRepository(session, session.snapshot)
	a.requestReviewedFiles()
	select {
	case <-session.started:
	case <-time.After(time.Second):
		t.Fatal("review read did not start")
	}

	a.shutdown()
	select {
	case <-session.canceled:
	case <-time.After(time.Second):
		t.Fatal("closing the repository did not cancel its review read")
	}
	updates.waitForUpdate(t)
}

func TestReplacingRepositoryCancelsReviewRead(t *testing.T) {
	a := newApp(fakeOpener{})
	updates := newUpdates()
	a.update = updates.update
	previous := &blockingReviewSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: "/work/previous"}},
		started:     make(chan struct{}),
		canceled:    make(chan struct{}),
	}
	a.installOpenedRepository(previous, previous.snapshot)
	a.requestReviewedFiles()
	select {
	case <-previous.started:
	case <-time.After(time.Second):
		t.Fatal("previous repository review read did not start")
	}

	replacement := fakeSession{snapshot: repository.Snapshot{Root: "/work/replacement"}}
	a.installOpenedRepository(replacement, replacement.snapshot)
	select {
	case <-previous.canceled:
	case <-time.After(time.Second):
		t.Fatal("replacing the repository did not cancel the previous review read")
	}
	if err := a.repositoryWork.Context().Err(); err != nil {
		t.Fatalf("replacement scope is already canceled: %v", err)
	}
	updates.waitForUpdate(t)
}

// The reviewed workflow is exercised through the same buttons, menu and keys a
// user has. Content invalidation itself is covered against real Git in the
// repository package; this test covers the UI and the session calls it makes.
func TestReviewedFileWorkflow(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "b.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "c.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	h.waitFor(t, "initial review progress", func() bool {
		return slices.Contains(h.texts(), "0/3 reviewed")
	})
	if err := h.tester.Click("Mark reviewed a.txt"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "review state saved", func() bool { return calls.has("review:a.txt:true") })
	h.settle(t)
	if !slices.Contains(h.texts(), "1/3 reviewed") {
		t.Fatalf("review progress did not advance: %q", h.texts())
	}
	if err := h.tester.Click("Next unreviewed"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if change, ok := h.app.selectedChange(); !ok || change.file.Path != "b.txt" {
		t.Fatalf("next unreviewed selected %#v, want b.txt", change)
	}

	h.tester.Key(0, ui.KeyR)
	h.waitFor(t, "review shortcut saved", func() bool { return calls.has("review:b.txt:true") })
	h.settle(t)
	openChangesFilterPopover(t, h)
	if err := h.tester.Click("Unreviewed"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if paths := changeRowPaths(h.app.changeRows); !slices.Equal(paths, []string{"unstaged:c.txt"}) {
		t.Fatalf("unreviewed filter left %v, want only c.txt", paths)
	}

	h.rightClick(t, "c.txt")
	h.requireMenuLabels(t, "Mark reviewed")
	h.chooseMenuItem(t, "Mark reviewed")
	h.waitFor(t, "menu review saved", func() bool { return calls.has("review:c.txt:true") })
	h.settle(t)
	if paths := changeRowPaths(h.app.changeRows); len(paths) != 0 {
		t.Fatalf("fully reviewed filter still shows %v", paths)
	}
	openChangesFilterPopover(t, h)
	if err := h.tester.Click("Unreviewed"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.rightClick(t, "a.txt")
	h.requireMenuLabels(t, "Mark unreviewed")
	h.chooseMenuItem(t, "Mark unreviewed")
	h.waitFor(t, "menu unreview saved", func() bool { return calls.has("review:a.txt:false") })
	h.settle(t)
	if !slices.Contains(h.texts(), "2/3 reviewed") {
		t.Fatalf("unreview did not lower progress: %q", h.texts())
	}
}

func TestReviewedRowHoverOffersUnreviewInsteadOfAnotherReview(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "b.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	h.app.review.files["a.txt"] = true
	if err := h.tester.Click("b.txt"); err != nil {
		t.Fatal(err)
	}
	h.tester.Move(2, 2)
	h.tester.Frame()
	if !h.tester.HasText("Reviewed") {
		t.Fatalf("the reviewed row has no resting status badge: %q", h.texts())
	}

	row, ok := h.tester.Find("a.txt")
	if !ok {
		t.Fatal("the reviewed file row is missing")
	}
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Frame()
	if h.tester.HasText("Reviewed") {
		t.Fatal("hover left the resting badge beside its action")
	}
	if _, found := h.tester.Find("Mark reviewed a.txt"); found {
		t.Fatal("an already-reviewed row still offers a check to review it again")
	}
	if err := h.tester.Click("Mark unreviewed a.txt"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "hover unreview action", func() bool { return calls.has("review:a.txt:false") })
	if got := reviewActionTooltip(); !strings.Contains(got, "only a status in TanGit") || !strings.Contains(got, "do not change Git information") {
		t.Fatalf("review action does not explain its app-only scope: %q", got)
	}
}

func TestReviewFailurePreservesVisibleFileAndDraft(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true}})
	h.app.commitSummary = "keep this draft"
	h.app.session = fakeSession{snapshot: h.app.snapshot, reviewErr: errors.New("review store is read-only")}
	h.tester.Frame()
	if err := h.tester.Click("Mark reviewed a.txt"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "review failure", func() bool { return h.app.actionError != "" })
	if h.app.commitSummary != "keep this draft" || h.app.review.files["a.txt"] {
		t.Fatalf("failed review changed draft or mark: draft=%q reviewed=%v", h.app.commitSummary, h.app.review.files["a.txt"])
	}
}
