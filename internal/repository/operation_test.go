package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func operationSession(t *testing.T, session Session) OperationSession {
	t.Helper()
	operations, ok := session.(OperationSession)
	if !ok {
		t.Fatal("repository session does not implement OperationSession")
	}
	return operations
}

func TestMergeOperationStateAndContinue(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	operations := operationSession(t, session)
	ctx := context.Background()

	state, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Type != OperationTypeMerge {
		t.Fatalf("operation type = %q, want merge", state.Type)
	}
	if !slices.Contains(state.FullRefs, "refs/heads/main") {
		t.Fatalf("full refs = %#v, want refs/heads/main", state.FullRefs)
	}
	if want := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "ORIG_HEAD")); state.OrigSHA != want {
		t.Fatalf("orig SHA = %q, want %q", state.OrigSHA, want)
	}
	if want := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "MERGE_HEAD")); state.IncomingSHA != want {
		t.Fatalf("incoming SHA = %q, want %q", state.IncomingSHA, want)
	}
	if state.Fingerprint == "" || !slices.Contains(state.Actions, OperationActionContinue) ||
		!slices.Contains(state.Actions, OperationActionAbort) || slices.Contains(state.Actions, OperationActionSkip) {
		t.Fatalf("merge state has wrong fingerprint/actions: %#v", state)
	}

	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.OperationState == nil || snapshot.OperationState.Fingerprint != state.Fingerprint || snapshot.Operation != "merge" {
		t.Fatalf("snapshot did not carry the live operation state: %#v", snapshot.OperationState)
	}
	if err := session.ResolveConflict(ctx, fileNamed(t, snapshot), false); err != nil {
		t.Fatal(err)
	}
	resolved, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Fingerprint == state.Fingerprint {
		t.Fatal("resolving an unmerged index stage did not change the operation fingerprint")
	}
	if err := operations.DriveOperation(ctx, OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: resolved.Fingerprint,
	}); err != nil {
		t.Fatalf("continue merge: %v", err)
	}
	finished, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Type != OperationTypeNone || finished.Fingerprint != "" {
		t.Fatalf("operation remains after continue: %#v", finished)
	}
}

func conflictedRebase(t *testing.T) (string, FullSession, string, string) {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feature work")
	featureSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "main work")
	ontoSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "feature")
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RebaseOntoBranch(context.Background(), "main"); err == nil {
		t.Fatal("conflicting rebase unexpectedly succeeded")
	}
	return dir, session, featureSHA, ontoSHA
}

func TestRebaseOperationStateContinueSkipAndAbort(t *testing.T) {
	t.Run("continue", func(t *testing.T) {
		dir, session, featureSHA, ontoSHA := conflictedRebase(t)
		operations := operationSession(t, session)
		ctx := context.Background()
		state, err := operations.OperationState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.Type != OperationTypeRebase || !slices.Contains(state.FullRefs, "refs/heads/feature") ||
			state.OntoSHA != ontoSHA || state.OrigSHA != featureSHA || state.StoppedSHA != featureSHA {
			t.Fatalf("unexpected rebase state: %#v (feature %s onto %s)", state, featureSHA, ontoSHA)
		}
		if state.Step != 1 || state.Done != 0 || state.Total != 1 {
			t.Fatalf("rebase progress = step %d done %d total %d, want 1/0/1", state.Step, state.Done, state.Total)
		}
		for _, action := range []OperationAction{OperationActionContinue, OperationActionSkip, OperationActionAbort} {
			if !state.Can(action) {
				t.Fatalf("rebase cannot %s: %#v", action, state.Actions)
			}
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ResolveConflict(ctx, fileNamed(t, snapshot), false); err != nil {
			t.Fatal(err)
		}
		resolved, err := operations.OperationState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := operations.DriveOperation(ctx, OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: resolved.Fingerprint}); err != nil {
			t.Fatalf("continue rebase: %v", err)
		}
		finished, err := operations.OperationState(ctx)
		if err != nil || finished.Type != OperationTypeNone {
			t.Fatalf("rebase remains after continue: %#v, %v", finished, err)
		}
		if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "feature\n" {
			t.Fatalf("continued rebase content = %q, want feature", got)
		}
	})

	for _, action := range []OperationAction{OperationActionSkip, OperationActionAbort} {
		t.Run(string(action), func(t *testing.T) {
			dir, session, featureSHA, ontoSHA := conflictedRebase(t)
			operations := operationSession(t, session)
			state, err := operations.OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: action, ExpectedFingerprint: state.Fingerprint}); err != nil {
				t.Fatalf("%s rebase: %v", action, err)
			}
			want := ontoSHA
			if action == OperationActionAbort {
				want = featureSHA
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != want {
				t.Fatalf("HEAD after %s = %s, want %s", action, got, want)
			}
		})
	}
}

