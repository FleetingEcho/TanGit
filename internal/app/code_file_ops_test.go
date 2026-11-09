package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestCodeFileOperationsCleansEntryNames(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "notes.txt", want: "notes.txt"},
		{input: "  release notes.txt \t", want: "release notes.txt"},
		{input: ".gitignore", want: ".gitignore"},
	} {
		got, ok := cleanCodeEntryName(test.input)
		if !ok || got != test.want {
			t.Errorf("cleanCodeEntryName(%q) = %q, %v, want %q, true", test.input, got, ok, test.want)
		}
	}
	for _, name := range []string{"", " \t ", ".", "..", ".git", ".GIT", "a/b", `a\b`, "a\x00b"} {
		if got, ok := cleanCodeEntryName(name); ok || got != "" {
			t.Errorf("cleanCodeEntryName(%q) = %q, %v, want empty, false", name, got, ok)
		}
	}
}

func TestCodeFileOperationsValidatesManagedPaths(t *testing.T) {
	for _, path := range []string{"file.txt", "folder/file.txt", ".github/workflows/test.yml", "folder/.git/config"} {
		if !validCodeManagedPath(path) {
			t.Errorf("valid managed path %q rejected", path)
		}
	}
	for _, path := range []string{"", ".", "..", "../outside", "/tmp/outside", "a/../../outside", "a//b", `a\b`, ".git", ".git/config", ".GIT/config", ".GiT"} {
		if validCodeManagedPath(path) {
			t.Errorf("invalid managed path %q accepted", path)
		}
	}
}

func TestCodeFileOperationsJoinsAndComparesPaths(t *testing.T) {
	if got, want := joinCodePath("", "file.txt"), "file.txt"; got != want {
		t.Fatalf("joinCodePath at root = %q, want %q", got, want)
	}
	if got, want := joinCodePath("docs/guide", "file.txt"), "docs/guide/file.txt"; got != want {
		t.Fatalf("joinCodePath below root = %q, want %q", got, want)
	}
	for _, test := range []struct {
		path      string
		directory string
		want      bool
	}{
		{path: "docs/guide/file.txt", directory: "", want: true},
		{path: "../outside", directory: "", want: false},
		{path: "docs", directory: "docs", want: true},
		{path: "docs/file.txt", directory: "docs", want: true},
		{path: "docs-old/file.txt", directory: "docs", want: false},
		{path: "docs", directory: "docs/file.txt", want: false},
		{path: ".git/config", directory: "", want: false},
		{path: "docs/file.txt", directory: "../outside", want: false},
	} {
		if got := codePathWithin(test.path, test.directory); got != test.want {
			t.Errorf("codePathWithin(%q, %q) = %v, want %v", test.path, test.directory, got, test.want)
		}
	}
}

