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
	total        int
	mu           sync.Mutex
	skips        []int
	branchLimits []int
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

func (s *pagedCommitSession) SearchCommits(_ context.Context, filter repository.CommitFilter) ([]repository.Commit, error) {
	return s.BranchCommits(context.Background(), filter.Branch, filter.Limit)
}

func (s *pagedCommitSession) BranchCommits(_ context.Context, _ string, limit int) ([]repository.Commit, error) {
	s.mu.Lock()
	s.branchLimits = append(s.branchLimits, limit)
	s.mu.Unlock()
	end := min(limit, s.total)
	commits := make([]repository.Commit, 0, end)
	for i := 0; i < end; i++ {
		commits = append(commits, repository.Commit{
			Hash: fmt.Sprintf("%040d", i), ShortHash: fmt.Sprintf("%07d", i), Subject: fmt.Sprintf("Commit %d", i),
		})
	}
	return commits, nil
}

func (s *pagedCommitSession) skipsRead() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.skips...)
}

func (s *pagedCommitSession) branchLimitsRead() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.branchLimits...)
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

func TestHistoryEndReachedLoadsTheNextPage(t *testing.T) {
	page := repository.SnapshotCommitLimit
	session := &pagedCommitSession{fakeSession: fakeSession{snapshot: repository.Snapshot{Branch: "main"}}, total: 2*page + 1}
	a := newApp(fakeOpener{})
	a.session = session
	a.snapshot.Commits, _ = session.RecentCommits(context.Background(), page, 0)
	a.commitsMore = true
	updated := newUpdates()
	a.update = updated.update
	a.workspaceTab = sectionHistory

	a.commitList.EndReached(page - 1)
	updated.waitForUpdate(t)
	if got := len(a.snapshot.Commits); got != 2*page {
		t.Fatalf("end reached loaded %d commits, want %d", got, 2*page)
	}
	a.commitList.EndReached(2*page - 1)
	updated.waitForUpdate(t)
	if got := len(a.snapshot.Commits); got != 2*page+1 || a.commitsMore {
		t.Fatalf("last page left %d commits and more=%v", got, a.commitsMore)
	}
}

func TestBranchHistoryPagesOlderCommits(t *testing.T) {
	page := repository.SnapshotCommitLimit
	total := 2*page + 7
	session := &pagedCommitSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Branch: "main", Branches: []repository.Branch{{Name: "main", Head: true}}}},
		total:       total,
	}
	a := newApp(fakeOpener{})
	a.session = session
	a.snapshot = session.snapshot
	a.selectedBranch = 0
	updated := newUpdates()
	a.update = updated.update
	a.workspaceTab = sectionBranches

	a.requestBranchCommits("main")
	updated.waitForUpdate(t)
	if len(a.branchCommits) != page || !a.branchCommitsMore {
		t.Fatalf("initial branch page = %d commits and more=%v, want %d and true", len(a.branchCommits), a.branchCommitsMore, page)
	}
	for want := 2 * page; ; want += page {
		want = min(want, total)
		a.branchCommitList.EndReached(len(a.branchCommits) - 1)
		updated.waitForUpdate(t)
		if len(a.branchCommits) != want {
			t.Fatalf("branch page = %d commits, want %d", len(a.branchCommits), want)
		}
		if want == total {
			break
		}
	}
	if a.branchCommitsMore {
		t.Fatal("a short final branch page still reports more commits")
	}
	wantLimits := fmt.Sprint([]int{page, 2 * page, 3 * page})
	if got := fmt.Sprint(session.branchLimitsRead()); got != wantLimits {
		t.Fatalf("branch reads used limits %s, want %s", got, wantLimits)
	}
}

