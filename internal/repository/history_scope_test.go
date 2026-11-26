package repository

import (
	"context"
	"strings"
	"testing"
)

func TestHistoryReadsExcludeInternalSnapshotRefs(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Human Author")
	runGit(t, dir, "config", "user.email", "human@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "ordinary work")
	ordinaryHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	runGit(t, dir, "checkout", "--detach")
	runGit(t, dir, "config", "user.name", "Internal Snapshot")
	runGit(t, dir, "config", "user.email", "snapshot@example.invalid")
	runGit(t, dir, "commit", "--allow-empty", "-m", "private recovery snapshot")
	internalHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "update-ref", "refs/tangit/test-snapshot", internalHash)
	runGit(t, dir, "checkout", "main")

	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	commits, err := session.RecentCommits(ctx, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Hash != ordinaryHash {
		t.Fatalf("RecentCommits exposed internal refs: %#v", commits)
	}
	for _, query := range []string{"private recovery snapshot", internalHash} {
		matches, err := session.SearchCommits(ctx, CommitFilter{Query: query, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Errorf("SearchCommits(%q) exposed internal refs: %#v", query, matches)
		}
	}
	authors, err := session.Authors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || authors[0] != "Human Author" {
		t.Fatalf("Authors included internal snapshot authors: %#v", authors)
	}
}