func TestCodeFileOperationsCreatesFileAndFolderWithoutClobbering(t *testing.T) {
	root := t.TempDir()
	if err := performCodeFileOperation(root, codeFileCreateFile, "", "notes.txt"); err != nil {
		t.Fatalf("create file: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatalf("created file has mode %v and size %d", info.Mode(), info.Size())
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := performCodeFileOperation(root, codeFileCreateFile, "", "notes.txt"); err == nil {
		t.Fatal("duplicate file creation succeeded")
	}
	assertCodeFileContent(t, root, "notes.txt", "keep")

	if err := performCodeFileOperation(root, codeFileCreateFolder, "", "docs"); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := performCodeFileOperation(root, codeFileCreateFolder, "", "docs"); err == nil {
		t.Fatal("duplicate folder creation succeeded")
	}
	assertCodeFileContent(t, root, "docs/keep.txt", "keep")
}

func TestCodeFileOperationsRenamesAndMovesFileWithoutClobbering(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "source/original.txt", "content")
	if err := os.Mkdir(filepath.Join(root, "destination"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := performCodeFileOperation(root, codeFileRename, "source/original.txt", "source/renamed.txt"); err != nil {
		t.Fatalf("rename file: %v", err)
	}
	assertCodeFileMissing(t, root, "source/original.txt")
	assertCodeFileContent(t, root, "source/renamed.txt", "content")

	if err := performCodeFileOperation(root, codeFileMove, "source/renamed.txt", "destination/moved.txt"); err != nil {
		t.Fatalf("move file: %v", err)
	}
	assertCodeFileMissing(t, root, "source/renamed.txt")
	assertCodeFileContent(t, root, "destination/moved.txt", "content")

	writeCodeTestFile(t, root, "source/keep.txt", "source")
	writeCodeTestFile(t, root, "destination/keep.txt", "destination")
	if err := performCodeFileOperation(root, codeFileMove, "source/keep.txt", "destination/keep.txt"); err == nil {
		t.Fatal("move onto an existing file succeeded")
	}
	assertCodeFileContent(t, root, "source/keep.txt", "source")
	assertCodeFileContent(t, root, "destination/keep.txt", "destination")
}

func TestCodeFileOperationsRenamesFileCaseOnly(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "notes.txt", "keep")
	if err := performCodeFileOperation(root, codeFileRename, "notes.txt", "NOTES.txt"); err != nil {
		t.Fatalf("case-only rename: %v", err)
	}
	assertCodeFileMissing(t, root, "notes.txt")
	assertCodeFileContent(t, root, "NOTES.txt", "keep")

	writeCodeTestFile(t, root, "other.txt", "source")
	writeCodeTestFile(t, root, "OTHER.txt", "destination")
	if err := performCodeFileOperation(root, codeFileRename, "other.txt", "OTHER.txt"); err == nil {
		t.Fatal("rename onto a different case-distinct file succeeded")
	}
	assertCodeFileContent(t, root, "other.txt", "source")
	assertCodeFileContent(t, root, "OTHER.txt", "destination")
}

func TestCodeFileOperationsRejectsSymlinkedParents(t *testing.T) {
	for _, test := range []struct {
		name           string
		kind           codeFileOperationKind
		source         string
		destination    string
		missingOutside string
		missingRoot    string
	}{
		{
			name:           "create file destination",
			kind:           codeFileCreateFile,
			destination:    "linked/created.txt",
			missingOutside: "created.txt",
		},
		{
			name:           "create folder destination",
			kind:           codeFileCreateFolder,
			destination:    "linked/created-folder",
			missingOutside: "created-folder",
		},
		{
			name:        "rename source",
			kind:        codeFileRename,
			source:      "linked/keep.txt",
			destination: "renamed.txt",
			missingRoot: "renamed.txt",
		},
		{
			name:        "move source",
			kind:        codeFileMove,
			source:      "linked/keep.txt",
			destination: "moved.txt",
			missingRoot: "moved.txt",
		},
		{
			name:           "move destination",
			kind:           codeFileMove,
			source:         "safe.txt",
			destination:    "linked/moved.txt",
			missingOutside: "moved.txt",
		},
		{
			name:        "delete source",
			kind:        codeFileDelete,
			source:      "linked/keep.txt",
			destination: "linked/keep.txt",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			writeCodeTestFile(t, outside, "keep.txt", "keep")
			if test.source == "safe.txt" {
				writeCodeTestFile(t, root, test.source, "safe")
			}
			if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
				t.Fatal(err)
			}

			if err := performCodeFileOperation(root, test.kind, test.source, test.destination); err == nil {
				t.Fatal("operation through a symlinked parent succeeded")
			}
			assertCodeFileContent(t, outside, "keep.txt", "keep")
			if test.missingOutside != "" {
				assertCodeFileMissing(t, outside, test.missingOutside)
			}
			if test.missingRoot != "" {
				assertCodeFileMissing(t, root, test.missingRoot)
			}
			if test.source == "safe.txt" {
				assertCodeFileContent(t, root, test.source, "safe")
			}
			info, err := os.Lstat(filepath.Join(root, "linked"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("parent symlink was removed or replaced")
			}
		})
	}
}

func TestCodeFileOperationsMovesPopulatedDirectory(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "tree/nested/file.txt", "nested content")
	writeCodeTestFile(t, root, "tree/root.txt", "root content")
	if err := os.Mkdir(filepath.Join(root, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := performCodeFileOperation(root, codeFileMove, "tree", "archive/tree"); err != nil {
		t.Fatalf("move populated directory: %v", err)
	}
	assertCodeFileMissing(t, root, "tree")
	assertCodeFileContent(t, root, "archive/tree/nested/file.txt", "nested content")
	assertCodeFileContent(t, root, "archive/tree/root.txt", "root content")
}

func TestCodeFileOperationsDeletesFileAndDirectoryRecursively(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "remove.txt", "remove")
	if err := performCodeFileOperation(root, codeFileDelete, "remove.txt", "remove.txt"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	assertCodeFileMissing(t, root, "remove.txt")

	writeCodeTestFile(t, root, "trash/nested/deep/file.txt", "remove")
	writeCodeTestFile(t, root, "trash/other.txt", "remove")
	if err := performCodeFileOperation(root, codeFileDelete, "trash", "trash"); err != nil {
		t.Fatalf("delete directory recursively: %v", err)
	}
	assertCodeFileMissing(t, root, "trash")
}

func TestCodeFileOperationsRejectsUnsafePaths(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "safe.txt", "safe")
	for _, destination := range []string{"", ".git/config", ".GIT/file", "../outside", "/tmp/outside", "a/../../outside", `a\b`} {
		if err := performCodeFileOperation(root, codeFileCreateFile, "", destination); err == nil {
			t.Errorf("created file at invalid destination %q", destination)
		}
	}
	for _, source := range []string{".git/config", ".GIT/file", "../outside", "/tmp/outside", "a/../../outside", `a\b`} {
		if err := performCodeFileOperation(root, codeFileRename, source, "renamed.txt"); err == nil {
			t.Errorf("renamed invalid source %q", source)
		}
	}
	assertCodeFileContent(t, root, "safe.txt", "safe")
	if err := performCodeFileOperation(root, codeFileOperationKind(255), "safe.txt", "unused.txt"); err == nil {
		t.Fatal("unknown operation kind succeeded")
	}
}

func TestCodeFileOperationsRejectsSymlinkSources(t *testing.T) {
	for _, test := range []struct {
		name string
		kind codeFileOperationKind
	}{
		{name: "rename", kind: codeFileRename},
		{name: "move", kind: codeFileMove},
		{name: "delete", kind: codeFileDelete},
	} {
		t.Run(test.name, func(t *testing.T) {
			kind := test.kind
			root := t.TempDir()
			writeCodeTestFile(t, root, "target.txt", "keep")
			if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
				t.Fatal(err)
			}
			destination := "new.txt"
			if kind == codeFileDelete {
				destination = "link.txt"
			}
			if err := performCodeFileOperation(root, kind, "link.txt", destination); err == nil {
				t.Fatal("operation on symlink succeeded")
			}
			info, err := os.Lstat(filepath.Join(root, "link.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("source symlink was removed or replaced")
			}
			assertCodeFileContent(t, root, "target.txt", "keep")
			if kind != codeFileDelete {
				assertCodeFileMissing(t, root, destination)
			}
		})
	}
}

func TestCodeFileOperationsRejectsMovingDirectoryIntoItsOwnSubtree(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "tree/nested/file.txt", "keep")
	if err := os.Mkdir(filepath.Join(root, "tree", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"tree/nested/moved", "tree/empty/moved"} {
		if err := performCodeFileOperation(root, codeFileMove, "tree", destination); err == nil {
			t.Errorf("move into own subtree at %q succeeded", destination)
		}
	}
	assertCodeFileContent(t, root, "tree/nested/file.txt", "keep")
}

func writeCodeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCodeFileContent(t *testing.T, root, name, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != want {
		t.Fatalf("%s contains %q, want %q", name, got, want)
	}
}

func assertCodeFileMissing(t *testing.T, root, name string) {
	t.Helper()
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s still exists or returned an unexpected error: %v", name, err)
	}
}