func branchScrollFixture(t *testing.T) (*app, *updates, *ui.Tester) {
	t.Helper()
	page := repository.SnapshotCommitLimit
	session := &pagedCommitSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root: "/work/project", Branch: "main",
			Branches: []repository.Branch{{Name: "main", Head: true}, {Name: "feature"}},
		}},
		total: 2*page + 7,
	}
	a := newApp(fakeOpener{})
	a.session = session
	a.snapshot = session.snapshot
	a.state = stateReady
	a.selectedBranch = 0
	a.workspaceTab = sectionBranches
	updated := newUpdates()
	a.update = updated.update
	a.requestBranchCommits("main")
	updated.waitForUpdate(t)
	a.requestMoreBranchCommits("main")
	updated.waitForUpdate(t)
	tester := ui.NewTester(a.branchesView, 800, 260)
	tester.Frame()
	a.branchCommitList.ScrollToEnd()
	tester.Frame()
	if first, _ := a.branchCommitList.Visible(); first <= 0 {
		t.Fatalf("fixture failed to scroll the branch list: first=%d", first)
	}
	return a, updated, tester
}

func TestBranchUpdatesResetCommitScrollToTop(t *testing.T) {
	for _, test := range []struct {
		name  string
		apply func(*app, *updates)
	}{
		{name: "switch branch", apply: func(a *app, _ *updates) {
			a.selectedBranch = 1
			a.resetBranchCommits()
		}},
		{name: "apply filter", apply: func(a *app, _ *updates) {
			a.filterTarget = sectionBranches
			a.branchFilterDraft.Query = "needle"
			a.applyCommitFilter()
		}},
		{name: "clear filter", apply: func(a *app, _ *updates) {
			a.branchFilter.Query = "needle"
			a.branchFilterDraft.Query = "needle"
			a.clearCommitFilter(sectionBranches)
		}},
		{name: "refresh", apply: func(a *app, _ *updates) {
			next := a.snapshot
			next.HeadHash = "new-head"
			a.applyQuietSnapshot(next, snapshotSigns{}, snapshotSigns{refs: "changed"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, updated, tester := branchScrollFixture(t)
			test.apply(a, updated)
			tester.Frame()
			updated.flushPending()
			tester.Frame()
			first, _ := a.branchCommitList.Visible()
			if first != 0 {
				t.Fatalf("after %s the branch list starts at row %d, want the top", test.name, first)
			}
		})
	}
}

func historyFilterScrollFixture(t *testing.T) (*app, *updates, *ui.Tester) {
	t.Helper()
	page := repository.SnapshotCommitLimit
	session := &pagedCommitSession{fakeSession: fakeSession{snapshot: repository.Snapshot{Branch: "main"}}, total: 2*page + 1}
	a := newApp(fakeOpener{})
	a.session = session
	a.snapshot.Commits, _ = session.RecentCommits(context.Background(), page, 0)
	a.workspaceTab = sectionHistory
	a.historyFilter.applied = repository.CommitFilter{Branch: "main"}
	a.historyFilter.limit = page
	a.historyFilter.matches = append([]repository.Commit(nil), a.snapshot.Commits...)
	a.historyFilter.more = true
	updated := newUpdates()
	a.update = updated.update
	tester := ui.NewTester(a.historyView, 1000, 220)
	tester.Frame()
	a.commitList.ScrollToEnd()
	tester.Frame()
	if first, _ := a.commitList.Visible(); first <= 0 {
		t.Fatalf("fixture failed to scroll History: first=%d", first)
	}
	return a, updated, tester
}

func TestHistoryFilterRefreshResetsTop(t *testing.T) {
	a, updated, tester := historyFilterScrollFixture(t)
	// Keep this case about replacement only, not the paging triggered by
	// scrolling to the end in the other regression.
	a.historyFilter.more = false
	a.runCommitFilter(sectionHistory, "")
	updated.waitForUpdate(t)
	tester.Frame()
	if first, _ := a.commitList.Visible(); first != 0 {
		t.Fatalf("refresh left History at row %d, want the top", first)
	}
}

func TestHistoryPaginationPreservesScrollPosition(t *testing.T) {
	a, updated, tester := historyFilterScrollFixture(t)
	before, _ := a.commitList.Visible()
	a.requestMoreFilteredCommits(sectionHistory)
	updated.waitForUpdate(t)
	tester.Frame()
	first, last := a.commitList.Visible()
	if first != before || last < 0 {
		t.Fatalf("pagination moved the reader from %d to first=%d last=%d", before, first, last)
	}
}