func conflictedCherryPick(t *testing.T) (string, FullSession, string, string) {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feature work")
	incomingSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "main work")
	headSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.CherryPickCommit(context.Background(), Commit{Hash: incomingSHA, ShortHash: incomingSHA[:7]}); err == nil {
		t.Fatal("conflicting cherry-pick unexpectedly succeeded")
	}
	return dir, session, incomingSHA, headSHA
}

func TestCherryPickOperationStateContinueSkipAndAbort(t *testing.T) {
	t.Run("continue", func(t *testing.T) {
		dir, session, incomingSHA, headSHA := conflictedCherryPick(t)
		operations := operationSession(t, session)
		ctx := context.Background()
		state, err := operations.OperationState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.Type != OperationTypeCherryPick || state.IncomingSHA != incomingSHA || state.OntoSHA != headSHA ||
			!slices.Contains(state.FullRefs, "refs/heads/main") || !state.Can(OperationActionSkip) {
			t.Fatalf("unexpected cherry-pick state: %#v", state)
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ResolveConflict(ctx, fileNamed(t, snapshot), false); err != nil {
			t.Fatal(err)
		}
		resolved, err := operations.OperationState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := operations.DriveOperation(ctx, OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: resolved.Fingerprint}); err != nil {
			t.Fatalf("continue cherry-pick: %v", err)
		}
		if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "feature\n" {
			t.Fatalf("continued cherry-pick content = %q, want feature", got)
		}
	})

	for _, action := range []OperationAction{OperationActionSkip, OperationActionAbort} {
		t.Run(string(action), func(t *testing.T) {
			dir, session, _, headSHA := conflictedCherryPick(t)
			operations := operationSession(t, session)
			state, err := operations.OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: action, ExpectedFingerprint: state.Fingerprint}); err != nil {
				t.Fatalf("%s cherry-pick: %v", action, err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != headSHA {
				t.Fatalf("HEAD after %s = %s, want %s", action, got, headSHA)
			}
		})
	}
}

func conflictedAM(t *testing.T) (string, FullSession, string, string) {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "feature work")
	incomingSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	patch := []byte(gitOutput(t, dir, "format-patch", "-1", "--stdout", incomingSHA))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "main work")
	headSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.AmPatch(context.Background(), "feature.patch", patch); err == nil {
		t.Fatal("conflicting am unexpectedly succeeded")
	}
	return dir, session, incomingSHA, headSHA
}

