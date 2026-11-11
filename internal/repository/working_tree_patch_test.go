package repository

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

func workingTreePatchSession(t *testing.T, dir string) *gitSession {
	t.Helper()
	opened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	session, ok := opened.(*gitSession)
	if !ok {
		t.Fatalf("opened session has type %T, want *gitSession", opened)
	}
	return session
}

func workingTreePatchClone(t *testing.T, source string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "target")
	runGit(t, source, "clone", "--quiet", source, target)
	return target
}

func applyWorkingTreeFilePatch(t *testing.T, dir, patch string) {
	t.Helper()
	name := filepath.Join(t.TempDir(), "change.patch")
	if err := os.WriteFile(name, []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "apply", "--check", name)
	runGit(t, dir, "apply", name)
}

func assertWorkingTreeFilePatchReadOnly(t *testing.T, dir, status, index string, content map[string][]byte) {
	t.Helper()
	if got := gitOutput(t, dir, "status", "--porcelain=v1", "-z"); got != status {
		t.Fatalf("patch read changed status\nbefore: %q\nafter:  %q", status, got)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage", "-z"); got != index {
		t.Fatalf("patch read changed index\nbefore: %q\nafter:  %q", index, got)
	}
	for name, want := range content {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("patch read changed %s: got %q, want %q", name, got, want)
		}
	}
}

func TestWorkingTreeFilePatchUsesTheRequestedWholeFileSide(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	base := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	writePatchImportFile(t, dir, "a.txt", []byte(base))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base")

	staged := strings.Replace(base, "two", "STAGED", 1)
	final := strings.Replace(staged, "nine", "WORKTREE", 1)
	writePatchImportFile(t, dir, "a.txt", []byte(staged))
	runGit(t, dir, "add", "a.txt")
	writePatchImportFile(t, dir, "a.txt", []byte(final))
	session := workingTreePatchSession(t, dir)
	statusBefore := gitOutput(t, dir, "status", "--porcelain=v1", "-z")
	indexBefore := gitOutput(t, dir, "ls-files", "--stage", "-z")

	common := []string{
		"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary",
		"--full-index", "--unified=3", "--submodule", "--find-renames=50%",
	}
	tests := []struct {
		name          string
		file          File
		expectedArgs  []string
		targetContent string
		targetIndex   string
	}{
		{
			name:          "staged and unstaged is HEAD to final worktree",
			file:          File{Path: "a.txt", Tracked: true, Staged: true, Unstaged: true},
			expectedArgs:  []string{"HEAD", "--", "a.txt"},
			targetContent: final,
			targetIndex:   base,
		},
		{
			name:          "staged only is HEAD to index",
			file:          File{Path: "a.txt", Tracked: true, Staged: true},
			expectedArgs:  []string{"--cached", "HEAD", "--", "a.txt"},
			targetContent: staged,
			targetIndex:   base,
		},
		{
			name:          "unstaged only is index to worktree",
			file:          File{Path: "a.txt", Tracked: true, Unstaged: true},
			expectedArgs:  []string{"--", "a.txt"},
			targetContent: final,
			targetIndex:   staged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			patch, err := session.WorkingTreeFilePatch(context.Background(), test.file)
			if err != nil {
				t.Fatal(err)
			}
			want := gitOutput(t, dir, append(append([]string(nil), common...), test.expectedArgs...)...)
			if patch != want {
				t.Fatalf("patch selects the wrong sides\nwant:\n%s\ngot:\n%s", want, patch)
			}
			if matched := regexp.MustCompile(`(?m)^index [0-9a-f]{40}\.\.[0-9a-f]{40}( 100644)?$`).MatchString(patch); !matched {
				t.Fatalf("patch does not contain full object IDs:\n%s", patch)
			}
			assertWorkingTreeFilePatchReadOnly(t, dir, statusBefore, indexBefore, map[string][]byte{"a.txt": []byte(final)})

			target := workingTreePatchClone(t, dir)
			if test.targetIndex != base {
				writePatchImportFile(t, target, "a.txt", []byte(test.targetIndex))
				runGit(t, target, "add", "a.txt")
			}
			applyWorkingTreeFilePatch(t, target, patch)
			content, err := os.ReadFile(filepath.Join(target, "a.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != test.targetContent {
				t.Fatalf("applied content = %q, want %q", content, test.targetContent)
			}
			if got := gitOutput(t, target, "show", ":a.txt"); got != test.targetIndex {
				t.Fatalf("applying the patch changed the index: got %q, want %q", got, test.targetIndex)
			}
		})
	}
}

