package app

import (
	"context"
	"errors"
	"fmt"

	"lazygitapp/internal/repository"
)

// runFileRecoveryMutation persists an unresolved baseline before the first
// action on a path, then advances the same record to the exact state left by
// each action. Later hunk picks and final staging therefore never replace the
// original marker bytes or unmerged index stages in Initial.
//
// Lightweight Session fakes predate V2 and take the legacy path. Real local
// and SSH sessions implement FileRecoveryWorkflowSession; unsupported file
// kinds return before mutate and are never silently resolved without a copy.
func runFileRecoveryMutation(ctx context.Context, session repository.Session, path string, mutate func() error) error {
	recovery, ok := session.(repository.FileRecoveryWorkflowSession)
	if !ok {
		return mutate()
	}

	id, err := activeFileRecoveryID(ctx, recovery, path)
	if err != nil {
		return fmt.Errorf("prepare persistent conflict recovery for %s: %w", path, err)
	}
	// Revalidate this exact path even when an earlier record exists. A path
	// replaced by a symlink or another unsupported kind must not inherit the
	// old record as permission to mutate without a faithful new after-state.
	initial, captureErr := recovery.CaptureFileRecoveryState(ctx, path)
	if captureErr != nil {
		return fmt.Errorf("prepare persistent conflict recovery for %s: %w", path, captureErr)
	}
	if id == "" {
		record, archiveErr := recovery.ArchiveFileRecovery(ctx, repository.FileRecoveryArchive{
			Path: path, After: initial,
		})
		if archiveErr != nil {
			return fmt.Errorf("save initial conflict recovery for %s: %w", path, archiveErr)
		}
		id = record.OperationID
	}

	mutationErr := mutate()
	// Record the actual after-state even when Git reports an error: conflict
	// resolution can fail after changing one of the worktree/index components.
	// Restore's per-path CAS will then protect precisely those visible bytes.
	after, captureErr := recovery.CaptureFileRecoveryState(ctx, path)
	if captureErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("capture recovery after-state for %s: %w", path, captureErr))
	}
	if _, recordErr := recovery.RecordFileRecoveryAfter(ctx, id, after); recordErr != nil {
		return errors.Join(mutationErr, fmt.Errorf("record recovery after-state for %s: %w", path, recordErr))
	}
	return mutationErr
}

func activeFileRecoveryID(ctx context.Context, recovery repository.FileRecoveryWorkflowSession, path string) (string, error) {
	records, err := recovery.ListFileRecovery(ctx)
	if err != nil {
		return "", err
	}
	for _, record := range records {
		if record.Path == path && record.Status == repository.FileRecoveryReady && record.Progress == repository.FileRecoveryPending {
			return record.OperationID, nil
		}
	}
	return "", nil
}

// refreshFileRecoveries publishes manifest discovery after direct hunk writes,
// which intentionally do not run a full repository refresh because the index
// is unchanged. Whole-file actions already receive it through Snapshot.
func (a *app) refreshFileRecoveries() {
	recovery, ok := a.session.(repository.FileRecoveryWorkflowSession)
	if !ok {
		return
	}
	generation := a.generation
	a.fileRecoveryRefreshSeq++
	sequence := a.fileRecoveryRefreshSeq
	go func() {
		defer a.recoverBackground()
		records, err := recovery.ListFileRecovery(context.Background())
		if err != nil {
			return
		}
		a.updateChanged(func() bool {
			if generation != a.generation || sequence != a.fileRecoveryRefreshSeq {
				return false
			}
			a.snapshot.FileRecoveries = records
			return true
		})
	}()
}

func fileRecoveryHasUnresolved(record repository.FileRecoveryRecord) bool {
	return record.Initial.Index.Stage1.Present || record.Initial.Index.Stage2.Present || record.Initial.Index.Stage3.Present
}

func fileRecoveryCanRestore(record repository.FileRecoveryRecord) bool {
	return record.Status == repository.FileRecoveryReady || record.Status == repository.FileRecoveryInterrupted
}

func (a *app) latestFileRecovery(path string, unresolvedOnly bool) (repository.FileRecoveryRecord, bool) {
	for _, record := range a.snapshot.FileRecoveries {
		if record.Path != path || !fileRecoveryCanRestore(record) {
			continue
		}
		if unresolvedOnly && !fileRecoveryHasUnresolved(record) {
			continue
		}
		return record, true
	}
	return repository.FileRecoveryRecord{}, false
}

func (a *app) hasRestorableFileRecovery() bool {
	for _, record := range a.snapshot.FileRecoveries {
		if fileRecoveryCanRestore(record) && fileRecoveryHasUnresolved(record) {
			return true
		}
	}
	return false
}

// confirmRestoreFileRecovery is shared by the staged row and Recovery panel.
// Retry deliberately uses RestoreFileRecovery again: V2 resumes the durable
// worktree/index step only after its normal CAS and payload verification.
func (a *app) confirmRestoreFileRecovery(record repository.FileRecoveryRecord) {
	if a.actionBusy || a.session == nil || !fileRecoveryCanRestore(record) {
		return
	}
	retry := record.Status == repository.FileRecoveryInterrupted
	title, confirmLabel := "Restore Unresolved Conflict", "Restore unresolved"
	if retry {
		title, confirmLabel = "Retry File Recovery", "Retry restore"
	}
	detail := "This replaces the current file and index entry with the saved unresolved worktree and original stages 1/2/3. Unrelated files stay untouched. New changes to this file stop the restore before overwriting them."
	if retry {
		detail = "Retry resumes the saved worktree/index step after verifying the current file against its after-state. Any newer work on this file is preserved when verification fails."
	}
	a.askConfirm(confirmRequest{
		title: title, message: record.Path, detail: detail,
		confirmLabel: confirmLabel, destructive: true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Restored unresolved conflict %s", record.Path), record.Path,
				func(ctx context.Context, session repository.Session) error {
					recovery, ok := session.(repository.FileRecoverySession)
					if !ok {
						return fmt.Errorf("%w: this session cannot read durable file recovery", repository.ErrFileRecoveryUnsupported)
					}
					return recovery.RestoreFileRecovery(ctx, record.OperationID)
				}, func() {
					a.fileRecoveryFocus = record.OperationID
				})
		},
	})
}
