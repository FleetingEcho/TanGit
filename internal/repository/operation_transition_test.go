package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transitionMergeFixture struct {
	dir, mainSHA string
	session      Session
}

func newTransitionMergeFixture(t *testing.T) transitionMergeFixture {
	t.Helper()
	dir, session := recoveryRepository(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "transition base")
	runGit(t, dir, "checkout", "-b", "incoming")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("incoming\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "incoming edit")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "main edit")
	if err := session.MergeBranch(context.Background(), "incoming"); err == nil {
		t.Fatal("merge fixture unexpectedly had no conflict")
	}
	state, err := session.(*gitSession).OperationState(context.Background())
	if err != nil || state.Type != OperationTypeMerge {
		t.Fatalf("merge fixture operation = %q, %v", state.Type, err)
	}
	return transitionMergeFixture{dir: dir, session: session, mainSHA: strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))}
}

func TestUndoMergeContinueRestoresResolvedPreCommitState(t *testing.T) {
	fixture := newTransitionMergeFixture(t)
	resolved := []byte("resolved\ntwo\nthree\nfour\n")
	if err := os.WriteFile(filepath.Join(fixture.dir, "a.txt"), resolved, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, fixture.dir, "add", "a.txt")
	beforeIndex := gitOutput(t, fixture.dir, "ls-files", "--stage")
	beforeMetadata, err := os.ReadFile(filepath.Join(fixture.dir, ".git", "MERGE_HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := fixture.session.(*gitSession).OperationState(context.Background())
	if err != nil || !state.Can(OperationActionContinue) {
		t.Fatalf("resolved merge is not continuable: %+v, %v", state, err)
	}
	if err := fixture.session.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	record := operationWithSummary(t, fixture.session, "Continue merge")
	if err := fixture.session.UndoOperation(context.Background(), record.ID); err != nil {
		t.Fatalf("undo merge continue: %v", err)
	}
	state, err = fixture.session.(*gitSession).OperationState(context.Background())
	if err != nil || state.Type != OperationTypeMerge || !state.Can(OperationActionContinue) {
		t.Fatalf("Undo did not restore a continuable merge: %+v, %v", state, err)
	}
	if got := strings.TrimSpace(gitOutput(t, fixture.dir, "rev-parse", "HEAD")); got != fixture.mainSHA {
		t.Fatalf("continue Undo moved HEAD to %s, want %s", got, fixture.mainSHA)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.dir, ".git", "MERGE_HEAD")); err != nil || string(got) != string(beforeMetadata) {
		t.Fatalf("continue Undo restored MERGE_HEAD = %q, want %q (%v)", got, beforeMetadata, err)
	}
	if got := gitOutput(t, fixture.dir, "ls-files", "--stage"); got != beforeIndex {
		t.Fatalf("continue Undo restored index = %q, want %q", got, beforeIndex)
	}
	if got, _ := os.ReadFile(filepath.Join(fixture.dir, "a.txt")); string(got) != string(resolved) {
		t.Fatalf("continue Undo restored %q, want %q", got, resolved)
	}
}

func TestUndoMergeAbortRestoresConflictAndIndex(t *testing.T) {
	fixture := newTransitionMergeFixture(t)
	conflicted, err := os.ReadFile(filepath.Join(fixture.dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	conflictedIndex := gitOutput(t, fixture.dir, "ls-files", "--stage")
	beforeMetadata, err := os.ReadFile(filepath.Join(fixture.dir, ".git", "MERGE_HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := fixture.session.(*gitSession).OperationState(context.Background())
	if err != nil || !state.Can(OperationActionAbort) {
		t.Fatalf("conflicted merge is not abortable: %+v, %v", state, err)
	}
	if err := fixture.session.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	record := operationWithSummary(t, fixture.session, "Abort merge")
	if err := fixture.session.UndoOperation(context.Background(), record.ID); err != nil {
		t.Fatalf("undo merge abort: %v", err)
	}
	state, err = fixture.session.(*gitSession).OperationState(context.Background())
	if err != nil || state.Type != OperationTypeMerge {
		t.Fatalf("abort Undo restored operation %q, %v", state.Type, err)
	}
	if got := gitOutput(t, fixture.dir, "ls-files", "--stage"); got != conflictedIndex {
		t.Fatalf("abort Undo restored conflict index = %q, want %q", got, conflictedIndex)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.dir, ".git", "MERGE_HEAD")); err != nil || string(got) != string(beforeMetadata) {
		t.Fatalf("abort Undo restored MERGE_HEAD = %q, want %q (%v)", got, beforeMetadata, err)
	}
	if got, err := os.ReadFile(filepath.Join(fixture.dir, "a.txt")); err != nil || string(got) != string(conflicted) {
		t.Fatalf("abort Undo restored %q, want original conflict %q (%v)", got, conflicted, err)
	}
}
