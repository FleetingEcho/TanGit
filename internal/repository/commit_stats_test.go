package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestCommitLineStatsReusesImmutableCommitTotals(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "one.txt")
	runGit(t, dir, "commit", "-m", "initial")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	countReads := func() int {
		count := 0
		for _, entry := range session.CommandLog().Entries() {
			if strings.Contains(entry.Command, "diff-tree") {
				count++
			}
		}
		return count
	}

	first, err := session.RecentCommits(context.Background(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstReads := countReads()
	if firstReads != 1 {
		t.Fatalf("initial stats reads = %d, want 1", firstReads)
	}
	second, err := session.RecentCommits(context.Background(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := countReads(); got != firstReads {
		t.Fatalf("identical commits caused another stats read: before=%d after=%d", firstReads, got)
	}
	if len(first) != len(second) || len(first) == 0 {
		t.Fatalf("cached result changed commit count: first=%d second=%d", len(first), len(second))
	}
	if first[0].LinesAdded != 2 || first[0].LinesDeleted != 0 {
		t.Fatalf("real numstat was replaced by empty stats: %+v", first[0])
	}
	for i := range first {
		if first[i].Hash != second[i].Hash || first[i].Subject != second[i].Subject ||
			first[i].LinesAdded != second[i].LinesAdded || first[i].LinesDeleted != second[i].LinesDeleted {
			t.Errorf("cached commit %d differs: first=%+v second=%+v", i, first[i], second[i])
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "two.txt"), []byte("new line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "two.txt")
	runGit(t, dir, "commit", "-m", "second commit")
	paged, err := session.RecentCommits(context.Background(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(paged) != 2 || paged[0].Subject != "second commit" || paged[1].Subject != "initial" {
		t.Fatalf("unexpected paged fixture: %+v", paged)
	}
	if paged[0].LinesAdded != 1 || paged[0].LinesDeleted != 0 {
		t.Fatalf("newly fetched head has wrong statistics: %+v", paged[0])
	}
	if paged[1].LinesAdded != 2 || paged[1].LinesDeleted != 0 {
		t.Fatalf("enlarging the page discarded cached statistics: %+v", paged[1])
	}
}

func TestCommitStatsFailurePreservesCommitHistory(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "one.txt")
	runGit(t, dir, "commit", "-m", "initial")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := session.RecentCommits(context.Background(), 20, 0)
	if err != nil || len(commits) != 1 || commits[0].LinesAdded != 2 {
		t.Fatalf("failed to prepare history: commits=%+v err=%v", commits, err)
	}
	commits = append(commits, Commit{Hash: "0000000000000000000000000000000000000000", Subject: "missing object"})
	result, err := session.(*gitSession).withCommitLineStats(context.Background(), commits)
	if err != nil {
		t.Fatalf("statistics-only failure discarded the accessible history: %v", err)
	}
	if len(result) != 2 || result[0].LinesAdded != 2 || result[0].LinesDeleted != 0 {
		t.Fatalf("a statistics error destroyed the real commit and its existing totals: %+v", result)
	}
	if result[1].LinesAdded != 0 || result[1].LinesDeleted != 0 {
		t.Fatalf("missing object acquired fictitious statistics: %+v", result[1])
	}
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
