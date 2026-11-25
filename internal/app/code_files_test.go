package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFilterCodePathsMatchesRepositoryPathsCaseInsensitively(t *testing.T) {
	paths := []string{"README.md", "internal/app/main.go", "internal/app/view_code.go"}
	if got, want := filterCodePaths(paths, "VIEW_code"), []string{"internal/app/view_code.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filterCodePaths() = %v, want %v", got, want)
	}
	if got := filterCodePaths(paths, "  "); !reflect.DeepEqual(got, paths) {
		t.Fatalf("blank filter changed paths: %v", got)
	}
	if got := filterCodePaths(paths, "missing"); len(got) != 0 {
		t.Fatalf("unexpected matches: %v", got)
	}
}

func TestFilterCodePathsUsesFzfMatchingAndRelevance(t *testing.T) {
	paths := []string{
		"internal/app/view_code.go",
		"code.go",
		"tests/vc/ode.txt",
		"docs/unrelated.txt",
	}
	if got, want := filterCodePaths(paths, "vcode"), []string{"tests/vc/ode.txt", "internal/app/view_code.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fuzzy path matches = %v, want %v", got, want)
	}
	if got := filterCodePaths(paths, "code"); len(got) == 0 || got[0] != "code.go" {
		t.Fatalf("fzf did not rank the shortest exact basename first: %v", got)
	}
}

func TestValidCodeRelativePath(t *testing.T) {
	for _, path := range []string{"main.go", "internal/app/main.go", "with space/file.txt"} {
		if !validCodeRelativePath(path) {
			t.Errorf("valid path %q rejected", path)
		}
	}
	for _, path := range []string{"", ".", "..", "../outside", "/tmp/outside", "a/../../outside", "a//b", `C:\\outside`} {
		if validCodeRelativePath(path) {
			t.Errorf("invalid path %q accepted", path)
		}
	}
}

func TestListCodeEmptyDirectoriesIncludesExternalFoldersButNotIgnoredOrNonempty(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"empty", "tracked-parent/empty", "ignored/empty", "untracked-nonempty"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directory)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		".gitignore":                  "ignored/\n",
		"tracked-parent/file.txt":     "tracked\n",
		"untracked-nonempty/file.txt": "untracked\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", ".gitignore", "tracked-parent/file.txt"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	if got, want := listCodeEmptyDirectories(root), []string{"empty", "tracked-parent/empty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("listCodeEmptyDirectories() = %v, want %v", got, want)
	}
}

func TestListCodeFilesUsesWorkingTreeAndGitIgnore(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"tracked.go":       "package sample\n",
		"deleted.go":       "package sample\n",
		"untracked.go":     "package sample\n",
		"ignored.go":       "package sample\n",
		"docs/guide.md":    "# Guide\n",
		".gitignore":       "ignored.go\n",
		"nested/empty.txt": "",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "tracked.go", "deleted.go", "docs/guide.md", ".gitignore", "nested/empty.txt"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}
	got, err := listCodeFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "docs/guide.md", "nested/empty.txt", "tracked.go", "untracked.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listCodeFiles() = %v, want %v", got, want)
	}
}

func TestListPlainCodeFilesIncludesRegularFilesAndEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z-last.txt", "README.md", "nested/file.txt", ".git/config", "git-file-parent/.git"} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"empty", "nested/empty-child", "nested/.git"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.txt"), filepath.Join(root, "linked-file.txt")); err != nil {
		t.Fatal(err)
	}

	found, err := listPlainCodeFiles(root)
	files, empty, skipped := found.Paths, found.ExtraDirectories, found.Skipped
	if skipped != 0 {
		t.Fatalf("readable folder reported %d skipped entries, want 0", skipped)
	}
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"README.md", "nested/file.txt", "z-last.txt"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("plain files = %v, want %v", files, want)
	}
	if want := []string{"empty", "nested/empty-child"}; !reflect.DeepEqual(empty, want) {
		t.Fatalf("plain empty directories = %v, want %v", empty, want)
	}
}

func TestListPlainCodeFilesReportsTraversalErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := listPlainCodeFiles(missing)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("listPlainCodeFiles(%q) error = %v, want os.ErrNotExist", missing, err)
	}
}

// An unreadable subtree inside a readable root is skipped, not fatal, and is
// counted so the caller can tell the user.
func TestListPlainCodeFilesSkipsUnreadableSubtrees(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any directory")
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"README.md":       "# Sample\n",
		"src/main.go":     "package sample\n",
		"data/huge/f.bin": "binary\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A sysfs-like unreadable directory inside the folder.
	unreadable := filepath.Join(root, "data", "huge")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	found, err := listPlainCodeFiles(root)
	files, empty, skipped := found.Paths, found.ExtraDirectories, found.Skipped
	if err != nil {
		t.Fatal(err)
	}
	if skipped == 0 {
		t.Fatal("the unreadable subtree was not reported")
	}
	for _, file := range files {
		if strings.HasPrefix(file, "data/huge") {
			t.Fatalf("an unreadable file was listed: %q", file)
		}
	}
	for _, name := range []string{"README.md", "src/main.go"} {
		found := false
		for _, file := range files {
			if file == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("readable file %q missing from %v", name, files)
		}
	}
	_ = empty
}

func TestTruncatedPlainFolderKeepsShallowFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"linuxbrew/cellar/first.txt",
		"linuxbrew/cellar/second.txt",
		"linuxbrew/cellar/third.txt",
		"user/projects/src/main.go",
		"user/notes.txt",
		"welcome.txt",
	} {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	found, err := scanPlainCodeFolder(root, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"linuxbrew/cellar/first.txt", "user/notes.txt", "welcome.txt"}
	if !found.Truncated || !reflect.DeepEqual(found.Paths, want) {
		t.Fatalf("truncated paths = %v, want %v", found.Paths, want)
	}
	rows := buildFileTreeRowsWithDirectories(found.Paths, found.ExtraDirectories, true, nil, codeFileTreeScope)
	if fileTreeDirectoryIndex(rows, "user/projects") < 0 {
		t.Fatalf("the Explorer lost a known directory with unlisted contents: %v", rows)
	}
}

// A folder over the explorer file limit returns a sorted, capped listing.
func TestListPlainCodeFilesStopsAtTheLimit(t *testing.T) {
	root := t.TempDir()
	for index := range plainFileLimit + 3 {
		path := filepath.Join(root, fmt.Sprintf("file-%05d.txt", index))
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	found, err := listPlainCodeFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if !found.Truncated {
		t.Fatal("a folder over the limit was not reported as truncated")
	}
	if len(found.Paths) != plainFileLimit {
		t.Fatalf("paths = %d, want %d", len(found.Paths), plainFileLimit)
	}
	for i := 1; i < len(found.Paths); i++ {
		if found.Paths[i-1] > found.Paths[i] {
			t.Fatalf("the listing is not in lexical order at %d: %q > %q", i, found.Paths[i-1], found.Paths[i])
		}
	}
	t.Logf("indexed the limit in %s", elapsed)
}
