package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBisectLifecycle(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		writeHistoryCommit(t, dir, "file.txt", strings.Repeat("x", i)+"\n", fmt.Sprintf("c%d", i))
	}
	head := gitOutput(t, dir, "rev-parse", "HEAD~3")

	state, err := session.(*gitSession).BisectState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active {
		t.Fatal("no bisect should be active")
	}

	root := strings.TrimSpace(gitOutput(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	if err := session.BisectStart(ctx, "HEAD", root); err != nil {
		t.Fatalf("start bisect: %v", err)
	}
	state, err = session.(*gitSession).BisectState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Active || state.Current == "" {
		t.Fatalf("bisect not active after start: %+v", state)
	}
	if len(state.Bad) != 1 || len(state.Good) != 1 {
		t.Fatalf("bad=%d good=%d, want 1 each", len(state.Bad), len(state.Good))
	}
	if state.NewTerm != "bad" || state.OldTerm != "good" {
		t.Fatalf("terms = %q/%q", state.NewTerm, state.OldTerm)
	}

	// Drive the bisect to completion by always testing "good" until git
	// names the first bad commit, then stop and reset.
	var done string
	for i := 0; i < 10; i++ {
		outcome, err := session.BisectMark(ctx, "good", "")
		if err != nil {
			t.Fatalf("mark good: %v", err)
		}
		if outcome != "" {
			done = outcome
			break
		}
	}
	if done == "" {
		t.Fatal("bisect never completed")
	}
	if err := session.BisectReset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	state, err = session.(*gitSession).BisectState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Active {
		t.Fatal("bisect still active after reset")
	}
	_ = head
}

func TestEditCommitPausesAndResumes(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	ctx := context.Background()
	writeHistoryCommit(t, dir, "one.txt", "one\n", "first")
	writeHistoryCommit(t, dir, "two.txt", "two\n", "second")
	writeHistoryCommit(t, dir, "three.txt", "three\n", "third")

	target := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD~1"))
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var commit Commit
	for _, candidate := range snapshot.Commits {
		if candidate.Hash == target {
			commit = candidate
		}
	}
	if commit.Hash == "" {
		t.Fatal("target commit not found in snapshot")
	}
	if err := session.EditCommit(ctx, commit); err != nil {
		t.Fatalf("edit commit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); err != nil {
		t.Fatalf("expected rebase in progress, stat: %v", err)
	}
	if head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); head != target {
		t.Fatalf("HEAD = %s, want paused at %s", head, target)
	}
	// Continue the rebase: history should be back whole.
	if err := session.ContinueOperation(ctx); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if got := gitOutput(t, dir, "log", "--format=%s"); strings.Count(strings.TrimSpace(got), "\n") != 2 {
		t.Fatalf("expected 3 commits after continue, got %q", got)
	}
}

func TestExportAndApplyPatch(t *testing.T) {
	sourceDir, sourceSession := newHistoryEditingSession(t)
	ctx := context.Background()
	writeHistoryCommit(t, sourceDir, "one.txt", "one\n", "first subject")
	writeHistoryCommit(t, sourceDir, "two.txt", "two\n", "second: with colon")

	snapshot, err := sourceSession.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(snapshot.Commits))
	}
	patchDir := t.TempDir()
	paths, err := sourceSession.ExportPatches(ctx, snapshot.Commits, patchDir)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 patches, got %v", paths)
	}

	targetDir, targetSession := newHistoryEditingSession(t)
	for i, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := targetSession.AmPatch(ctx, filepath.Base(path), content); err != nil {
			t.Fatalf("apply patch %s: %v", path, err)
		}
		_ = i
	}
	log := gitOutput(t, targetDir, "log", "--format=%s")
	if !strings.Contains(log, "first subject") || !strings.Contains(log, "second: with colon") {
		t.Fatalf("applied history %q", log)
	}
}

