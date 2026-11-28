package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conflictedOperation starts the named Git operation on a repository where it
// stops on a conflict in a.txt, and says what it started.
func conflictedOperation(t *testing.T, kind OperationType) (string, FullSession) {
	t.Helper()
	dir, session := recoveryRepository(t)
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "one\ntwo\nthree\nfour\n")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "matrix base")
	switch kind {
	case OperationTypeMerge, OperationTypeCherryPick, OperationTypeAM:
		runGit(t, dir, "checkout", "-b", "incoming")
		write("a.txt", "incoming\ntwo\nthree\nfour\n")
		runGit(t, dir, "commit", "-am", "incoming edit")
		runGit(t, dir, "checkout", "main")
		write("a.txt", "mine\ntwo\nthree\nfour\n")
		runGit(t, dir, "commit", "-am", "main edit")
	case OperationTypeRevert:
		write("a.txt", "mine\ntwo\nthree\nfour\n")
		runGit(t, dir, "commit", "-am", "edit to revert")
		write("a.txt", "later\ntwo\nthree\nfour\n")
		runGit(t, dir, "commit", "-am", "later edit")
	}
	switch kind {
	case OperationTypeMerge:
		runGitExpectFailure(t, dir, "merge", "incoming")
	case OperationTypeCherryPick:
		runGitExpectFailure(t, dir, "cherry-pick", "incoming")
	case OperationTypeRevert:
		runGitExpectFailure(t, dir, "revert", "HEAD~1")
	case OperationTypeAM:
		patch := gitOutput(t, dir, "format-patch", "--stdout", "-1", "incoming")
		write("incoming.patch", patch)
		runGitExpectFailure(t, dir, "am", "--3way", "incoming.patch")
	}
	state, err := session.(*gitSession).OperationState(context.Background())
	if err != nil || state.Type != kind {
		t.Fatalf("%s fixture operation = %q, %v", kind, state.Type, err)
	}
	return dir, session
}

// Every action that ends or advances a conflicted merge, cherry-pick, revert or
// am must leave a recovery point, and Undo must put back exactly the stopped
// operation: the same HEAD, the same unmerged index and the same file.
func TestUndoRestoresEveryOperationTransition(t *testing.T) {
	for _, kind := range []OperationType{OperationTypeMerge, OperationTypeCherryPick, OperationTypeRevert, OperationTypeAM} {
		for _, action := range []OperationAction{OperationActionAbort, OperationActionContinue, OperationActionSkip} {
			t.Run(fmt.Sprintf("%s/%s", kind, action), func(t *testing.T) {
				dir, session := conflictedOperation(t, kind)
				ctx := context.Background()
				state, err := session.(*gitSession).OperationState(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !state.Can(action) {
					if action == OperationActionSkip && kind == OperationTypeMerge {
						return // Git cannot skip a merge, and the app must not offer it.
					}
					t.Fatalf("%s cannot %s: %v", kind, action, state.Actions)
				}
				if action == OperationActionContinue {
					if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("resolved\ntwo\nthree\nfour\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					runGit(t, dir, "add", "a.txt")
					state, err = session.(*gitSession).OperationState(ctx)
					if err != nil {
						t.Fatal(err)
					}
				}
				head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
				index := gitOutput(t, dir, "ls-files", "--stage")
				file, _ := os.ReadFile(filepath.Join(dir, "a.txt"))

				if err := session.(OperationSession).DriveOperation(ctx, OperationDriveOptions{Action: action, ExpectedFingerprint: state.Fingerprint}); err != nil {
					t.Fatalf("%s %s: %v", action, kind, err)
				}
				label := map[OperationAction]string{OperationActionContinue: "Continue", OperationActionSkip: "Skip", OperationActionAbort: "Abort"}[action]
				record := operationWithSummary(t, session, label+" ")
				if record.Kind != OperationTransition || !record.CanUndo() {
					t.Fatalf("%s %s left no undoable recovery point: %+v", action, kind, record)
				}
				if err := session.UndoOperation(ctx, record.ID); err != nil {
					t.Fatalf("undo %s %s: %v", action, kind, err)
				}
				after, err := session.(*gitSession).OperationState(ctx)
				if err != nil || after.Type != kind {
					t.Fatalf("Undo restored operation %q, want %q (%v)", after.Type, kind, err)
				}
				if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != head {
					t.Fatalf("Undo moved HEAD to %s, want %s", got, head)
				}
				if got := gitOutput(t, dir, "ls-files", "--stage"); got != index {
					t.Fatalf("Undo restored index %q, want %q", got, index)
				}
				if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != string(file) {
					t.Fatalf("Undo restored a.txt %q, want %q", got, file)
				}
				// The restored operation is live again, not just look-alike files.
				if !after.Can(OperationActionAbort) {
					t.Fatalf("restored %s cannot be aborted: %v", kind, after.Actions)
				}
			})
		}
	}
}
