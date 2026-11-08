package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
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
