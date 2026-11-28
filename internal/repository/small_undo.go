package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// Small, exactly describable actions: a single ref (a tag) or a single file
// (an ignore rule). Each records what it changed, so Undo can put the one
// thing back and refuses if that thing has been touched since, without
// caring about the rest of the working tree.
const (
	OperationIgnore    = "ignore"
	OperationExclude   = "exclude"
	OperationTagCreate = "tag-create"
	OperationTagDelete = "tag-delete"

	UndoRestoreFile = "restore-file"
	UndoRestoreRef  = "restore-ref"
)

func isSmallOperation(kind string) bool {
	switch kind {
	case OperationStage, OperationUnstage, OperationIgnore, OperationExclude, OperationTagCreate, OperationTagDelete:
		return true
	}
	return false
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *gitSession) beforeFilePath(id string) string {
	return s.joinPath(s.journalDir(), id+".before")
}

// beginSmallRecord publishes a pending record for a small action.
func (s *gitSession) beginSmallRecord(kind, summary, undoKind string) (OperationRecord, string, error) {
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return OperationRecord{}, "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return OperationRecord{}, "", err
	}
	record := OperationRecord{
		Version: journalVersion, ID: hex.EncodeToString(random[:]), Kind: kind,
		Summary: RedactSecrets(strings.TrimSpace(summary)), Status: OperationPending,
		CreatedAt: time.Now().Unix(), Sequence: time.Now().UnixNano(), Worktree: s.git.RepoPaths.WorktreePath(),
		Branch: branch, HeadBefore: head, UndoKind: undoKind,
	}
	name, err := s.operationPath(record.ID)
	if err != nil {
		return OperationRecord{}, "", err
	}
	if err := s.writeOperationRecord(name, record); err != nil {
		return OperationRecord{}, "", err
	}
	return record, name, nil
}

// finishSmallRecord seals a record whose action ran. An action that changed
// nothing leaves no entry for Ctrl+Z to offer.
func (s *gitSession) finishSmallRecord(ctx context.Context, record OperationRecord, name string, changed bool, actionErr error) error {
	if !changed {
		s.discardSmallRecord(record)
		return actionErr
	}
	branch, head, identityErr := s.currentOperationIdentity()
	record.BranchAfter, record.HeadAfter = branch, head
	record.FinishedAt = time.Now().Unix()
	record.Status = OperationSucceeded
	if actionErr != nil {
		record.Error = RedactSecrets(actionErr.Error())
		record.Status = OperationFailed
		record.Partial = true
	}
	if identityErr != nil {
		record.Status = OperationInterrupted
		record.UndoKind = ""
	}
	if err := s.writeOperationRecord(name, record); err != nil {
		if actionErr != nil {
			return fmt.Errorf("%w (also could not finish the safety journal: %v)", actionErr, err)
		}
		return fmt.Errorf("the change was made, but its safety journal could not be finished: %w", err)
	}
	s.pruneSmallOperations(ctx)
	return actionErr
}

func (s *gitSession) discardSmallRecord(record OperationRecord) {
	if name, err := s.operationPath(record.ID); err == nil {
		_ = s.fileService().Remove(name)
	}
	_ = s.fileService().Remove(s.beforeFilePath(record.ID))
	if record.RefName != "" {
		_, _ = s.cmd.New([]string{"git", "update-ref", "-d", record.RefName}).RunWithOutput()
	}
}

// mutateFile runs an action that rewrites one file and records its content
// before and after.
func (s *gitSession) mutateFile(ctx context.Context, kind, summary, path string, mutate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if !SafetyJournalEnabled() {
		return mutate()
	}
	before, readErr := s.fileService().ReadFile(path)
	existed := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return mutate()
	}
	record, name, err := s.beginSmallRecord(kind, summary, UndoRestoreFile)
	if err != nil {
		return mutate()
	}
	record.FilePath, record.FileExisted = path, existed
	record.FileMode = 0o644
	if info, err := s.fileService().Stat(path); err == nil {
		record.FileMode = uint32(info.Mode().Perm())
	}
	if existed {
		if err := s.fileService().WriteFileAtomic(s.beforeFilePath(record.ID), before, 0o600); err != nil {
			s.discardSmallRecord(record)
			return mutate()
		}
	}
	actionErr := mutate()
	after, afterErr := s.fileService().ReadFile(path)
	afterExists := afterErr == nil
	if afterErr != nil && !os.IsNotExist(afterErr) {
		record.Status, record.UndoKind = OperationInterrupted, ""
		_ = s.writeOperationRecord(name, record)
		return actionErr
	}
	changed := afterExists != existed || (afterExists && digestHex(after) != digestHex(before))
	if afterExists {
		record.FileAfter = digestHex(after)
	}
	return s.finishSmallRecord(ctx, record, name, changed, actionErr)
}

