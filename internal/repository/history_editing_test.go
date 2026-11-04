package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitCommitMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    CommitMessage
	}{
		{name: "subject only", message: "Subject\n", want: CommitMessage{Summary: "Subject"}},
		{
			name:    "subject and body",
			message: "Subject\r\n\r\nFirst paragraph.\r\n\r\nSecond paragraph.\r\n",
			want:    CommitMessage{Summary: "Subject", Description: "First paragraph.\n\nSecond paragraph."},
		},
		{
			name:    "multiline subject uses Git's first-line convention",
			message: "Subject\ncontinuation\n\nBody",
			want:    CommitMessage{Summary: "Subject", Description: "continuation\n\nBody"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := splitCommitMessage(test.message); got != test.want {
				t.Fatalf("splitCommitMessage() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRewordHeadPreservesMessageBodyAndStagedChanges(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "file.txt", "first\n", "original subject\n\noriginal body")

	modified := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(modified, []byte("staged but not part of the reword\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")

	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	head := snapshot.Commits[0]
	message, err := session.CommitMessage(context.Background(), head)
	if err != nil {
		t.Fatal(err)
	}
	if want := (CommitMessage{Summary: "original subject", Description: "original body"}); message != want {
		t.Fatalf("CommitMessage() = %#v, want %#v", message, want)
	}

	if err := session.RewordCommit(context.Background(), head, "new subject", "new body"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%B")); got != "new subject\n\nnew body" {
		t.Fatalf("unexpected reworded message %q", got)
	}
	if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "first\n" {
		t.Fatalf("HEAD absorbed staged content: %q", got)
	}
	if got := gitOutput(t, dir, "diff", "--cached", "--", "file.txt"); !strings.Contains(got, "+staged but not part of the reword") {
		t.Fatalf("staged change was not preserved: %q", got)
	}
}

func TestRewordOlderCommitRewritesHistoryWithoutChangingFiles(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "one.txt", "one\n", "initial")
	writeHistoryCommit(t, dir, "two.txt", "two\n", "middle")
	writeHistoryCommit(t, dir, "three.txt", "three\n", "tip")

	before, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	middle := before.Commits[1]
	oldTip := before.Commits[0].Hash
	if err := session.RewordCommit(context.Background(), middle, "middle reworded", "middle body"); err != nil {
		t.Fatal(err)
	}

	after, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Commits) != 3 {
		t.Fatalf("got %d commits, want 3", len(after.Commits))
	}
	if after.Commits[1].Subject != "middle reworded" || after.Commits[0].Subject != "tip" || after.Commits[2].Subject != "initial" {
		t.Fatalf("unexpected history after reword: %#v", after.Commits)
	}
	if after.Commits[0].Hash == oldTip || after.Commits[2].Hash != before.Commits[2].Hash {
		t.Fatalf("rebase rewrote the wrong range: before %#v, after %#v", before.Commits, after.Commits)
	}
	for _, file := range []string{"one.txt", "two.txt", "three.txt"} {
		want := strings.TrimSuffix(file, ".txt") + "\n"
		content, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || string(content) != want {
			t.Fatalf("%s contains %q, %v; want %q", file, content, err, want)
		}
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "show", "-s", "--format=%B", after.Commits[1].Hash)); got != "middle reworded\n\nmiddle body" {
		t.Fatalf("unexpected older message %q", got)
	}
}

func TestRewordRejectsDetachedHEADAndCommitsOutsideHEAD(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "file.txt", "first\n", "initial")
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	head := snapshot.Commits[0]

	outside := head
	outside.Hash = strings.Repeat("0", 40)
	outside.ShortHash = "0000000"
	if err := session.RewordCommit(context.Background(), outside, "invalid", ""); err == nil || !strings.Contains(err.Error(), "not in current HEAD history") {
		t.Fatalf("reword outside HEAD returned %v", err)
	}

	runGit(t, dir, "checkout", "--detach", "HEAD")
	if err := session.RewordCommit(context.Background(), head, "invalid", ""); err == nil || !strings.Contains(err.Error(), "requires a local branch checkout") {
		t.Fatalf("reword on detached HEAD returned %v", err)
	}
	if got := gitOutput(t, dir, "log", "-1", "--format=%s"); got != "initial\n" {
		t.Fatalf("failed reword changed history: %q", got)
	}
}

func newHistoryEditingSession(t *testing.T) (string, Session) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "History Test")
	runGit(t, dir, "config", "user.email", "history@example.com")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

func writeHistoryCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", name)
	runGit(t, dir, "commit", "-m", message)
}
