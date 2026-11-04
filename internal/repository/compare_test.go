package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareRefsListsFilesAndReadsFileDiff(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writeCompareFixture(t, dir, "code.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	writeCompareFixture(t, dir, "space.txt", "left value\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "before")

	runGit(t, dir, "checkout", "-b", "feature")
	writeCompareFixture(t, dir, "code.txt", "one\ntwo changed\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	writeCompareFixture(t, dir, "space.txt", "left   value\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "after")
	runGit(t, dir, "checkout", "main")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	options := DiffOptions{ContextLines: 3}
	files, err := session.CompareFiles(context.Background(), "main", "feature", options)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("comparison files = %#v, want code.txt and space.txt", files)
	}
	if files[0].Path != "code.txt" || files[0].Status != "M" ||
		files[0].LinesAdded != 1 || files[0].LinesDeleted != 1 {
		t.Errorf("code comparison = %#v, want one changed line", files[0])
	}

	diff, err := session.CompareFileDiff(context.Background(), "main", "feature", files[0], options)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Kind != "Compare" || !strings.Contains(diff.Patch, "-two\n+two changed\n") {
		t.Errorf("comparison diff = %#v", diff)
	}

	ignored := DiffOptions{ContextLines: 3, IgnoreWhitespace: true}
	files, err = session.CompareFiles(context.Background(), "main", "feature", ignored)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[1].Path != "space.txt" ||
		files[1].LinesAdded != 0 || files[1].LinesDeleted != 0 {
		t.Fatalf("whitespace-ignoring files = %#v, want space.txt with no changed lines", files)
	}
	space := CommitFile{Path: "space.txt", Status: "M"}
	diff, err = session.CompareFileDiff(context.Background(), "main", "feature", space, ignored)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Patch != "" {
		t.Errorf("whitespace-ignoring patch = %q, want empty", diff.Patch)
	}
}

func TestCompareFileDiffHonorsContextLines(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writeCompareFixture(t, dir, "code.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "before")
	writeCompareFixture(t, dir, "code.txt", "one\ntwo changed\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "after")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	file := CommitFile{Path: "code.txt", Status: "M"}
	zero, err := session.CompareFileDiff(context.Background(), "HEAD~1", "HEAD", file, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(zero.Patch, "@@ -2 +2 @@") {
		t.Errorf("zero-context hunk = %q", zero.Patch)
	}
	if strings.Contains(zero.Patch, "\n one\n") || strings.Contains(zero.Patch, "\n three\n") {
		t.Errorf("zero-context patch includes neighboring lines: %q", zero.Patch)
	}

	three, err := session.CompareFileDiff(context.Background(), "HEAD~1", "HEAD", file, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(three.Patch, "\n one\n") || !strings.Contains(three.Patch, "\n three\n") {
		t.Errorf("three-context patch lacks neighboring lines: %q", three.Patch)
	}
}

func TestCompareRejectsInvalidRefsAndOptions(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "commit", "--allow-empty", "-m", "initial")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := session.CompareFiles(context.Background(), "missing", "HEAD", DiffOptions{ContextLines: 3}); err == nil {
		t.Error("CompareFiles accepted an invalid ref")
	}
	if _, err := session.CompareFileDiff(context.Background(), "HEAD~1", "missing", CommitFile{Path: "file.txt"}, DiffOptions{ContextLines: 3}); err == nil {
		t.Error("CompareFileDiff accepted an invalid ref")
	}
	if _, err := session.CompareFiles(context.Background(), "", "HEAD", DiffOptions{ContextLines: 3}); err == nil {
		t.Error("CompareFiles accepted an empty ref")
	}
	if _, err := session.CompareFiles(context.Background(), "HEAD~1", "HEAD", DiffOptions{ContextLines: -1}); err == nil {
		t.Error("CompareFiles accepted negative context")
	}
}

func writeCompareFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
