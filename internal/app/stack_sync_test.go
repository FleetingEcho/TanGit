package app

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type stackSyncTestSession struct {
	fakeSession
	mu           sync.Mutex
	plans        []repository.StackSyncPlan
	provides     int
	options      []repository.StackSyncOptions
	applied      []string
	applyResult  repository.StackSyncResult
	applyErr     error
	applyErrOnce bool
}

func (s *stackSyncTestSession) ProvideStackSync(_ context.Context, options repository.StackSyncOptions) (repository.StackSyncPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls.record("stack-sync-provide")
	s.provides++
	s.options = append(s.options, options)
	index := min(s.provides-1, len(s.plans)-1)
	if index < 0 {
		return repository.StackSyncPlan{}, fmt.Errorf("no test plan")
	}
	return s.plans[index], nil
}

func (s *stackSyncTestSession) PlanStackSync(ctx context.Context, options repository.StackSyncOptions) (repository.StackSyncPlan, error) {
	return s.ProvideStackSync(ctx, options)
}

func (s *stackSyncTestSession) ApplyStackSync(_ context.Context, plan repository.StackSyncPlan) (repository.StackSyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls.record("stack-sync-apply:" + plan.SourceID)
	s.applied = append(s.applied, plan.SourceID)
	if s.applyErrOnce {
		s.applyErrOnce = false
		return repository.StackSyncResult{}, s.applyErr
	}
	return s.applyResult, s.applyErr
}

func (s *stackSyncTestSession) counts() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.provides, append([]string(nil), s.applied...)
}

func stackSyncTestPlan(id, branch string) repository.StackSyncPlan {
	local := "1111111111111111111111111111111111111111"
	remote := "2222222222222222222222222222222222222222"
	return repository.StackSyncPlan{
		Repository: "test", MainBranch: "main", MainOID: "3333333333333333333333333333333333333333",
		CurrentBranch: branch, CurrentOID: local, Mode: repository.StackSyncRebase, Publish: true,
		RebaseUpdateRefs: true, SourceID: id,
		Branches: []repository.StackSyncBranch{{
			Name: branch, Ref: "refs/heads/" + branch, LocalOID: local,
			Upstream: repository.StackSyncUpstream{Remote: "origin", RemoteBranch: branch, Ref: "refs/remotes/origin/" + branch, OID: remote},
			Parent:   "main", ParentOID: "3333333333333333333333333333333333333333", ParentTargetOID: "3333333333333333333333333333333333333333",
			Relation: repository.StackSyncFastForward, Action: repository.StackSyncActionUpdateRef,
			TargetOID: remote, Current: true, CheckedOut: true, WorktreePath: "/work/other",
			MayUseUpdateRefs: true, Warnings: []string{"another worktree will be skipped"},
			Error: "review this live operation warning",
		}},
		Merged: []repository.StackSyncMergedBranch{{Name: "finished", Ref: "refs/heads/finished", LocalOID: local}},
	}
}

func stackSyncUIHarness(t *testing.T, session *stackSyncTestSession) *harness {
	t.Helper()
	h := menuTestApp(t)
	h.app.workspaceTab = sectionBranches
	h.app.session = session
	h.tester.Frame()
	return h
}

func TestStackSyncPreviewProvidesFreshSourceAndShowsBranchActions(t *testing.T) {
	plan := stackSyncTestPlan("preview-one", "feature/stack")
	session := &stackSyncTestSession{plans: []repository.StackSyncPlan{plan}}
	h := stackSyncUIHarness(t, session)
	if err := h.tester.Click("Sync stack"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "stack sync preview", func() bool { return h.app.stackSync.plan != nil && !h.app.stackSync.loading })
	h.tester.Frame()
	for _, text := range []string{
		"Sync stack", "feature/stack", "CAS update-ref → 2222222222", "ff",
		"another worktree will be skipped", "review this live operation warning",
		"Excluded 1 branch already merged into main.", "rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.",
		"Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.",
	} {
		if !h.tester.HasText(text) {
			t.Errorf("preview is missing %q; texts=%q", text, h.tester.Texts())
		}
	}
	if !h.app.modalOpen() {
		t.Fatal("stack sync did not own the modal keyboard scope")
	}
	provides, _ := session.counts()
	if provides != 1 || len(session.options) != 1 || !session.options[0].Publish || !session.options[0].Fetch || session.options[0].Mode != repository.StackSyncRebase {
		t.Fatalf("Provide options = %#v, count=%d", session.options, provides)
	}

	// Refresh is a new source read, not a rerender of the previous plan.
	if err := h.tester.Click("Refresh plan"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "second Provide", func() bool { provides, _ := session.counts(); return provides == 2 && !h.app.stackSync.loading })
	if h.app.stackSync.plan == nil || h.app.stackSync.plan.SourceID != "preview-one" {
		t.Fatalf("fresh Provide result was not installed: %#v", h.app.stackSync.plan)
	}
}

