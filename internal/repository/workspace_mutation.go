package repository

import (
	"context"
	"fmt"
)

const (
	OperationCommit       = "commit"
	OperationMerge        = "merge"
	OperationCherryPick   = "cherry-pick"
	OperationRevert       = "revert"
	OperationStashApply   = "stash-apply"
	OperationStashSave    = "stash-save"
	OperationCheckout     = "checkout"
	OperationFileRestore  = "file-restore"
	OperationBranchCreate = "branch-create"
	OperationBranchRename = "branch-rename"
	OperationStashRewrite = "stash-rewrite"
	OperationTransition   = "operation-transition"
)

func stashStateTrackedOperation(kind string) bool {
	switch kind {
	case OperationStashDrop, OperationStashSave, OperationStashApply, OperationStashRewrite:
		return true
	}
	return false
}

func supportsOperationTransitionRecovery(type_ OperationType) bool {
	switch type_ {
	case OperationTypeMerge, OperationTypeCherryPick, OperationTypeRevert, OperationTypeAM:
		return true
	}
	return false
}

// withWorkspaceMutation serializes local mutations and captures exact
// before/after restore points. The action itself supplies any user-facing
// confirmation and remains responsible for trusted Git behavior. Arbitrary
// custom shell commands and remote effects do not belong to this seam.
type workspaceMutationCapture func(context.Context) (WorkSnapshot, error)

func (s *gitSession) withWorkspaceMutation(ctx context.Context, kind, summary string, movesHead bool, mutate func() error, captures ...workspaceMutationCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if mutate == nil {
		return fmt.Errorf("%s: missing mutation", kind)
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	return s.runWorkspaceMutation(ctx, kind, summary, movesHead, mutate, captures...)
}

// runWorkspaceMutation must be called with the checkout's mutation lock held.
// It lets existing history actions reuse the same recovery contract without
// recursively taking the lock that they already own.
func (s *gitSession) runWorkspaceMutation(ctx context.Context, kind, summary string, movesHead bool, mutate func() error, captures ...workspaceMutationCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(captures) > 1 {
		return fmt.Errorf("%s: multiple recovery captures", kind)
	}
	capture := func(ctx context.Context) (WorkSnapshot, error) {
		return s.captureWorkSnapshot(ctx, RecoverySnapshotKind, summary)
	}
	if len(captures) == 1 {
		if captures[0] == nil {
			return fmt.Errorf("%s: missing recovery capture", kind)
		}
		capture = captures[0]
	}
	snapshot, err := capture(ctx)
	if err != nil {
		return err
	}
	record, err := s.beginOperation(kind, summary, UndoRestoreWork, &snapshot)
	if err != nil {
		return err
	}
	if movesHead && record.HeadBefore != "" {
		record.UndoKind = UndoResetHead
	}
	// An unborn HEAD cannot be reset to an earlier commit. RestoreWork also
	// restores refs, so removing the new head safely recreates the unborn state.
	if record, err = s.pinOperationBefore(record); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return s.failJournalOperation(record, err)
	}
	actionErr := mutate()
	if err := s.completeJournalOperation(record, actionErr); err != nil {
		return err
	}
	return s.pruneRecovery(ctx)
}
