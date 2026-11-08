package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStashPatchSavesAndRemovesOnlyOneHunk(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	base := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\nthirteen\nfourteen\nfifteen\nsixteen\nseventeen\neighteen\nnineteen\ntwenty\n"
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte(base), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "app.txt")
	runGit(t, dir, "commit", "-m", "base")
	edited := strings.NewReplacer("\ntwo\n", "\nTWO\n", "\nfour\n", "\nFOUR\n", "\nsixteen\n", "\nsixteen\nsixteen and a half\n").Replace(base)
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "diff", "--no-color", "--no-ext-diff", "--", "app.txt")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	patch := string(output)
	first := strings.Index(patch, "\n@@")
	second := strings.Index(patch[first+3:], "\n@@")
	if first < 0 || second < 0 {
		t.Fatalf("fixture did not create two hunks:\n%s", patch)
	}
	patch = patch[:first+3+second] + "\n"
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.StashPatch(context.Background(), "first hunk", patch, patch); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "TWO") || strings.Contains(string(content), "FOUR") || !strings.Contains(string(content), "sixteen and a half") {
		t.Fatalf("stash removed more than one hunk:\n%s", content)
	}
	stashes, err := session.Snapshot(context.Background())
	if err != nil || len(stashes.Stashes) != 1 || !strings.Contains(stashes.Stashes[0].Name, "first hunk") {
		t.Fatalf("native stash = %#v, %v", stashes.Stashes, err)
	}
	if cached := gitOutput(t, dir, "diff", "--cached"); cached != "" {
		t.Fatalf("hunk stash touched the index:\n%s", cached)
	}
	// Native stash application deliberately refuses to overwrite a dirty
	// path. Clean the unrelated second hunk before restoring this one.
	runGit(t, dir, "restore", "--worktree", "app.txt")
	if err := session.ApplyStash(context.Background(), 0, false); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	want := strings.NewReplacer("\ntwo\n", "\nTWO\n", "\nfour\n", "\nFOUR\n").Replace(base)
	if err != nil || string(restored) != want {
		t.Fatalf("restored content = %q, want %q (%v)", restored, want, err)
	}
}

func TestStashSelectedFilesLeavesOtherPathsAlone(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "other.txt")
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("other staged\nother unstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashSelectedFiles(ctx, "only a", []File{{Path: "a.txt", Tracked: true, Unstaged: true}}, false); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(content) != "base\n" {
		t.Fatalf("selected file was not stashed: %q", content)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "other.txt")); string(content) != "other staged\nother unstaged\n" {
		t.Fatalf("other file changed: %q", content)
	}
	if cached := gitOutput(t, dir, "show", ":other.txt"); cached != "other\n" {
		t.Fatalf("other index entry changed: %q", cached)
	}
	if err := session.ApplyStash(ctx, 0, false); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(content) != "selected\n" {
		t.Fatalf("selected work did not restore: %q", content)
	}
}
