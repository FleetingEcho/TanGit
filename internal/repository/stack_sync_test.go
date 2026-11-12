package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stackSyncFixture struct {
	root   string
	remote string
	repo   string
	peer   string
}

func newStackSyncFixture(t *testing.T) stackSyncFixture {
	t.Helper()
	root := t.TempDir()
	f := stackSyncFixture{
		root: root, remote: filepath.Join(root, "remote.git"),
		repo: filepath.Join(root, "repo"), peer: filepath.Join(root, "peer"),
	}
	runGit(t, root, "init", "--bare", f.remote)
	runGit(t, root, "init", "--initial-branch=main", f.repo)
	runGit(t, f.repo, "config", "user.name", "Stack Test")
	runGit(t, f.repo, "config", "user.email", "stack@example.com")
	stackSyncCommit(t, f.repo, "base")
	runGit(t, f.repo, "remote", "add", "origin", f.remote)
	runGit(t, f.repo, "push", "-u", "origin", "main")
	runGit(t, root, "clone", f.remote, f.peer)
	runGit(t, f.peer, "config", "user.name", "Peer Test")
	runGit(t, f.peer, "config", "user.email", "peer@example.com")
	return f
}

func stackSyncCommit(t *testing.T, dir, name string) string {
	t.Helper()
	name = strings.ReplaceAll(name, "/", "-")
	path := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(path, []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", name+".txt")
	runGit(t, dir, "commit", "-m", name)
	return strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
}

func (f stackSyncFixture) branch(t *testing.T, name string) string {
	t.Helper()
	runGit(t, f.repo, "checkout", "-b", name)
	oid := stackSyncCommit(t, f.repo, name+"-local")
	runGit(t, f.repo, "push", "-u", "origin", name)
	runGit(t, f.repo, "checkout", "main")
	return oid
}

func (f stackSyncFixture) peerCommit(t *testing.T, branch, name string) string {
	t.Helper()
	runGit(t, f.peer, "fetch", "origin")
	runGit(t, f.peer, "checkout", branch)
	runGit(t, f.peer, "reset", "--hard", "origin/"+branch)
	oid := stackSyncCommit(t, f.peer, name)
	runGit(t, f.peer, "push", "origin", branch)
	runGit(t, f.repo, "fetch", "origin")
	return oid
}

func stackSyncBranchNamed(t *testing.T, plan StackSyncPlan, name string) StackSyncBranch {
	t.Helper()
	for _, branch := range plan.Branches {
		if branch.Name == name {
			return branch
		}
	}
	t.Fatalf("branch %s is absent from plan: %#v", name, plan.Branches)
	return StackSyncBranch{}
}

func stackSyncSessionFor(t *testing.T, dir string) StackSyncSession {
	t.Helper()
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	sync, ok := session.(StackSyncSession)
	if !ok {
		t.Fatal("Git session does not implement StackSyncSession")
	}
	return sync
}

func stackSyncOID(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(gitOutput(t, dir, "rev-parse", "--verify", ref))
}

func TestStackSyncPlanFreezesTopologyRelationsAndMergedFilter(t *testing.T) {
	f := newStackSyncFixture(t)
	oneOID := f.branch(t, "stack-one")
	runGit(t, f.repo, "checkout", "-b", "stack-two", "stack-one")
	twoOID := stackSyncCommit(t, f.repo, "stack-two-local")
	runGit(t, f.repo, "push", "-u", "origin", "stack-two")
	runGit(t, f.repo, "checkout", "main")
	runGit(t, f.repo, "branch", "merged", "main")
	behindOID := f.branch(t, "behind")
	remoteBehindOID := f.peerCommit(t, "behind", "behind-remote")
	f.branch(t, "ahead")
	runGit(t, f.repo, "checkout", "ahead")
	stackSyncCommit(t, f.repo, "ahead-extra")
	runGit(t, f.repo, "checkout", "main")
	rewriteOID := f.branch(t, "rewritten")
	runGit(t, f.peer, "fetch", "origin")
	runGit(t, f.peer, "checkout", "rewritten")
	runGit(t, f.peer, "commit", "--amend", "-m", "rewritten remote patch")
	runGit(t, f.peer, "push", "--force", "origin", "rewritten")
	runGit(t, f.repo, "fetch", "origin")
	remoteRewriteOID := stackSyncOID(t, f.repo, "refs/remotes/origin/rewritten")

	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourceID == "" || plan.MainOID != stackSyncOID(t, f.repo, "main") {
		t.Fatalf("plan did not freeze its source: %#v", plan)
	}
	one := stackSyncBranchNamed(t, plan, "stack-one")
	two := stackSyncBranchNamed(t, plan, "stack-two")
	if one.LocalOID != oneOID || len(one.LocalOID) != 40 || two.LocalOID != twoOID {
		t.Fatalf("local OIDs were not frozen in full: %#v %#v", one, two)
	}
	if one.Parent != "main" || one.ParentOID != plan.MainOID || two.Parent != "stack-one" || two.ParentOID != oneOID {
		t.Fatalf("wrong stack topology: one=%#v two=%#v", one, two)
	}
	if len(one.Upstream.OID) != 40 || one.Upstream.Ref != "refs/remotes/origin/stack-one" || one.Upstream.RemoteBranch != "stack-one" {
		t.Fatalf("upstream was not frozen: %#v", one.Upstream)
	}
	oneIndex, twoIndex := -1, -1
	for index, branch := range plan.Branches {
		if branch.Name == one.Name {
			oneIndex = index
		}
		if branch.Name == two.Name {
			twoIndex = index
		}
	}
	if oneIndex < 0 || twoIndex <= oneIndex {
		t.Fatalf("parents must precede children: %#v", plan.Branches)
	}
	for _, branch := range plan.Branches {
		if branch.Name == "merged" {
			t.Fatal("merged branch was not excluded")
		}
		if branch.Actionable() && (branch.Error != "" || branch.TargetOID == "") {
			t.Fatalf("invalid action preview: %#v", branch)
		}
	}
	if len(plan.Merged) != 1 || plan.Merged[0].Name != "merged" {
		t.Fatalf("merged exclusion not recorded: %#v", plan.Merged)
	}
	behind := stackSyncBranchNamed(t, plan, "behind")
	if behind.Relation != StackSyncFastForward || behind.Action != StackSyncActionUpdateRef || behind.LocalOID != behindOID || behind.Upstream.OID != remoteBehindOID {
		t.Fatalf("fast-forward classification: %#v", behind)
	}
	ahead := stackSyncBranchNamed(t, plan, "ahead")
	if ahead.Relation != StackSyncLocalAheadOnly || ahead.Ahead != 1 || ahead.Behind != 0 {
		t.Fatalf("local-ahead classification: %#v", ahead)
	}
	rewritten := stackSyncBranchNamed(t, plan, "rewritten")
	if rewritten.Relation != StackSyncRemoteRewritten || rewritten.LocalOID != rewriteOID || rewritten.Upstream.OID != remoteRewriteOID || rewritten.Action != StackSyncActionUpdateRef {
		t.Fatalf("remote-rewrite classification: %#v", rewritten)
	}
	if got := stackSyncBranchNamed(t, plan, "stack-one"); got.Relation != StackSyncUpToDate {
		t.Fatalf("up-to-date classification: %#v", got)
	}
}

func TestStackSyncProvideFetchesBeforeFreezing(t *testing.T) {
	f := newStackSyncFixture(t)
	local := f.branch(t, "feature")
	runGit(t, f.peer, "fetch", "origin")
	runGit(t, f.peer, "checkout", "feature")
	remote := stackSyncCommit(t, f.peer, "remote-before-provide")
	runGit(t, f.peer, "push", "origin", "feature")
	// Deliberately do not fetch in the reviewed repository. Provide's Fetch
	// option must get this real bare-remote update before taking its OIDs.
	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.ProvideStackSync(context.Background(), StackSyncOptions{Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	branch := stackSyncBranchNamed(t, plan, "feature")
	if branch.LocalOID != local || branch.Upstream.OID != remote || branch.Relation != StackSyncFastForward {
		t.Fatalf("Provide did not fetch before freezing: %#v", branch)
	}
}

func TestStackSyncDivergedLocalWorkIsNotRemoteRewrite(t *testing.T) {
	f := newStackSyncFixture(t)
	f.branch(t, "feature")
	runGit(t, f.peer, "fetch", "origin")
	runGit(t, f.peer, "checkout", "feature")
	runGit(t, f.peer, "commit", "--amend", "-m", "remote reword")
	runGit(t, f.peer, "push", "--force", "origin", "feature")
	runGit(t, f.repo, "fetch", "origin")
	runGit(t, f.repo, "checkout", "feature")
	local := stackSyncCommit(t, f.repo, "unique local work")
	runGit(t, f.repo, "checkout", "main")
	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	branch := stackSyncBranchNamed(t, plan, "feature")
	if branch.Relation != StackSyncDiverged || branch.LocalOID != local || branch.Action != StackSyncActionSkip {
		t.Fatalf("local work was misclassified or made destructively applicable: %#v", branch)
	}
}

func TestStackSyncApplyUpdateRefTransactionAndLocalSourceCAS(t *testing.T) {
	f := newStackSyncFixture(t)
	f.branch(t, "one")
	f.branch(t, "two")
	oneRemote := f.peerCommit(t, "one", "one-remote")
	twoRemote := f.peerCommit(t, "two", "two-remote")
	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := sync.ApplyStackSync(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	for name, remote := range map[string]string{"one": oneRemote, "two": twoRemote} {
		if got := stackSyncOID(t, f.repo, "refs/heads/"+name); got != remote {
			t.Fatalf("%s = %s, want transaction target %s", name, got, remote)
		}
	}
	for _, branch := range result.Branches {
		if branch.Driver == "git update-ref transaction" && !branch.Updated {
			t.Fatalf("successful CAS was not reported: %#v", branch)
		}
	}

	// Advancing either frozen local ref invalidates the whole old plan. The
	// other already-fresh branch must not be applied again under new state.
	stale, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.peerCommit(t, "one", "one-next-remote")
	twoBefore := stackSyncOID(t, f.repo, "refs/heads/two")
	oldOne := stackSyncOID(t, f.repo, "refs/heads/one")
	runGit(t, f.repo, "update-ref", "refs/heads/one", twoBefore, oldOne)
	_, err = sync.ApplyStackSync(context.Background(), stale)
	if !errors.Is(err, ErrStackSyncChanged) {
		t.Fatalf("stale local plan error = %v", err)
	}
	if got := stackSyncOID(t, f.repo, "refs/heads/one"); got != twoBefore {
		t.Fatalf("concurrent local value was overwritten: %s", got)
	}
	if got := stackSyncOID(t, f.repo, "refs/heads/two"); got != twoBefore {
		t.Fatalf("unrelated branch changed after stale plan: %s", got)
	}
}

func TestStackSyncRemoteTrackingSourceChangeRejectsOldPlan(t *testing.T) {
	f := newStackSyncFixture(t)
	local := f.branch(t, "feature")
	f.peerCommit(t, "feature", "remote-preview")
	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.peerCommit(t, "feature", "remote-after-preview")
	result, err := sync.ApplyStackSync(context.Background(), plan)
	if !errors.Is(err, ErrStackSyncChanged) {
		t.Fatalf("remote-tracking source change error = %v", err)
	}
	if got := stackSyncOID(t, f.repo, "refs/heads/feature"); got != local {
		t.Fatalf("old plan moved local ref after remote source changed: %s", got)
	}
	if len(result.Pushes) != 0 {
		t.Fatalf("stale plan attempted pushes: %#v", result.Pushes)
	}
}

func TestStackSyncUpdateRefTransactionCASFailureChangesNoRefs(t *testing.T) {
	f := newStackSyncFixture(t)
	one := f.branch(t, "one")
	two := f.branch(t, "two")
	target := stackSyncOID(t, f.repo, "main")
	// The second branch moved after its expected old value was captured. Git
	// must reject the whole prepared batch, including the still-valid first
	// update. This invokes the actual --stdin transaction, not a mock.
	runGit(t, f.repo, "update-ref", "refs/heads/two", one, two)
	sync := stackSyncSessionFor(t, f.repo).(*gitSession)
	updates := []stackSyncRefUpdate{
		{branch: StackSyncBranch{Ref: "refs/heads/one", LocalOID: one, TargetOID: target}},
		{branch: StackSyncBranch{Ref: "refs/heads/two", LocalOID: two, TargetOID: target}},
	}
	if before := stackSyncOID(t, f.repo, "refs/heads/two"); before != one {
		t.Fatalf("race setup failed: refs/heads/two = %s, want %s", before, one)
	}
	command := sync.cmd.New([]string{"git", "update-ref", "-z", "--stdin"}).SetStdin(stackSyncRefTransaction(updates))
	output, err := sync.combinedOutput(command)
	if err == nil {
		t.Fatalf("CAS transaction unexpectedly succeeded: output=%q input=%q", output, stackSyncRefTransaction(updates))
	}
	if got := stackSyncOID(t, f.repo, "refs/heads/one"); got != one {
		t.Fatalf("first ref ran despite another CAS failure: %s", got)
	}
	if got := stackSyncOID(t, f.repo, "refs/heads/two"); got != one {
		t.Fatalf("concurrent second ref changed during failed transaction: %s", got)
	}
}

func TestStackSyncCheckedOutSafetyAndLiveOperation(t *testing.T) {
	t.Run("other worktree skips and current fast-forwards", func(t *testing.T) {
		f := newStackSyncFixture(t)
		f.branch(t, "other")
		f.peerCommit(t, "other", "other-remote")
		runGit(t, f.repo, "checkout", "other")
		currentRemote := f.peerCommit(t, "other", "current-remote")
		// Use separate branches for the current and other-worktree cases.
		runGit(t, f.repo, "checkout", "main")
		f.branch(t, "linked")
		linkedRemote := f.peerCommit(t, "linked", "linked-remote")
		linkedOld := stackSyncOID(t, f.repo, "refs/heads/linked")
		linkedPath := filepath.Join(f.root, "linked-worktree")
		runGit(t, f.repo, "worktree", "add", linkedPath, "linked")
		runGit(t, f.repo, "checkout", "other")
		sync := stackSyncSessionFor(t, f.repo)
		plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
		if err != nil {
			t.Fatal(err)
		}
		current := stackSyncBranchNamed(t, plan, "other")
		linked := stackSyncBranchNamed(t, plan, "linked")
		if current.Action != StackSyncActionMerge || current.TargetOID != currentRemote || linked.Action != StackSyncActionSkip || !linked.CheckedOut || linked.Current || linked.Upstream.OID != linkedRemote {
			t.Fatalf("checked-out preview is unsafe: current=%#v linked=%#v", current, linked)
		}
		result, err := sync.ApplyStackSync(context.Background(), plan)
		if err != nil {
			t.Fatal(err)
		}
		if got := stackSyncOID(t, f.repo, "refs/heads/other"); got != currentRemote {
			t.Fatalf("current checked-out branch did not really merge: %s", got)
		}
		if got := stackSyncOID(t, f.repo, "refs/heads/linked"); got != linkedOld {
			t.Fatalf("linked worktree ref changed despite skip: %s", got)
		}
		if got := stackSyncOID(t, linkedPath, "HEAD"); got != linkedOld {
			t.Fatalf("linked worktree HEAD changed despite skip: %s", got)
		}
		foundSkip := false
		for _, branch := range result.Branches {
			foundSkip = foundSkip || branch.Name == "linked" && branch.Skipped
		}
		if !foundSkip {
			t.Fatalf("linked worktree skip was not reported: %#v", result.Branches)
		}
	})

	t.Run("dirty checked-out branch is refused", func(t *testing.T) {
		f := newStackSyncFixture(t)
		local := f.branch(t, "feature")
		f.peerCommit(t, "feature", "feature-remote")
		runGit(t, f.repo, "checkout", "feature")
		if err := os.WriteFile(filepath.Join(f.repo, "local-edit.txt"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		sync := stackSyncSessionFor(t, f.repo)
		plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := stackSyncBranchNamed(t, plan, "feature"); got.Action != StackSyncActionSkip || !strings.Contains(got.Error, "uncommitted") {
			t.Fatalf("dirty worktree was not refused: %#v", got)
		}
		runGit(t, f.repo, "clean", "-f")
		if _, err := sync.ApplyStackSync(context.Background(), plan); !errors.Is(err, ErrStackSyncChanged) {
			t.Fatalf("cleaning the source did not invalidate the reviewed plan: %v", err)
		}
		if got := stackSyncOID(t, f.repo, "refs/heads/feature"); got != local {
			t.Fatalf("dirty source protection moved feature: %s", got)
		}
	})

	t.Run("real live merge has a refusal reason", func(t *testing.T) {
		f := newStackSyncFixture(t)
		local := f.branch(t, "feature")
		f.peerCommit(t, "feature", "feature-remote")
		runGit(t, f.repo, "checkout", "feature")
		runGit(t, f.repo, "checkout", "-b", "merge-source")
		stackSyncCommit(t, f.repo, "change-to-merge")
		runGit(t, f.repo, "checkout", "feature")
		runGit(t, f.repo, "merge", "--no-commit", "--no-ff", "merge-source")
		if got := strings.TrimSpace(gitOutput(t, f.repo, "rev-parse", "--verify", "MERGE_HEAD")); got == "" {
			t.Fatal("test did not create a real MERGE_HEAD")
		}
		sync := stackSyncSessionFor(t, f.repo)
		plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
		if err != nil {
			t.Fatal(err)
		}
		branch := stackSyncBranchNamed(t, plan, "feature")
		if branch.LiveOperation != "merge" || branch.Action != StackSyncActionSkip || !strings.Contains(branch.Error, "live merge") {
			t.Fatalf("live merge was not refused with its reason: %#v", branch)
		}
		result, err := sync.ApplyStackSync(context.Background(), plan)
		if err != nil {
			t.Fatal(err)
		}
		if got := stackSyncOID(t, f.repo, "refs/heads/feature"); got != local {
			t.Fatalf("live merge protection moved feature: %s", got)
		}
		if len(result.Pushes) != 0 {
			t.Fatalf("refused live operation attempted pushes: %#v", result.Pushes)
		}
	})
}

func TestStackSyncDetachedLiveRebaseRetainsBranchRefusal(t *testing.T) {
	dir, _, _, _ := conflictedRebase(t)
	sync := stackSyncSessionFor(t, dir)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	branch := stackSyncBranchNamed(t, plan, "feature")
	if plan.CurrentBranch != "feature" || !branch.Current || branch.LiveOperation != "rebase" || branch.Action != StackSyncActionSkip || !strings.Contains(branch.Error, "live rebase") {
		t.Fatalf("detached live rebase lost its branch refusal: plan=%#v branch=%#v", plan, branch)
	}
}

func TestStackSyncPushLeaseAndPerBranchResults(t *testing.T) {
	t.Run("published", func(t *testing.T) {
		f := newStackSyncFixture(t)
		f.branch(t, "feature")
		runGit(t, f.repo, "checkout", "feature")
		local := stackSyncCommit(t, f.repo, "publish-local")
		expected := stackSyncOID(t, f.repo, "refs/remotes/origin/feature")
		runGit(t, f.repo, "checkout", "main")
		sync := stackSyncSessionFor(t, f.repo)
		plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{Publish: true})
		if err != nil {
			t.Fatal(err)
		}
		result, err := sync.ApplyStackSync(context.Background(), plan)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Pushes) != 1 || !result.Pushes[0].Published || result.Pushes[0].Error != "" || !result.Pushes[0].ForceWithLease || result.Pushes[0].ExpectedRemoteOID != expected {
			t.Fatalf("publication result = %#v", result.Pushes)
		}
		if got := stackSyncOID(t, f.remote, "refs/heads/feature"); got != local {
			t.Fatalf("bare remote = %s, want %s", got, local)
		}
	})

	t.Run("lease rejects remote changed after preview", func(t *testing.T) {
		f := newStackSyncFixture(t)
		base := f.branch(t, "feature")
		runGit(t, f.repo, "checkout", "feature")
		stackSyncCommit(t, f.repo, "publish-local")
		runGit(t, f.repo, "checkout", "main")
		sync := stackSyncSessionFor(t, f.repo)
		plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{Publish: true})
		if err != nil {
			t.Fatal(err)
		}
		expected := stackSyncOID(t, f.remote, "refs/heads/feature")
		// Resetting the bare branch to its parent makes the local push an
		// ordinary fast-forward: only an explicit lease bound to the preview
		// can reject it. No fetch hides the remote race from Plan/Apply.
		runGit(t, f.remote, "update-ref", "refs/heads/feature", base+"^")
		remoteBefore := stackSyncOID(t, f.remote, "refs/heads/feature")
		result, err := sync.ApplyStackSync(context.Background(), plan)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Pushes) != 1 || result.Pushes[0].Published || result.Pushes[0].Error == "" || result.Pushes[0].ExpectedRemoteOID != expected {
			t.Fatalf("stale lease result = %#v", result.Pushes)
		}
		if got := stackSyncOID(t, f.remote, "refs/heads/feature"); got != remoteBefore {
			t.Fatalf("failed lease changed the bare remote: %s != %s", got, remoteBefore)
		}
	})
}

func TestStackSyncPushReportsIndependentRemoteOutcomes(t *testing.T) {
	f := newStackSyncFixture(t)
	backup := filepath.Join(f.root, "backup.git")
	runGit(t, f.root, "init", "--bare", backup)
	runGit(t, f.repo, "remote", "add", "backup", backup)
	f.branch(t, "published")
	runGit(t, f.repo, "checkout", "published")
	publishedOID := stackSyncCommit(t, f.repo, "published-extra")
	runGit(t, f.repo, "checkout", "main")
	failedOID := f.branch(t, "failed")
	runGit(t, f.repo, "push", "-u", "backup", "failed")
	runGit(t, f.repo, "checkout", "failed")
	stackSyncCommit(t, f.repo, "failed-extra")
	runGit(t, f.repo, "checkout", "main")
	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{Publish: true})
	if err != nil {
		t.Fatal(err)
	}
	// Let the ordinary result succeed while the other remote races its lease.
	runGit(t, backup, "update-ref", "refs/heads/failed", failedOID+"^")
	remoteBefore := stackSyncOID(t, backup, "refs/heads/failed")
	result, err := sync.ApplyStackSync(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pushes) != 2 {
		t.Fatalf("independent pushes = %#v", result.Pushes)
	}
	for _, push := range result.Pushes {
		switch push.Remote + "/" + push.Branch {
		case "origin/published":
			if !push.Published || push.Error != "" || push.LocalOID != publishedOID {
				t.Fatalf("successful remote result = %#v", push)
			}
		case "backup/failed":
			if push.Published || push.Error == "" {
				t.Fatalf("failed remote result = %#v", push)
			}
		default:
			t.Fatalf("unexpected remote/branch result: %#v", push)
		}
	}
	if got := stackSyncOID(t, backup, "refs/heads/failed"); got != remoteBefore {
		t.Fatalf("failed remote changed after another remote succeeded: %s", got)
	}
}

func TestStackSyncRebaseAdvancesStackUsingRealUpdateRefsTODO(t *testing.T) {
	f := newStackSyncFixture(t)
	oneOld := f.branch(t, "one")
	runGit(t, f.repo, "checkout", "-b", "two", "one")
	stackSyncCommit(t, f.repo, "two-local")
	runGit(t, f.repo, "push", "-u", "origin", "two")
	// The parent independently has a fast-forward waiting. Because its label
	// is also in the child's real updateRefs TODO, Apply must not update it a
	// second time through the update-ref transaction.
	oneRemote := f.peerCommit(t, "one", "one-remote-refresh")
	// Replace origin/two with independently based work. Replaying one and two
	// onto it moves both labels while preserving their actual local commits.
	runGit(t, f.peer, "fetch", "origin")
	runGit(t, f.peer, "checkout", "two")
	runGit(t, f.peer, "reset", "--hard", "origin/main")
	remoteBase := stackSyncCommit(t, f.peer, "remote-base")
	runGit(t, f.peer, "push", "--force", "origin", "two")
	runGit(t, f.repo, "fetch", "origin")
	runGit(t, f.repo, "config", "rebase.updateRefs", "true")
	runGit(t, f.repo, "checkout", "two")
	twoOld := stackSyncOID(t, f.repo, "refs/heads/two")
	sync := stackSyncSessionFor(t, f.repo)
	plan, err := sync.PlanStackSync(context.Background(), StackSyncOptions{Publish: true})
	if err != nil {
		t.Fatal(err)
	}
	one := stackSyncBranchNamed(t, plan, "one")
	two := stackSyncBranchNamed(t, plan, "two")
	if one.Action != StackSyncActionUpdateRef || one.Upstream.OID != oneRemote {
		t.Fatalf("parent did not have its own pending CAS update: %#v", one)
	}
	if !plan.RebaseUpdateRefs || two.Action != StackSyncActionRebase || two.Relation != StackSyncDiverged || !two.MayUseUpdateRefs {
		t.Fatalf("rebase preview = %#v, plan=%#v", two, plan)
	}
	result, err := sync.ApplyStackSync(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	oneNew := stackSyncOID(t, f.repo, "refs/heads/one")
	twoNew := stackSyncOID(t, f.repo, "refs/heads/two")
	if oneNew == oneOld || oneNew == oneRemote || twoNew == twoOld {
		t.Fatalf("stack refs were not advanced by the rebase TODO: one(old=%s remote=%s new=%s), two %s -> %s", oneOld, oneRemote, oneNew, twoOld, twoNew)
	}
	if got := stackSyncOID(t, f.repo, "one^"); got != remoteBase {
		t.Fatalf("replayed parent does not sit on frozen remote target: %s", got)
	}
	if got := stackSyncOID(t, f.repo, "two^"); got != oneNew {
		t.Fatalf("child no longer points at advanced parent: %s != %s", got, oneNew)
	}
	var driven bool
	for _, branch := range result.Branches {
		if branch.Name == "one" && branch.Driver == "git rebase.updateRefs" && branch.UpdateRefsTODO == "update-ref refs/heads/one" {
			driven = true
		}
	}
	if !driven {
		t.Fatalf("real rebase TODO was not reported: %#v", result.Branches)
	}
	if got := stackSyncOID(t, f.remote, "refs/heads/one"); got != oneNew {
		t.Fatalf("advanced parent was not published: %s != %s", got, oneNew)
	}
	if got := stackSyncOID(t, f.remote, "refs/heads/two"); got != twoNew {
		t.Fatalf("advanced child was not published: %s != %s", got, twoNew)
	}
	if len(result.Pushes) != 2 {
		t.Fatalf("wanted independent parent/child publication results: %#v", result.Pushes)
	}
	for _, push := range result.Pushes {
		if !push.Published || !push.ForceWithLease {
			t.Fatalf("rebased push lost its lease: %#v", push)
		}
		if push.Branch == "one" && !push.Rewriting {
			t.Fatalf("rewritten parent was not reported as a lease-protected rewrite: %#v", push)
		}
	}
}
