package app

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// pagedCommitSession serves a history longer than the page a snapshot
// carries, recording the skip of every read so a test can see the list walk
// down it.
type pagedCommitSession struct {
	fakeSession
	total int
	mu    sync.Mutex
	skips []int
}

func (s *pagedCommitSession) RecentCommits(_ context.Context, limit, skip int) ([]repository.Commit, error) {
	s.mu.Lock()
	s.skips = append(s.skips, skip)
	s.mu.Unlock()
	if skip >= s.total {
		return nil, nil
	}
	end := min(skip+limit, s.total)
	commits := make([]repository.Commit, 0, end-skip)
	for i := skip; i < end; i++ {
		commits = append(commits, repository.Commit{
			Hash:      fmt.Sprintf("%040d", i),
			ShortHash: fmt.Sprintf("%07d", i),
			Subject:   fmt.Sprintf("Commit %d", i),
		})
	}
	return commits, nil
}

func (s *pagedCommitSession) skipsRead() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.skips...)
}

// History and the Graph page older commits in when their list reaches the
// end, so a repository is not cut off at the snapshot's first page.
func TestHistoryPagesOlderCommits(t *testing.T) {
	page := repository.SnapshotCommitLimit
	total := 2*page + 7
	first := make([]repository.Commit, 0, page)
	for i := 0; i < page; i++ {
		first = append(first, repository.Commit{
			Hash:      fmt.Sprintf("%040d", i),
			ShortHash: fmt.Sprintf("%07d", i),
			Subject:   fmt.Sprintf("Commit %d", i),
		})
	}
	session := &pagedCommitSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root:    "/work/project",
			Branch:  "main",
			Commits: first,
		}},
		total: total,
	}
	a := newApp(fixedSessionOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)

	if !a.commitsMore {
		t.Fatal("a full snapshot page did not report more commits")
	}
	a.requestMoreCommits()
	h.waitFor(t, "the second page of commits", func() bool {
		return !a.commitsLoading && len(a.snapshot.Commits) == 2*page
	})
	a.requestMoreCommits()
	h.waitFor(t, "the last page of commits", func() bool {
		return len(a.snapshot.Commits) == total
	})
	if a.commitsMore {
		t.Fatal("a short page still reported more commits")
	}
	want := fmt.Sprint([]int{page, 2 * page})
	if got := fmt.Sprint(session.skipsRead()); got != want {
		t.Fatalf("the commit list skipped %s, want %s", got, want)
	}

	// The History screen marks the end of the history once nothing is left.
	a.workspaceTab = sectionHistory
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Beginning of history") {
		t.Fatalf("the History screen does not show the end of history: %q", h.texts())
	}
}
