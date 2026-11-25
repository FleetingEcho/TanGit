package app

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type pagedReflogSession struct {
	fakeSession
	total int
	mu    sync.Mutex
	skips []int
}

func (s *pagedReflogSession) Reflog(_ context.Context, limit, skip int) ([]repository.Commit, error) {
	s.mu.Lock()
	s.skips = append(s.skips, skip)
	s.mu.Unlock()
	if skip >= s.total {
		return nil, nil
	}
	end := min(skip+limit, s.total)
	entries := make([]repository.Commit, 0, end-skip)
	for index := skip; index < end; index++ {
		entries = append(entries, repository.Commit{
			Hash:      fmt.Sprintf("%040d", index),
			ShortHash: fmt.Sprintf("%07d", index),
			Subject:   fmt.Sprintf("Reflog entry %d", index),
		})
	}
	return entries, nil
}

func (s *pagedReflogSession) skipsRead() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.skips...)
}

func TestReflogEndReachedPagesOlderEntries(t *testing.T) {
	page := repository.SnapshotCommitLimit
	total := 2*page + 7
	session := &pagedReflogSession{total: total}
	entries, err := session.Reflog(context.Background(), page, 0)
	if err != nil {
		t.Fatal(err)
	}
	session.skips = nil
	session.fakeSession.snapshot = repository.Snapshot{Root: "/work/project", Branch: "main", Reflog: entries}
	a := newApp(fakeOpener{})
	a.session = session
	a.snapshot = session.snapshot
	a.selectedReflogRow, a.selectedReflog = 7, 7
	a.resetReflogPaging(a.snapshot)
	a.workspaceTab = sectionReflog
	updated := newUpdates()
	a.update = updated.update

	a.reflogList.EndReached(page - 1)
	updated.waitForUpdate(t)
	if got := len(a.snapshot.Reflog); got != 2*page {
		t.Fatalf("second page left %d entries, want %d", got, 2*page)
	}
	if !a.reflogMore {
		t.Fatal("another full page incorrectly ended the reflog")
	}
	a.reflogList.EndReached(2*page - 1)
	updated.waitForUpdate(t)
	if got := len(a.snapshot.Reflog); got != total {
		t.Fatalf("last page left %d entries, want %d", got, total)
	}
	if a.reflogMore {
		t.Fatal("a short final page still reports more entries")
	}
	if a.selectedReflogRow != 7 || a.selectedReflog != 7 {
		t.Fatal("paging moved the selected reflog entry")
	}
	want := fmt.Sprint([]int{page, 2 * page})
	if got := fmt.Sprint(session.skipsRead()); got != want {
		t.Fatalf("reflog reads skipped %s, want %s", got, want)
	}
}
