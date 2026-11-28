package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func operationsOfKind(t *testing.T, session Session, kind string) []OperationRecord {
	t.Helper()
	records, err := session.Operations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []OperationRecord
	for _, record := range records {
		if record.Kind == kind {
			out = append(out, record)
		}
	}
	return out
}

func latestOfKind(t *testing.T, session Session, kind string) OperationRecord {
	t.Helper()
	records := operationsOfKind(t, session, kind)
	if len(records) == 0 {
		t.Fatalf("no %s record in the journal", kind)
	}
	return records[0]
}

func TestUndoingStageRestoresTheExactIndexAndLeavesFilesAlone(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"a.txt", "b.txt", "del.txt", "sp ace/ünï.txt", "new.txt", "empty.txt", "bin.dat"} {
		t.Run(path, func(t *testing.T) {
			dir, session := richWorkspace(t)
			before := fullDigest(t, dir)
			if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), path)); err != nil {
				t.Fatal(err)
			}
			record := latestOfKind(t, session, OperationStage)
			if !record.CanUndo() || record.Summary != "Stage "+path {
				t.Fatalf("stage record = %#v", record)
			}
			worktree := worktreeDigest(t, dir)
			if err := session.UndoOperation(ctx, record.ID); err != nil {
				t.Fatalf("undo stage: %v", err)
			}
			if got := worktreeDigest(t, dir); got != worktree {
				t.Fatal("undoing a stage changed a working-tree file")
			}
			if got := fullDigest(t, dir); got != before {
				t.Fatalf("undo stage did not restore the exact state\n got: %s\nwant: %s", got, before)
			}
		})
	}
}

func TestUndoingUnstageRestoresTheExactIndex(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := fullDigest(t, dir)
	if err := session.Unstage(ctx, fileByPath(t, snapshotFiles(t, session), "staged-new.txt")); err != nil {
		t.Fatal(err)
	}
	if got := fullDigest(t, dir); got == before {
		t.Fatal("unstage changed nothing")
	}
	record := latestOfKind(t, session, OperationUnstage)
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo unstage: %v", err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatalf("undo unstage did not restore the exact state\n got: %s\nwant: %s", got, before)
	}
	if got := gitOutput(t, dir, "show", ":staged-new.txt"); got != "brand new\n" {
		t.Fatalf("staged content = %q", got)
	}
}

func TestUndoingStageAllAndUnstageAll(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := fullDigest(t, dir)
	if err := session.StageAll(ctx); err != nil {
		t.Fatal(err)
	}
	staged := fullDigest(t, dir)
	if err := session.UndoOperation(ctx, latestOfKind(t, session, OperationStage).ID); err != nil {
		t.Fatalf("undo stage all: %v", err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("undo stage all did not restore the starting state")
	}
	if err := session.StageAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fullDigest(t, dir); got != staged {
		t.Fatal("staging again gave a different index")
	}
	if err := session.UnstageAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, latestOfKind(t, session, OperationUnstage).ID); err != nil {
		t.Fatalf("undo unstage all: %v", err)
	}
	if got := fullDigest(t, dir); got != staged {
		t.Fatal("undo unstage all did not bring back the staged index")
	}
}

func TestUndoingStageSurvivesEditsElsewhereAndARestart(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	record := latestOfKind(t, session, OperationStage)
	// Work in other files after staging must not block the undo.
	writeSafetyFile(t, dir, "b.txt", "edited after staging\n")
	writeSafetyFile(t, dir, "another-untracked.txt", "x\n")
	reopened, err := openFullRepositorySessionForTest((Opener{}).Open(ctx, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo after unrelated edits and a restart: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "edited after staging\n" {
		t.Fatalf("undo disturbed an unrelated edit: %q", got)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage", "--", "new.txt"); got != "" {
		t.Fatalf("new.txt is still staged: %q", got)
	}
	if final := latestOfKind(t, reopened, OperationStage); final.UndoneAt == 0 || final.CanUndo() {
		t.Fatalf("an undone stage can be undone again: %#v", final)
	}
}

func TestUndoingStageRefusesWhenTheIndexOrHeadMoved(t *testing.T) {
	ctx := context.Background()
	t.Run("staged something else afterwards", func(t *testing.T) {
		dir, session := richWorkspace(t)
		if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
			t.Fatal(err)
		}
		first := latestOfKind(t, session, OperationStage)
		if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "b.txt")); err != nil {
			t.Fatal(err)
		}
		before := fullDigest(t, dir)
		err := session.UndoOperation(ctx, first.ID)
		if err == nil || !strings.Contains(err.Error(), "staged or unstaged after") {
			t.Fatalf("undo of an older stage = %v, want a refusal that says why", err)
		}
		if got := fullDigest(t, dir); got != before {
			t.Fatal("a refused undo changed the repository")
		}
		// The newest one still undoes, and then the older one becomes possible.
		records := operationsOfKind(t, session, OperationStage)
		if err := session.UndoOperation(ctx, records[0].ID); err != nil {
			t.Fatal(err)
		}
		if err := session.UndoOperation(ctx, first.ID); err != nil {
			t.Fatalf("undoing in order failed: %v", err)
		}
	})
	t.Run("committed afterwards", func(t *testing.T) {
		dir, session := richWorkspace(t)
		if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
			t.Fatal(err)
		}
		record := latestOfKind(t, session, OperationStage)
		if err := session.Commit(ctx, "commit it", "", false); err != nil {
			t.Fatal(err)
		}
		before := fullDigest(t, dir)
		err := session.UndoOperation(ctx, record.ID)
		if err == nil || !strings.Contains(err.Error(), "HEAD") {
			t.Fatalf("undo after a commit = %v, want a HEAD refusal", err)
		}
		if got := fullDigest(t, dir); got != before {
			t.Fatal("a refused undo changed the repository")
		}
	})
}