func TestStackSyncRejectsOldPlanAndProvidesReplacement(t *testing.T) {
	oldPlan := stackSyncTestPlan("old-source", "feature/old")
	newPlan := stackSyncTestPlan("new-source", "feature/new")
	session := &stackSyncTestSession{
		plans:    []repository.StackSyncPlan{oldPlan, newPlan},
		applyErr: repository.ErrStackSyncChanged, applyErrOnce: true,
	}
	h := stackSyncUIHarness(t, session)
	if err := h.tester.Click("Sync stack"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "old plan", func() bool { return h.app.stackSync.plan != nil })
	if err := h.tester.Click("Apply plan"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "replacement plan", func() bool {
		provides, _ := session.counts()
		return provides == 2 && h.app.stackSync.plan != nil && h.app.stackSync.plan.SourceID == "new-source" && !h.app.actionBusy
	})
	h.tester.Frame()
	_, applied := session.counts()
	if len(applied) != 1 || applied[0] != "old-source" {
		t.Fatalf("old plan was retried or a new plan applied without review: %#v", applied)
	}
	if h.app.stackSync.result != nil || h.app.stackSync.applyError == "" {
		t.Fatalf("stale plan failure was not visible: result=%#v error=%q", h.app.stackSync.result, h.app.stackSync.applyError)
	}
	if !h.tester.HasText("feature/new") || h.tester.HasText("feature/old") {
		t.Fatalf("old preview survived re-Provide: %q", h.tester.Texts())
	}
}

func TestStackSyncShowsUpdateRefsAndIndependentRemoteResults(t *testing.T) {
	plan := stackSyncTestPlan("apply-source", "feature/stack")
	result := repository.StackSyncResult{
		Branches: []repository.StackSyncBranchResult{{
			Name: "feature/stack", OldOID: plan.Branches[0].LocalOID, NewOID: plan.Branches[0].TargetOID,
			Driver: "git rebase.updateRefs", UpdateRefsTODO: "update-ref refs/heads/feature/stack", Updated: true,
		}},
		Pushes: []repository.StackSyncPushResult{
			{Remote: "origin", Branch: "feature/stack", RemoteBranch: "feature/stack", LocalOID: plan.Branches[0].TargetOID, ExpectedRemoteOID: plan.Branches[0].Upstream.OID, Published: true, ForceWithLease: true},
			{Remote: "backup", Branch: "feature/other", RemoteBranch: "other", LocalOID: plan.Branches[0].TargetOID, ExpectedRemoteOID: plan.Branches[0].LocalOID, Error: "stale info", ForceWithLease: true},
		},
	}
	session := &stackSyncTestSession{plans: []repository.StackSyncPlan{plan}, applyResult: result}
	h := stackSyncUIHarness(t, session)
	if err := h.tester.Click("Sync stack"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "plan before apply", func() bool { return h.app.stackSync.plan != nil })
	if err := h.tester.Click("Apply plan"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "remote results", func() bool { return h.app.stackSync.result != nil && !h.app.actionBusy })
	h.tester.Frame()
	for _, text := range []string{
		"Stack sync finished with 1 publication failure", "Driven by actual rebase TODO: update-ref refs/heads/feature/stack",
		"origin", "Published · feature/stack → origin/feature/stack · 2222222222",
		"backup", "Failed · feature/other → backup/other · 2222222222", "stale info",
		"Remote publication · independent, not transactional", "force-with-lease expected remote 1111111111",
	} {
		if !h.tester.HasText(text) {
			t.Errorf("results are missing %q; texts=%q", text, h.tester.Texts())
		}
	}
	if h.app.stackSync.plan != nil {
		t.Fatal("consumed plan remained applicable after results arrived")
	}
}
