package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkingDiffHonorsWhitespaceAndContextOptions(t *testing.T) {
	dir := newDiffOptionsRepository(t)
	writeDiffOptionsFixture(t, dir, "code.txt", "one\ntwo changed\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	writeDiffOptionsFixture(t, dir, "space.txt", "left   value\n")
	session := openDiffOptionsRepository(t, dir)
	ctx := context.Background()

	assertDiffOptionsWhitespace(t, func(options DiffOptions) (Diff, error) {
		return session.Diff(ctx, File{Path: "space.txt", Tracked: true, Unstaged: true}, options)
	})
	assertDiffOptionsContext(t, func(options DiffOptions) (Diff, error) {
		return session.Diff(ctx, File{Path: "code.txt", Tracked: true, Unstaged: true}, options)
	})
}

func TestCommitFileDiffHonorsWhitespaceAndContextOptions(t *testing.T) {
	dir := newDiffOptionsRepository(t)
	writeDiffOptionsFixture(t, dir, "code.txt", "one\ntwo changed\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	writeDiffOptionsFixture(t, dir, "space.txt", "left   value\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "changed")
	session := openDiffOptionsRepository(t, dir)
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Commits) == 0 {
		t.Fatal("changed commit is missing")
	}
	commit := snapshot.Commits[0]
	ctx := context.Background()

	assertDiffOptionsWhitespace(t, func(options DiffOptions) (Diff, error) {
		return session.CommitFileDiff(ctx, commit, CommitFile{Path: "space.txt", Status: "M"}, options)
	})
	assertDiffOptionsContext(t, func(options DiffOptions) (Diff, error) {
		return session.CommitFileDiff(ctx, commit, CommitFile{Path: "code.txt", Status: "M"}, options)
	})
}

