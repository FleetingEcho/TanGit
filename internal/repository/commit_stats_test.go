package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitListTotalsCoverWholeCommit(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "one.txt", "old\nkeep\n", "root")
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("new\nkeep\nextra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHistoryCommit(t, dir, "two.txt", "another\n", "change")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "--amend", "--no-edit")
	runGit(t, dir, "checkout", "-b", "feature")
	writeHistoryCommit(t, dir, "feature.txt", "feature\nsecond line\n", "feature change")
	writeHistoryCommit(t, dir, "binary.dat", "\x00binary", "binary")
	runGit(t, dir, "mv", "two.txt", "renamed\tfile.txt")
	runGit(t, dir, "commit", "-m", "rename")
	runGit(t, dir, "checkout", "main")
	writeHistoryCommit(t, dir, "main.txt", "main\n", "main change")
	runGit(t, dir, "merge", "--no-ff", "feature", "-m", "merge")
	runGit(t, dir, "commit", "--allow-empty", "-m", "empty")
	ctx := context.Background()
	check := func(commits []Commit, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if len(commits) == 0 {
			t.Fatal("no commits")
		}
		for _, commit := range commits {
			files, err := session.CommitFiles(ctx, commit)
			if err != nil {
				t.Fatal(err)
			}
			var added, deleted int
			for _, file := range files {
				added += file.LinesAdded
				deleted += file.LinesDeleted
			}
			if commit.LinesAdded != added || commit.LinesDeleted != deleted {
				t.Errorf("%s totals +%d -%d, want +%d -%d", commit.Subject, commit.LinesAdded, commit.LinesDeleted, added, deleted)
			}
			if commit.Subject == "change" && (added != 3 || deleted != 1) {
				t.Errorf("change fixture totals +%d -%d, want +3 -1", added, deleted)
			}
		}
	}
	check(session.RecentCommits(ctx, 20, 0))
	check(session.RecentCommits(ctx, 20, 2))
	check(session.BranchCommits(ctx, "main", 20))
	check(session.SearchCommits(ctx, CommitFilter{Query: "change", Limit: 20}))
	check(session.SearchCommits(ctx, CommitFilter{Query: gitOutput(t, dir, "rev-parse", "HEAD"), Limit: 20}))
	check(session.FileCommits(ctx, "one.txt", 20, 0))
	snapshot, err := session.Snapshot(ctx)
	check(snapshot.Commits, err)
}

func TestParseCommitLineStats(t *testing.T) {
	output := "100\t10\tmain.go\x00" +
		"-\t-\tassets/logo.png\x00" +
		"5\t2\t\x00old/name.go\x00new/name.go\x00" +
		"3\t0\tpath with spaces.txt\x00"
	stats := parseCommitLineStats(output)
	tests := []struct {
		path           string
		added, deleted int
	}{
		{path: "main.go", added: 100, deleted: 10},
		{path: "assets/logo.png"},
		{path: "new/name.go", added: 5, deleted: 2},
		{path: "path with spaces.txt", added: 3},
	}
	for _, test := range tests {
		got, ok := stats[test.path]
		if !ok {
			t.Errorf("numstat lost %q", test.path)
			continue
		}
		if got.added != test.added || got.deleted != test.deleted {
			t.Errorf("%q = +%d -%d, want +%d -%d",
				test.path, got.added, got.deleted, test.added, test.deleted)
		}
	}
	if _, ok := stats["old/name.go"]; ok {
		t.Error("a rename was keyed by its old path rather than its new path")
	}
}
