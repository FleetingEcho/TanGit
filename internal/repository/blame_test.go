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
	if lines[0].Hash != first || lines[0].Author != "First Author" || lines[0].Text != "first" ||
		lines[0].CommitPath != "file.txt" || lines[0].CommitLine != 1 {
		t.Fatalf("unexpected unchanged-line attribution: %#v", lines[0])
	}
	if lines[1].Hash != second || lines[1].Author != "Second Author" || lines[1].Text != "changed" ||
		lines[1].CommitPath != "file.txt" || lines[1].CommitLine != 2 ||
		lines[2].Hash != second || lines[2].Text != "third" || lines[2].CommitLine != 3 {
		t.Fatalf("unexpected latest attribution: %#v", lines)
	}

	workingTree, err := session.BlameWorkingTreeFile(context.Background(), "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(workingTree) != 5 || workingTree[0].Hash != first || workingTree[1].Hash != second ||
		workingTree[2].Hash != second || workingTree[3].Text != "uncommitted fourth" ||
		workingTree[4].Text != "uncommitted fifth" {
		t.Fatalf("working-tree attribution = %#v", workingTree)
	}
	if workingTree[0].Subject != "initial" || workingTree[1].Subject != "more" ||
		workingTree[0].AuthorEmail != "first@example.com" || workingTree[0].UnixTimestamp <= 0 ||
		!workingTree[0].Current || !workingTree[1].Current {
		t.Fatalf("working-tree commit metadata = %#v", workingTree[:2])
	}
	for _, line := range workingTree[3:] {
		if strings.Trim(line.Hash, "0") != "" || line.Author != "Not Committed Yet" {
			t.Fatalf("uncommitted line has committed identity: %#v", line)
		}
	}
}

func TestBlamePreservesCommitCoordinatesAcrossRenameAndLineShift(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Author")
	runGit(t, dir, "config", "user.email", "author@example.com")
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("first\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "old.txt")
	runGit(t, dir, "commit", "-m", "initial")
	first := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "mv", "old.txt", "new.txt")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("inserted\nfirst\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "new.txt")
	runGit(t, dir, "commit", "-m", "rename and insert")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := session.BlameWorkingTreeFile(context.Background(), "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || lines[0].CommitPath != "new.txt" || lines[0].CommitLine != 1 {
		t.Fatalf("inserted-line coordinates = %#v", lines)
	}
	for index, line := range lines[1:] {
		if line.Hash != first || line.CommitPath != "old.txt" || line.CommitLine != index+1 {
			t.Fatalf("line %d lost its pre-rename coordinates: %#v", index+2, line)
		}
	}
}

func TestBlameWorkingTreeFileAttributesUntrackedAndUnbornFiles(t *testing.T) {
	for _, committedHead := range []bool{false, true} {
		t.Run(map[bool]string{false: "unborn", true: "untracked"}[committedHead], func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "--initial-branch=main")
			runGit(t, dir, "config", "user.name", "Author")
			runGit(t, dir, "config", "user.email", "author@example.com")
			if committedHead {
				if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("committed\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				runGit(t, dir, "add", "tracked.txt")
				runGit(t, dir, "commit", "-m", "initial")
			}
			if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("first\r\nsecond\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			session, err := (Opener{}).Open(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			lines, err := session.BlameWorkingTreeFile(context.Background(), "new.txt")
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) < 2 || lines[0].Text != "first" || lines[1].Text != "second" {
				t.Fatalf("uncommitted lines = %#v", lines)
			}
			for _, line := range lines[:2] {
				if strings.Trim(line.Hash, "0") != "" || line.Author != "Not Committed Yet" {
					t.Fatalf("new line has committed identity: %#v", line)
				}
			}
		})
	}
}

func TestParseBlamePorcelainPreservesContentAndAuthorDay(t *testing.T) {
	hash := strings.Repeat("a", 40)
	input := hash + " 8 1 2\n" +
		"author 李 小明\n" +
		"author-mail <li@example.com>\n" +
		"author-time 0\n" +
		"author-tz -0100\n" +
		"summary Preserve source\n" +
		"filename file.txt\n" +
		"\t  leading spaces\tand a tab\n" +
		hash + " 9 2\n" +
		"\tsecond\n"
	lines, err := parseBlamePorcelain(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("parsed lines = %#v", lines)
	}
	line := lines[0]
	if line.Author != "李 小明" || line.Date != "1969-12-31" || line.Text != "  leading spaces\tand a tab" || line.Hash != hash {
		t.Fatalf("parsed blame = %#v", line)
	}
	if line.CommitPath != "file.txt" || line.CommitLine != 8 || lines[1].CommitPath != "file.txt" || lines[1].CommitLine != 9 {
		t.Fatalf("blamed commit coordinates were not retained: %#v", lines)
	}
}