func TestWorkingTreeFilePatchAddsUntrackedTextEmptyBinaryAndSpecialPaths(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	files := map[string][]byte{
		"plain.txt":          []byte("one\ntwo\nthree\n"),
		"empty.txt":          {},
		"binary.dat":         {0, 1, 2, 0, 3},
		"-leading [x]*:.txt": []byte("literal path\n"),
	}
	for name, content := range files {
		writePatchImportFile(t, dir, name, content)
	}
	session := workingTreePatchSession(t, dir)
	statusBefore := gitOutput(t, dir, "status", "--porcelain=v1", "-z")
	indexBefore := gitOutput(t, dir, "ls-files", "--stage", "-z")
	target := t.TempDir()
	runGit(t, target, "init", "--initial-branch=main")

	for name, content := range files {
		t.Run(name, func(t *testing.T) {
			patch, err := session.WorkingTreeFilePatch(context.Background(), File{
				Path: name, Status: "??", Unstaged: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(patch, "new file mode 100644\n") {
				t.Fatalf("untracked patch lacks a new-file header:\n%s", patch)
			}
			if matched := regexp.MustCompile(`(?m)^index 0{40}\.\.[0-9a-f]{40}$`).MatchString(patch); !matched {
				t.Fatalf("untracked patch lacks a full /dev/null-to-file index:\n%s", patch)
			}
			if name != "empty.txt" && name != "binary.dat" && !strings.Contains(patch, "--- /dev/null\n") {
				t.Fatalf("untracked patch does not start at /dev/null:\n%s", patch)
			}
			if name == "empty.txt" && !strings.Contains(patch, "..e69de29bb2d1d6434b8b29ae775ad8c2e48c5391\n") {
				t.Fatalf("empty file does not have its canonical empty-blob addition:\n%s", patch)
			}
			if name == "binary.dat" && !strings.Contains(patch, "GIT binary patch\nliteral 5\n") {
				t.Fatalf("binary file lacks its binary literal:\n%s", patch)
			}
			assertWorkingTreeFilePatchReadOnly(t, dir, statusBefore, indexBefore, files)

			applyWorkingTreeFilePatch(t, target, patch)
			got, err := os.ReadFile(filepath.Join(target, name))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(content) {
				t.Fatalf("applied untracked content = %v, want %v", got, content)
			}
			if staged := gitOutput(t, target, "diff", "--cached"); staged != "" {
				t.Fatalf("applying an untracked patch staged content:\n%s", staged)
			}
		})
	}
}

func TestWorkingTreeFilePatchHandlesTrackedDeletions(t *testing.T) {
	for _, staged := range []bool{false, true} {
		name := "unstaged"
		if staged {
			name = "staged"
		}
		t.Run(name, func(t *testing.T) {
			dir, opened := patchImportRepository(t)
			if staged {
				runGit(t, dir, "rm", "a.txt")
			} else if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
				t.Fatal(err)
			}
			session, ok := opened.(*gitSession)
			if !ok {
				t.Fatalf("opened session has type %T", opened)
			}
			patch, err := session.WorkingTreeFilePatch(context.Background(), File{
				Path: "a.txt", Tracked: true, Staged: staged, Unstaged: !staged, Deleted: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(patch, "deleted file mode 100644\n") || !strings.Contains(patch, "+++ /dev/null\n") {
				t.Fatalf("patch does not delete the whole file:\n%s", patch)
			}
			target := workingTreePatchClone(t, dir)
			applyWorkingTreeFilePatch(t, target, patch)
			if _, err := os.Stat(filepath.Join(target, "a.txt")); !os.IsNotExist(err) {
				t.Fatalf("deleted file still exists after apply, stat error = %v", err)
			}
		})
	}
}

func TestWorkingTreeFilePatchUsesEmptyTreeForUnbornHead(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	writePatchImportFile(t, dir, "new.txt", []byte("staged\n"))
	runGit(t, dir, "add", "new.txt")
	writePatchImportFile(t, dir, "new.txt", []byte("staged\nfinal\n"))
	session := workingTreePatchSession(t, dir)

	patch, err := session.WorkingTreeFilePatch(context.Background(), File{
		Path: "new.txt", Tracked: true, Staged: true, Unstaged: true, Added: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := gitOutput(t, dir, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--unified=3", "--submodule", "--find-renames=50%", models.EmptyTreeCommitHash, "--", "new.txt")
	if patch != want || !strings.Contains(patch, "+staged\n+final\n") {
		t.Fatalf("unborn HEAD patch is not empty-tree-to-final-worktree:\n%s", patch)
	}
	stagedPatch, err := session.WorkingTreeFilePatch(context.Background(), File{
		Path: "new.txt", Tracked: true, Staged: true, Added: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stagedPatch, "+staged\n") || strings.Contains(stagedPatch, "final") {
		t.Fatalf("unborn HEAD staged patch is not empty-tree-to-index:\n%s", stagedPatch)
	}

	target := t.TempDir()
	runGit(t, target, "init", "--initial-branch=main")
	applyWorkingTreeFilePatch(t, target, patch)
	content, err := os.ReadFile(filepath.Join(target, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "staged\nfinal\n" {
		t.Fatalf("applied unborn patch = %q", content)
	}
}

func TestWorkingTreeFilePatchUsesLiteralTrackedPathspecs(t *testing.T) {
	dir, opened := patchImportRepository(t)
	writePatchImportFile(t, dir, "*.txt", []byte("base\n"))
	writePatchImportFile(t, dir, "ordinary.txt", []byte("ordinary base\n"))
	runGit(t, dir, "add", "--", "*.txt", "ordinary.txt")
	runGit(t, dir, "commit", "-m", "literal path")
	writePatchImportFile(t, dir, "*.txt", []byte("changed\n"))
	writePatchImportFile(t, dir, "ordinary.txt", []byte("ordinary changed\n"))
	session := opened.(*gitSession)

	patch, err := session.WorkingTreeFilePatch(context.Background(), File{
		Path: "*.txt", Tracked: true, Unstaged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "-base\n+changed\n") || strings.Contains(patch, "ordinary changed") {
		t.Fatalf("pathspec magic selected more than the literal file:\n%s", patch)
	}
}

func TestWorkingTreeFilePatchRejectsConflictsWithoutMutation(t *testing.T) {
	dir := conflictedRepository(t)
	session := workingTreePatchSession(t, dir)
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := fileNamed(t, snapshot)
	statusBefore := gitOutput(t, dir, "status", "--porcelain=v1", "-z")
	indexBefore := gitOutput(t, dir, "ls-files", "--stage", "-z")
	contentBefore, err := os.ReadFile(filepath.Join(dir, file.Path))
	if err != nil {
		t.Fatal(err)
	}

	patch, err := session.WorkingTreeFilePatch(context.Background(), file)
	if err == nil || !strings.Contains(err.Error(), "resolve merge conflicts") || patch != "" {
		t.Fatalf("conflict patch = %q, error = %v", patch, err)
	}
	assertWorkingTreeFilePatchReadOnly(t, dir, statusBefore, indexBefore, map[string][]byte{file.Path: contentBefore})
}
