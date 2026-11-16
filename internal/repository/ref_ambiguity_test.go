package repository

import (
	"context"
	"strings"
	"testing"
)

// ambiguousBranchFixture creates a tag at the older main commit, then advances
// the real local branch. Any bare "main" revision is genuinely ambiguous.
func ambiguousBranchFixture(t *testing.T) (string, Session, string, string) {
	t.Helper()
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base\n", "tagged base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "tag", "main")
	writeHistoryCommit(t, dir, "tip.txt", "tip\n", "branch tip")
	tip := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	return dir, session, base, tip
}

func TestResolveCommitRejectsAmbiguousBranchTag(t *testing.T) {
	_, session, base, tip := ambiguousBranchFixture(t)
	if hash, err := session.ResolveCommit(context.Background(), "main"); err == nil {
		t.Fatalf("ambiguous base resolved to %s without requiring an explicit ref", hash)
	} else if !strings.Contains(err.Error(), "refs/heads/main") || !strings.Contains(err.Error(), "refs/tags/main") {
		t.Fatalf("ambiguity error is not actionable: %v", err)
	}
	for ref, want := range map[string]string{"refs/heads/main": tip, "refs/tags/main": base} {
		hash, err := session.ResolveCommit(context.Background(), ref)
		if err != nil || hash != want {
			t.Fatalf("ResolveCommit(%q) = %s, %v; want %s", ref, hash, err, want)
		}
	}
}

func TestResolveCommitRejectsRemoteCollisionAndRevisionSuffix(t *testing.T) {
	dir, session, base, tip := ambiguousBranchFixture(t)
	runGit(t, dir, "update-ref", "refs/remotes/origin/main", tip)
	runGit(t, dir, "tag", "origin/main", base)
	for _, ref := range []string{"origin/main", "main~0"} {
		if hash, err := session.ResolveCommit(context.Background(), ref); err == nil {
			t.Fatalf("ambiguous revision %q resolved to %s", ref, hash)
		} else if !strings.Contains(err.Error(), "ambiguous ref") {
			t.Fatalf("ResolveCommit(%q) error = %v, want ambiguity guidance", ref, err)
		}
	}
	if hash, err := session.ResolveCommit(context.Background(), "refs/heads/main~0"); err != nil || hash != tip {
		t.Fatalf("explicit branch revision = %s, %v; want %s", hash, err, tip)
	}
	for ref, want := range map[string]string{"refs/remotes/origin/main": tip, "refs/tags/origin/main": base} {
		if hash, err := session.ResolveCommit(context.Background(), ref); err != nil || hash != want {
			t.Fatalf("ResolveCommit(%q) = %s, %v; want %s", ref, hash, err, want)
		}
	}
}

func TestAgentTargetStatusRejectsAmbiguousBranchTag(t *testing.T) {
	dir, session, _, baseTip := ambiguousBranchFixture(t)
	runGit(t, dir, "checkout", "-b", "agent")
	writeHistoryCommit(t, dir, "agent.txt", "agent\n", "agent work")
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	if status, err := session.AgentTargetStatus(context.Background(), "main"); err == nil {
		t.Fatalf("ambiguous base returned status %+v", status)
	} else if !strings.Contains(err.Error(), "refs/heads/main") || !strings.Contains(err.Error(), "refs/tags/main") {
		t.Fatalf("ambiguity error is not actionable: %v", err)
	}
	status, err := session.AgentTargetStatus(context.Background(), "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	want := AgentTargetStatus{HeadSHA: head, BaseSHA: baseTip, Ahead: 1, Behind: 0}
	if status != want {
		t.Fatalf("explicit base status = %+v, want %+v", status, want)
	}
}

func TestPrepareRebasePlanRejectsAmbiguousTarget(t *testing.T) {
	dir, session, _, targetTip := ambiguousBranchFixture(t)
	runGit(t, dir, "checkout", "-b", "topic")
	writeHistoryCommit(t, dir, "topic.txt", "topic\n", "topic work")
	plans := rebasePlanSession(t, session)

	if plan, err := plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{TargetRef: "main"}); err == nil {
		t.Fatalf("ambiguous target generated a draft against tag history: %+v", plan)
	} else if !strings.Contains(err.Error(), "refs/heads/main") || !strings.Contains(err.Error(), "refs/tags/main") {
		t.Fatalf("ambiguity error is not actionable: %v", err)
	}
	state, err := plans.OperationState(context.Background())
	if err != nil || state.Type != OperationTypeNone {
		t.Fatalf("rejected draft left an operation: state=%+v err=%v", state, err)
	}

	plan, err := plans.PrepareRebasePlan(context.Background(), RebasePlanOptions{TargetRef: "refs/heads/main"})
	if err != nil {
		t.Fatalf("explicit local branch did not prepare a draft: %v", err)
	}
	if plan.TargetSHA != targetTip {
		t.Fatalf("draft target = %s, want %s", plan.TargetSHA, targetTip)
	}
	picks := 0
	for _, step := range plan.Steps {
		if isRebaseCommitAction(step.Action) {
			picks++
		}
	}
	if picks != 1 {
		t.Fatalf("explicit branch draft has %d picks, want only topic work: %+v", picks, plan.Steps)
	}
}

func TestCompareRejectsAmbiguousRevision(t *testing.T) {
	_, session, _, _ := ambiguousBranchFixture(t)
	if files, err := session.CompareFiles(context.Background(), "main", "main", DiffOptions{ContextLines: 3}); err == nil {
		t.Fatalf("ambiguous revisions returned a comparison instead of an error: %+v", files)
	} else if !strings.Contains(err.Error(), "refs/heads/main") || !strings.Contains(err.Error(), "refs/tags/main") {
		t.Fatalf("ambiguity error is not actionable: %v", err)
	}
	files, err := session.CompareFiles(context.Background(), "refs/tags/main", "refs/heads/main", DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatalf("explicit refs did not compare: %v", err)
	}
	if len(files) != 1 || files[0].Path != "tip.txt" {
		t.Fatalf("explicit comparison = %+v, want only tip.txt", files)
	}
}

func TestMoveUnpushedUsesLocalDefaultBranchOverSameNamedTag(t *testing.T) {
	dir, session, _, mainTip := ambiguousBranchFixture(t)
	runGit(t, dir, "checkout", "-b", "topic")
	writeHistoryCommit(t, dir, "topic.txt", "topic\n", "only unpushed work")
	topicTip := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	moved, err := session.MoveUnpushedCommits(context.Background(), "moved")
	if err != nil {
		t.Fatalf("move unpushed commits: %v", err)
	}
	if moved != 1 {
		t.Fatalf("moved %d commits, want only the topic commit", moved)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/topic")); got != mainTip {
		t.Fatalf("old branch reset to %s, want local main %s", got, mainTip)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/moved")); got != topicTip {
		t.Fatalf("new branch = %s, want original topic %s", got, topicTip)
	}
}
