package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartRebasePlanRejectsMovedTargetRef(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	runGit(t, dir, "checkout", "-b", "target", commits[0].Hash)
	runGit(t, dir, "checkout", "main")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{TargetRef: "target"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetSHA != commits[0].Hash {
		t.Fatalf("draft target SHA = %q, want full %q", plan.TargetSHA, commits[0].Hash)
	}
	runGit(t, dir, "checkout", "target")
	runGit(t, dir, "commit", "--allow-empty", "-m", "target moved")
	runGit(t, dir, "checkout", "main")
	before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := plans.StartRebasePlan(context.Background(), plan, plan.Text); !errors.Is(err, ErrRebasePlanChanged) {
		t.Fatalf("stale-target start returned %v, want ErrRebasePlanChanged", err)
	}
	if after := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); after != before {
		t.Fatalf("stale-target start changed HEAD to %s", after)
	}
	state, err := plans.OperationState(context.Background())
	if err != nil || state.Type != OperationTypeNone {
		t.Fatalf("stale-target start left an operation: %#v, %v", state, err)
	}
}

func TestActiveTodoGitCASPreservesExecUpdateRefResetAndLabel(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.EditCommit(context.Background(), commits[0]); err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.ReadRebasePlan(context.Background())
	if err != nil || len(plan.Steps) != 2 {
		t.Fatalf("initial active plan = %#v, %v", plan, err)
	}
	directives := []string{
		"label onto",
		"reset [new root]",
		"pick " + plan.Steps[0].Hash + " " + plan.Steps[0].Subject,
		"exec printf 'keep this exact command'",
		"update-ref refs/heads/stack/topic",
		"pick " + plan.Steps[1].Hash + " " + plan.Steps[1].Subject,
	}
	path := filepath.Join(dir, ".git", "rebase-merge", "git-rebase-todo")
	external := strings.Join(directives, "\n") + "\n"
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = plans.ReadRebasePlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(external, "pick "+plan.Steps[2].Hash, "reword "+plan.Steps[2].Hash, 1)
	if err := plans.EditRebasePlan(context.Background(), plan, edited); err != nil {
		t.Fatalf("Git editor handoff: %v", err)
	}
	latest, err := plans.ReadRebasePlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, directive := range []string{"label onto", "reset [new root]", "exec printf 'keep this exact command'", "update-ref refs/heads/stack/topic"} {
		if strings.Count(latest.Text, directive) != 1 {
			t.Errorf("directive %q was not preserved exactly once in %q", directive, latest.Text)
		}
	}
	if len(latest.Steps) != len(plan.Steps) {
		t.Fatalf("Git edit changed instruction count from %d to %d", len(plan.Steps), len(latest.Steps))
	}
	if err := plans.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionAbort, ExpectedFingerprint: latest.OperationFingerprint,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestActiveTodoGitCASHandlesShortHashesAndAliases(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.EditCommit(context.Background(), commits[0]); err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.ReadRebasePlan(context.Background())
	if err != nil || len(plan.Steps) != 2 {
		t.Fatalf("initial active plan = %#v, %v", plan, err)
	}
	short := "l onto\nt [new root]\np " + commits[1].Hash[:7] +
		" first\nx printf 'alias preserved'\nu refs/heads/stack/topic\np " + commits[2].Hash[:7] + " second\nb\n"
	path := filepath.Join(dir, ".git", "rebase-merge", "git-rebase-todo")
	if err := os.WriteFile(path, []byte(short), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = plans.ReadRebasePlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	edited := "l onto\nt [new root]\nr " + commits[1].Hash[:7] +
		" first\nx printf 'alias preserved'\nu refs/heads/stack/topic\np " + commits[2].Hash[:7] + " second\nb\n"
	if err := plans.EditRebasePlan(context.Background(), plan, edited); err != nil {
		t.Fatalf("short-hash alias CAS was rejected: %v", err)
	}
	updated, err := plans.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	directives := map[string]bool{}
	for _, step := range updated.Steps {
		if step.Status == OperationStepPending && step.Action == "reword" && step.Hash == commits[1].Hash {
			found = true
		}
		if step.Status == OperationStepPending {
			directives[step.Action] = true
		}
	}
	for _, action := range []string{"label", "reset", "exec", "update-ref", "break"} {
		if !directives[action] {
			t.Errorf("non-commit alias %s was lost: %#v", action, updated.Steps)
		}
	}
	if !found {
		t.Fatalf("short-hash edit did not reach OperationState: %#v", updated.Steps)
	}
	if err := plans.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionAbort, ExpectedFingerprint: updated.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConflictedRebaseAllowsWholeTodoCAS(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "feature one")
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("feature two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "second.txt")
	runGit(t, dir, "commit", "-m", "feature two")
	secondHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "main work")
	runGit(t, dir, "checkout", "feature")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RebaseOntoBranch(context.Background(), "main"); err == nil {
		t.Fatal("conflicting rebase unexpectedly succeeded")
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.ReadRebasePlan(context.Background())
	if err != nil || len(plan.Steps) != 1 {
		t.Fatalf("pending plan during conflict = %#v, %v", plan, err)
	}
	edited := strings.Replace(plan.Text, plan.Steps[0].Raw, "reword "+secondHash+" feature two", 1)
	if err := plans.EditRebasePlan(context.Background(), plan, edited); err != nil {
		t.Fatalf("edit-todo handoff during an unresolved conflict: %v", err)
	}
	latest, err := plans.ReadRebasePlan(context.Background())
	if err != nil || len(latest.Steps) != 1 || latest.Steps[0].Action != "reword" || latest.Steps[0].Hash != secondHash {
		t.Fatalf("conflicted todo did not retain the edited pending pick: %#v, %v", latest, err)
	}
	if err := plans.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionAbort, ExpectedFingerprint: latest.OperationFingerprint,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedTodoKeepsOperationAndAbortAvailable(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.EditCommit(context.Background(), commits[0]); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".git", "rebase-merge", "git-rebase-todo")
	if err := os.WriteFile(path, []byte("not-a-git-todo-command\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	state, err := plans.OperationState(context.Background())
	if err != nil {
		t.Fatalf("malformed todo made the operation unreadable: %v", err)
	}
	if state.Type != OperationTypeRebase || state.StepsError == "" || !state.Can(OperationActionAbort) {
		t.Fatalf("malformed todo state = %#v, want readable rebase with Abort", state)
	}
	if err := plans.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint,
	}); err != nil {
		t.Fatalf("Abort must remain available after a plan display error: %v", err)
	}
	state, err = plans.OperationState(context.Background())
	if err != nil || state.Type != OperationTypeNone {
		t.Fatalf("operation remains after Abort: %#v, %v", state, err)
	}
}
