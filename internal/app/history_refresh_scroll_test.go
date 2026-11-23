package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type prependHistorySession struct {
	*pagedCommitSession
	prefix repository.Commit
}

func (s *prependHistorySession) SearchCommits(ctx context.Context, filter repository.CommitFilter) ([]repository.Commit, error) {
	commits, err := s.pagedCommitSession.SearchCommits(ctx, filter)
	if err != nil {
		return nil, err
	}
	commits = append([]repository.Commit{s.prefix}, commits...)
	if filter.Limit > 0 && len(commits) > filter.Limit {
		commits = commits[:filter.Limit]
	}
	return commits, nil
}

func (s *prependHistorySession) RecentCommits(ctx context.Context, limit, skip int) ([]repository.Commit, error) {
	commits, err := s.pagedCommitSession.RecentCommits(ctx, limit+skip, 0)
	if err != nil {
		return nil, err
	}
	commits = append([]repository.Commit{s.prefix}, commits...)
	if skip >= len(commits) {
		return nil, nil
	}
	return commits[skip:min(len(commits), skip+limit)], nil
}

func historyRefreshFixture(t *testing.T, narrowed, prepend bool) (*app, *updates, *ui.Tester) {
	t.Helper()
	a, updated, tester := historyFilterScrollFixture(t)
	if !narrowed {
		// Clearing filter results does not itself page the raw history. Build
		// the same two-page ordinary-History baseline a user would reach.
		a.historyFilter.reset()
		a.commitsMore = true
		a.requestMoreCommits()
		updated.waitForUpdate(t)
		tester.Frame()
	}
	first, _ := a.commitList.Visible()
	if first <= 0 {
		t.Fatalf("fixture did not scroll far enough: first=%d", first)
	}
	if !narrowed && len(a.snapshot.Commits) <= repository.SnapshotCommitLimit {
		t.Fatalf("fixture did not load the second page: %d commits", len(a.snapshot.Commits))
	}
	if prepend {
		a.session = &prependHistorySession{
			pagedCommitSession: a.session.(*pagedCommitSession),
			prefix:             repository.Commit{Hash: "new-head", ShortHash: "new-head", Subject: "New head"},
		}
	}
	if narrowed {
		a.historyFilter.more = false
	} else {
		a.commitsMore = false
	}
	// Settle programmatic layout requests with MyGo's virtual clock instead
	// of introducing wall-clock sleeps into a GUI regression test.
	tester.Advance(time.Second)
	tester.Frame()
	return a, updated, tester
}

func assertCommitVisible(t *testing.T, a *app, hash string) {
	assertListCommitVisible(t, &a.commitList, a.visibleCommits(), hash)
}

func assertListCommitVisible(t *testing.T, list *ui.ListState, commits []repository.Commit, hash string) {
	t.Helper()
	index := -1
	for i, commit := range commits {
		if commit.Hash == hash {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatalf("commit %s disappeared from refreshed data", hash)
	}
	first, last := list.Visible()
	if index < first || index > last {
		t.Fatalf("visible commit moved out of the viewport: hash=%s index=%d range=[%d,%d]",
			hash, index, first, last)
	}
}

func TestQuietHistoryRefreshKeepsVisibleCommit(t *testing.T) {
	for _, test := range []struct {
		name     string
		narrowed bool
		prepend  bool
	}{
		{name: "filtered replacement", narrowed: true},
		{name: "filtered new head", narrowed: true, prepend: true},
		{name: "unfiltered new head", prepend: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, updated, tester := historyRefreshFixture(t, test.narrowed, test.prepend)
			before, _ := a.commitList.Visible()
			wantHash := a.visibleCommits()[before].Hash
			loaded := len(a.snapshot.Commits)
			if test.narrowed {
				loaded = len(a.historyFilter.matches)
			}
			token := a.historyFilter.token
			next := a.snapshot
			next.HeadHash = "new-head"
			next.Commits = append([]repository.Commit(nil), next.Commits[:repository.SnapshotCommitLimit]...)
			if test.prepend {
				next.Commits = append([]repository.Commit{{Hash: "new-head", ShortHash: "new-head", Subject: "New head"}}, next.Commits[:repository.SnapshotCommitLimit-1]...)
			}
			a.applyQuietSnapshot(next, snapshotSigns{}, snapshotSigns{refs: "changed"})
			h := &harness{app: a, tester: tester, updates: updated}
			if test.narrowed && a.historyFilter.token == token {
				t.Fatalf("refresh did not start the filtered reload: tab=%d filter=%+v", a.workspaceTab, a.historyFilter.applied)
			}
			h.waitFor(t, "the full visible prefix after refresh", func() bool {
				if len(a.visibleCommits()) < loaded {
					return false
				}
				if test.prepend && a.visibleCommits()[0].Hash != "new-head" {
					return false
				}
				if test.narrowed {
					return !a.historyFilter.loading
				}
				return !a.commitsLoading
			})
			tester.Advance(time.Second)
			tester.Frame()
			assertCommitVisible(t, a, wantHash)
			if len(a.visibleCommits()) < loaded {
				t.Fatalf("refresh discarded loaded history: got %d, want at least %d", len(a.visibleCommits()), loaded)
			}
			if test.prepend && test.narrowed {
				if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
					saveScreenshot(t, dir, "history-prepend", tester)
				}
			}
		})
	}
}

func TestHistoryRefreshDoesNotOverrideNewerUserScroll(t *testing.T) {
	a, updated, tester := historyRefreshFixture(t, true, false)
	before, _ := a.commitList.Visible()
	target := before - 1
	if target < 0 {
		target = before + 1
	}
	a.runCommitFilterMode(sectionHistory, "", false)
	a.commitList.ScrollTo(target, ui.Start)
	tester.Frame()
	userFirst, _ := a.commitList.Visible()
	if userFirst < 0 || userFirst >= len(a.visibleCommits()) {
		t.Fatalf("user scroll left no visible commit: first=%d", userFirst)
	}
	userHash := a.visibleCommits()[userFirst].Hash
	h := &harness{app: a, tester: tester, updates: updated}
	h.waitFor(t, "the replaced history after the newer user scroll", func() bool {
		return !a.historyFilter.loading
	})
	tester.Advance(time.Second)
	tester.Frame()
	first, _ := a.commitList.Visible()
	if first != userFirst || a.visibleCommits()[first].Hash != userHash {
		t.Fatalf("refresh overrode the later user scroll: first=%d want=%d", first, userFirst)
	}
}