func (s *gitSession) requireUnchangedFile(record OperationRecord) error {
	if record.Worktree != s.git.RepoPaths.WorktreePath() {
		return fmt.Errorf("operation belongs to a different worktree")
	}
	current, err := s.fileService().ReadFile(record.FilePath)
	switch {
	case err != nil && !os.IsNotExist(err):
		return err
	case err != nil && record.FileAfter != "":
		return fmt.Errorf("the file was removed after this operation; undo would overwrite that")
	case err == nil && digestHex(current) != record.FileAfter:
		return fmt.Errorf("the file was edited after this operation; undo would overwrite that newer edit")
	}
	if record.FileExisted {
		if _, err := s.fileService().ReadFile(s.beforeFilePath(record.ID)); err != nil {
			return fmt.Errorf("the saved copy of the file is gone; undo stopped before changing it")
		}
	}
	return nil
}

func (s *gitSession) undoFileOperation(record OperationRecord) error {
	if !record.FileExisted {
		if err := s.fileService().Remove(record.FilePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove the file: %w", err)
		}
		return nil
	}
	before, err := s.fileService().ReadFile(s.beforeFilePath(record.ID))
	if err != nil {
		return fmt.Errorf("read the saved copy: %w", err)
	}
	if err := s.fileService().WriteFileAtomic(record.FilePath, before, os.FileMode(record.FileMode)); err != nil {
		return fmt.Errorf("restore the file: %w", err)
	}
	return nil
}

func (s *gitSession) refObject(ref string) (string, error) {
	output, err := s.cmd.New([]string{"git", "rev-parse", "--verify", "--quiet", ref}).RunWithOutput()
	if err != nil {
		if strings.TrimSpace(output) == "" {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// mutateRef runs an action that creates, moves or deletes one ref and records
// the object it pointed to before and after.
func (s *gitSession) mutateRef(ctx context.Context, kind, summary, ref string, mutate func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if !SafetyJournalEnabled() {
		return mutate()
	}
	before, err := s.refObject(ref)
	if err != nil {
		return mutate()
	}
	record, name, err := s.beginSmallRecord(kind, summary, UndoRestoreRef)
	if err != nil {
		return mutate()
	}
	record.RefFull, record.RefBefore = ref, before
	if before != "" {
		pin, err := s.pinJournalObject(record.ID, before)
		if err != nil {
			s.discardSmallRecord(record)
			return mutate()
		}
		record.RefName = pin
	}
	actionErr := mutate()
	after, afterErr := s.refObject(ref)
	if afterErr != nil {
		record.Status, record.UndoKind = OperationInterrupted, ""
		_ = s.writeOperationRecord(name, record)
		return actionErr
	}
	record.RefAfter = after
	return s.finishSmallRecord(ctx, record, name, after != before, actionErr)
}

func (s *gitSession) requireUnchangedRef(record OperationRecord) error {
	if record.Worktree != s.git.RepoPaths.WorktreePath() {
		return fmt.Errorf("operation belongs to a different worktree")
	}
	current, err := s.refObject(record.RefFull)
	if err != nil {
		return err
	}
	if current != record.RefAfter {
		return fmt.Errorf("%s was changed after this operation; undo would overwrite that", strings.TrimPrefix(record.RefFull, "refs/tags/"))
	}
	if record.RefBefore != "" {
		return s.validateJournalPin(record.RefName, record.RefBefore)
	}
	return nil
}

func (s *gitSession) undoRefOperation(record OperationRecord) error {
	var args []string
	if record.RefBefore == "" {
		args = []string{"git", "update-ref", "-d", record.RefFull, record.RefAfter}
	} else {
		args = []string{"git", "update-ref", record.RefFull, record.RefBefore, record.RefAfter}
	}
	if output, err := s.cmd.New(args).RunWithOutput(); err != nil {
		return fmt.Errorf("restore %s: %w: %s", record.RefFull, err, strings.TrimSpace(output))
	}
	restored, err := s.refObject(record.RefFull)
	if err != nil || restored != record.RefBefore {
		return fmt.Errorf("undo did not reproduce the original %s", record.RefFull)
	}
	return nil
}

// pruneSmallOperations drops the oldest small records past indexJournalKeep.
func (s *gitSession) pruneSmallOperations(ctx context.Context) {
	records, err := s.Operations(ctx)
	if err != nil {
		return
	}
	kept := 0
	for _, record := range records {
		if !isSmallOperation(record.Kind) {
			continue
		}
		kept++
		if kept > indexJournalKeep && record.CanDelete() {
			s.discardSmallRecord(record)
		}
	}
}
