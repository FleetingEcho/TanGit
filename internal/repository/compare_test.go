package repository

import (
	"context"
	"fmt"
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

func TestCompareDiffReadsWholePatchAndHonorsWhitespace(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writeCompareFixture(t, dir, "code.txt", "one\ntwo\nthree\n")
	writeCompareFixture(t, dir, "space.txt", "left value\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "before")

	runGit(t, dir, "checkout", "-b", "feature")
	writeCompareFixture(t, dir, "code.txt", "one\ntwo changed\nthree\n")
	writeCompareFixture(t, dir, "space.txt", "left   value\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "after")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := session.CompareDiff(context.Background(), "main", "feature", DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Kind != "Compare" {
		t.Errorf("comparison diff kind = %q, want Compare", diff.Kind)
	}
	for _, want := range []string{
		"diff --git a/code.txt b/code.txt",
		"-two\n+two changed\n",
		"diff --git a/space.txt b/space.txt",
		"-left value\n+left   value\n",
	} {
		if !strings.Contains(diff.Patch, want) {
			t.Errorf("whole comparison patch lacks %q: %q", want, diff.Patch)
		}
	}

	ignored, err := session.CompareDiff(context.Background(), "main", "feature", DiffOptions{ContextLines: 3, IgnoreWhitespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ignored.Patch, "diff --git a/code.txt b/code.txt") {
		t.Errorf("whitespace-ignoring patch lacks code.txt: %q", ignored.Patch)
	}
	if strings.Contains(ignored.Patch, "diff --git a/space.txt b/space.txt") {
		t.Errorf("whitespace-ignoring patch includes space.txt: %q", ignored.Patch)
	}
}

func TestCompareFilesKeepsRenameWarningsOutOfNameStatus(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "diff.renameLimit", "1")
	for i := 1; i <= 4; i++ {
		writeCompareFixture(t, dir, fmt.Sprintf("file%d.txt", i),
			fmt.Sprintf("shared first line\nshared second line\nfile %d\n", i))
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "before")

	runGit(t, dir, "checkout", "-b", "feature")
	for i := 1; i <= 4; i++ {
		runGit(t, dir, "mv", fmt.Sprintf("file%d.txt", i), fmt.Sprintf("renamed%d.txt", i))
		writeCompareFixture(t, dir, fmt.Sprintf("renamed%d.txt", i),
			fmt.Sprintf("shared first line\nchanged second line\nfile %d\n", i))
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "after")
	runGit(t, dir, "checkout", "main")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := session.CompareFiles(context.Background(), "main", "feature", DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 8 {
		t.Fatalf("comparison files = %#v, want four deletions and four additions", files)
	}
	for _, file := range files {
		if file.Status != "A" && file.Status != "D" {
			t.Errorf("stderr contaminated the name-status record: %#v", file)
		}
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

func TestCompareDiffHonorsContextLines(t *testing.T) {
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
	zero, err := session.CompareDiff(context.Background(), "HEAD~1", "HEAD", DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(zero.Patch, "@@ -2 +2 @@") {
		t.Errorf("zero-context hunk = %q", zero.Patch)
	}
	if strings.Contains(zero.Patch, "\n one\n") || strings.Contains(zero.Patch, "\n three\n") {
		t.Errorf("zero-context patch includes neighboring lines: %q", zero.Patch)
	}

	three, err := session.CompareDiff(context.Background(), "HEAD~1", "HEAD", DiffOptions{ContextLines: 3})
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
	if _, err := session.CompareDiff(context.Background(), "missing", "HEAD", DiffOptions{ContextLines: 3}); err == nil {
		t.Error("CompareDiff accepted an invalid ref")
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
