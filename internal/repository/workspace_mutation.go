package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
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
	if !SafetyJournalEnabled() && kind != OperationTransition {
		// The user turned the safety journal off: ordinary actions run without a
		// recovery point. Operation transitions are the exception: Git's reflog
		// cannot reconstruct unmerged index stages or operation metadata after an
		// Abort, Continue or Skip, so those transitions always keep an exact point.
		return mutate()
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
	_, record, err := s.captureAndBeginOperation(ctx, capture, kind, summary, UndoRestoreWork)
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

// recoveryAttempts is how often a recovery point is taken again when the
// workspace changed under it. The action has not run yet at that stage, so
// trying again is always safe.
const recoveryAttempts = 4

// isTransientRecoveryError recognizes the two ways a busy checkout defeats one
// attempt at a recovery point: a file changed while it was being saved, or the
// workspace changed between taking the point and checking it.
func isTransientRecoveryError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "unstable object source data") ||
		strings.Contains(text, "workspace changed while creating the recovery point") ||
		strings.Contains(text, "the file kept changing while it was being saved")
}

// captureAndBeginOperation takes a recovery point and opens the journal entry
// that protects it. Other programs write into a checkout all the time (an
// indexer's database, a build, an editor), and one write between the two steps
// used to refuse the whole action. Because nothing has run yet, the attempt is
// repeated a few times before it is given up; a point that did not hold is
// dropped rather than left behind.
func (s *gitSession) captureAndBeginOperation(ctx context.Context, capture func(context.Context) (WorkSnapshot, error), kind, summary, undoKind string) (WorkSnapshot, OperationRecord, error) {
	var lastErr error
	for attempt := 0; attempt < recoveryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return WorkSnapshot{}, OperationRecord{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * 150 * time.Millisecond):
			}
		}
		snapshot, err := capture(ctx)
		if err != nil {
			if !isTransientRecoveryError(err) {
				return WorkSnapshot{}, OperationRecord{}, err
			}
			lastErr = err
			continue
		}
		record, err := s.beginOperation(kind, summary, undoKind, &snapshot)
		if err == nil {
			return snapshot, record, nil
		}
		_ = s.DeleteWorkSnapshot(ctx, snapshot)
		if !isTransientRecoveryError(err) {
			return WorkSnapshot{}, OperationRecord{}, err
		}
		lastErr = err
	}
	return WorkSnapshot{}, OperationRecord{}, lastErr
}
