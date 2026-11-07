package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Working-tree hunk application neither stages unrelated changes nor touches
// the index. The same captured hunk can subsequently be staged on its own.
func TestPartialPatchesAddressWorktreeAndIndexIndependently(t *testing.T) {
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
	edited := strings.NewReplacer("two", "TWO", "four", "FOUR", "sixteen", "sixteen\nsixteen and a half").Replace(base)
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "diff", "--no-color", "--no-ext-diff", "--", "app.txt")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	firstHunk := string(output)
	if first := strings.Index(firstHunk, "\n@@"); first >= 0 {
		if second := strings.Index(firstHunk[first+3:], "\n@@"); second >= 0 {
			firstHunk = firstHunk[:first+3+second] + "\n"
		}
	}
	if !strings.Contains(firstHunk, "+TWO") || strings.Contains(firstHunk, "sixteen and a half") {
		t.Fatalf("test did not isolate the first hunk:\n%s", firstHunk)
	}

	ctx := context.Background()
	session, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyWorkingTreePatch(ctx, firstHunk, true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "+TWO") || strings.Contains(string(content), "TWO") || !strings.Contains(string(content), "sixteen and a half") {
		t.Fatalf("discard changed more than the first hunk:\n%s", content)
	}
	cached := exec.Command("git", "diff", "--cached", "--", "app.txt")
	cached.Dir = dir
	cachedOutput, err := cached.Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(cachedOutput) != 0 {
		t.Fatalf("working-tree patch changed the index:\n%s", cachedOutput)
	}

	if err := session.ApplyCachedPatch(ctx, firstHunk, false); err != nil {
		t.Fatal(err)
	}
	cached = exec.Command("git", "diff", "--cached", "--", "app.txt")
	cached.Dir = dir
	cachedOutput, err = cached.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cachedOutput), "+TWO") || strings.Contains(string(cachedOutput), "sixteen and a half") {
		t.Fatalf("cached patch staged the wrong content:\n%s", cachedOutput)
	}
}