func TestStageThatChangesNothingLeavesNoUndoEntry(t *testing.T) {
	_, session := richWorkspace(t)
	ctx := context.Background()
	if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "staged-new.txt")); err != nil {
		t.Fatal(err)
	}
	if got := operationsOfKind(t, session, OperationStage); len(got) != 0 {
		t.Fatalf("a no-op stage left a record: %#v", got)
	}
}

func TestStagingIsNotRecordedWhileTheSafetyJournalIsOff(t *testing.T) {
	dir, session := richWorkspace(t)
	previous := SafetyJournalEnabled()
	SetSafetyJournalEnabled(false)
	defer SetSafetyJournalEnabled(previous)
	if err := session.Stage(context.Background(), fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	if got := operationsOfKind(t, session, OperationStage); len(got) != 0 {
		t.Fatalf("journal off but a record was written: %#v", got)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage", "--", "new.txt"); got == "" {
		t.Fatal("staging must still work with the journal off")
	}
}

func TestStagingAConflictedFileStillWorksUnrecorded(t *testing.T) {
	dir := conflictedRepository(t)
	enableSafetyJournalForTest(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Stage(context.Background(), fileNamed(t, mustSnapshot(t, session))); err != nil {
		t.Fatalf("mark resolved: %v", err)
	}
	if got := operationsOfKind(t, session, OperationStage); len(got) != 0 {
		t.Fatalf("an unmerged index cannot be a tree; got a record %#v", got)
	}
	if file := fileNamed(t, mustSnapshot(t, session)); file.HasMergeConflicts {
		t.Fatal("the conflict was not marked resolved")
	}
}

func TestStagingRecordsAreBounded(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	for i := 0; i < indexJournalKeep+15; i++ {
		writeSafetyFile(t, dir, "a.txt", strings.Repeat("x", i+1)+"\n")
		if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "a.txt")); err != nil {
			t.Fatal(err)
		}
		if err := session.Unstage(ctx, fileByPath(t, snapshotFiles(t, session), "a.txt")); err != nil {
			t.Fatal(err)
		}
	}
	count := len(operationsOfKind(t, session, OperationStage)) + len(operationsOfKind(t, session, OperationUnstage))
	if count > indexJournalKeep {
		t.Fatalf("journal kept %d staging records, limit %d", count, indexJournalKeep)
	}
	// The newest is still undoable.
	if err := session.UndoOperation(ctx, latestOfKind(t, session, OperationUnstage).ID); err != nil {
		t.Fatalf("newest record lost to pruning: %v", err)
	}
}

func TestDiscardOfAStagedUntrackedFileStillUndoesExactly(t *testing.T) {
	// discardFile unstages through the raw command, not the journaled one: a
	// second journal write under the discard's lock would deadlock.
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := fullDigest(t, dir)
	if err := session.Discard(ctx, fileByPath(t, snapshotFiles(t, session), "staged-new.txt")); err != nil {
		t.Fatal(err)
	}
	if got := operationsOfKind(t, session, OperationUnstage); len(got) != 0 {
		t.Fatalf("a discard's inner unstage leaked its own record: %#v", got)
	}
	if err := session.UndoOperation(ctx, latestOfKind(t, session, OperationDiscard).ID); err != nil {
		t.Fatal(err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("undo discard did not restore the exact state")
	}
}
