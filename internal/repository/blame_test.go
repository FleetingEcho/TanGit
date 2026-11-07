package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlameReadsAllHEADLinesDespiteWorkingTreeChanges(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "First Author")
	runGit(t, dir, "config", "user.email", "first@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\n\tindented\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	first := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	runGit(t, dir, "config", "user.name", "Second Author")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\nchanged\nthird\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "more")
	second := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\nchanged\nthird\nuncommitted fourth\nuncommitted fifth\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := session.BlameFile(context.Background(), File{Path: "file.txt", Tracked: true, Unstaged: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("blame has %d lines, want the three lines at HEAD: %#v", len(lines), lines)
	}
	if lines[0].Hash != first || lines[0].Author != "First Author" || lines[0].Text != "first" {
		t.Fatalf("unexpected unchanged-line attribution: %#v", lines[0])
	}
	if lines[1].Hash != second || lines[1].Author != "Second Author" || lines[1].Text != "changed" ||
		lines[2].Hash != second || lines[2].Text != "third" {
		t.Fatalf("unexpected latest attribution: %#v", lines)
	}
}

func TestParseBlamePorcelainPreservesContentAndAuthorDay(t *testing.T) {
	hash := strings.Repeat("a", 40)
	input := hash + " 8 1 1\n" +
		"author 李 小明\n" +
		"author-mail <li@example.com>\n" +
		"author-time 0\n" +
		"author-tz -0100\n" +
		"summary Preserve source\n" +
		"filename file.txt\n" +
		"\t  leading spaces\tand a tab\n"
	lines, err := parseBlamePorcelain(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("parsed lines = %#v", lines)
	}
	line := lines[0]
	if line.Author != "李 小明" || line.Date != "1969-12-31" || line.Text != "  leading spaces\tand a tab" || line.Hash != hash {
		t.Fatalf("parsed blame = %#v", line)
	}
}