func TestAMOperationStateContinueSkipAndAbort(t *testing.T) {
	t.Run("continue", func(t *testing.T) {
		dir, session, incomingSHA, headSHA := conflictedAM(t)
		operations := operationSession(t, session)
		ctx := context.Background()
		state, err := operations.OperationState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Some Git versions retain mailbox From <sha>, while others strip it.
		// Either the exact source SHA or an honest unknown is valid, never an
		// inferred blob or nearby commit.
		if state.Type != OperationTypeAM || (state.IncomingSHA != "" && state.IncomingSHA != incomingSHA) ||
			state.StoppedSHA != state.IncomingSHA || state.OrigSHA != headSHA || state.OntoSHA != headSHA ||
			!slices.Contains(state.FullRefs, "refs/heads/main") {
			t.Fatalf("unexpected am state: %#v (incoming %s HEAD %s)", state, incomingSHA, headSHA)
		}
		if state.Step != 1 || state.Done != 0 || state.Total != 1 {
			t.Fatalf("am progress = step %d done %d total %d, want 1/0/1", state.Step, state.Done, state.Total)
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := session.ResolveConflict(ctx, fileNamed(t, snapshot), false); err != nil {
			t.Fatal(err)
		}
		resolved, err := operations.OperationState(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := operations.DriveOperation(ctx, OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: resolved.Fingerprint}); err != nil {
			t.Fatalf("continue am: %v", err)
		}
		if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "feature\n" {
			t.Fatalf("continued am content = %q, want feature", got)
		}
	})

	for _, action := range []OperationAction{OperationActionSkip, OperationActionAbort} {
		t.Run(string(action), func(t *testing.T) {
			dir, session, _, headSHA := conflictedAM(t)
			operations := operationSession(t, session)
			state, err := operations.OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: action, ExpectedFingerprint: state.Fingerprint}); err != nil {
				t.Fatalf("%s am: %v", action, err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != headSHA {
				t.Fatalf("HEAD after %s = %s, want %s", action, got, headSHA)
			}
			finished, err := operations.OperationState(context.Background())
			if err != nil || finished.Type != OperationTypeNone {
				t.Fatalf("am remains after %s: %#v, %v", action, finished, err)
			}
		})
	}
}

func conflictedRevert(t *testing.T) (string, FullSession, string, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for _, step := range []struct {
		content string
		subject string
	}{{"base\n", "initial"}, {"target\n", "target change"}, {"latest\n", "latest change"}} {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(step.content), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		runGit(t, dir, "commit", "-m", step.subject)
	}
	incomingSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD~1"))
	headSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	snapshotSession, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshotSession.RevertCommit(context.Background(), Commit{Hash: incomingSHA, ShortHash: incomingSHA[:7], Parents: []string{"parent"}}); err == nil {
		t.Fatal("conflicting revert unexpectedly succeeded")
	}
	return dir, snapshotSession, incomingSHA, headSHA
}

func TestRevertOperationStateContinueSkipAndAbort(t *testing.T) {
	t.Run("continue", func(t *testing.T) {
		dir, session, incomingSHA, headSHA := conflictedRevert(t)
		operations := operationSession(t, session)
		state, err := operations.OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if state.Type != OperationTypeRevert || state.IncomingSHA != incomingSHA || state.OntoSHA != headSHA ||
			!slices.Contains(state.FullRefs, "refs/heads/main") || !state.Can(OperationActionSkip) {
			t.Fatalf("unexpected revert state: %#v", state)
		}
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		resolved, err := operations.OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: resolved.Fingerprint}); err != nil {
			t.Fatalf("continue revert: %v", err)
		}
		if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != "base\n" {
			t.Fatalf("continued revert content = %q, want base", got)
		}
	})
	for _, action := range []OperationAction{OperationActionSkip, OperationActionAbort} {
		t.Run(string(action), func(t *testing.T) {
			dir, session, _, headSHA := conflictedRevert(t)
			operations := operationSession(t, session)
			state, err := operations.OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: action, ExpectedFingerprint: state.Fingerprint}); err != nil {
				t.Fatalf("%s revert: %v", action, err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != headSHA {
				t.Fatalf("HEAD after %s = %s, want %s", action, got, headSHA)
			}
		})
	}
}

