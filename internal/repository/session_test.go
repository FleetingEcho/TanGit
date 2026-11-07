package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRepository(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Root != dir {
		t.Errorf("root %q, want %q", snapshot.Root, dir)
	}
	if snapshot.Branch != "main" {
		t.Errorf("branch %q, want main", snapshot.Branch)
	}
	if snapshot.Head != "" {
		t.Errorf("head %q, want no detached HEAD", snapshot.Head)
	}
	if snapshot.ChangeCount() != 1 {
		t.Errorf("change count %d, want 1", snapshot.ChangeCount())
	}
	if snapshot.Files[0].Path != "new.txt" || snapshot.Files[0].Status != "??" {
		t.Errorf("unexpected file snapshot %#v", snapshot.Files[0])
	}
	if snapshot.Files[0].LinesAdded != 1 || snapshot.Files[0].LinesDeleted != 0 {
		t.Errorf("untracked line stats = +%d -%d, want +1 -0",
			snapshot.Files[0].LinesAdded, snapshot.Files[0].LinesDeleted)
	}
}

func TestUntrackedLineStatisticsHandleTextEmptyAndBinaryFiles(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	files := map[string][]byte{
		"text.txt":   []byte(strings.Repeat("added\n", 154)),
		"empty.txt":  {},
		"binary.dat": {0, 1, 2},
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(dir, path), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != len(files) {
		t.Fatalf("snapshot has %d files, want %d: %#v", len(snapshot.Files), len(files), snapshot.Files)
	}
	for _, file := range snapshot.Files {
		want := 0
		if file.Path == "text.txt" {
			want = 154
		}
		if file.LinesAdded != want || file.LinesDeleted != 0 {
			t.Errorf("%s line stats = +%d -%d, want +%d -0",
				file.Path, file.LinesAdded, file.LinesDeleted, want)
		}
	}
}

func TestStageDiffAndUnstage(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := snapshot.Files[0]

	diff, err := session.Diff(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	// A file git does not know is shown as the patch that adds it, so its
	// lines can be staged one by one.
	if !strings.Contains(diff.Patch, "+new file") || diff.Kind != "Untracked" {
		t.Errorf("unexpected untracked diff %#v", diff)
	}

	if err := session.Stage(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file = snapshot.Files[0]
	if !file.Staged || file.Unstaged {
		t.Fatalf("unexpected staged file %#v", file)
	}
	diff, err = session.Diff(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+new file") || diff.Kind != "Staged" {
		t.Errorf("unexpected staged diff %#v", diff)
	}

	if err := session.Unstage(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Files[0].Staged || !snapshot.Files[0].Unstaged {
		t.Errorf("unexpected unstaged file %#v", snapshot.Files[0])
	}
	if err := session.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.UnstageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Files[0].Staged || !snapshot.Files[0].Unstaged {
		t.Errorf("unexpected file after unstage all %#v", snapshot.Files[0])
	}
}

func TestTrackedLineStatisticsSurviveStageAndUnstageAll(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	assertStats := func(stage string, added, deleted int) {
		t.Helper()
		snapshot, err := session.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Files) != 1 {
			t.Fatalf("%s snapshot has %d files, want one", stage, len(snapshot.Files))
		}
		file := snapshot.Files[0]
		if file.LinesAdded != added || file.LinesDeleted != deleted {
			t.Errorf("%s line stats = +%d -%d, want +%d -%d",
				stage, file.LinesAdded, file.LinesDeleted, added, deleted)
		}
	}
	assertStats("unstaged", 2, 1)
	if err := session.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStats("staged", 2, 1)
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// One more unstaged line sits beside the staged patch. Both rows use the
	// file's total change from HEAD rather than either side losing its counts.
	assertStats("staged and unstaged", 3, 1)
	// Reversing two of the staged additions in the worktree must not add the
	// cached and uncached numstats: their edits overlap, so only the final
	// HEAD-to-worktree replacement remains.
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStats("overlapping staged and unstaged", 1, 1)
	if err := session.UnstageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStats("after unstage all", 1, 1)
}

func TestTrackedLineStatisticsDoNotMaskAnUnreadableHead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "initial")
	blobOutput, err := exec.Command("git", "-C", dir, "hash-object", "tracked.txt").Output()
	if err != nil {
		t.Fatal(err)
	}
	blob := strings.TrimSpace(string(blobOutput))
	if err := os.WriteFile(path, []byte("changed\ncontent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// The old blob only exists in HEAD now. Removing it makes HEAD unreadable
	// while HEAD's commit and branch remain present; this is not an unborn
	// branch and must not silently diff against the empty tree.
	if err := os.Remove(filepath.Join(dir, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Snapshot(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "load tracked line statistics") {
		t.Fatalf("an unreadable HEAD was hidden by the empty-tree fallback: %v", err)
	}
}

// ApplyCachedPatch moves only the lines its patch names, forward to stage
// them and backwards to take them out of the index again, and leaves the
// working tree as it was.
func TestApplyCachedPatchStagesAndUnstagesSelectedLines(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "lines.txt")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nFOUR\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// The patch the pane builds for the second line's change, with the fourth
	// line's change left as context.
	patchText := "--- a/lines.txt\n+++ b/lines.txt\n@@ -1,4 +1,4 @@\n one\n-two\n+TWO\n three\n four\n"
	if err := session.ApplyCachedPatch(context.Background(), patchText, false); err != nil {
		t.Fatal(err)
	}
	staged := gitOutput(t, dir, "diff", "--cached", "--", "lines.txt")
	if !strings.Contains(staged, "-two") || !strings.Contains(staged, "+TWO") {
		t.Fatalf("the index misses the selected line: %q", staged)
	}
	if strings.Contains(staged, "FOUR") {
		t.Fatalf("the index took the unselected change: %q", staged)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "one\nTWO\nthree\nFOUR\n" {
		t.Fatalf("the working tree changed: %q %v", content, err)
	}

	if err := session.ApplyCachedPatch(context.Background(), patchText, true); err != nil {
		t.Fatal(err)
	}
	if staged := strings.TrimSpace(gitOutput(t, dir, "diff", "--cached", "--", "lines.txt")); staged != "" {
		t.Fatalf("the reverse left the line staged: %q", staged)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "one\nTWO\nthree\nFOUR\n" {
		t.Fatalf("the reverse changed the working tree: %q %v", content, err)
	}
}

func TestCommitAndAmend(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "initial subject", "initial body", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "amended subject", "amended body", true); err != nil {
		t.Fatal(err)
	}

	message := gitOutput(t, dir, "log", "-1", "--format=%s%n%b")
	if !strings.Contains(message, "amended subject\namended body") {
		t.Errorf("unexpected commit message %q", message)
	}
	count := gitOutput(t, dir, "rev-list", "--count", "HEAD")
	if strings.TrimSpace(count) != "1" {
		t.Errorf("commit count %q, want 1", count)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ChangeCount() != 0 {
		t.Errorf("working tree still has changes: %#v", snapshot.Files)
	}
	if len(snapshot.Commits) != 1 || snapshot.Commits[0].Subject != "amended subject" {
		t.Errorf("unexpected commits %#v", snapshot.Commits)
	}
	if len(snapshot.Reflog) == 0 {
		t.Fatal("expected reflog entries")
	}
	if len(snapshot.Branches) != 1 || snapshot.Branches[0].Name != "main" || !snapshot.Branches[0].Head {
		t.Errorf("unexpected branches %#v", snapshot.Branches)
	}
	files, err := session.CommitFiles(context.Background(), snapshot.Commits[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "file.txt" {
		t.Fatalf("unexpected commit files %#v", files)
	}
	// Amending the root commit still diffs against the empty tree, so the
	// surviving line is wholly added rather than a replacement of the old one.
	if files[0].LinesAdded != 1 || files[0].LinesDeleted != 0 {
		t.Fatalf("commit line stats = +%d -%d, want +1 -0", files[0].LinesAdded, files[0].LinesDeleted)
	}
	diff, err := session.CommitFileDiff(context.Background(), snapshot.Commits[0], files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+second") {
		t.Errorf("unexpected commit diff %q", diff.Patch)
	}
	blame, err := session.BlameFile(context.Background(), File{Path: "file.txt", Tracked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blame, "Desktop Test") || !strings.Contains(blame, "second") {
		t.Fatalf("unexpected blame output %q", blame)
	}
}

func TestBranchCommitsReadWithoutCheckout(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")

	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feature work")
	runGit(t, dir, "checkout", "main")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := session.BranchCommits(context.Background(), "feature", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Subject != "feature work" {
		t.Fatalf("unexpected feature commits %#v", commits)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Branch != "main" {
		t.Errorf("browsing feature checked it out: branch %q", snapshot.Branch)
	}
}

// SearchCommits narrows Git's history by message, author and branch, and
// treats the message as literal text rather than a regular expression.
func TestSearchCommitsFilters(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Ada Lovelace")
	runGit(t, dir, "config", "user.email", "ada@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial setup")

	runGit(t, dir, "checkout", "-b", "feature")
	runGit(t, dir, "config", "user.name", "Grace Hopper")
	runGit(t, dir, "config", "user.email", "grace@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "Feature work")

	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "config", "user.name", "Ada Lovelace")
	runGit(t, dir, "config", "user.email", "ada@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "FIX the README")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := session.SearchCommits(context.Background(), CommitFilter{Query: "fix", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Subject != "FIX the README" {
		t.Fatalf("case-insensitive message search = %#v, want the fix commit", matches)
	}

	// The query is literal: "." would match everything as a regular
	// expression, and must match nothing here.
	dots, err := session.SearchCommits(context.Background(), CommitFilter{Query: ".", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(dots) != 0 {
		t.Fatalf("a literal dot matched %d commits, want none", len(dots))
	}

	byAuthor, err := session.SearchCommits(context.Background(), CommitFilter{Author: "grace", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAuthor) != 1 || byAuthor[0].Subject != "Feature work" {
		t.Fatalf("author search = %#v, want the feature commit", byAuthor)
	}

	onFeature, err := session.SearchCommits(context.Background(), CommitFilter{Branch: "feature", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(onFeature) != 2 || onFeature[0].Subject != "Feature work" {
		t.Fatalf("branch search = %#v, want feature's two commits", onFeature)
	}
	page, err := session.SearchCommits(context.Background(), CommitFilter{Branch: "feature", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Subject != "Feature work" {
		t.Fatalf("a one-commit page = %#v, want the newest commit", page)
	}

	authors, err := session.Authors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantAuthors := []string{"Ada Lovelace", "Grace Hopper"}
	if len(authors) != len(wantAuthors) || authors[0] != wantAuthors[0] || authors[1] != wantAuthors[1] {
		t.Fatalf("authors = %#v, want %#v", authors, wantAuthors)
	}
}

// FileCommits reads a file's own history a page at a time: the skip walks down
// the commits that changed it, newest first, past the commits that did not.
func TestFileCommitsPages(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for i := range 5 {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), fmt.Appendf(nil, "line %d\n", i), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		runGit(t, dir, "commit", "-m", fmt.Sprintf("change %d", i))
	}
	runGit(t, dir, "commit", "--allow-empty", "-m", "unrelated")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.FileCommits(context.Background(), "file.txt", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Subject != "change 4" || first[1].Subject != "change 3" {
		t.Fatalf("first page = %#v, want changes 4 and 3", first)
	}
	second, err := session.FileCommits(context.Background(), "file.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].Subject != "change 2" || second[1].Subject != "change 1" {
		t.Fatalf("second page = %#v, want changes 2 and 1", second)
	}
	last, err := session.FileCommits(context.Background(), "file.txt", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 1 || last[0].Subject != "change 0" {
		t.Fatalf("last page = %#v, want change 0 alone", last)
	}
	beyond, err := session.FileCommits(context.Background(), "file.txt", 2, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(beyond) != 0 {
		t.Fatalf("a page past the history = %#v, want none", beyond)
	}
}

func TestDiscardTrackedAndUntrackedChanges(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	trackedPath := filepath.Join(dir, "tracked.txt")
	if err := os.WriteFile(trackedPath, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(trackedPath, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	untrackedPath := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(untrackedPath, []byte("remove me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Files {
		if err := session.Discard(context.Background(), file); err != nil {
			t.Fatal(err)
		}
	}

	content, err := os.ReadFile(trackedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original\n" {
		t.Errorf("tracked content %q, want original", content)
	}
	if _, err := os.Stat(untrackedPath); !os.IsNotExist(err) {
		t.Errorf("untracked file still exists: %v", err)
	}
}

func TestLocalBranchLifecycle(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CreateBranch(context.Background(), "feature", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "feature work", "", false); err != nil {
		t.Fatal(err)
	}
	if err := session.RenameBranch(context.Background(), "feature", "topic"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Branch != "topic" {
		t.Errorf("current branch %q, want topic", snapshot.Branch)
	}
	if err := session.CheckoutBranch(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	if err := session.MergeBranch(context.Background(), "topic"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Fatalf("merged file missing: %v", err)
	}
	if err := session.DeleteBranch(context.Background(), "topic", false); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Branches) != 1 || snapshot.Branches[0].Name != "main" {
		t.Errorf("unexpected branches %#v", snapshot.Branches)
	}
}

func TestSessionRecordsCommandsInTheLog(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}

	entries := session.CommandLog().Entries()
	if len(entries) == 0 {
		t.Fatal("the session ran Git without recording it")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Command, "git ") {
			t.Errorf("logged command %q is not a Git command", entry.Command)
		}
	}
}

func TestPullBranchFastForwardsAnUncheckedOutBranch(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remote)

	seed := t.TempDir()
	runGit(t, seed, "init", "--initial-branch=main")
	runGit(t, seed, "config", "user.name", "Remote Test")
	runGit(t, seed, "config", "user.email", "remote@example.com")
	runGit(t, seed, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(seed, "shared.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "shared.txt")
	runGit(t, seed, "commit", "-m", "initial")
	runGit(t, seed, "push", "origin", "main")
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")

	runGit(t, seed, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "feature.txt")
	runGit(t, seed, "commit", "-m", "feature work")
	runGit(t, seed, "push", "-u", "origin", "feature")
	runGit(t, seed, "checkout", "main")

	dir := filepath.Join(t.TempDir(), "clone")
	runGit(t, t.TempDir(), "clone", remote, dir)
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "branch", "--track", "feature", "origin/feature")

	// The remote feature gains a commit the clone has not seen; pulling the
	// branch while main is checked out moves only its ref.
	runGit(t, seed, "checkout", "feature")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "feature.txt")
	runGit(t, seed, "commit", "-m", "more feature work")
	runGit(t, seed, "push", "origin", "feature")
	remoteFeature := strings.TrimSpace(gitOutput(t, seed, "rev-parse", "HEAD"))

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.PullBranch(context.Background(), "feature"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/feature")); got != remoteFeature {
		t.Fatalf("feature at %q, want %q", got, remoteFeature)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "symbolic-ref", "--short", "HEAD")); got != "main" {
		t.Fatalf("pull switched to %q, want main", got)
	}

	// Fast-forwarding the checked-out branch takes the working tree along.
	runGit(t, seed, "checkout", "main")
	if err := os.WriteFile(filepath.Join(seed, "shared.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", "shared.txt")
	runGit(t, seed, "commit", "-m", "remote main change")
	runGit(t, seed, "push", "origin", "main")
	remoteMain := strings.TrimSpace(gitOutput(t, seed, "rev-parse", "HEAD"))

	if err := session.FastForwardBranch(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != remoteMain {
		t.Fatalf("main at %q, want %q", got, remoteMain)
	}
	content, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "second\n" {
		t.Fatalf("fast-forward left %q on disk, want second", content)
	}
}

func TestRebaseOntoBranch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "base.txt")
	runGit(t, dir, "commit", "-m", "initial")

	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "feature.txt")
	runGit(t, dir, "commit", "-m", "feature work")

	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "main.txt")
	runGit(t, dir, "commit", "-m", "main work")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RebaseOntoBranch(context.Background(), "feature"); err != nil {
		t.Fatal(err)
	}
	// feature is now an ancestor of main, which is what a rebase onto it
	// means, and main keeps its own commit.
	runGit(t, dir, "merge-base", "--is-ancestor", "feature", "main")
	if got := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s")); got != "main work" {
		t.Fatalf("tip %q, want main work", got)
	}
}

func TestForceCheckoutBranchDiscardsConflictingWork(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feature work")
	runGit(t, dir, "checkout", "main")
	// Uncommitted work in the way of the checkout.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CheckoutBranch(context.Background(), "feature"); err == nil {
		t.Fatal("a plain checkout over dirty work succeeded")
	}
	if err := session.ForceCheckoutBranch(context.Background(), "feature"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "symbolic-ref", "--short", "HEAD")); got != "feature" {
		t.Fatalf("checked out %q, want feature", got)
	}
}

func TestFetchPullAndPush(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remote)

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "shared.txt")
	runGit(t, dir, "commit", "-m", "initial")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if upstream := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "--abbrev-ref", "@{upstream}")); upstream != "origin/main" {
		t.Fatalf("upstream %q, want origin/main", upstream)
	}

	other := t.TempDir()
	runGit(t, other, "init", "--initial-branch=main")
	runGit(t, other, "config", "user.name", "Remote Test")
	runGit(t, other, "config", "user.email", "remote@example.com")
	runGit(t, other, "remote", "add", "origin", remote)
	runGit(t, other, "fetch", "origin", "main")
	runGit(t, other, "reset", "--hard", "origin/main")
	if err := os.WriteFile(filepath.Join(other, "shared.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, other, "add", "shared.txt")
	runGit(t, other, "commit", "-m", "remote change")
	runGit(t, other, "push", "origin", "main")

	if err := session.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "origin/main")), strings.TrimSpace(gitOutput(t, other, "rev-parse", "HEAD")); got != want {
		t.Fatalf("fetched revision %q, want %q", got, want)
	}
	if err := session.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "second\n" {
		t.Fatalf("pulled content %q, want second", content)
	}

	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StageAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "local change", "", false); err != nil {
		t.Fatal(err)
	}
	if err := session.Push(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(gitOutput(t, remote, "rev-parse", "refs/heads/main")), strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != want {
		t.Fatalf("pushed revision %q, want %q", got, want)
	}
}

func TestFetchRemoteLeavesOtherRemoteTrackingRefsUntouched(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	mirror := filepath.Join(t.TempDir(), "mirror.git")
	for _, remote := range []string{origin, mirror} {
		runGit(t, t.TempDir(), "init", "--bare", remote)
	}
	source := t.TempDir()
	runGit(t, source, "init", "--initial-branch=main")
	runGit(t, source, "config", "user.name", "Remote Test")
	runGit(t, source, "config", "user.email", "remote@example.com")
	runGit(t, source, "remote", "add", "origin", origin)
	runGit(t, source, "remote", "add", "mirror", mirror)
	if err := os.WriteFile(filepath.Join(source, "shared.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "shared.txt")
	runGit(t, source, "commit", "-m", "initial")
	runGit(t, source, "push", "origin", "main")
	runGit(t, source, "push", "mirror", "main")
	initial := strings.TrimSpace(gitOutput(t, source, "rev-parse", "HEAD"))

	dir := t.TempDir()
	runGit(t, dir, "clone", "--branch", "main", origin, filepath.Join(dir, "clone"))
	dir = filepath.Join(dir, "clone")
	runGit(t, dir, "remote", "add", "mirror", mirror)
	runGit(t, dir, "fetch", "mirror")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "shared.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "shared.txt")
	runGit(t, source, "commit", "-m", "remote change")
	runGit(t, source, "push", "origin", "main")
	runGit(t, source, "push", "mirror", "main")
	updated := strings.TrimSpace(gitOutput(t, source, "rev-parse", "HEAD"))

	if err := session.FetchRemote(context.Background(), "origin"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "origin/main")); got != updated {
		t.Fatalf("selected remote ref = %q, want %q", got, updated)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "mirror/main")); got != initial {
		t.Fatalf("unselected remote ref = %q, want original %q", got, initial)
	}
}

func TestRemoteLifecycle(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remote)
	source := t.TempDir()
	runGit(t, source, "init", "--initial-branch=main")
	runGit(t, source, "config", "user.name", "Remote Test")
	runGit(t, source, "config", "user.email", "remote@example.com")
	runGit(t, source, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(source, "main.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "main.txt")
	runGit(t, source, "commit", "-m", "initial")
	runGit(t, source, "push", "origin", "main")
	runGit(t, source, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(source, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "feature.txt")
	runGit(t, source, "commit", "-m", "feature")
	runGit(t, source, "push", "origin", "feature")

	parent := t.TempDir()
	dir := filepath.Join(parent, "clone")
	runGit(t, parent, "clone", "--branch", "main", remote, dir)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Remotes) != 1 || snapshot.Remotes[0].Name != "origin" {
		t.Fatalf("unexpected remotes %#v", snapshot.Remotes)
	}
	if !hasRemoteBranch(snapshot.Remotes[0].Branches, "feature") {
		t.Fatalf("feature missing from remote branches %#v", snapshot.Remotes[0].Branches)
	}
	if err := session.CheckoutCommit(context.Background(), Commit{
		Hash: "origin/feature", ShortHash: "origin/feature",
	}); err != nil {
		t.Fatal(err)
	}
	runGitExpectFailure(t, dir, "symbolic-ref", "-q", "HEAD")
	if got, want := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")), strings.TrimSpace(gitOutput(t, dir, "rev-parse", "origin/feature")); got != want {
		t.Fatalf("detached HEAD = %q, want remote branch revision %q", got, want)
	}
	if err := session.CheckoutRemoteBranch(context.Background(), "origin", "feature", "topic"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Branch != "topic" {
		t.Fatalf("branch %q, want topic", snapshot.Branch)
	}
	if err := session.UnsetUpstream(context.Background(), "topic"); err != nil {
		t.Fatal(err)
	}
	runGitExpectFailure(t, dir, "rev-parse", "--abbrev-ref", "topic@{upstream}")
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range snapshot.Branches {
		if branch.Name == "topic" && (branch.UpstreamRemote != "" || branch.UpstreamBranch != "") {
			t.Fatalf("unset upstream = %s/%s", branch.UpstreamRemote, branch.UpstreamBranch)
		}
	}
	if err := session.SetUpstream(context.Background(), "origin", "feature", "topic"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "--abbrev-ref", "topic@{upstream}")); got != "origin/feature" {
		t.Fatalf("upstream = %q, want origin/feature", got)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, branch := range snapshot.Branches {
		if branch.Name == "topic" && (branch.UpstreamRemote != "origin" || branch.UpstreamBranch != "feature") {
			t.Fatalf("snapshot upstream = %s/%s", branch.UpstreamRemote, branch.UpstreamBranch)
		}
	}
	if err := session.AddRemote(context.Background(), "mirror", remote); err != nil {
		t.Fatal(err)
	}
	updatedRemote := filepath.Join(t.TempDir(), "updated.git")
	runGit(t, t.TempDir(), "init", "--bare", updatedRemote)
	if err := session.UpdateRemote(context.Background(), "mirror", "backup", updatedRemote); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "remote", "get-url", "backup")); got != updatedRemote {
		t.Fatalf("updated remote URL = %q, want %q", got, updatedRemote)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snapshot.Remotes {
		if item.Name == "mirror" {
			t.Fatal("the old remote name survived the update")
		}
		if item.Name == "backup" && (len(item.URLs) != 1 || item.URLs[0] != updatedRemote) {
			t.Fatalf("updated snapshot remote = %#v", item)
		}
	}
	if err := session.RemoveRemote(context.Background(), "backup"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "remote")); got != "origin" {
		t.Fatalf("remotes %q, want origin", got)
	}
	if err := session.DeleteRemoteBranch(context.Background(), "origin", "feature"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "ls-remote", "--heads", "origin", "feature")); got != "" {
		t.Fatalf("deleted remote branch remains: %q", got)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "--verify", "topic")); got == "" {
		t.Fatal("deleting the remote branch deleted the local topic branch")
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hasRemoteBranch(snapshot.Remotes[0].Branches, "feature") {
		t.Fatal("the deleted remote branch survived the snapshot refresh")
	}
}

// Stashing the index alone puts the staged files away while the unstaged
// work stays in the tree, as git stash push --staged does.
func TestStashStagedChangesKeepsTheWorkTree(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	staged := filepath.Join(dir, "staged.txt")
	working := filepath.Join(dir, "working.txt")
	for _, path := range []string{staged, working} {
		if err := os.WriteFile(path, []byte("initial\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "staged.txt", "working.txt")
	runGit(t, dir, "commit", "-m", "initial")

	if err := os.WriteFile(staged, []byte("staged line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged.txt")
	if err := os.WriteFile(working, []byte("working line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.StashStagedChanges(context.Background(), "index only"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 1 || !strings.Contains(snapshot.Stashes[0].Name, "index only") {
		t.Fatalf("unexpected stashes %#v", snapshot.Stashes)
	}
	if content, err := os.ReadFile(working); err != nil || string(content) != "working line\n" {
		t.Fatalf("the unstaged work left the tree: %q, err %v", content, err)
	}
	if content, err := os.ReadFile(staged); err != nil || string(content) != "initial\n" {
		t.Fatalf("the staged file stayed in the tree: %q, err %v", content, err)
	}
	if cached := strings.TrimSpace(gitOutput(t, dir, "diff", "--cached", "--name-only")); cached != "" {
		t.Fatalf("the index still holds %q", cached)
	}
	stagedDiff, err := session.StashFileDiff(context.Background(), 0,
		CommitFile{Path: "staged.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stagedDiff.Patch, "+staged line") {
		t.Fatalf("the stash misses the index: %q", stagedDiff.Patch)
	}
	if strings.Contains(stagedDiff.Patch, "+working line") {
		t.Fatalf("the stash took the unstaged work too: %q", stagedDiff.Patch)
	}
}

func TestStashLifecycle(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	tracked := filepath.Join(dir, "tracked.txt")
	untracked := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(tracked, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(tracked, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untracked, []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(context.Background(), "work in progress", true); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 1 || !strings.Contains(snapshot.Stashes[0].Name, "work in progress") {
		t.Fatalf("unexpected stashes %#v", snapshot.Stashes)
	}
	if snapshot.ChangeCount() != 0 {
		t.Fatalf("working tree not clean after stash: %#v", snapshot.Files)
	}
	files, err := session.StashFiles(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	statusByPath := map[string]string{}
	for _, file := range files {
		statusByPath[file.Path] = file.Status
	}
	if statusByPath["tracked.txt"] != "M" || statusByPath["untracked.txt"] != "A" {
		t.Fatalf("unexpected stash files %#v", files)
	}
	trackedDiff, err := session.StashFileDiff(context.Background(), 0,
		CommitFile{Path: "tracked.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trackedDiff.Patch, "+changed") || strings.Contains(trackedDiff.Patch, "+untracked") {
		t.Fatalf("unexpected tracked stash diff %q", trackedDiff.Patch)
	}
	untrackedDiff, err := session.StashFileDiff(context.Background(), 0,
		CommitFile{Path: "untracked.txt", Status: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(untrackedDiff.Patch, "+untracked") || strings.Contains(untrackedDiff.Patch, "+changed") {
		t.Fatalf("unexpected untracked stash diff %q", untrackedDiff.Patch)
	}
	if err := session.RenameStash(context.Background(), 0, "renamed work"); err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyStash(context.Background(), 0, false); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(tracked); err != nil || string(content) != "changed\n" {
		t.Fatalf("applied tracked content %q, err %v", content, err)
	}
	runGit(t, dir, "reset", "--hard", "HEAD")
	if err := os.Remove(untracked); err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyStash(context.Background(), 0, true); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 0 {
		t.Fatalf("stash still present after pop: %#v", snapshot.Stashes)
	}
	runGit(t, dir, "reset", "--hard", "HEAD")
	if err := os.Remove(untracked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracked, []byte("drop me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(context.Background(), "drop me", false); err != nil {
		t.Fatal(err)
	}
	if err := session.DropStash(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 0 {
		t.Fatalf("stash still present after drop: %#v", snapshot.Stashes)
	}
}

// stashFileFixture makes a repository whose stash saves changes to both
// committed files and takes one untracked file along: one.txt and two.txt
// hold "initial" in the committed tree and "saved one"/"saved two" in the
// saved work, and the untracked new.txt holds "saved new" there.
func stashFileFixture(t *testing.T) (Session, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("initial\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "one.txt", "two.txt")
	runGit(t, dir, "commit", "-m", "initial")
	for _, name := range []string{"one.txt", "two.txt", "new.txt"} {
		content := "saved " + strings.TrimSuffix(name, ".txt") + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(context.Background(), "delicate work", true); err != nil {
		t.Fatal(err)
	}
	return session, dir
}

// stashFilesByPath indexes one stash's saved files by their paths.
func stashFilesByPath(t *testing.T, session Session, index int) map[string]CommitFile {
	t.Helper()
	files, err := session.StashFiles(context.Background(), index)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]CommitFile{}
	for _, file := range files {
		byPath[file.Path] = file
	}
	return byPath
}

// ApplyStashFile writes one saved file's version into the working tree and
// leaves the saved work saved: the file it applied is still listed and the
// other saved files land nowhere.
func TestApplyStashFileLeavesTheSavedWorkSaved(t *testing.T) {
	session, dir := stashFileFixture(t)

	files := stashFilesByPath(t, session, 0)
	one, untracked := files["one.txt"], files["new.txt"]
	if one.Path == "" || untracked.Path == "" {
		t.Fatalf("the stash misses the files to apply: %#v", files)
	}

	// A tracked file's saved version lands in the tree, and only it.
	if err := session.ApplyStashFile(context.Background(), 0, one); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "one.txt")); err != nil || string(content) != "saved one\n" {
		t.Fatalf("applied one.txt holds %q, err %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "two.txt")); err != nil || string(content) != "initial\n" {
		t.Fatalf("apply touched two.txt: %q, err %v", content, err)
	}

	// An untracked file rode in its own commit beside the stash: applying
	// it brings the file back rather than taking anything out of the tree.
	if err := session.ApplyStashFile(context.Background(), 0, untracked); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(content) != "saved new\n" {
		t.Fatalf("applied new.txt holds %q, err %v", content, err)
	}

	// Applying is no discard: the saved work still holds every file.
	after := stashFilesByPath(t, session, 0)
	if len(after) != len(files) {
		t.Fatalf("applying thinned the saved work: %#v", after)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 1 {
		t.Fatalf("the saved work is gone: %#v", snapshot.Stashes)
	}
}

// DiscardStashFile rewrites the saved work without one file's change: the
// rest stays saved — tracked and untracked alike — and the entry keeps its
// name.
func TestDiscardStashFileRewritesTheSavedWork(t *testing.T) {
	session, dir := stashFileFixture(t)

	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	name := snapshot.Stashes[0].Name
	if err := session.DiscardStashFile(context.Background(), 0,
		CommitFile{Path: "one.txt", Status: "M"}, name); err != nil {
		t.Fatal(err)
	}

	// The entry keeps its name: the rewrite changed the saved work, not
	// what the user called it.
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 1 || snapshot.Stashes[0].Name != name {
		t.Fatalf("the rewrite renamed or lost the entry: %#v", snapshot.Stashes)
	}

	// Only the discarded file leaves the saved work.
	files := stashFilesByPath(t, session, 0)
	if _, dropped := files["one.txt"]; dropped {
		t.Fatalf("the discarded file is still saved: %#v", files)
	}
	if files["two.txt"].Status != "M" || files["new.txt"].Status != "A" {
		t.Fatalf("the rewrite lost more than one file: %#v", files)
	}

	// Applying what is left brings back the kept changes and nothing else.
	if err := session.ApplyStash(context.Background(), 0, false); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "one.txt")); err != nil || string(content) != "initial\n" {
		t.Fatalf("the discarded change came back: %q, err %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "two.txt")); err != nil || string(content) != "saved two\n" {
		t.Fatalf("the kept change is missing: %q, err %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(content) != "saved new\n" {
		t.Fatalf("the kept untracked file is missing: %q, err %v", content, err)
	}
}

// Discarding the untracked file out of the saved work takes just that file:
// the changed files stay saved and the rest still applies.
func TestDiscardStashFileDropsAnUntrackedFile(t *testing.T) {
	session, dir := stashFileFixture(t)

	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	name := snapshot.Stashes[0].Name
	if err := session.DiscardStashFile(context.Background(), 0,
		CommitFile{Path: "new.txt", Status: "A"}, name); err != nil {
		t.Fatal(err)
	}

	files := stashFilesByPath(t, session, 0)
	if _, dropped := files["new.txt"]; dropped {
		t.Fatalf("the discarded untracked file is still saved: %#v", files)
	}
	if files["one.txt"].Status != "M" || files["two.txt"].Status != "M" {
		t.Fatalf("the rewrite lost more than one file: %#v", files)
	}

	if err := session.ApplyStash(context.Background(), 0, false); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "one.txt")); err != nil || string(content) != "saved one\n" {
		t.Fatalf("the kept change is missing: %q, err %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "two.txt")); err != nil || string(content) != "saved two\n" {
		t.Fatalf("the kept change is missing: %q, err %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the discarded untracked file came back: err %v", err)
	}
}

// DiscardStashFile rewrites the one stash it is given: the other saved work
// keeps its files, and the rewrite rises to the top of the list as any
// written entry does.
func TestDiscardStashFileLeavesOtherStashesAlone(t *testing.T) {
	session, dir := stashFileFixture(t)

	// A second, newer stash on top of the fixture's.
	if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(context.Background(), "second work", false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second := snapshot.Stashes[0].Name
	older := snapshot.Stashes[1].Name
	if !strings.Contains(older, "delicate work") || !strings.Contains(second, "second work") {
		t.Fatalf("unexpected stashes %#v", snapshot.Stashes)
	}

	if err := session.DiscardStashFile(context.Background(), 1,
		CommitFile{Path: "one.txt", Status: "M"}, older); err != nil {
		t.Fatal(err)
	}

	// The rewritten entry rises to the top keeping its name, and the newer
	// stash sits below it untouched.
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Stashes) != 2 || snapshot.Stashes[0].Name != older || snapshot.Stashes[1].Name != second {
		t.Fatalf("the rewrite moved through the list: %#v", snapshot.Stashes)
	}
	rewritten := stashFilesByPath(t, session, 0)
	if _, dropped := rewritten["one.txt"]; dropped {
		t.Fatalf("the discarded file is still saved: %#v", rewritten)
	}
	if rewritten["two.txt"].Status != "M" || rewritten["new.txt"].Status != "A" {
		t.Fatalf("the rewrite lost more than one file: %#v", rewritten)
	}
	untouched := stashFilesByPath(t, session, 1)
	if untouched["one.txt"].Status != "M" {
		t.Fatalf("the other stash was rewritten too: %#v", untouched)
	}
}

func TestTagLifecycle(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remote)
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "remote", "add", "origin", remote)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "release")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CreateTag(context.Background(), "v1.0.0", "HEAD", "", false); err != nil {
		t.Fatal(err)
	}
	if err := session.CreateTag(context.Background(), "v1.1.0", "HEAD", "Version 1.1", true); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tags) != 2 {
		t.Fatalf("unexpected tags %#v", snapshot.Tags)
	}
	if err := session.PushTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, remote, "rev-parse", "refs/tags/v1.0.0")); got == "" {
		t.Fatal("pushed tag missing")
	}
	if err := session.DeleteTag(context.Background(), "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := session.DeleteTag(context.Background(), "v1.1.0"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tags) != 0 {
		t.Fatalf("tags still present after delete: %#v", snapshot.Tags)
	}
}

func TestCherryPickAndRevert(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "base.txt")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "feature.txt")
	runGit(t, dir, "commit", "-m", "feature commit")
	featureHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var feature Commit
	for _, commit := range snapshot.Commits {
		if commit.Hash == featureHash {
			feature = commit
			break
		}
	}
	if feature.Hash == "" {
		t.Fatalf("feature commit missing from history %#v", snapshot.Commits)
	}
	if err := session.CherryPickCommit(context.Background(), feature); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Fatalf("cherry-picked file missing: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RevertCommit(context.Background(), snapshot.Commits[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); !os.IsNotExist(err) {
		t.Fatalf("reverted file still present: %v", err)
	}
	if count := strings.TrimSpace(gitOutput(t, dir, "rev-list", "--count", "main")); count != "3" {
		t.Fatalf("commit count %q, want 3", count)
	}
}

// A commit deeper than the page a snapshot carries can still be
// cherry-picked: the action builds what it needs from the commit itself, not
// from the bounded list the views loaded.
func TestCherryPickOlderThanTheSnapshotPage(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "base.txt")
	runGit(t, dir, "commit", "-m", "base")

	// Build a chain deeper than one page on a feature branch, without one Git
	// process per checkout: commit-tree extends the parent chain directly.
	tree := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^{tree}"))
	parent := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	deepest := ""
	for i := 0; i <= SnapshotCommitLimit; i++ {
		hash := strings.TrimSpace(gitOutput(t, dir, "commit-tree", tree, "-p", parent, "-m", fmt.Sprintf("deep %d", i)))
		if i == 0 {
			deepest = hash
		}
		parent = hash
	}
	runGit(t, dir, "update-ref", "refs/heads/feature", parent)

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, commit := range snapshot.Commits {
		if commit.Hash == deepest {
			t.Fatalf("the deepest commit is inside the snapshot page, so the test cannot prove the fix")
		}
	}
	if err := session.CherryPickCommit(context.Background(), Commit{Hash: deepest, ShortHash: deepest[:7]}); err != nil {
		t.Fatalf("cherry-pick a commit below the snapshot page: %v", err)
	}
	if subject := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s")); subject != "deep 0" {
		t.Fatalf("cherry-pick created %q, want deep 0", subject)
	}
}

// RecentCommits pages across every ref with a skip, and refuses a read with
// no page size.
func TestRecentCommitsPages(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("file%d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", name)
		runGit(t, dir, "commit", "-m", fmt.Sprintf("commit %d", i))
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	all, err := session.RecentCommits(context.Background(), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("read %d commits, want 3", len(all))
	}
	page, err := session.RecentCommits(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].Hash != all[1].Hash {
		t.Fatalf("page 1 is %#v, want commit %s", page, all[1].ShortHash)
	}
	past, err := session.RecentCommits(context.Background(), 5, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(past) != 0 {
		t.Fatalf("a skip past the history read %d commits, want none", len(past))
	}
	if _, err := session.RecentCommits(context.Background(), 0, 0); err == nil {
		t.Fatal("a read with no page size succeeded")
	}
}

func TestResolveContinueAndAbortMerge(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(path, []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "conflict.txt")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(path, []byte("incoming\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "incoming")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(path, []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "current")
	runGitExpectFailure(t, dir, "merge", "feature")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Operation != "merge" || len(snapshot.Files) != 1 || !snapshot.Files[0].HasMergeConflicts {
		t.Fatalf("unexpected merge snapshot %#v", snapshot)
	}
	if err := session.ResolveConflict(context.Background(), snapshot.Files[0], true); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "current\n" {
		t.Fatalf("resolved content %q, err %v", content, err)
	}
	if err := session.ContinueOperation(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Operation != "" {
		t.Fatalf("merge still active after continue: %q", snapshot.Operation)
	}

	runGit(t, dir, "checkout", "-b", "another")
	if err := os.WriteFile(path, []byte("another\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "another")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(path, []byte("main again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "main again")
	runGitExpectFailure(t, dir, "merge", "another")
	if err := session.AbortOperation(context.Background()); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "main again\n" {
		t.Fatalf("content after abort %q, err %v", content, err)
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "feature-worktree")
	if err := session.CreateWorktree(context.Background(), linked, "main", "feature", false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Worktrees) != 2 {
		t.Fatalf("unexpected worktrees %#v", snapshot.Worktrees)
	}
	found := false
	for _, worktree := range snapshot.Worktrees {
		if worktree.Path == linked && worktree.Branch == "feature" && !worktree.Current {
			found = true
		}
	}
	if !found {
		t.Fatalf("linked worktree missing from %#v", snapshot.Worktrees)
	}
	if err := session.DeleteWorktree(context.Background(), linked, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(linked); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Worktrees) != 1 || !snapshot.Worktrees[0].Current {
		t.Fatalf("unexpected worktrees after delete %#v", snapshot.Worktrees)
	}
}

func TestSubmoduleLifecycle(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	submoduleRepo := t.TempDir()
	runGit(t, submoduleRepo, "init", "--initial-branch=main")
	runGit(t, submoduleRepo, "config", "user.name", "Submodule Test")
	runGit(t, submoduleRepo, "config", "user.email", "submodule@example.com")
	if err := os.WriteFile(filepath.Join(submoduleRepo, "library.txt"), []byte("library\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, submoduleRepo, "add", "library.txt")
	runGit(t, submoduleRepo, "commit", "-m", "library")

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "protocol.file.allow", "always")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "root")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AddSubmodule(context.Background(), "library", "vendor/library", submoduleRepo); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Submodules) != 1 || snapshot.Submodules[0].Path != "vendor/library" || snapshot.Submodules[0].URL != submoduleRepo {
		t.Fatalf("unexpected submodules %#v", snapshot.Submodules)
	}
	if err := os.RemoveAll(filepath.Join(dir, "vendor", "library")); err != nil {
		t.Fatal(err)
	}
	if err := session.UpdateSubmodule(context.Background(), "vendor/library"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vendor", "library", "library.txt")); err != nil {
		t.Fatalf("updated submodule content missing: %v", err)
	}
	if err := session.DeleteSubmodule(context.Background(), "vendor/library"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Submodules) != 0 {
		t.Fatalf("submodule still configured after delete %#v", snapshot.Submodules)
	}
}

func hasRemoteBranch(branches []RemoteBranch, name string) bool {
	for _, branch := range branches {
		if branch.Name == name {
			return true
		}
	}
	return false
}

func TestOpenRejectsNonRepository(t *testing.T) {
	_, err := (Opener{}).Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNotRepository) {
		t.Fatalf("error %v, want ErrNotRepository", err)
	}
}

// A folder the picker offers to adopt becomes a repository the opener takes.
func TestInitRepositoryAdoptsAFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Opener{}).Open(context.Background(), dir); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("a plain folder opened as %v, want ErrNotRepository", err)
	}
	if err := (Opener{}).Init(context.Background(), dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("open after init: %v", err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot after init: %v", err)
	}
	if len(snapshot.Files) != 1 || snapshot.Files[0].Path != "notes.txt" {
		t.Fatalf("the initialized repository does not show the folder's file: %+v", snapshot.Files)
	}
}

// conflictedRepository leaves a merge of feature into main stopped on a
// conflict in file.txt.
func conflictedRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "base")

	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feature work")

	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "main work")

	runGitExpectFailure(t, dir, "merge", "feature", "-m", "merge feature")
	return dir
}

func fileNamed(t *testing.T, snapshot Snapshot) File {
	t.Helper()
	for _, file := range snapshot.Files {
		if file.Path == "file.txt" {
			return file
		}
	}
	t.Fatalf("file.txt is not among the changes: %#v", snapshot.Files)
	return File{}
}

// A conflicted merge shows its operation and conflicted file, yields a diff
// with the markers, and continues once the user picks a side.
func TestMergeConflictResolveAndContinue(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Operation != "merge" {
		t.Fatalf("operation %q, want merge", snapshot.Operation)
	}
	file := fileNamed(t, snapshot)
	if !file.HasMergeConflicts || !file.Unstaged {
		t.Fatalf("file.txt = %#v, want an unstaged conflict", file)
	}

	diff, err := session.Diff(context.Background(), file)
	if err != nil {
		t.Fatalf("read the conflicted diff: %v", err)
	}
	if !strings.Contains(diff.Patch, "<<<<<<<") {
		t.Fatalf("the conflicted diff has no markers:\n%s", diff.Patch)
	}

	// The incoming side is feature's version, which continues the merge.
	if err := session.ResolveConflict(context.Background(), file, false); err != nil {
		t.Fatalf("resolve with the incoming side: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file = fileNamed(t, snapshot)
	if file.HasMergeConflicts || !file.Staged {
		t.Fatalf("resolved file.txt = %#v, want it staged and clean", file)
	}
	if err := session.ContinueOperation(context.Background()); err != nil {
		t.Fatalf("continue the merge: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Operation != "" {
		t.Fatalf("operation %q is still in progress", snapshot.Operation)
	}
	if len(snapshot.Files) != 0 {
		t.Fatalf("a finished merge left changes: %#v", snapshot.Files)
	}
	if !strings.Contains(gitOutput(t, dir, "log", "--oneline", "-1"), "merge feature") {
		t.Fatalf("the merge did not commit: %s", gitOutput(t, dir, "log", "--oneline", "-3"))
	}
	if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "feature\n" {
		t.Fatalf("the merge kept %q, want the incoming side", got)
	}
}

// Aborting a conflicted merge restores the branch and the working tree.
func TestMergeConflictAbortRestores(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, _ := session.Snapshot(context.Background()); snapshot.Operation != "merge" {
		t.Fatalf("the merge is not in progress: %+v", snapshot)
	}
	if err := session.AbortOperation(context.Background()); err != nil {
		t.Fatalf("abort the merge: %v", err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Operation != "" {
		t.Fatalf("operation %q is still in progress", snapshot.Operation)
	}
	if len(snapshot.Files) != 0 {
		t.Fatalf("aborting left changes: %#v", snapshot.Files)
	}
	if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "main\n" {
		t.Fatalf("aborting kept %q, want main's version", got)
	}
}

// ResetToCommit keeps the dropped commits' changes staged on a soft reset,
// unstaged on a mixed reset, and throws them away on a hard one.
func TestResetToCommitModes(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for i := range 3 {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), fmt.Appendf(nil, "line %d\n", i), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		runGit(t, dir, "commit", "-m", fmt.Sprintf("change %d", i))
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Commits) != 3 {
		t.Fatalf("commits = %d, want 3", len(snapshot.Commits))
	}
	// The oldest commit is last.
	oldest := snapshot.Commits[2]

	if err := session.ResetToCommit(context.Background(), oldest, ResetSoft); err != nil {
		t.Fatalf("soft reset: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Commits) != 1 {
		t.Fatalf("after a soft reset commits = %d, want 1", len(snapshot.Commits))
	}
	if file := fileNamed(t, snapshot); !file.Staged {
		t.Fatalf("a soft reset did not leave the changes staged: %#v", snapshot.Files)
	}

	if err := session.ResetToCommit(context.Background(), oldest, ResetMixed); err != nil {
		t.Fatalf("mixed reset: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if file := fileNamed(t, snapshot); file.Staged || !file.Unstaged {
		t.Fatalf("a mixed reset did not leave the changes unstaged: %#v", snapshot.Files)
	}

	if err := session.ResetToCommit(context.Background(), oldest, ResetHard); err != nil {
		t.Fatalf("hard reset: %v", err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Commits) != 1 {
		t.Fatalf("after a hard reset commits = %d, want 1", len(snapshot.Commits))
	}
	if len(snapshot.Files) != 0 {
		t.Fatalf("a hard reset left changes: %#v", snapshot.Files)
	}
}

func TestCheckoutCommitDetachesHead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for i := range 2 {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), fmt.Appendf(nil, "line %d\n", i), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		runGit(t, dir, "commit", "-m", fmt.Sprintf("change %d", i))
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	oldest := snapshot.Commits[len(snapshot.Commits)-1]
	if err := session.CheckoutCommit(context.Background(), oldest); err != nil {
		t.Fatal(err)
	}
	if branch := strings.TrimSpace(gitOutput(t, dir, "branch", "--show-current")); branch != "" {
		t.Fatalf("checkout remained on branch %q", branch)
	}
	if head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); head != oldest.Hash {
		t.Fatalf("HEAD = %q, want %q", head, oldest.Hash)
	}
}

// conflictRepository leaves a repository in a merge conflict of the given
// kind: "DU" deletes f.txt on main and changes it on the feature branch, "UD"
// changes it on main and deletes it on feature, and "UU" changes it on both.
func conflictRepository(t *testing.T, kind string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "f.txt")
	runGit(t, dir, "commit", "-m", "base")

	runGit(t, dir, "checkout", "-b", "feature")
	if kind == "UD" {
		runGit(t, dir, "rm", "-q", "f.txt")
		runGit(t, dir, "commit", "-m", "delete")
	} else {
		if err := os.WriteFile(path, []byte("one\nTWO\nthree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "commit", "-am", "feature")
	}

	runGit(t, dir, "checkout", "main")
	switch kind {
	case "DU":
		runGit(t, dir, "rm", "-q", "f.txt")
		runGit(t, dir, "commit", "-m", "delete")
	case "UD":
		if err := os.WriteFile(path, []byte("one\nMAIN\nthree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "commit", "-am", "main")
	case "UU":
		if err := os.WriteFile(path, []byte("one\nMAIN\nthree\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "commit", "-am", "main")
	}
	runGitExpectFailure(t, dir, "merge", "feature", "-m", "merge")
	return dir
}

// conflictFile is the one unmerged file of the conflict repository.
func conflictFile(t *testing.T, snapshot Snapshot) File {
	t.Helper()
	for _, file := range snapshot.Files {
		if file.HasMergeConflicts {
			return file
		}
	}
	t.Fatalf("the snapshot has no conflicted file: %#v", snapshot.Files)
	return File{}
}

// Git marks a delete/modify conflict in the status only: it keeps the surviving
// side in the tree without markers, so the snapshot must say which kind of
// conflict this is and not claim a merged text.
func TestSnapshotReportsAConflictWithoutMarkers(t *testing.T) {
	dir := conflictRepository(t, "DU")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := conflictFile(t, snapshot)
	if file.Status != "DU" || !file.HasMergeConflicts {
		t.Fatalf("conflict file = %#v, want a DU conflict", file)
	}
	if file.HasInlineMergeConflicts {
		t.Fatalf("a delete/modify conflict claims inline markers: %#v", file)
	}
	content, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil || string(content) != "one\nTWO\nthree\n" {
		t.Fatalf("Git did not keep the incoming version: %q, %v", content, err)
	}
}

// The conflict-marker-size attribute changes how many characters Git writes,
// and the pane can only find the markers when the snapshot carries that size.
func TestSnapshotReadsTheConflictMarkerSize(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("f.txt conflict-marker-size=12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gitattributes", "f.txt")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(path, []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "feature")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(path, []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "main")
	runGitExpectFailure(t, dir, "merge", "feature", "-m", "merge")

	if content, err := os.ReadFile(path); err != nil || !strings.Contains(string(content), strings.Repeat("<", 12)) {
		t.Fatalf("Git did not write 12-character markers: %q, %v", content, err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := conflictFile(t, snapshot)
	if !file.HasInlineMergeConflicts || file.ConflictMarkerSize != 12 {
		t.Fatalf("conflict file = %#v, want inline markers of 12", file)
	}
}

// Resolving a delete/modify conflict to the side that deleted the file takes
// the path out of the tree and the index; git checkout --ours cannot do it,
// because that side has no version to check out.
func TestResolveDeleteModifyConflict(t *testing.T) {
	ctx := context.Background()
	path := "f.txt"

	t.Run("keep the incoming change", func(t *testing.T) {
		dir := conflictRepository(t, "DU")
		session, err := (Opener{}).Open(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ResolveConflict(ctx, conflictFile(t, snapshot), false); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil || string(content) != "one\nTWO\nthree\n" {
			t.Fatalf("resolved content = %q, %v", content, err)
		}
		if unmerged := gitOutput(t, dir, "ls-files", "-u"); unmerged != "" {
			t.Fatalf("the file is still unmerged: %q", unmerged)
		}
	})

	t.Run("keep the deletion", func(t *testing.T) {
		dir := conflictRepository(t, "DU")
		session, err := (Opener{}).Open(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ResolveConflict(ctx, conflictFile(t, snapshot), true); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the deleted side kept the file: %v", err)
		}
		if unmerged := gitOutput(t, dir, "ls-files", "-u"); unmerged != "" {
			t.Fatalf("the file is still unmerged: %q", unmerged)
		}
		if status := gitOutput(t, dir, "status", "--porcelain"); strings.TrimSpace(status) != "" {
			t.Fatalf("the resolution left changes: %q", status)
		}
	})

	t.Run("keep the incoming deletion", func(t *testing.T) {
		dir := conflictRepository(t, "UD")
		session, err := (Opener{}).Open(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ResolveConflict(ctx, conflictFile(t, snapshot), false); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the deleted side kept the file: %v", err)
		}
		if unmerged := gitOutput(t, dir, "ls-files", "-u"); unmerged != "" {
			t.Fatalf("the file is still unmerged: %q", unmerged)
		}
		if got := gitOutput(t, dir, "status", "--porcelain"); !strings.HasPrefix(got, "D  f.txt") {
			t.Fatalf("the deletion was not staged: %q", got)
		}
	})
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

func runGitExpectFailure(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("git %s unexpectedly succeeded: %s", strings.Join(args, " "), output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
