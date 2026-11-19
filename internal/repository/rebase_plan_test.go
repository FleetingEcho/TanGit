package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRebasePlanPreservesDirectivesAndRejectsCopiedOrDroppedLines(t *testing.T) {
	original := strings.Join([]string{
		"pick abc123 first subject",
		"exec make test -- --run 'a b'",
		"update-ref refs/heads/stack/topic",
		"label onto",
		"reset onto",
		"merge -C def456 topic # merge topic",
		"fixup -C 789abc folded subject",
		"break",
		"noop",
	}, "\n") + "\n"
	steps, err := ParseRebasePlan(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 9 {
		t.Fatalf("parsed %d steps, want 9: %#v", len(steps), steps)
	}
	if steps[2].Action != "update-ref" || steps[2].UpdateRef != "refs/heads/stack/topic" {
		t.Fatalf("update-ref was not retained: %#v", steps[2])
	}
	if steps[4].Action != "reset" || steps[3].Action != "label" || steps[1].Raw != "exec make test -- --run 'a b'" {
		t.Fatalf("directives were not retained exactly: %#v", steps[:5])
	}
	if steps[5].Hash != "def456" || steps[6].Hash != "789abc" {
		t.Fatalf("commit-bearing directives were not parsed: %#v", steps[5:7])
	}

	edited := strings.Replace(original, "pick abc123", "reword abc123", 1)
	if err := validateRebasePlanEdit(original, edited); err != nil {
		t.Fatalf("an action-only edit was rejected: %v", err)
	}
	for name, invalid := range map[string]string{
		"copied pick":     strings.Replace(original, "pick abc123 first subject", "pick abc123 first subject\npick abc123 first subject", 1),
		"removed exec":    strings.Replace(original, "exec make test -- --run 'a b'\n", "", 1),
		"removed update":  strings.Replace(original, "update-ref refs/heads/stack/topic\n", "", 1),
		"replaced label":  strings.Replace(original, "label onto", "label elsewhere", 1),
		"unknown command": original + "frobnicate abc123\n",
		"empty plan":      "# every pick was deleted\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateRebasePlanEdit(original, invalid); !errors.Is(err, ErrRebasePlanInvalid) {
				t.Fatalf("validate edit returned %v, want ErrRebasePlanInvalid", err)
			}
		})
	}
}

func TestParseRebasePlanRejectsMalformedInstructions(t *testing.T) {
	for _, text := range []string{
		"pick\n",
		"update-ref refs/heads/one refs/heads/two\n",
		"reset\n",
		"exec\n",
		"merge -C\n",
		"drop --not-a-commit-is-still syntactically a token\nunknown\n",
	} {
		if _, err := ParseRebasePlan(text); !errors.Is(err, ErrRebasePlanInvalid) {
			t.Errorf("ParseRebasePlan(%q) returned %v, want ErrRebasePlanInvalid", text, err)
		}
	}
}

func rebasePlanCommit(t *testing.T, dir, name, content, subject string) Commit {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", name)
	runGit(t, dir, "commit", "-m", subject)
	hash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	return Commit{Hash: hash, ShortHash: hash[:7], Subject: subject}
}

func newRebasePlanRepository(t *testing.T) (string, []Commit) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	var commits []Commit
	for index, subject := range []string{"root", "first", "second"} {
		commits = append(commits, rebasePlanCommit(t, dir, fmt.Sprintf("file-%d.txt", index), fmt.Sprintf("content %d\n", index), subject))
	}
	return dir, commits
}

func rebasePlanSession(t *testing.T, session Session) RebasePlanSession {
	t.Helper()
	plans, ok := session.(RebasePlanSession)
	if !ok {
		t.Fatal("repository session does not implement RebasePlanSession")
	}
	return plans
}

func TestActiveRebasePlanReadsLiveStepsAndRejectsExternalCAS(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.EditCommit(context.Background(), commits[0]); err != nil {
		t.Fatalf("start edit-stop rebase: %v", err)
	}
	plans := rebasePlanSession(t, session)
	state, err := plans.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Steps) != 5 {
		t.Fatalf("live steps = %#v, want two done directives, current root and two pending picks", state.Steps)
	}
	if state.Steps[0].Status != OperationStepDone || state.Steps[1].Status != OperationStepDone {
		t.Fatalf("initial directives are not done: %#v", state.Steps[:2])
	}
	if state.Steps[2].Status != OperationStepCurrent || state.Steps[2].Action != "edit" ||
		state.Steps[2].Hash != commits[0].Hash || state.Steps[2].Subject != "root" {
		t.Fatalf("current live step = %#v, want root edit with full identity", state.Steps[2])
	}
	for index, status := range []OperationStepStatus{OperationStepPending, OperationStepPending} {
		step := state.Steps[index+3]
		if step.Status != status || step.Hash != commits[index+1].Hash || step.Subject != commits[index+1].Subject {
			t.Fatalf("pending step %d = %#v", index, step)
		}
	}

	plan, err := plans.ReadRebasePlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Active || len(plan.Steps) != 2 || plan.OperationFingerprint != state.Fingerprint || plan.Fingerprint == "" {
		t.Fatalf("active plan identity = %#v", plan)
	}
	edited := strings.Replace(plan.Text, "pick "+commits[1].ShortHash, "reword "+commits[1].ShortHash, 1)
	if edited == plan.Text {
		// Git chooses its own abbreviation length; replace by the parsed raw
		// token rather than assuming the repository's seven-column display.
		line := plan.Steps[0].Raw
		edited = strings.Replace(plan.Text, line, strings.Replace(line, "pick ", "reword ", 1), 1)
	}
	if err := plans.EditRebasePlan(context.Background(), plan, edited); err != nil {
		t.Fatalf("CAS edit active plan: %v", err)
	}
	updated, err := plans.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if updated.Steps[3].Action != "reword" || updated.Steps[3].Status != OperationStepPending ||
		updated.Steps[2].Status != OperationStepCurrent {
		t.Fatalf("state did not follow the active todo edit: %#v", updated.Steps)
	}

	stale, err := plans.ReadRebasePlan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	todoPath := filepath.Join(dir, ".git", "rebase-merge", "git-rebase-todo")
	external := strings.Replace(stale.Steps[0].Raw, "reword ", "edit ", 1)
	if err := os.WriteFile(todoPath, []byte(external+"\n"+stale.Steps[1].Raw+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := plans.EditRebasePlan(context.Background(), stale, edited); !errors.Is(err, ErrRebasePlanChanged) {
		t.Fatalf("stale active edit returned %v, want ErrRebasePlanChanged", err)
	}
	content, err := os.ReadFile(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), external) {
		t.Fatalf("stale edit overwrote external todo content: %q", content)
	}
}