func TestStashFileDiffHonorsWhitespaceAndContextOptions(t *testing.T) {
	dir := newDiffOptionsRepository(t)
	writeDiffOptionsFixture(t, dir, "code.txt", "one\ntwo changed\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	writeDiffOptionsFixture(t, dir, "space.txt", "left   value\n")
	runGit(t, dir, "stash", "push", "-m", "saved changes")
	session := openDiffOptionsRepository(t, dir)
	ctx := context.Background()

	assertDiffOptionsWhitespace(t, func(options DiffOptions) (Diff, error) {
		return session.StashFileDiff(ctx, 0, CommitFile{Path: "space.txt", Status: "M"}, options)
	})
	assertDiffOptionsContext(t, func(options DiffOptions) (Diff, error) {
		return session.StashFileDiff(ctx, 0, CommitFile{Path: "code.txt", Status: "M"}, options)
	})
}

func TestUntrackedDiffWorksWithOptions(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "initial")
	writeDiffOptionsFixture(t, dir, "new.txt", "alpha\nbeta\n")
	session := openDiffOptionsRepository(t, dir)

	diff, err := session.Diff(context.Background(), File{Path: "new.txt"}, DiffOptions{
		IgnoreWhitespace: true,
		ContextLines:     0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Kind != "Untracked" {
		t.Errorf("diff kind = %q, want Untracked", diff.Kind)
	}
	if !strings.Contains(diff.Patch, "@@ -0,0 +1,2 @@") ||
		!strings.Contains(diff.Patch, "+alpha\n+beta\n") {
		t.Errorf("untracked patch with options = %q", diff.Patch)
	}
}

func assertDiffOptionsWhitespace(t *testing.T, readDiff func(DiffOptions) (Diff, error)) {
	t.Helper()
	visible, err := readDiff(DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(visible.Patch, "-left value\n+left   value\n") {
		t.Errorf("whitespace-sensitive patch = %q, want the whitespace change", visible.Patch)
	}

	hidden, err := readDiff(DiffOptions{ContextLines: 3, IgnoreWhitespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(hidden.Patch) != "" {
		t.Errorf("whitespace-ignoring patch = %q, want empty", hidden.Patch)
	}
}

func assertDiffOptionsContext(t *testing.T, readDiff func(DiffOptions) (Diff, error)) {
	t.Helper()
	zero, err := readDiff(DiffOptions{ContextLines: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(zero.Patch, "@@ -2 +2 @@") {
		t.Errorf("zero-context patch = %q, want a one-line hunk", zero.Patch)
	}
	if strings.Contains(zero.Patch, "\n one\n") || strings.Contains(zero.Patch, "\n three\n") {
		t.Errorf("zero-context patch includes neighboring lines: %q", zero.Patch)
	}

	three, err := readDiff(DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(three.Patch, "\n one\n") || !strings.Contains(three.Patch, "\n three\n") {
		t.Errorf("three-context patch lacks neighboring lines: %q", three.Patch)
	}
}

func newDiffOptionsRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writeDiffOptionsFixture(t, dir, "code.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	writeDiffOptionsFixture(t, dir, "space.txt", "left value\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func openDiffOptionsRepository(t *testing.T, dir string) Session {
	t.Helper()
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func writeDiffOptionsFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIgnoreAndExcludeHideOnlyUntrackedFiles(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
		exactFile  bool
		apply      func(Session, context.Context, File) error
	}{
		{
			name:       "ignore",
			configPath: ".gitignore",
			exactFile:  true,
			apply:      Session.IgnoreFile,
		},
		{
			name:       "exclude",
			configPath: filepath.Join(".git", "info", "exclude"),
			apply:      Session.ExcludeFile,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--initial-branch=main")
			runGit(t, dir, "config", "user.name", "Desktop Test")
			runGit(t, dir, "config", "user.email", "desktop@example.com")
			writeDiffOptionsFixture(t, dir, "tracked.txt", "tracked\n")
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "-m", "initial")
			writeDiffOptionsFixture(t, dir, "local.tmp", "temporary\n")
			session := openDiffOptionsRepository(t, dir)
			ctx := context.Background()

			if err := test.apply(session, ctx, File{Path: "local.tmp"}); err != nil {
				t.Fatal(err)
			}
			config, err := os.ReadFile(filepath.Join(dir, test.configPath))
			if err != nil {
				t.Fatal(err)
			}
			if test.exactFile && string(config) != "/local.tmp\n" {
				t.Errorf("%s = %q, want %q", test.configPath, config, "/local.tmp\n")
			}
			if !test.exactFile && !strings.HasSuffix(string(config), "/local.tmp\n") {
				t.Errorf("%s = %q, want an appended local.tmp rule", test.configPath, config)
			}
			assertDiffOptionsPathHidden(t, session, "local.tmp")

			if err := test.apply(session, ctx, File{Path: "tracked.txt", Tracked: true}); err == nil {
				t.Errorf("%s accepted a tracked file", test.name)
			}
			if err := test.apply(session, ctx, File{Path: "staged.txt", Staged: true}); err == nil {
				t.Errorf("%s accepted a staged file", test.name)
			}
			config, err = os.ReadFile(filepath.Join(dir, test.configPath))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(config), "tracked.txt") || strings.Contains(string(config), "staged.txt") {
				t.Errorf("rejecting an ineligible file changed %s: %q", test.configPath, config)
			}
		})
	}
}

func assertDiffOptionsPathHidden(t *testing.T, session Session, path string) {
	t.Helper()
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Files {
		if file.Path == path {
			t.Errorf("%s is still listed after its rule was written: %#v", path, file)
		}
	}
}

func TestRestoreCommitFileChangesOnlyTheWorkingTree(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writeDiffOptionsFixture(t, dir, "restore.txt", "old content\n")
	writeDiffOptionsFixture(t, dir, "staged.txt", "initial\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "historical version")
	writeDiffOptionsFixture(t, dir, "restore.txt", "new content\n")
	writeDiffOptionsFixture(t, dir, "absent.txt", "current content\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "current version")

	session := openDiffOptionsRepository(t, dir)
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Commits) < 2 || snapshot.Commits[1].Subject != "historical version" {
		t.Fatalf("historical commit is missing: %#v", snapshot.Commits)
	}
	historical := snapshot.Commits[1]

	writeDiffOptionsFixture(t, dir, "restore.txt", "uncommitted content\n")
	writeDiffOptionsFixture(t, dir, "absent.txt", "uncommitted addition\n")
	writeDiffOptionsFixture(t, dir, "staged.txt", "staged content\n")
	runGit(t, dir, "add", "staged.txt")
	indexBefore := gitOutput(t, dir, "write-tree")

	if err := session.RestoreCommitFile(context.Background(), historical, CommitFile{Path: "restore.txt", Status: "M"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "restore.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old content\n" {
		t.Errorf("restored content = %q, want old content", content)
	}

	if err := session.RestoreCommitFile(context.Background(), historical, CommitFile{Path: "absent.txt", Status: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "absent.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("path absent from the historical commit still exists: %v", err)
	}

	if indexAfter := gitOutput(t, dir, "write-tree"); indexAfter != indexBefore {
		t.Errorf("restoring changed the index: before %q, after %q", indexBefore, indexAfter)
	}
	if staged := gitOutput(t, dir, "show", ":staged.txt"); staged != "staged content\n" {
		t.Errorf("staged content = %q, want it unchanged", staged)
	}
}
