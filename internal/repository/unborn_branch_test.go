package repository

import (
	"context"
	"lazygitapp/internal/sshintegration"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnbornBranchHistoryIsEmpty(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	check := func(branch string) {
		t.Helper()
		commits, err := session.BranchCommits(context.Background(), branch, 20)
		if err != nil || len(commits) != 0 {
			t.Fatalf("branch %q commits=%v err=%v", branch, commits, err)
		}
		for _, query := range []string{"", "initial", "deadbeef"} {
			commits, err = session.SearchCommits(context.Background(), CommitFilter{Branch: branch, Query: query, Limit: 20})
			if err != nil || len(commits) != 0 {
				t.Fatalf("search %q on %q commits=%v err=%v", query, branch, commits, err)
			}
		}
	}
	for _, branch := range []string{"main", "refs/heads/main", "HEAD"} {
		check(branch)
	}
	if _, err := session.SearchCommits(context.Background(), CommitFilter{Branch: "missing", Limit: 20}); err == nil {
		t.Fatal("missing branch error hidden")
	}
	// Existing history on another branch must remain browsable from an orphan.
	runGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "first")
	runGit(t, dir, "checkout", "--orphan", "orphan")
	check("orphan")
	commits, err := session.BranchCommits(context.Background(), "main", 20)
	if err != nil || len(commits) != 1 {
		t.Fatalf("existing branch history=%v err=%v", commits, err)
	}
	runGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/broken")
	if err := os.WriteFile(filepath.Join(dir, ".git", "refs", "heads", "broken"), []byte(strings.Repeat("1", 40)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.SearchCommits(context.Background(), CommitFilter{Branch: "broken", Limit: 20}); err == nil {
		t.Fatal("corrupt ref error hidden")
	}
	if _, err := session.BranchCommits(context.Background(), "broken", 20); err == nil {
		t.Fatal("corrupt branch error hidden")
	}

}

func TestSSHUnbornBranchHistoryIsEmpty(t *testing.T) {
	server := sshintegration.Start(t)
	connector, err := sshworkspace.NewConnector(server.SecretStore, filepath.Join(t.TempDir(), "known_hosts"), func(sshworkspace.HostKeyInfo) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=master")
	session, err := OpenSSHWorkspace(ctx, client, workspace.Location{Kind: workspace.SSH, ProfileID: server.Profile.ID, Root: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := session.BranchCommits(ctx, "master", 20)
	if err != nil || len(commits) != 0 {
		t.Fatalf("remote unborn history=%v err=%v", commits, err)
	}
	commits, err = session.SearchCommits(ctx, CommitFilter{Branch: "master", Query: "first", Limit: 20})
	if err != nil || len(commits) != 0 {
		t.Fatalf("remote unborn search=%v err=%v", commits, err)
	}
	if _, err := session.SearchCommits(ctx, CommitFilter{Branch: "missing", Limit: 20}); err == nil {
		t.Fatal("remote missing branch error hidden")
	}
}
