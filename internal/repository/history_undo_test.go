package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindReflogAction(t *testing.T) {
	tests := []struct {
		name    string
		entries []reflogEntry
		redo    bool
		want    reflogAction
		ok      bool
	}{
		{
			name: "undo commit uses adjacent hashes",
			entries: []reflogEntry{
				{hash: "b", name: "commit: second"},
				{hash: "a", name: "commit (initial): first"},
			},
			want: reflogAction{kind: reflogCommit, from: "a", to: "b"},
			ok:   true,
		},
		{
			name: "redo requires an unmatched undo",
			entries: []reflogEntry{
				{hash: "b", name: "commit: second"},
				{hash: "a", name: "commit (initial): first"},
			},
			redo: true,
		},
		{
			name: "redo selects newest undone action",
			entries: []reflogEntry{
				{hash: "a", name: reflogUndoMarker + " reset: moving to a"},
				{hash: "b", name: "commit: second"},
				{hash: "a", name: "commit (initial): first"},
			},
			redo: true,
			want: reflogAction{kind: reflogCommit, from: "a", to: "b"},
			ok:   true,
		},
		{
			name: "repeated undo and redo skip balanced actions",
			entries: []reflogEntry{
				{hash: "b", name: reflogUndoMarker},
				{hash: "c", name: reflogUndoMarker},
				{hash: "d", name: "commit: fourth"},
				{hash: "c", name: "commit: third"},
				{hash: "b", name: "commit: second"},
				{hash: "a", name: "commit (initial): first"},
			},
			want: reflogAction{kind: reflogCommit, from: "a", to: "b"},
			ok:   true,
		},
		{
			name: "checkout keeps ref names rather than hashes",
			entries: []reflogEntry{
				{hash: "b", name: "checkout: moving from main to feature"},
				{hash: "a", name: "commit: first"},
			},
			want: reflogAction{kind: reflogCheckout, from: "main", to: "feature"},
			ok:   true,
		},
		{
			name: "interactive rebase finish and start form one action",
			entries: []reflogEntry{
				{hash: "c", name: "rebase -i (finish): refs/heads/main onto target"},
				{hash: "temporary", name: "commit: temporary rebase commit"},
				{hash: "b", name: "rebase -i (start): checkout target"},
				{hash: "a", name: "commit: before rebase"},
			},
			want: reflogAction{kind: reflogRebase, from: "a", to: "c"},
			ok:   true,
		},
		{
			name: "noninteractive rebase finish is also supported",
			entries: []reflogEntry{
				{hash: "c", name: "rebase (finish): refs/heads/main onto target"},
				{hash: "b", name: "rebase (start): checkout target"},
				{hash: "a", name: "commit: before rebase"},
			},
			want: reflogAction{kind: reflogRebase, from: "a", to: "c"},
			ok:   true,
		},
		{
			name: "active rebase cannot be replayed",
			entries: []reflogEntry{
				{hash: "b", name: "rebase -i (start): checkout target"},
				{hash: "a", name: "commit: before rebase"},
			},
		},
		{
			name: "same hash and root actions are ignored",
			entries: []reflogEntry{
				{hash: "a", name: "reset: moving to a"},
				{hash: "a", name: "commit (initial): first"},
			},
		},
		{
			name: "unsupported operations are skipped",
			entries: []reflogEntry{
				{hash: "c", name: "merge feature: Fast-forward"},
				{hash: "b", name: "pull origin main: Fast-forward"},
				{hash: "a", name: "commit: first"},
			},
			want: reflogAction{kind: reflogCommit, from: "a", to: "b"},
			ok:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := findReflogAction(test.entries, test.redo)
			if ok != test.ok || got != test.want {
				t.Fatalf("findReflogAction() = %#v, %v; want %#v, %v", got, ok, test.want, test.ok)
			}
		})
	}

	t.Run("redo selects the last of two undone actions first", func(t *testing.T) {
		entries := []reflogEntry{
			{hash: "b", name: reflogUndoMarker},
			{hash: "c", name: reflogUndoMarker},
			{hash: "d", name: "commit: fourth"},
			{hash: "c", name: "commit: third"},
			{hash: "b", name: "commit: second"},
			{hash: "a", name: "commit (initial): first"},
		}
		got, ok := findReflogAction(entries, true)
		want := reflogAction{kind: reflogCommit, from: "b", to: "c"}
		if !ok || got != want {
			t.Fatalf("findReflogAction(redo) = %#v, %v; want %#v", got, ok, want)
		}
	})
}

