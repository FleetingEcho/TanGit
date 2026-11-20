package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUndoFirstCommitRestoresUnbornHeadAndStagedFiles(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "TanGit Test")
	runGit(t, dir, "config", "user.email", "tangit@example.invalid")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("first staged payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "first.txt")
	index := gitOutput(t, dir, "ls-files", "--stage")
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := session.Commit(ctx, "Initial protected commit", "", false); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if record.Kind != "commit" || record.HeadBefore != "" || !record.CanUndo() {
		t.Fatalf("initial commit recovery plan is missing or unsafe: %+v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo initial commit: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/main").Run(); err == nil {
		t.Fatal("undo left the first commit's branch ref in place")
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "symbolic-ref", "HEAD")); got != "refs/heads/main" {
		t.Fatalf("Undo changed the unborn branch identity: %s", got)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage"); got != index {
		t.Fatalf("Undo did not restore the initial staged index:\n%s\nwant\n%s", got, index)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "first.txt")); string(got) != "first staged payload\n" {
		t.Fatalf("Undo changed the initial file payload: %q", got)
	}
	if final := onlyOperation(t, session); final.UndoneAt == 0 || final.AfterSnapshot == nil {
		t.Fatal("Undo consumed the durable operation history instead of recording completion")
	}
}

func TestUndoConflictedMergeRestoresItsStartingWorkspace(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	base := []byte("one\ntwo\nthree\nfour\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), base, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "merge undo base")
	runGit(t, dir, "checkout", "-b", "incoming")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("incoming\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "incoming edit")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "outgoing edit")
	before := gitOutput(t, dir, "rev-parse", "HEAD")
	beforeContent, readErr := os.ReadFile(filepath.Join(dir, "a.txt"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := session.MergeBranch(ctx, "incoming"); err == nil {
		t.Fatal("merge fixture did not produce a conflict")
	}
	state, err := session.(*gitSession).OperationState(ctx)
	if err != nil || state.Type != OperationTypeMerge {
		t.Fatalf("merge did not leave operation metadata: %q, %v", state.Type, err)
	}
	record := onlyOperation(t, session)
	if record.Kind != "merge" || !record.Partial || !record.CanUndo() {
		t.Fatalf("conflicted merge lacks a recoverable partial state: %+v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo conflicted merge: %v", err)
	}
	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("Undo moved HEAD to %s, want %s", got, before)
	}
	state, err = session.(*gitSession).OperationState(ctx)
	if err != nil || state.Type != OperationTypeNone {
		t.Fatalf("Undo left a merge in progress: %q, %v", state.Type, err)
	}
	if got := gitOutput(t, dir, "diff", "--name-only", "--diff-filter=U"); got != "" {
		t.Fatalf("Undo left unmerged index entries: %s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != string(beforeContent) {
		t.Fatalf("Undo restored the wrong merge-start content: %q", got)
	}
}
