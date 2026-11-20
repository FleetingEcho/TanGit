package repository

import (
	"context"
	"fmt"
)

// capturePostOperationSnapshot keeps the state whose exact fingerprint the
// operation already recorded. A failed Undo therefore has both exact target
// and roll-forward material. No downtime or failure is allowed to erase the
// original post-operation record when this capture is incomplete.
func (s *gitSession) capturePostOperationSnapshot(record OperationRecord) (WorkSnapshot, workspaceArchiveScope, error) {
	scope, enabled, err := storedWorkspaceScope(s, record.Snapshot)
	if err != nil || !enabled {
		return WorkSnapshot{}, workspaceArchiveScope{}, fmt.Errorf("cannot preserve operation result without its workspace capture policy")
	}
	ignored := append([]string(nil), scope.IgnoredPaths...)
	if scope.AllIgnored {
		ignored, err = s.ignoredWorkspacePaths()
		if err != nil {
			return WorkSnapshot{}, workspaceArchiveScope{}, err
		}
	}
	point, err := s.captureWorkSnapshotWithIgnored(context.Background(), RecoverySnapshotKind, "After "+record.Summary, ignored, scope)
	if err != nil {
		return WorkSnapshot{}, workspaceArchiveScope{}, fmt.Errorf("preserve operation result: %w", err)
	}
	captured, present, err := storedWorkspaceScope(s, &point)
	if err != nil || !present {
		return WorkSnapshot{}, workspaceArchiveScope{}, fmt.Errorf("verify preserved operation result: metadata is incomplete")
	}
	current, err := s.currentWorkspaceScope(captured)
	if err != nil {
		return WorkSnapshot{}, workspaceArchiveScope{}, fmt.Errorf("verify preserved operation result: %w", err)
	}
	if current.GitStateGuard != captured.GitStateGuard || current.WorkspaceGuard != captured.WorkspaceGuard {
		return WorkSnapshot{}, workspaceArchiveScope{}, fmt.Errorf("workspace changed while preserving the operation result; recovery points remain available")
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return WorkSnapshot{}, workspaceArchiveScope{}, err
	}
	if branch != record.BranchAfter || head != record.HeadAfter {
		return WorkSnapshot{}, workspaceArchiveScope{}, fmt.Errorf("HEAD changed while preserving the operation result; recovery points remain available")
	}
	return point, captured, nil
}
