package app

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestFileHistoryLoadsOnlySelectedCommitDetails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := &callRecorder{}
		a := newApp(fakeOpener{})
		a.session = &pagedFileSession{fakeSession: fakeSession{calls: calls}, total: 5}
		updated := newUpdates()
		a.update = updated.update

		a.openFileHistory("one.txt")
		updated.waitForUpdate(t) // commit list
		updated.waitForUpdate(t) // selected commit's files
		updated.waitForUpdate(t) // selected file's diff
		synctest.Wait()
		commits := a.fileHistoryCommits
		if len(commits) != 5 {
			t.Fatalf("File History lists %d commits, want 5", len(commits))
		}

		checkReads := func(hashes ...string) {
			t.Helper()
			var want []string
			for _, hash := range hashes {
				want = append(want, "commit-files:"+hash, "commit-file-diff:"+hash+":one.txt")
			}
			got := slices.DeleteFunc(calls.all(), func(call string) bool {
				return !strings.HasPrefix(call, "commit-files:") && !strings.HasPrefix(call, "commit-file-diff:")
			})
			if !slices.Equal(got, want) {
				t.Fatalf("commit detail reads = %v, want %v", got, want)
			}
		}
		checkReads(commits[0].Hash)

		older := commits[3]
		a.fileHistoryList.Choose(3)
		a.loadFileHistoryCommit(older, false)
		updated.waitForUpdate(t) // selected commit's files
		updated.waitForUpdate(t) // selected file's diff
		synctest.Wait()
		if a.fileHistoryDiffHash != older.Hash || a.fileHistoryDiffState == nil {
			t.Fatalf("selecting %q did not show its diff", older.Subject)
		}
		checkReads(commits[0].Hash, older.Hash)
	})
}

func TestFileHistoryFromOlderCommitKeepsFullOrderSelectionAndDiff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const originIndex = fileHistoryPage + 3
		calls := &callRecorder{}
		session := &pagedFileSession{
			fakeSession: fakeSession{calls: calls},
			total:       2*fileHistoryPage + 7,
		}
		a := newApp(fakeOpener{})
		a.session = session
		updated := newUpdates()
		var publications atomic.Int64
		a.update = func(fn func()) {
			publications.Add(1)
			updated.update(fn)
		}
		origin := repository.Commit{
			Hash:      fmt.Sprintf("%040d", originIndex),
			ShortHash: fmt.Sprintf("%07d", originIndex),
			Subject:   fmt.Sprintf("Commit %d", originIndex),
		}

		a.openFileHistoryFrom("one.txt", origin)
		for a.fileHistoryLoading || a.fileHistoryDiffState == nil {
			updated.waitForUpdate(t)
			synctest.Wait()
		}
		if len(a.fileHistoryCommits) != 2*fileHistoryPage {
			t.Fatalf("history contains %d commits, want the two complete pages through the origin", len(a.fileHistoryCommits))
		}
		if a.fileHistorySelected != originIndex || a.fileHistoryCommits[0].Hash == origin.Hash ||
			a.fileHistoryCommits[originIndex].Hash != origin.Hash || a.fileHistoryDiffHash != origin.Hash {
			t.Fatalf("first=%q selected=%d/%q diff=%q, want the newest row and origin at %d with its diff",
				a.fileHistoryCommits[0].Hash, a.fileHistorySelected,
				commitHashAt(a.fileHistoryCommits, a.fileHistorySelected), a.fileHistoryDiffHash, originIndex)
		}
		if !slices.Equal(session.skipsRead(), []int{0, fileHistoryPage}) {
			t.Fatalf("initial page offsets = %v, want [0 %d]", session.skipsRead(), fileHistoryPage)
		}
		if got := publications.Load(); got != 3 {
			t.Fatalf("initial open published %d updates, want one list and two detail updates", got)
		}

		a.loadMoreFileHistory()
		for a.fileHistoryLoadingMore {
			updated.waitForUpdate(t)
			synctest.Wait()
		}
		if len(a.fileHistoryCommits) != 2*fileHistoryPage+7 || a.fileHistoryMore ||
			a.fileHistorySelected != originIndex || a.fileHistoryDiffHash != origin.Hash {
			t.Fatal("the final page changed the full order, the origin selection, or its diff")
		}
		if got := publications.Load(); got != 4 {
			t.Fatalf("paging published %d total updates, want 4", got)
		}
		detailReads := slices.DeleteFunc(calls.all(), func(call string) bool {
			return !strings.HasPrefix(call, "commit-files:") && !strings.HasPrefix(call, "commit-file-diff:")
		})
		wantDetails := []string{"commit-files:" + origin.Hash, "commit-file-diff:" + origin.Hash + ":one.txt"}
		if !slices.Equal(detailReads, wantDetails) {
			t.Fatalf("paging repeated or replaced the origin detail reads: got %v, want %v", detailReads, wantDetails)
		}
	})
}