func TestMoveUnpushedCommits(t *testing.T) {
	base := t.TempDir()
	// Bare remote so upstream tracking works.
	runGit(t, base, "init", "--bare", "remote.git")
	remote := filepath.Join(base, "remote.git")

	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "push", "-u", "origin", "main")

	// Two unpushed commits on main.
	writeHistoryCommit(t, dir, "a.txt", "a\n", "commit a")
	writeHistoryCommit(t, dir, "b.txt", "b\n", "commit b")

	count, err := session.MoveUnpushedCommits(context.Background(), "topic")
	if err != nil {
		t.Fatalf("move commits: %v", err)
	}
	if count != 2 {
		t.Fatalf("moved %d commits, want 2", count)
	}
	if branch := strings.TrimSpace(gitOutput(t, dir, "branch", "--show-current")); branch != "topic" {
		t.Fatalf("on branch %q, want topic", branch)
	}
	if got := gitOutput(t, dir, "log", "--format=%s", "main"); strings.Contains(got, "commit a") {
		t.Fatalf("main kept unpushed commits:\n%s", got)
	}
	if got := gitOutput(t, dir, "log", "--format=%s", "topic"); !strings.Contains(got, "commit a") || !strings.Contains(got, "commit b") {
		t.Fatalf("topic missing moved commits:\n%s", got)
	}
}

func TestStackedBranchesOrder(t *testing.T) {
	base := t.TempDir()
	runGit(t, base, "init", "--bare", "remote.git")
	remote := filepath.Join(base, "remote.git")

	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "push", "-u", "origin", "main")
	// main -> one (A), then a commit on top of one (A2).
	runGit(t, dir, "checkout", "-b", "one")
	writeHistoryCommit(t, dir, "one.txt", "one\n", "one")
	runGit(t, dir, "push", "-u", "origin", "one")
	writeHistoryCommit(t, dir, "one2.txt", "one2\n", "one two")
	// two forks from one after A2, gets its own commit, is pushed, then one more.
	runGit(t, dir, "checkout", "-b", "two")
	writeHistoryCommit(t, dir, "two.txt", "two\n", "two")
	runGit(t, dir, "push", "-u", "origin", "two")
	writeHistoryCommit(t, dir, "two2.txt", "two2\n", "two two")
	// three forks from two (with two2) and carries the tip commit.
	runGit(t, dir, "checkout", "-b", "three")
	writeHistoryCommit(t, dir, "three.txt", "three\n", "three")

	got, err := session.(*gitSession).StackedBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("stacked = %v, want [one two]", got)
	}

	// Pushing the stack publishes one and two; both branches are ahead.
	if err := session.PushStack(context.Background(), got); err != nil {
		t.Fatalf("push stack: %v", err)
	}
	for _, branch := range []string{"one", "two"} {
		remoteHead := strings.TrimSpace(gitOutput(t, remote, "rev-parse", "refs/heads/"+branch))
		localHead := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/"+branch))
		if remoteHead != localHead {
			t.Fatalf("remote %s = %s, want %s", branch, remoteHead, localHead)
		}
	}
}

func TestFetchPruneDropsStaleRemoteTrackingRef(t *testing.T) {
	base := t.TempDir()
	runGit(t, base, "init", "--bare", "remote.git")
	remote := filepath.Join(base, "remote.git")

	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base\n", "base")
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "push", "-u", "origin", "main")
	runGit(t, dir, "checkout", "-b", "stale")
	runGit(t, dir, "push", "-u", "origin", "stale")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "branch", "-D", "stale")
	runGit(t, remote, "branch", "-D", "stale")
	if out := gitOutput(t, dir, "branch", "-r"); !strings.Contains(out, "origin/stale") {
		t.Fatalf("expected origin/stale before prune, got %q", out)
	}
	if err := session.FetchPrune(context.Background()); err != nil {
		t.Fatalf("fetch prune: %v", err)
	}
	if out := gitOutput(t, dir, "branch", "-r"); strings.Contains(out, "origin/stale") {
		t.Fatalf("expected origin/stale to be pruned, got %q", out)
	}
}

