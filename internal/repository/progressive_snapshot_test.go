package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreAndDetailsSnapshotsComposeTheFullSnapshot(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "first")
	runGit(t, dir, "tag", "v1.0.0")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	progressive := session.(ProgressiveSession)
	core, err := progressive.CoreSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if core.Root != dir || core.Branch != "main" || len(core.Files) != 1 {
		t.Fatalf("incomplete core: %+v", core)
	}
	if len(core.Commits) != 0 || len(core.Tags) != 0 || len(core.Branches) != 0 {
		t.Fatalf("core waited for deferred details: %+v", core)
	}
	details, err := progressive.DetailsSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	combined := core.WithDetails(details)
	if len(combined.Commits) != 1 || len(combined.Branches) != 1 || len(combined.Tags) != 1 {
		t.Fatalf("combined snapshot is missing details: %+v", combined)
	}
	if combined.Root != core.Root || len(combined.Files) != 1 || combined.Files[0].Path != "file.txt" {
		t.Fatalf("details replaced the working tree: %+v", combined)
	}
}

func TestRecentReflogBoundsTheGitRead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for _, subject := range []string{"first", "second", "third"} {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(subject+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		runGit(t, dir, "commit", "-m", subject)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	git := session.(*gitSession)
	entries, err := git.recentReflog(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("recentReflog returned %d entries, want 2", len(entries))
	}
	found := false
	for _, entry := range git.CommandLog().Entries() {
		if strings.Contains(entry.Command, "git log -g --max-count=2") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the reflog limit was not passed to Git: %+v", git.CommandLog().Entries())
	}
}
