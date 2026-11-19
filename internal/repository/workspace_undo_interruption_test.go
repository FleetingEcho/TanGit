package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type undoFailChmodFiles struct{ sessionFiles }

func (undoFailChmodFiles) Chmod(string, os.FileMode) error {
	return fmt.Errorf("injected workspace permission restore failure")
}

func TestInterruptedUndoPreservesPostStateForReviewedRecovery(t *testing.T) {
	dir, session := workspaceRecoveryFixture(t)
	if err := os.WriteFile(filepath.Join(dir, ".env.extra"), []byte("leave-unselected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := discardWorkspaceRecoveryFiles(t, dir, session)
	if record.AfterSnapshot == nil {
		t.Fatal("the operation did not retain its post-operation state")
	}
	afterState, gitAfter, workspaceAfter := record.AfterState, record.GitStateAfter, record.WorkspaceGuardAfter
	concrete := session.(*gitSession)
	concrete.files = undoFailChmodFiles{sessionFiles: localSessionFiles{}}
	if err := session.UndoOperation(context.Background(), record.ID); err == nil || !strings.Contains(err.Error(), "restore permissions") {
		t.Fatalf("permission restoration failure was not returned: %v", err)
	}
	interrupted := onlyOperation(t, session)
	if interrupted.UndoStartedAt == 0 || interrupted.UndoneAt != 0 || interrupted.CanUndo() {
		t.Fatalf("interrupted operation is presented as retryable or completed: %+v", interrupted)
	}
	if interrupted.AfterState != afterState || interrupted.GitStateAfter != gitAfter || interrupted.WorkspaceGuardAfter != workspaceAfter {
		t.Fatal("interrupted Undo rewrote its original post-operation fingerprints")
	}
	if interrupted.AfterSnapshot == nil || interrupted.AfterSnapshot.ID != record.AfterSnapshot.ID || interrupted.Snapshot == nil || interrupted.Snapshot.ID != record.Snapshot.ID {
		t.Fatal("interrupted Undo lost one of its exact recovery points")
	}
	reopened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.UndoOperation(context.Background(), record.ID); err == nil {
		t.Fatal("interrupted Undo was blindly replayed after restart")
	}
	// This is the reviewed roll-forward seam used after examining the failed
	// restoration. It must reconcile extra files left by the partial restore,
	// rather than overwrite all of .git or the complete checkout blindly.
	if err := reopened.(*gitSession).restoreJournalWorkSnapshot(*interrupted.AfterSnapshot); err != nil {
		t.Fatalf("restore preserved post-operation state: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".env.extra")); string(got) != "leave-unselected\n" {
		t.Fatalf("reviewed recovery changed unrelated ignored work: %q", got)
	}
	scope, present, err := storedWorkspaceScope(reopened.(*gitSession), interrupted.AfterSnapshot)
	if err != nil || !present {
		t.Fatalf("post-operation capture policy is unavailable: %v", err)
	}
	current, err := reopened.(*gitSession).currentWorkspaceScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if current.GitStateGuard != gitAfter || current.WorkspaceGuard != workspaceAfter {
		t.Fatal("preserved after snapshot does not reproduce the original post-operation workspace")
	}
}