func TestReflogReplayDescription(t *testing.T) {
	tests := []struct {
		name       string
		entries    []Commit
		operations []OperationRecord
		redo       bool
		want       string
	}{
		{
			name:    "commit",
			entries: []Commit{{Hash: "b", Subject: "commit: Add safe history editing"}, {Hash: "a"}},
			want:    "commit: Add safe history editing",
		},
		{
			name:    "reset",
			entries: []Commit{{Hash: "b", Subject: "reset: moving to HEAD~1"}, {Hash: "a"}},
			want:    "reset: moving to HEAD~1",
		},
		{
			name:    "pull",
			entries: []Commit{{Hash: "b", Subject: "pull origin main: Fast-forward"}, {Hash: "a"}},
			want:    "pull origin main: Fast-forward",
		},
		{
			name:    "checkout",
			entries: []Commit{{Hash: "b", Subject: "checkout: moving from main to feature"}},
			want:    "checkout: moving from main to feature",
		},
		{
			name: "completed rebase",
			entries: []Commit{
				{Hash: "c", Subject: "rebase (finish): returning to refs/heads/main"},
				{Hash: "b", Subject: "rebase (pick): Change"},
				{Hash: "start", Subject: "rebase (start): checkout a"},
				{Hash: "a", Subject: "reset: moving to main"},
			},
			want: "completed rebase",
		},
		{
			name: "journaled squash",
			entries: []Commit{
				{Hash: "c", Subject: "rebase (finish): returning to refs/heads/main"},
				{Hash: "b", Subject: "rebase (pick): Change"},
				{Hash: "start", Subject: "rebase (start): checkout a"},
				{Hash: "a", Subject: "reset: moving to main"},
			},
			operations: []OperationRecord{{Kind: OperationRebase, Summary: "Squash 2 commits", Status: OperationSucceeded, HeadBefore: "a", HeadAfter: "c"}},
			want:       "Squash 2 commits",
		},
		{
			name: "journaled squash with a stale hash",
			entries: []Commit{
				{Hash: "c", Subject: "rebase (finish): returning to refs/heads/main"},
				{Hash: "b", Subject: "rebase (pick): Change"},
				{Hash: "start", Subject: "rebase (start): checkout a"},
				{Hash: "a", Subject: "reset: moving to main"},
			},
			operations: []OperationRecord{{Kind: OperationRebase, Summary: "Squash 2 unrelated commits", Status: OperationSucceeded, HeadBefore: "old", HeadAfter: "different"}},
			want:       "completed rebase",
		},
		{
			name: "redo",
			entries: []Commit{
				{Hash: "a", Subject: "[lazygit undo]: moving to b"},
				{Hash: "b", Subject: "commit: Add safe history editing"},
				{Hash: "a"},
			},
			redo: true,
			want: "commit: Add safe history editing",
		},
		{
			name:    "unsupported",
			entries: []Commit{{Hash: "b", Subject: "merge topic: Merge made by the 'ort' strategy"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ReflogReplayDescription(test.entries, test.operations, test.redo)
			if ok != (test.want != "") || got != test.want {
				t.Fatalf("description = %q, %v; want %q, %v", got, ok, test.want, test.want != "")
			}
		})
	}
}

func TestUndoRedoCommitsRepeatedlyAndAcrossSessions(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	before := historySnapshot(t, session)
	initial := before.Commits[2].Hash
	middle := before.Commits[1].Hash
	tip := before.Commits[0].Hash

	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, middle)
	assertStagedPath(t, dir, "three.txt")
	if got := gitOutput(t, dir, "log", "-g", "-1", "--format=%gs"); !strings.HasPrefix(got, reflogUndoMarker) {
		t.Fatalf("undo reflog marker = %q, want prefix %q", got, reflogUndoMarker)
	}
	cleanWorkingTreeWithoutMovingHead(t, dir)

	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, initial)
	cleanWorkingTreeWithoutMovingHead(t, dir)

	// A new Session proves that replay state lives in Git's reflog, not in the
	// old session's memory. Reopening is equivalent to restarting the app here.
	reopened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, middle)
	if err := reopened.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, tip)
	if got := gitOutput(t, dir, "log", "-g", "-1", "--format=%gs"); !strings.HasPrefix(got, reflogRedoMarker) {
		t.Fatalf("redo reflog marker = %q, want prefix %q", got, reflogRedoMarker)
	}
	if err := reopened.Redo(context.Background()); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("extra Redo returned %v, want ErrNothingToRedo", err)
	}

	if err := reopened.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, middle)
	cleanWorkingTreeWithoutMovingHead(t, dir)
	if err := reopened.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, initial)
}

func TestUndoRedoCheckout(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "file.txt", "first\n", "initial")
	runGit(t, dir, "branch", "feature")
	if err := session.CheckoutBranch(context.Background(), "feature"); err != nil {
		t.Fatal(err)
	}

	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := currentBranchName(t, dir); got != "main" {
		t.Fatalf("checkout undo left branch %q, want main", got)
	}
	if err := session.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := currentBranchName(t, dir); got != "feature" {
		t.Fatalf("checkout redo left branch %q, want feature", got)
	}
}

