package repository

import (
	"fmt"
	"strings"
)

// restoreOperationHead restores symbolic HEAD only after workspace, index and
// refs are recovered. Detached HEAD is updated without dereferencing it, so an
// undo cannot accidentally move the branch that happened to be checked out.
func (s *gitSession) restoreOperationHead(record OperationRecord) error {
	if record.Branch == "" {
		message, err := s.cmd.New([]string{"git", "update-ref", "--no-deref", "HEAD", record.HeadBefore, record.HeadAfter}).RunWithOutput()
		if err != nil {
			return fmt.Errorf("restore detached HEAD without moving its old branch: %w: %s", err, strings.TrimSpace(message))
		}
		return nil
	}
	if record.Branch == record.BranchAfter {
		return nil
	}
	ref := "refs/heads/" + record.Branch
	if err := s.cmd.New([]string{"git", "check-ref-format", ref}).Run(); err != nil {
		return fmt.Errorf("restore HEAD branch: invalid saved branch name")
	}
	if err := s.cmd.New([]string{"git", "symbolic-ref", "HEAD", ref}).Run(); err != nil {
		return fmt.Errorf("restore HEAD to branch %s: %w", record.Branch, err)
	}
	return nil
}

// verifyWorkspaceUndo proves the newly written target before consuming the
// recovery action. A hook or I/O anomaly cannot turn a partial restore into a
// completed Undo or erase its original post-operation fingerprints.
func (s *gitSession) verifyWorkspaceUndo(record OperationRecord) error {
	if record.WorkspaceGuardBefore == "" {
		return nil
	}
	scope, present, err := storedWorkspaceScope(s, record.Snapshot)
	if err != nil || !present {
		return s.failJournalOperation(record, fmt.Errorf("restored workspace metadata could not be verified"))
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return s.failJournalOperation(record, fmt.Errorf("verify restored HEAD: %w", err))
	}
	wantBranch := record.Branch
	if record.Kind == OperationBranchDelete {
		// The deleted branch's name is retained for legacy ref plans; this
		// operation must not switch the user's actual checked-out branch.
		wantBranch = record.BranchAfter
	}
	if branch != wantBranch || head != record.HeadBefore {
		return s.failJournalOperation(record, fmt.Errorf("Undo did not restore the exact original HEAD and branch; before and after restore points were retained"))
	}
	current, err := s.currentWorkspaceScope(scope)
	if err != nil {
		return s.failJournalOperation(record, fmt.Errorf("verify restored workspace: %w", err))
	}
	if current.GitStateGuard != record.GitStateBefore || current.WorkspaceGuard != record.WorkspaceGuardBefore {
		return s.failJournalOperation(record, fmt.Errorf("Undo did not reproduce the complete original workspace; before and after restore points were retained"))
	}
	return nil
}
