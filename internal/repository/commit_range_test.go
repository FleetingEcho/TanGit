package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestForcePushLeaseRejectsUnseenRemoteChanges(t *testing.T) {
	ctx := context.Background()
	dir, s := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "a", "first", "first")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, dir, "init", "--bare", remote)
	runGit(t, dir, "remote", "add", "origin", remote)
	if err := s.Push(ctx); err != nil {
		t.Fatal(err)
	}
	writeHistoryCommit(t, dir, "a", "second", "second")
	if err := s.Push(ctx); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "--amend", "-m", "rewritten")
	if err := s.Push(ctx); !errors.Is(err, ErrPushRejected) {
		t.Fatalf("normal push error = %v, want ErrPushRejected", err)
	}
	if err := s.ForcePushWithLease(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(gitOutput(t, remote, "rev-parse", "refs/heads/main")), strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != want {
		t.Fatalf("remote=%s want %s", got, want)
	}
	other := filepath.Join(t.TempDir(), "other")
	runGit(t, dir, "clone", "--branch", "main", remote, other)
	runGit(t, other, "config", "user.name", "Other")
	runGit(t, other, "config", "user.email", "other@example.com")
	writeHistoryCommit(t, other, "other", "unseen", "other")
	runGit(t, other, "push", "origin", "main")
	before := gitOutput(t, remote, "rev-parse", "refs/heads/main")
	runGit(t, dir, "commit", "--amend", "-m", "rewritten again")
	if err := s.ForcePushWithLease(ctx); !errors.Is(err, ErrPushLeaseRejected) {
		t.Fatalf("lease push error = %v, want ErrPushLeaseRejected", err)
	}
	if got := gitOutput(t, remote, "rev-parse", "refs/heads/main"); got != before {
		t.Fatal("remote changed on rejected push")
	}
}

func TestEditCommitRange(t *testing.T) {
	for _, action := range []CommitRangeAction{RangeSquash, RangeFixup, RangeDrop, RangeMoveOlder, RangeMoveNewer} {
		t.Run(string(action), func(t *testing.T) {
			dir, s := newHistoryEditingSession(t)
			for _, name := range []string{"one", "two", "three", "four", "five"} {
				writeHistoryCommit(t, dir, name, name, name)
			}
			snap, err := s.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// Four and three are a contiguous middle range, independent file changes.
			if err := s.EditCommitRange(context.Background(), snap.Commits[1:3], action); err != nil {
				t.Fatal(err)
			}
			got := strings.Fields(gitOutput(t, dir, "log", "--format=%s"))
			want := map[CommitRangeAction]string{RangeSquash: "five three two one", RangeFixup: "five three two one", RangeDrop: "five two one", RangeMoveOlder: "five two four three one", RangeMoveNewer: "four three five two one"}[action]
			// Squash joins message subjects into a body; %s remains the oldest subject.
			if action == RangeSquash {
				want = "five three two one"
				if !strings.Contains(gitOutput(t, dir, "log", "--format=%B"), "four") {
					t.Fatal("squash lost selected message")
				}
			}
			if strings.Join(got, " ") != want {
				t.Fatalf("history=%q want %q", got, want)
			}
		})
	}
}

func TestEditCommitRangeRejectsGapsAndForeignCommits(t *testing.T) {
	dir, s := newHistoryEditingSession(t)
	for _, name := range []string{"one", "two", "three", "four"} {
		writeHistoryCommit(t, dir, name, name, name)
	}
	snap, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := gitOutput(t, dir, "rev-parse", "HEAD")
	for _, selected := range [][]Commit{{snap.Commits[0], snap.Commits[2]}, {snap.Commits[0], {Hash: "missing"}}, {snap.Commits[0], snap.Commits[0]}} {
		if err := s.EditCommitRange(context.Background(), selected, RangeDrop); err == nil {
			t.Fatal("invalid selection accepted")
		}
		if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != before {
			t.Fatal("invalid selection changed HEAD")
		}
	}
}

func TestCherryPickCommitsAppliesOldestFirst(t *testing.T) {
	dir, s := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base", "base", "base")
	runGit(t, dir, "checkout", "-b", "feature")
	writeHistoryCommit(t, dir, "file", "first", "first")
	writeHistoryCommit(t, dir, "file", "second", "second")
	snap, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "checkout", "main")
	if err := s.CherryPickCommits(context.Background(), snap.Commits[:2]); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s", "-2")); got != "second\nfirst" {
		t.Fatalf("wrong order: %q", got)
	}
}
