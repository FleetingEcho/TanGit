package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	OperationStage   = "stage"
	OperationUnstage = "unstage"

	// UndoRestoreIndex puts back the index tree an action started from. It
	// touches no working-tree file, so edits made elsewhere after the action
	// do not block it; only a change to the index itself or to HEAD does.
	UndoRestoreIndex = "restore-index"

	// indexJournalKeep bounds how many staging records a checkout keeps. They
	// are cheap, but a click per file would otherwise grow without limit.
	indexJournalKeep = 200
)

// indexTree writes the index as a tree object. It fails while the index holds
// unmerged entries, which is exactly when a plain tree cannot describe it.
func (s *gitSession) indexTree() (string, error) {
	tree, err := s.gitText("write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(tree), nil
}

// mutateIndex runs an index-only action and, with the safety journal on,
// records the index before and after it so Undo can put it back. An index with
// unmerged entries (a conflict) cannot be described as a tree; those actions
// run unrecorded here and are covered by the conflict recovery records.
func (s *gitSession) mutateIndex(ctx context.Context, kind, summary string, mutate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	return s.mutateIndexLocked(ctx, kind, summary, mutate)
}

// mutateIndexLocked is mutateIndex for a caller that already holds the
// worktree mutation lock, as patch application does.
func (s *gitSession) mutateIndexLocked(ctx context.Context, kind, summary string, mutate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !SafetyJournalEnabled() {
		return mutate()
	}
	before, err := s.indexTree()
	if err != nil {
		return mutate()
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return mutate()
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return mutate()
	}
	record := OperationRecord{
		Version: journalVersion, ID: hex.EncodeToString(random[:]), Kind: kind,
		Summary: RedactSecrets(strings.TrimSpace(summary)), Status: OperationPending,
		CreatedAt: time.Now().Unix(), Sequence: time.Now().UnixNano(), Worktree: s.git.RepoPaths.WorktreePath(),
		Branch: branch, HeadBefore: head, UndoKind: UndoRestoreIndex, IndexBefore: before,
	}
	ref, err := s.pinJournalObject(record.ID, before)
	if err != nil {
		// Without a pinned tree there is nothing to restore later; the action
		// itself is still safe to run.
		return mutate()
	}
	record.RefName = ref
	name, _ := s.operationPath(record.ID)
	if err := s.writeOperationRecord(name, record); err != nil {
		_, _ = s.cmd.New([]string{"git", "update-ref", "-d", ref}).RunWithOutput()
		return mutate()
	}

	actionErr := mutate()

	after, afterErr := s.indexTree()
	if afterErr == nil && after == before {
		// Nothing changed (already staged, or the action failed early): there
		// is nothing to undo, so leave no entry for Ctrl+Z to offer.
		s.discardSmallRecord(record)
		return actionErr
	}
	branchAfter, headAfter, identityErr := s.currentOperationIdentity()
	record.BranchAfter, record.HeadAfter = branchAfter, headAfter
	record.IndexAfter = after
	record.FinishedAt = time.Now().Unix()
	record.Status = OperationSucceeded
	if actionErr != nil {
		record.Error = RedactSecrets(actionErr.Error())
		record.Status = OperationFailed
		record.Partial = true
	}
	if afterErr != nil || identityErr != nil {
		record.Status = OperationInterrupted
		record.UndoKind = ""
	}
	if err := s.writeOperationRecord(name, record); err != nil {
		if actionErr != nil {
			return fmt.Errorf("%w (also could not finish the safety journal: %v)", actionErr, err)
		}
		return fmt.Errorf("the index changed, but its safety journal could not be finished: %w", err)
	}
	s.pruneSmallOperations(ctx)
	return actionErr
}

// requireUnchangedIndex is the precondition of UndoRestoreIndex: the index and
// HEAD are exactly what the action left. Working-tree edits do not matter.
func (s *gitSession) requireUnchangedIndex(record OperationRecord) error {
	if record.Worktree != s.git.RepoPaths.WorktreePath() {
		return fmt.Errorf("operation belongs to a different worktree")
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return err
	}
	if branch != record.BranchAfter || head != record.HeadAfter {
		return fmt.Errorf("HEAD or its branch moved after this operation; undo would change what a later commit recorded")
	}
	current, err := s.indexTree()
	if err != nil {
		return fmt.Errorf("the index has unresolved conflicts; resolve or abort them first")
	}
	if current != record.IndexAfter {
		return fmt.Errorf("files were staged or unstaged after this operation; undo would overwrite that newer staging")
	}
	return s.validateJournalPin(record.RefName, record.IndexBefore)
}

// undoIndexOperation restores the saved index and verifies the result.
func (s *gitSession) undoIndexOperation(record OperationRecord) error {
	if record.IndexBefore == "" {
		return fmt.Errorf("undo operation: the saved index is unavailable")
	}
	if output, err := s.cmd.New([]string{"git", "read-tree", record.IndexBefore}).RunWithOutput(); err != nil {
		return fmt.Errorf("restore the index: %w: %s", err, strings.TrimSpace(output))
	}
	restored, err := s.indexTree()
	if err != nil || restored != record.IndexBefore {
		return fmt.Errorf("undo did not reproduce the original index")
	}
	return nil
}