func TestContinueStoppingAtTheNextRebaseConflictIsNormal(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	for _, name := range []string{"file.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("base\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "file.txt", "second.txt")
	runGit(t, dir, "commit", "-m", "initial")
	runGit(t, dir, "checkout", "-b", "feature")
	var commits []string
	for _, change := range []struct {
		name    string
		content string
	}{{"file.txt", "feature one\n"}, {"second.txt", "feature two\n"}} {
		if err := os.WriteFile(filepath.Join(dir, change.name), []byte(change.content), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", change.name)
		runGit(t, dir, "commit", "-m", strings.TrimSpace(change.content))
		commits = append(commits, strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")))
	}
	runGit(t, dir, "checkout", "main")
	for _, name := range []string{"file.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("main\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", "file.txt", "second.txt")
	runGit(t, dir, "commit", "-m", "main work")
	runGit(t, dir, "checkout", "feature")
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RebaseOntoBranch(context.Background(), "main"); err == nil {
		t.Fatal("first conflicting pick unexpectedly succeeded")
	}
	operations := operationSession(t, session)
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ResolveConflict(context.Background(), fileNamed(t, snapshot), false); err != nil {
		t.Fatal(err)
	}
	resolved, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: resolved.Fingerprint}); err != nil {
		t.Fatalf("continue to the next conflict returned an exceptional error: %v", err)
	}
	stopped, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Type != OperationTypeRebase || stopped.Step != 2 || stopped.Done != 1 || stopped.Total != 2 || stopped.StoppedSHA != commits[1] {
		t.Fatalf("did not stop at the second pick: %#v (want %s)", stopped, commits[1])
	}
}

func TestConflictedRebaseJournalStaysActiveUntilAbort(t *testing.T) {
	_, session, _, _ := conflictedRebase(t)
	ctx := context.Background()
	records, err := session.Operations(ctx)
	if err != nil || len(records) == 0 {
		t.Fatalf("read rebase journal: %#v, %v", records, err)
	}
	record := records[0]
	if record.Status == OperationFailed || !record.RebasePaused || record.ActiveOperation != OperationTypeRebase || record.CanUndo() {
		t.Fatalf("conflicted rebase was not kept active: %#v", record)
	}
	operations := operationSession(t, session)
	state, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := operations.DriveOperation(ctx, OperationDriveOptions{Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	records, err = session.Operations(ctx)
	if err != nil || len(records) == 0 {
		t.Fatalf("read aborted rebase journal: %#v, %v", records, err)
	}
	record = records[0]
	if record.ActiveOperation != OperationTypeNone || record.RebasePaused || record.Status != OperationInterrupted || record.CanUndo() {
		t.Fatalf("aborted rebase journal was not closed: %#v", record)
	}
}

func TestConflictedHistoryRebaseJournalFinalizesAfterContinue(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	var commits []Commit
	for index, content := range []string{"base\n", "middle\n", "tip\n"} {
		if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "file.txt")
		runGit(t, dir, "commit", "-m", fmt.Sprintf("commit %d", index))
		hash := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
		commits = append(commits, Commit{Hash: hash, ShortHash: hash[:7]})
	}
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.DropCommit(context.Background(), commits[1]); err == nil {
		t.Fatal("dropping the middle commit did not conflict with its child")
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("tip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	operations := operationSession(t, session)
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := operations.DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint}); err != nil {
		t.Fatalf("continue conflicted history rebase: %v", err)
	}
	records, err := session.Operations(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("history rebase journal = %#v, %v", records, err)
	}
	if records[0].ActiveOperation != OperationTypeNone || records[0].RebasePaused || records[0].Status != OperationSucceeded || !records[0].CanUndo() {
		t.Fatalf("continued history rebase journal was not finalized: %#v", records[0])
	}
}

func TestPullRebaseProvenanceRequiresOwnJournal(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial")
	remoteRoot := t.TempDir()
	remote := filepath.Join(remoteRoot, "remote.git")
	runGit(t, remoteRoot, "init", "--bare", remote)
	runGit(t, dir, "remote", "add", "origin", remote)
	runGit(t, dir, "push", "-u", "origin", "main")

	other := filepath.Join(remoteRoot, "other")
	runGit(t, remoteRoot, "clone", remote, other)
	runGit(t, other, "checkout", "-B", "main", "origin/main")
	runGit(t, other, "config", "user.name", "Remote Test")
	runGit(t, other, "config", "user.email", "remote@example.com")
	if err := os.WriteFile(filepath.Join(other, "file.txt"), []byte("remote\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, other, "add", "file.txt")
	runGit(t, other, "commit", "-m", "remote work")
	runGit(t, other, "push", "origin", "main")
	ontoSHA := strings.TrimSpace(gitOutput(t, other, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "local work")
	origSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := session.PullWithStrategy(ctx, PullRebase); err == nil {
		t.Fatal("diverged pull-rebase unexpectedly succeeded")
	}
	operations := operationSession(t, session)
	state, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Type != OperationTypePullRebase || state.OrigSHA != origSHA || state.OntoSHA != ontoSHA {
		t.Fatalf("own pull-rebase state = %#v, want pull-rebase", state)
	}
	if err := operations.DriveOperation(ctx, OperationDriveOptions{Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint}); err != nil {
		t.Fatalf("abort own pull-rebase: %v", err)
	}

	runGitExpectFailure(t, dir, "pull", "--no-edit", "--rebase", "origin", "refs/heads/main")
	external, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if external.Type != OperationTypeRebase {
		t.Fatalf("external rebase with no active own journal was labeled %q", external.Type)
	}
	runGit(t, dir, "rebase", "--abort")
}

func TestDriveOperationReturnsLockErrorWithoutDeletingLock(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	operations := operationSession(t, session)
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ResolveConflict(context.Background(), fileNamed(t, snapshot), false); err != nil {
		t.Fatal(err)
	}
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("held by another process\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = operations.DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint})
	if err == nil || !strings.Contains(err.Error(), "index.lock") {
		t.Fatalf("locked continue returned %v, want Git's index.lock error", err)
	}
	if content, readErr := os.ReadFile(lock); readErr != nil || string(content) != "held by another process\n" {
		t.Fatalf("index.lock was removed or replaced: %q, %v", content, readErr)
	}
}

func TestDriveOperationRefusesUnresolvedContinue(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	operations := operationSession(t, session)
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = operations.DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint})
	if err == nil || !strings.Contains(err.Error(), "resolve all conflicts") || errors.Is(err, ErrOperationChanged) {
		t.Fatalf("unresolved continue returned %v", err)
	}
	if got := gitOutput(t, dir, "rev-parse", "--verify", "MERGE_HEAD"); got == "" {
		t.Fatal("unresolved preflight unexpectedly finished the merge")
	}
}

func TestDriveOperationRejectsExternallyChangedMetadata(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	operations := operationSession(t, session)
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "MERGE_MSG"), []byte("edited outside lazygitGo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = operations.DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint})
	if !errors.Is(err, ErrOperationChanged) {
		t.Fatalf("metadata-changed abort returned %v, want ErrOperationChanged", err)
	}
	if got := gitOutput(t, dir, "rev-parse", "--verify", "MERGE_HEAD"); got == "" {
		t.Fatal("metadata CAS failure unexpectedly aborted the merge")
	}
}

func TestDriveOperationRejectsExternallyChangedState(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	operations := operationSession(t, session)
	ctx := context.Background()
	state, err := operations.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Resolve outside lazygitGo after the UI captured its operation fingerprint.
	runGit(t, dir, "checkout", "--theirs", "--", "file.txt")
	runGit(t, dir, "add", "--", "file.txt")
	err = operations.DriveOperation(ctx, OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
	})
	if !errors.Is(err, ErrOperationChanged) {
		t.Fatalf("stale operation continue returned %v, want ErrOperationChanged", err)
	}
	if got := gitOutput(t, dir, "rev-parse", "--verify", "MERGE_HEAD"); got == "" {
		t.Fatal("stale continue unexpectedly finished the merge")
	}
}
