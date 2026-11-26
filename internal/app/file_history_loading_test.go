package app

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
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