func TestPrepareAndStartMarkedBasePlanUsesFixedFullSHA(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=base-branch")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for _, subject := range []string{"one", "two", "three"} {
		runGit(t, dir, "commit", "--allow-empty", "-m", subject)
	}
	runGit(t, dir, "checkout", "-b", "active-branch")
	runGit(t, dir, "commit", "--allow-empty", "-m", "active one")
	marked := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "commit", "--allow-empty", "-m", "active two")
	runGit(t, dir, "commit", "--allow-empty", "-m", "active three")
	runGit(t, dir, "checkout", "base-branch")
	runGit(t, dir, "checkout", "-b", "target-branch")
	runGit(t, dir, "commit", "--allow-empty", "-m", "target one")
	runGit(t, dir, "checkout", "active-branch")

	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{
		TargetRef: "target-branch", MarkedBase: "active-one-does-not-exist",
	})
	if err == nil {
		t.Fatal("unknown marked base unexpectedly prepared a plan")
	}
	plan, err = plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{
		TargetRef: "target-branch", MarkedBase: marked[:9],
	})
	if err != nil {
		t.Fatalf("prepare marked-base plan: %v", err)
	}
	if plan.MarkedBaseSHA != marked || len(plan.Steps) != 4 {
		t.Fatalf("marked-base draft = base %q steps %#v, want full %s plus two picks and two directives", plan.MarkedBaseSHA, plan.Steps, marked)
	}
	picks := 0
	for _, step := range plan.Steps {
		if isRebaseCommitAction(step.Action) {
			picks++
			if len(step.Hash) != 40 || step.Subject == "" {
				t.Fatalf("draft step lacks full Git identity: %#v", step)
			}
		}
	}
	if picks != 2 {
		t.Fatalf("draft has %d picks, want 2: %#v", picks, plan.Steps)
	}
	if err := plans.StartRebasePlan(context.Background(), plan, plan.Text); err != nil {
		t.Fatalf("start marked-base plan: %v", err)
	}
	subjects := strings.Split(strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s", "--first-parent")), "\n")
	want := []string{"active three", "active two", "target one", "three", "two", "one"}
	if len(subjects) != len(want) {
		t.Fatalf("rebased history = %#v, want %#v", subjects, want)
	}
	for index := range want {
		if subjects[index] != want[index] {
			t.Fatalf("rebased history = %#v, want %#v", subjects, want)
		}
	}
	if state, err := plans.OperationState(context.Background()); err != nil || state.Type != OperationTypeNone {
		t.Fatalf("rebase remains after successful plan: %#v, %v", state, err)
	}
}

func TestPreparedPlanStartsActionEditWithoutDroppingTodoLines(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	runGit(t, dir, "checkout", "-b", "target", commits[0].Hash)
	runGit(t, dir, "checkout", "main")
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{TargetRef: "target"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.MarkedBaseSHA != "" || len(plan.Steps) != 4 {
		t.Fatalf("ordinary plan = %#v", plan)
	}
	line := ""
	for _, step := range plan.Steps {
		if step.Action == "pick" {
			line = step.Raw
			break
		}
	}
	edited := strings.Replace(plan.Text, line, strings.Replace(line, "pick ", "drop ", 1), 1)
	if err := plans.StartRebasePlan(context.Background(), plan, edited); err != nil {
		t.Fatalf("start drop-action plan: %v", err)
	}
	subjects := strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s", "--first-parent"))
	if subjects != "second\nroot" {
		t.Fatalf("history after dropping first = %q, want second/root", subjects)
	}
}

func TestPreparedPlanShowsUpdateRefsFromGit(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	runGit(t, dir, "branch", "stack/topic", commits[1].Hash)
	runGit(t, dir, "config", "rebase.updateRefs", "true")
	runGit(t, dir, "checkout", "-b", "target", commits[0].Hash)
	runGit(t, dir, "checkout", "main")
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	plans := rebasePlanSession(t, session)
	plan, err := plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{TargetRef: "target"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range plan.Steps {
		if step.Action == "update-ref" && step.UpdateRef == "refs/heads/stack/topic" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Git-generated update-ref missing from draft steps: %#v\n%s", plan.Steps, plan.Text)
	}
}