func TestReflogReplayDescriptionUsesSquashJournal(t *testing.T) {
	_, session := newThreeCommitHistory(t)
	before := historySnapshot(t, session)
	if err := session.CombineCommit(context.Background(), before.Commits[1], CombineSquash); err != nil {
		t.Fatal(err)
	}
	after := historySnapshot(t, session)
	description, ok := ReflogReplayDescription(after.Reflog, after.Operations, false)
	if !ok || description != "Squash 2 commits" {
		t.Fatalf("Undo preview = %q, %v; want the journaled squash", description, ok)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	undone := historySnapshot(t, session)
	description, ok = ReflogReplayDescription(undone.Reflog, undone.Operations, true)
	if !ok || description != "Squash 2 commits" {
		t.Fatalf("Redo preview = %q, %v; want the journaled squash", description, ok)
	}
}

func TestUndoRedoCompletedInteractiveRebase(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	before := historySnapshot(t, session)
	oldTip := before.Commits[0].Hash
	middle := before.Commits[1]
	if err := session.RewordCommit(context.Background(), middle, "middle reworded", "body"); err != nil {
		t.Fatal(err)
	}
	rebased := historySnapshot(t, session)
	newTip := rebased.Commits[0].Hash
	if newTip == oldTip {
		t.Fatal("test setup did not rewrite the rebase tip")
	}

	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, oldTip)
	if got := strings.Join(historySubjects(t, dir), ", "); got != "tip, middle, initial" {
		t.Fatalf("history after rebase undo = %s", got)
	}

	if err := session.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, newTip)
	if got := strings.Join(historySubjects(t, dir), ", "); got != "tip, middle reworded, initial" {
		t.Fatalf("history after rebase redo = %s", got)
	}
}

func TestHardResetReplayAutoStashesTrackedChanges(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	commits := historySnapshot(t, session).Commits
	middle, tip := commits[1].Hash, commits[0].Hash
	runGit(t, dir, "reset", "--hard", middle)

	local := "local work that must survive\n"
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, tip)
	if err := session.Redo(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertHead(t, dir, middle)
	content, err := os.ReadFile(filepath.Join(dir, "one.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != local {
		t.Fatalf("auto-stash restored %q, want %q", content, local)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "stash", "list")); got != "" {
		t.Fatalf("successful replay left auto-stashes: %s", got)
	}
}

func TestFailedHardResetReplayRestoresAutoStash(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	commits := historySnapshot(t, session).Commits
	middle := commits[1].Hash
	runGit(t, dir, "reset", "--hard", middle)

	local := "local work restored after a failed reset\n"
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.Undo(context.Background()); err != nil {
		t.Fatal(err)
	}

	// A directory at the branch lock path makes Git fail to update main after
	// the auto-stash. It is deterministic even in tests running as root.
	lock := filepath.Join(dir, ".git", "refs", "heads", "main.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock)
	if err := session.Redo(context.Background()); err == nil || !strings.Contains(err.Error(), "hard reset") {
		t.Fatalf("Redo with a locked branch returned %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "one.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != local {
		t.Fatalf("failed reset restored %q, want %q", content, local)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "stash", "list")); got != "" {
		t.Fatalf("failed reset left its auto-stash behind: %s", got)
	}
}

func TestUndoRedoRefuseAnOperationInProgress(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	if err := os.Mkdir(filepath.Join(dir, ".git", "rebase-merge"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := session.Undo(context.Background()); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("Undo during rebase returned %v", err)
	}
	if err := session.Redo(context.Background()); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("Redo during rebase returned %v", err)
	}
}

func TestUndoWithoutEligibleAction(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "file.txt", "first\n", "initial")
	if err := session.Undo(context.Background()); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo with only a root commit returned %v, want ErrNothingToUndo", err)
	}
}

func assertHead(t *testing.T, dir, want string) {
	t.Helper()
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != want {
		t.Fatalf("HEAD = %s, want %s", got, want)
	}
}

func assertStagedPath(t *testing.T, dir, path string) {
	t.Helper()
	if got := gitOutput(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(got, path) {
		t.Fatalf("staged paths %q do not include %q", got, path)
	}
}

func cleanWorkingTreeWithoutMovingHead(t *testing.T, dir string) {
	t.Helper()
	// git restore changes only the index and files, not HEAD or its reflog, so
	// it cannot perturb the undo/redo sequence under test.
	runGit(t, dir, "restore", "--source=HEAD", "--staged", "--worktree", ":/")
}

func currentBranchName(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitOutput(t, dir, "branch", "--show-current"))
}