func TestSubmoduleBulkUpdateAndSync(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub.git")
	runGit(t, base, "init", "--bare", "sub.git")
	// Seed the submodule remote.
	seed := filepath.Join(base, "seed")
	runGit(t, base, "clone", sub, "seed")
	if err := os.WriteFile(filepath.Join(seed, "lib.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, seed, "add", ".")
	runGit(t, seed, "-c", "user.email=t@e", "-c", "user.name=t", "commit", "-m", "initial")
	runGit(t, seed, "push", "origin", "master")

	dir, session := newHistoryEditingSession(t)
	runGit(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", sub, "libs/one")
	writeHistoryCommit(t, dir, "top.txt", "top\n", "top")

	if err := session.UpdateSubmodules(context.Background(), nil, true, false); err != nil {
		t.Fatalf("update all: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "libs", "one", "lib.txt")); err != nil {
		t.Fatalf("submodule file missing after update: %v", err)
	}
	if err := session.SyncSubmodules(context.Background(), []string{"libs/one"}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := session.UpdateSubmodules(context.Background(), []string{"libs/one"}, false, false); err != nil {
		t.Fatalf("update one: %v", err)
	}
}

func TestExportPatchesDoesNotOverwriteExistingFiles(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	ctx := context.Background()
	writeHistoryCommit(t, dir, "one.txt", "one\n", "first")
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	patchDir := t.TempDir()
	first, err := session.ExportPatches(ctx, snapshot.Commits, patchDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.ExportPatches(ctx, snapshot.Commits, patchDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || first[0] == second[0] {
		t.Fatalf("repeat export reused a filename: first=%v second=%v", first, second)
	}
}

func TestMoveUnpushedPreservesUntrackedFileThatExistsAtForkPoint(t *testing.T) {
	base := t.TempDir()
	runGit(t, base, "init", "--bare", "remote.git")
	remote := filepath.Join(base, "remote.git")
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "precious.txt", "fork-point content\n", "fork point")
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "push", "-u", "origin", "main")
	if err := os.Remove(filepath.Join(dir, "precious.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "precious.txt")
	runGit(t, dir, "commit", "-m", "delete precious file")
	// This path is untracked at HEAD, but tracked at the upstream fork point.
	// The old reset implementation silently replaced it.
	if err := os.WriteFile(filepath.Join(dir, "precious.txt"), []byte("new untracked work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := session.MoveUnpushedCommits(context.Background(), "topic"); err != nil {
		t.Fatalf("move commits: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "precious.txt"))
	if err != nil || string(content) != "new untracked work\n" {
		t.Fatalf("untracked content changed during move: %q, %v", content, err)
	}
	if branch := strings.TrimSpace(gitOutput(t, dir, "branch", "--show-current")); branch != "topic" {
		t.Fatalf("current branch = %q, want topic", branch)
	}
}

func TestPullAndFastForwardProtectCheckedOutLinkedWorktrees(t *testing.T) {
	base := t.TempDir()
	runGit(t, base, "init", "--bare", "remote.git")
	remote := filepath.Join(base, "remote.git")
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "initial.txt", "initial\n", "initial")
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "branch", "topic")
	runGit(t, dir, "push", "-u", "origin", "topic")
	linked := filepath.Join(t.TempDir(), "linked wt")
	runGit(t, dir, "worktree", "add", linked, "topic")

	// Advance the remote from a separate checkout, without touching the
	// checked-out linked worktree that needs a fast-forward.
	other := filepath.Join(t.TempDir(), "other")
	runGit(t, base, "clone", remote, other)
	runGit(t, other, "checkout", "topic")
	runGit(t, other, "config", "user.name", "Remote Author")
	runGit(t, other, "config", "user.email", "remote@example.com")
	writeHistoryCommit(t, other, "advanced.txt", "upstream\n", "upstream commit")
	runGit(t, other, "push", "origin", "topic")
	localTip := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/topic"))

	for name, run := range map[string]func(context.Context, string) error{
		"pull":         session.PullBranch,
		"fast-forward": session.FastForwardBranch,
	} {
		if err := run(context.Background(), "topic"); err == nil ||
			!strings.Contains(filepath.ToSlash(err.Error()), "checked out in "+filepath.ToSlash(linked)) {
			t.Fatalf("%s updated a linked worktree without updating its files: %v", name, err)
		}
		if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/topic")); got != localTip {
			t.Fatalf("%s moved a checked-out branch behind its working tree", name)
		}
		if _, err := os.Stat(filepath.Join(linked, "advanced.txt")); !os.IsNotExist(err) {
			t.Fatalf("%s wrote foreground content into the linked worktree indirectly", name)
		}
	}
}

func TestPausedEditSupportsAmendContinueAndJournalUndo(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	ctx := context.Background()
	writeHistoryCommit(t, dir, "one.txt", "one\n", "first")
	writeHistoryCommit(t, dir, "two.txt", "two\n", "second")
	writeHistoryCommit(t, dir, "three.txt", "three\n", "third")

	pausedHash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD~1"))
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var target Commit
	for _, candidate := range snapshot.Commits {
		if candidate.Hash == pausedHash {
			target = candidate
		}
	}
	if err := session.EditCommit(ctx, target); err != nil {
		t.Fatalf("edit commit: %v", err)
	}
	records, err := session.Operations(ctx)
	if err != nil || len(records) == 0 {
		t.Fatalf("missing paused journal record: %+v %v", records, err)
	}
	record := records[0]
	if !record.RebasePaused || record.CanUndo() {
		t.Fatalf("paused record must not offer undo yet: %+v", record)
	}

	if err := os.WriteFile(filepath.Join(dir, "two.txt"), []byte("amended content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "two.txt")
	if err := session.AmendCommit(ctx, target); err != nil {
		t.Fatalf("amend stopped commit: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got == pausedHash {
		t.Fatal("amend did not replace the stopped commit")
	}
	if err := session.ContinueOperation(ctx); err != nil {
		t.Fatalf("continue edited rebase: %v", err)
	}
	if count := strings.TrimSpace(gitOutput(t, dir, "rev-list", "--count", "HEAD")); count != "3" {
		t.Fatalf("history lost commits after continue: %s", count)
	}
	if !strings.Contains(gitOutput(t, dir, "show", "HEAD~1:two.txt"), "amended content") {
		t.Fatal("amended file content did not survive continuation")
	}

	records, err = session.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record = records[0]
	if record.RebasePaused || record.BranchAfter != "main" || !record.CanUndo() {
		t.Fatalf("final rebase state cannot be recovered properly: %+v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo completed amend: %v", err)
	}
	if got := gitOutput(t, dir, "show", "HEAD:two.txt"); got != "two\n" {
		t.Fatalf("undo did not restore pre-edit content: %q", got)
	}
}

func TestAbortPausedEditOffersNoUndo(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	ctx := context.Background()
	writeHistoryCommit(t, dir, "one.txt", "one\n", "first")
	writeHistoryCommit(t, dir, "two.txt", "two\n", "second")
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.EditCommit(ctx, snapshot.Commits[0]); err != nil {
		t.Fatal(err)
	}
	if err := session.AbortOperation(ctx); err != nil {
		t.Fatal(err)
	}
	records, err := session.Operations(ctx)
	if err != nil || len(records) == 0 {
		t.Fatalf("abort left no journal history: %v", err)
	}
	if records[0].RebasePaused || records[0].CanUndo() {
		t.Fatalf("aborted edit incorrectly offers undo: %+v", records[0])
	}
}
