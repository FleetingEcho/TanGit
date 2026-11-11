package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

func TestCommitReadinessRetriesAllChecks(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "ready", "", false); err == nil || !strings.Contains(err.Error(), "stage changes first") {
		t.Fatalf("unstaged commit readiness error = %v", err)
	}
	runGit(t, dir, "add", "a.txt")
	if err := session.Commit(context.Background(), "ready", "fresh retry", false); err != nil {
		t.Fatalf("retry did not rerun readiness: %v", err)
	}
	if subject := gitOutput(t, dir, "log", "-1", "--format=%B"); subject != "ready\n\nfresh retry\n\n" {
		t.Fatalf("commit message = %q", subject)
	}
}

func TestCommitHooksAlwaysRunAndRejectionPreservesHEAD(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	runGit(t, dir, "config", "tangit.test.initial", "true")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "config", "lazygit.skipHookPrefix", "WIP")
	head := gitOutput(t, dir, "rev-parse", "HEAD")
	hook := "#!/bin/sh\necho called >> \"" + filepath.Join(dir, "hook-calls") + "\"\n"
	if os.Getenv("TANGIT_TEST_ALLOW_HOOK") != "1" {
		hook += "exit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte(hook), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGIT_TEST_ALLOW_HOOK", "0")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "WIP never skips hooks", "", false); err == nil {
		t.Fatal("rejecting pre-commit hook was bypassed")
	}
	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("rejected hook moved HEAD from %s to %s", head, got)
	}
	calls, err := os.ReadFile(filepath.Join(dir, "hook-calls"))
	if err != nil || !strings.Contains(string(calls), "called") {
		t.Fatalf("pre-commit hook did not run: %q, %v", calls, err)
	}
	// Replace the rejecting hook, then retry the complete operation normally.
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "WIP never skips hooks", "", false); err != nil {
		t.Fatalf("retry after fixing hook failed: %v", err)
	}
}

func TestCommitReadinessRejectsConflictsAndActiveOperations(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "must not commit", "", false); err == nil || !strings.Contains(err.Error(), "resolve merge conflicts") {
		t.Fatalf("conflict readiness error = %v", err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := fileNamed(t, snapshot)
	if err := session.ResolveConflict(context.Background(), file, true); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "wrong operation", "", false); err == nil || !strings.Contains(err.Error(), "merge in progress") {
		t.Fatalf("active-operation readiness error = %v", err)
	}
}

func TestCommandBlockingCommitOnlyExemptsSquashMerge(t *testing.T) {
	for _, test := range []struct {
		name  string
		state models.WorkingTreeState
		want  string
	}{
		{name: "none"},
		{name: "squash only", state: models.WorkingTreeState{SquashMerging: true}},
		{name: "squash and rebase", state: models.WorkingTreeState{SquashMerging: true, Rebasing: true}, want: "rebase"},
		{name: "squash and am", state: models.WorkingTreeState{SquashMerging: true, ApplyingPatches: true}, want: "am"},
		{name: "squash and cherry-pick", state: models.WorkingTreeState{SquashMerging: true, CherryPicking: true}, want: "cherry-pick"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := commandBlockingCommit(test.state); got != test.want {
				t.Fatalf("commandBlockingCommit() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCommitCompletesSquashMerge(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-b", "topic")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("squashed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "topic")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "merge", "--squash", "topic")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "squashed topic", "", false); err != nil {
		t.Fatalf("committing a squash merge: %v", err)
	}
	if subject := gitOutput(t, dir, "log", "-1", "--format=%s"); subject != "squashed topic\n" {
		t.Fatalf("commit subject = %q", subject)
	}
}

func TestCommitReadinessRejectsEmptyIdentityBeforeHooks(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "")
	runGit(t, dir, "config", "user.email", "")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	hookPath := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\ntouch \""+filepath.Join(dir, "hook-ran")+"\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "no identity", "", false); err == nil || !strings.Contains(err.Error(), "configure Git author") {
		t.Fatalf("identity readiness error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hook-ran")); !os.IsNotExist(err) {
		t.Fatal("identity preflight reached the pre-commit hook")
	}
}
