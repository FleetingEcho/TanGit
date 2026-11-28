package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	OperationDiscard      = "discard"
	OperationReset        = "reset"
	OperationRebase       = "rebase"
	OperationBranchDelete = "branch-delete"
	OperationStashDrop    = "stash-drop"
	OperationPatchApply   = "patch-apply"

	OperationPending     = "pending"
	OperationSucceeded   = "succeeded"
	OperationFailed      = "failed"
	OperationInterrupted = "interrupted"
	OperationUndoing     = "undoing"

	UndoRestoreWork    = "restore-work"
	UndoResetHead      = "reset-head"
	UndoRecreateBranch = "recreate-branch"
	UndoRestoreStash   = "restore-stash"
	UndoReversePatch   = "reverse-patch"

	journalVersion   = 1
	journalRefPrefix = "refs/tangit/journal/"
)

// OperationRecord is one repository-changing action and the immutable facts a
// later recovery must revalidate. UndoKind describes a possible plan, never a
// promise: UndoOperation checks current HEAD, branch and worktree state again.
type OperationRecord struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary"`
	Status  string `json:"status"`
	// Partial marks a changed workspace after a failed or conflict-stopped
	// action. Exact before and after recovery points support a revalidated
	// Undo without assuming that a non-zero Git exit means that nothing ran.
	Partial   bool  `json:"partial,omitempty"`
	CreatedAt int64 `json:"created_at"`
	// Sequence orders records made within the same second, so "the latest
	// change" never depends on their random IDs.
	Sequence             int64             `json:"sequence,omitempty"`
	FinishedAt           int64             `json:"finished_at,omitempty"`
	UndoneAt             int64             `json:"undone_at,omitempty"`
	UndoStartedAt        int64             `json:"undo_started_at,omitempty"`
	Worktree             string            `json:"worktree"`
	Branch               string            `json:"branch_before,omitempty"`
	BranchAfter          string            `json:"branch_after,omitempty"`
	HeadBefore           string            `json:"head_before,omitempty"`
	HeadAfter            string            `json:"head_after,omitempty"`
	BeforeState          string            `json:"before_state,omitempty"`
	AfterState           string            `json:"after_state,omitempty"`
	GitStateBefore       string            `json:"git_state_before,omitempty"`
	GitStateAfter        string            `json:"git_state_after,omitempty"`
	RemoteEffects        []RemoteRefEffect `json:"remote_effects,omitempty"`
	WorkspaceGuardBefore string            `json:"workspace_guard_before,omitempty"`
	WorkspaceGuardAfter  string            `json:"workspace_guard_after,omitempty"`
	StashAfter           string            `json:"stash_after,omitempty"`
	ObjectID             string            `json:"object_id,omitempty"`
	// IndexBefore and IndexAfter are the index trees around a staging action;
	// IndexBefore stays pinned at RefName until the record is pruned.
	IndexBefore string `json:"index_before,omitempty"`
	IndexAfter  string `json:"index_after,omitempty"`
	// FilePath, FileExisted and FileAfter describe a single-file change: the
	// copy it replaced is kept beside the record as <id>.before.
	FilePath    string `json:"file_path,omitempty"`
	FileExisted bool   `json:"file_existed,omitempty"`
	FileAfter   string `json:"file_after,omitempty"`
	FileMode    uint32 `json:"file_mode,omitempty"`
	// RefFull, RefBefore and RefAfter describe a single-ref change; an empty
	// object means the ref did not exist at that point.
	RefFull       string        `json:"ref_full,omitempty"`
	RefBefore     string        `json:"ref_before,omitempty"`
	RefAfter      string        `json:"ref_after,omitempty"`
	RefName       string        `json:"ref_name,omitempty"`
	Message       string        `json:"message,omitempty"`
	UndoKind      string        `json:"undo_kind,omitempty"`
	Snapshot      *WorkSnapshot `json:"snapshot,omitempty"`
	AfterSnapshot *WorkSnapshot `json:"after_snapshot,omitempty"`
	Error         string        `json:"error,omitempty"`
	// RebasePaused is the legacy active-rebase marker used by edit-stops and
	// conflict pauses. Its undo is finalized only after the rebase returns to
	// the branch, not against the temporary detached HEAD.
	RebasePaused bool `json:"rebase_paused,omitempty"`
	// ActiveOperation keeps journal provenance while Git is paused. It is
	// cleared only after the matching operation has actually finished.
	ActiveOperation OperationType `json:"active_operation,omitempty"`
	// HistoryPatch is the durable coordinator for a single-source Custom Patch
	// rewrite. It survives process restarts and is advanced only by the
	// CAS-checked Operation driver, never by a transient apply callback.
	HistoryPatch *HistoryPatchState `json:"history_patch,omitempty"`

	patchFile string
}

// CanUndo is only a UI hint. Repository preflights remain authoritative.
func (r OperationRecord) CanUndo() bool {
	if len(r.RemoteEffects) != 0 || r.UndoStartedAt != 0 || r.WorkspaceGuardBefore != "" && r.AfterSnapshot == nil {
		return false
	}
	recoverable := r.Status == OperationSucceeded || (r.Status == OperationFailed && r.Partial)
	return recoverable && r.UndoneAt == 0 && r.UndoKind != "" && !r.RebasePaused && r.ActiveOperation == OperationTypeNone
}

// CanDelete reports whether a journal entry can be removed without losing
// the continuation state of an active operation.
func (r OperationRecord) CanDelete() bool {
	return !r.RebasePaused && r.ActiveOperation == OperationTypeNone && r.Status != OperationUndoing
}

func (s *gitSession) journalDir() string {
	return s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "tangit", "journal")
}

func validOperationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (s *gitSession) operationPath(id string) (string, error) {
	if !validOperationID(id) {
		return "", fmt.Errorf("invalid safety journal operation id")
	}
	return s.joinPath(s.journalDir(), id+".json"), nil
}

// syncDirectory persists a rename's directory entry on Unix filesystems.
// Directory fsync is not supported by Windows, where the file sync before the
// rename remains the best available durability boundary.
func (s *gitSession) syncDirectory(name string) error {
	if s.sshClient != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(name)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		directory.Close()
		return err
	}
	return directory.Close()
}

func (s *gitSession) writeOperationRecord(name string, record OperationRecord) error {
	if s.sshClient != nil {
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("write safety journal: %w", err)
		}
		if err := s.fileService().WriteFileAtomic(name, append(data, '\n'), 0o600); err != nil {
			return err
		}
		if err := s.syncRemotePath(name); err != nil {
			return fmt.Errorf("sync safety journal: %w", err)
		}
		if err := s.syncRemotePath(path.Dir(name)); err != nil {
			return fmt.Errorf("sync safety journal directory: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return fmt.Errorf("create safety journal: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(name), "operation-*.json")
	if err != nil {
		return fmt.Errorf("write safety journal: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("protect safety journal: %w", err)
	}
	if err := json.NewEncoder(file).Encode(record); err != nil {
		file.Close()
		return fmt.Errorf("write safety journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("write safety journal: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write safety journal: %w", err)
	}
	if err := os.Rename(temporary, name); err != nil {
		return fmt.Errorf("replace safety journal: %w", err)
	}
	if err := s.syncDirectory(filepath.Dir(name)); err != nil {
		return fmt.Errorf("sync safety journal: %w", err)
	}
	return nil
}

func (s *gitSession) readOperationRecord(name string) (OperationRecord, error) {
	data, err := s.fileService().ReadFile(name)
	if err != nil {
		return OperationRecord{}, fmt.Errorf("read safety journal: %w", err)
	}
	var record OperationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return OperationRecord{}, fmt.Errorf("read safety journal: %w", err)
	}
	if record.Version != journalVersion || !validOperationID(record.ID) {
		return OperationRecord{}, fmt.Errorf("read safety journal: unsupported or malformed operation")
	}
	if record.Status == OperationPending {
		record.Status = OperationInterrupted
	}
	return record, nil
}

func (s *gitSession) currentOperationIdentity() (branch, head string, err error) {
	head, headErr := s.gitText("rev-parse", "--verify", "HEAD")
	branch, branchErr := s.gitText("symbolic-ref", "--quiet", "--short", "HEAD")
	if headErr != nil && branchErr != nil {
		// Only a symbolic branch without a commit is unborn. A detached or
		// damaged HEAD that cannot be resolved stops the destructive action.
		return "", "", fmt.Errorf("read HEAD before recording operation: %w", headErr)
	}
	return strings.TrimSpace(branch), strings.TrimSpace(head), nil
}

func (s *gitSession) pinJournalObject(id string, object string) (string, error) {
	object = strings.TrimSpace(object)
	if object == "" {
		return "", nil
	}
	if _, err := s.gitText("cat-file", "-e", object+"^{object}"); err != nil {
		return "", fmt.Errorf("pin recovery object: %w", err)
	}
	ref := journalRefPrefix + id + "/before"
	output, err := s.cmd.New([]string{"git", "update-ref", ref, object}).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("pin recovery object: %w: %s", err, strings.TrimSpace(output))
	}
	return ref, nil
}

func (s *gitSession) beginOperation(kind, summary string, undoKind string, snapshot *WorkSnapshot) (OperationRecord, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return OperationRecord{}, fmt.Errorf("create safety journal id: %w", err)
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return OperationRecord{}, err
	}
	scope, enabled, err := storedWorkspaceScope(s, snapshot)
	if err != nil {
		return OperationRecord{}, err
	}
	includeIgnored := true
	if enabled && !scope.AllIgnored && len(scope.IgnoredPaths) == 0 {
		// An explicitly empty capture scope proves ignored files are outside
		// this action. Keep the fingerprint at the same scale rather than
		// rejecting a legal filename that neither the action nor its Undo reads.
		includeIgnored = false
	}
	state, err := s.patchRepositoryStateWithIgnored(includeIgnored)
	if err != nil {
		return OperationRecord{}, fmt.Errorf("record operation start: %w", err)
	}
	record := OperationRecord{
		Version: journalVersion, ID: hex.EncodeToString(random[:]), Kind: kind,
		Summary: RedactSecrets(strings.TrimSpace(summary)), Status: OperationPending,
		CreatedAt: time.Now().Unix(), Sequence: time.Now().UnixNano(), Worktree: s.git.RepoPaths.WorktreePath(),
		Branch: branch, HeadBefore: head, BeforeState: state,
		UndoKind: undoKind, Snapshot: snapshot,
	}
	if enabled {
		current, err := s.currentWorkspaceScope(scope)
		if err != nil {
			return OperationRecord{}, fmt.Errorf("verify operation recovery start: %w", err)
		}
		if current.GitStateGuard != scope.GitStateGuard || current.WorkspaceGuard != scope.WorkspaceGuard {
			return OperationRecord{}, fmt.Errorf("workspace changed while creating the recovery point; the action was not run")
		}
		record.GitStateBefore = scope.GitStateGuard
		record.WorkspaceGuardBefore = scope.WorkspaceGuard
	}
	name, _ := s.operationPath(record.ID)
	if err := s.writeOperationRecord(name, record); err != nil {
		return OperationRecord{}, err
	}
	return record, nil
}

func storedWorkspaceScope(s *gitSession, snapshot *WorkSnapshot) (workspaceArchiveScope, bool, error) {
	if snapshot == nil {
		return workspaceArchiveScope{}, false, nil
	}
	archive, present, err := readWorkspaceRepositoryArchive(s, snapshot.Hash)
	if err != nil || !present {
		return workspaceArchiveScope{}, present, err
	}
	return archive.Scope, true, nil
}

func (s *gitSession) pinOperationBefore(record OperationRecord) (OperationRecord, error) {
	object := record.HeadBefore
	if record.Kind == OperationBranchDelete || record.Kind == OperationStashDrop {
		object = record.ObjectID
		if object == "" {
			return record, fmt.Errorf("record operation recovery: deleted object is unavailable")
		}
	}
	ref, err := s.pinJournalObject(record.ID, object)
	if err != nil {
		return record, err
	}
	record.RefName = ref
	name, _ := s.operationPath(record.ID)
	if err := s.writeOperationRecord(name, record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *gitSession) stashOperationState() (string, error) {
	state, err := s.gitText("stash", "list", "--format=%H")
	if err != nil {
		return "", fmt.Errorf("read stash state: %w", err)
	}
	return strings.TrimSpace(state), nil
}

func (s *gitSession) completeJournalOperation(record OperationRecord, actionErr error) error {
	if record.UndoStartedAt != 0 {
		if actionErr == nil {
			actionErr = errors.New("undo was interrupted before completion")
		}
		record.Status = OperationFailed
		record.Error = RedactSecrets(actionErr.Error())
		record.FinishedAt = time.Now().Unix()
		name, _ := s.operationPath(record.ID)
		if err := s.writeOperationRecord(name, record); err != nil {
			return fmt.Errorf("undo was interrupted and its journal could not be updated: %w (also %v)", actionErr, err)
		}
		return fmt.Errorf("%w (Undo was interrupted; its before and after recovery points remain available)", actionErr)
	}
	branch, head, identityErr := s.currentOperationIdentity()
	var state string
	var stateErr error
	if record.UndoKind == UndoResetHead {
		state, stateErr = s.resetRepositoryState(record.HeadBefore, head)
	} else {
		state, stateErr = s.patchRepositoryState()
	}
	record.BranchAfter, record.HeadAfter = branch, head
	if stateErr == nil {
		record.AfterState = state
	}
	var guardErr error
	if record.WorkspaceGuardBefore != "" {
		var point WorkSnapshot
		var scope workspaceArchiveScope
		point, scope, guardErr = s.capturePostOperationSnapshot(record)
		if guardErr == nil {
			record.AfterSnapshot = &point
			record.GitStateAfter = scope.GitStateGuard
			record.WorkspaceGuardAfter = scope.WorkspaceGuard
		}
	}
	var stashErr error
	if stashStateTrackedOperation(record.Kind) {
		record.StashAfter, stashErr = s.stashOperationState()
	}
	record.FinishedAt = time.Now().Unix()
	record.Status = OperationSucceeded
	if actionErr != nil && record.WorkspaceGuardBefore != "" {
		if record.Snapshot != nil && record.AfterSnapshot != nil {
			record.Partial = record.GitStateAfter != record.GitStateBefore || record.WorkspaceGuardAfter != record.WorkspaceGuardBefore
		}
	}
	if actionErr != nil {
		record.Error = RedactSecrets(actionErr.Error())
		record.Status = OperationFailed
		if !record.Partial {
			record.UndoKind = ""
		}
	}
	if guardErr != nil && actionErr == nil {
		record.Error = RedactSecrets(guardErr.Error())
	}
	if identityErr != nil || stateErr != nil || guardErr != nil || stashErr != nil {
		record.Status = OperationInterrupted
		record.UndoKind = ""
	}
	name, _ := s.operationPath(record.ID)
	if err := s.writeOperationRecord(name, record); err != nil {
		if actionErr != nil {
			return fmt.Errorf("%w (also could not finish the safety journal: %v)", actionErr, err)
		}
		return fmt.Errorf("operation completed, but its safety journal could not be finished; do not repeat it blindly: %w", err)
	}
	return actionErr
}

func (s *gitSession) failJournalOperation(record OperationRecord, err error) error {
	if finishErr := s.completeJournalOperation(record, err); finishErr != nil {
		return finishErr
	}
	return err
}

// Operations returns newest operations first. Interrupted writes stay visible
// and deliberately expose no undo action.
func (s *gitSession) Operations(ctx context.Context) ([]OperationRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := s.fileService().ReadDir(s.journalDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list safety journal: %w", err)
	}
	records := make([]OperationRecord, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		// Only atomically published, canonical records are candidates. In
		// particular, never read an operation-*.json temporary while its JSON
		// is still being encoded.
		if entry.IsDir() || entry.Name() != id+".json" || !validOperationID(id) {
			continue
		}
		record, readErr := s.readOperationRecord(s.joinPath(s.journalDir(), entry.Name()))
		if readErr != nil || record.ID != id {
			// A damaged record must not hide healthy history, worktrees or other
			// operations. Keep the damage visible without retaining any partially
			// decoded recovery instructions that could offer an unsafe Undo.
			record = OperationRecord{
				Version: journalVersion, ID: id, Summary: "Unreadable safety journal entry",
				Status: OperationInterrupted,
				Error:  "The record is corrupt or incomplete; Undo is unavailable.",
			}
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt == records[j].CreatedAt {
			if records[i].Sequence != records[j].Sequence {
				return records[i].Sequence > records[j].Sequence
			}
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt > records[j].CreatedAt
	})
	return records, nil
}

// DeleteOperation removes the journal entry. Recovery snapshots have their
// own lifetime and can still be restored from Recovery & checkpoints.
func (s *gitSession) DeleteOperation(ctx context.Context, id string) error {
	name, err := s.operationPath(id)
	if err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := s.readOperationRecord(name)
	if os.IsNotExist(err) {
		return nil
	}
	// Operations also exposes unreadable records under their canonical IDs,
	// so a user can remove a damaged entry from the journal.
	if err == nil && !record.CanDelete() {
		return fmt.Errorf("finish the active operation before deleting its safety journal entry")
	}
	_ = s.fileService().Remove(s.beforeFilePath(id))
	if err := s.fileService().Remove(name); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete safety journal entry: %w", err)
	}
	return s.syncDirectory(s.journalDir())
}

func (s *gitSession) validateJournalSnapshot(snapshot *WorkSnapshot) error {
	if snapshot == nil {
		return nil
	}
	ref, err := workSnapshotRef(snapshot.Kind, snapshot.ID)
	if err != nil {
		return fmt.Errorf("saved work is invalid: %w", err)
	}
	hash, err := s.gitText("rev-parse", "--verify", ref+"^{commit}")
	if err != nil || strings.TrimSpace(hash) != snapshot.Hash {
		return fmt.Errorf("saved work was deleted or changed; undo stopped before modifying the repository")
	}
	if _, err = s.readWorkspacePermissions(snapshot.Hash); err != nil {
		return err
	}
	_, _, err = readWorkspaceRepositoryArchive(s, snapshot.Hash)
	return err
}

func (s *gitSession) validateJournalPin(ref, object string) error {
	if ref == "" || object == "" {
		return fmt.Errorf("recovery object information is incomplete")
	}
	hash, err := s.gitText("rev-parse", "--verify", ref+"^{object}")
	if err != nil || strings.TrimSpace(hash) != object {
		return fmt.Errorf("recovery object is no longer pinned to the exact operation")
	}
	return nil
}

func (s *gitSession) requireUnchangedOperation(record OperationRecord) error {
	switch record.UndoKind {
	case UndoRestoreIndex:
		return s.requireUnchangedIndex(record)
	case UndoRestoreFile:
		return s.requireUnchangedFile(record)
	case UndoRestoreRef:
		return s.requireUnchangedRef(record)
	}
	if record.Worktree != s.git.RepoPaths.WorktreePath() {
		return fmt.Errorf("operation belongs to a different worktree")
	}
	if state := s.workingTreeState(); state.Any() && (record.WorkspaceGuardAfter == "" || record.GitStateAfter == "" || record.AfterSnapshot == nil) {
		return fmt.Errorf("cannot undo while %s is in progress", state.Effective().CommandName())
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return err
	}
	if record.UndoKind == UndoResetHead && (branch != record.BranchAfter || head != record.HeadAfter) {
		return fmt.Errorf("HEAD or its branch moved after this operation; undo would overwrite newer history")
	}
	var state string
	if record.UndoKind == UndoResetHead {
		state, err = s.resetRepositoryState(record.HeadBefore, record.HeadAfter)
	} else {
		state, err = s.patchRepositoryState()
	}
	if err != nil {
		return err
	}
	if state != record.AfterState {
		return fmt.Errorf("the worktree changed after this operation; preserve the newer work instead of undoing")
	}
	if stashStateTrackedOperation(record.Kind) {
		stashState, err := s.stashOperationState()
		if err != nil {
			return err
		}
		if stashState != record.StashAfter {
			return fmt.Errorf("the stash list changed after this operation; preserve the newer stash state instead of undoing")
		}
	}
	scope, enabled, guardErr := storedWorkspaceScope(s, record.Snapshot)
	if guardErr != nil {
		return guardErr
	}
	if enabled {
		if scope.GitStateGuard != record.GitStateBefore || scope.WorkspaceGuard != record.WorkspaceGuardBefore {
			return fmt.Errorf("the recovery point does not match this operation's original state")
		}
		if scope.WorkspaceGuard == "" || record.GitStateBefore == "" || record.GitStateAfter == "" || record.WorkspaceGuardBefore == "" || record.WorkspaceGuardAfter == "" {
			return fmt.Errorf("the operation's workspace recovery information is incomplete")
		}
		if branch != record.BranchAfter || head != record.HeadAfter {
			return fmt.Errorf("HEAD or its branch moved after this operation; undo would overwrite newer history")
		}
		current, err := s.currentWorkspaceScope(scope)
		if err != nil {
			return err
		}
		if current.GitStateGuard != record.GitStateAfter || current.WorkspaceGuard != record.WorkspaceGuardAfter {
			return fmt.Errorf("the worktree changed after this operation; preserve the newer work and Git metadata instead of undoing")
		}
	}
	if err := s.validateJournalSnapshot(record.Snapshot); err != nil {
		return err
	}
	switch record.UndoKind {
	case UndoResetHead:
		return s.validateJournalPin(record.RefName, record.HeadBefore)
	case UndoRecreateBranch, UndoRestoreStash:
		return s.validateJournalPin(record.RefName, record.ObjectID)
	}
	return nil
}

func (s *gitSession) markOperationUndone(record OperationRecord) error {
	if record.UndoStartedAt != 0 {
		record.Status = OperationSucceeded
	}
	record.UndoneAt = time.Now().Unix()
	name, _ := s.operationPath(record.ID)
	if err := s.writeOperationRecord(name, record); err != nil {
		return fmt.Errorf("undo completed, but the safety journal could not be updated: %w", err)
	}
	return nil
}

// trackedSnapshotWithoutUntracked gives stash apply only the W/I parents.
// The third, pure-untracked parent needs separate handling: unchanged files
// that deliberately survive discard or reset look like conflicts to stash.
func (s *gitSession) trackedSnapshotWithoutUntracked(snapshot WorkSnapshot) (WorkSnapshot, error) {
	tree, err := s.gitText("rev-parse", snapshot.Hash+"^{tree}")
	if err != nil {
		return WorkSnapshot{}, err
	}
	base, err := s.gitText("rev-parse", snapshot.Hash+"^1")
	if err != nil {
		return WorkSnapshot{}, err
	}
	index, err := s.gitText("rev-parse", snapshot.Hash+"^2")
	if err != nil {
		return WorkSnapshot{}, err
	}
	env := []string{"GIT_AUTHOR_NAME=lazygitGo", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=lazygitGo", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	hash, err := s.cmd.New([]string{"git", "commit-tree", strings.TrimSpace(tree), "-p", strings.TrimSpace(base), "-p", strings.TrimSpace(index), "-m", "lazygitGo journal recovery"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("prepare tracked recovery: %w: %s", err, strings.TrimSpace(hash))
	}
	tracked := snapshot
	tracked.Hash = strings.TrimSpace(hash)
	return tracked, nil
}

// restoreSnapshotUntracked copies back only paths recorded in the untracked
// parent. Exact literal pathspecs avoid both false stash collisions and the
// tracked-file deletions that restoring an entire tree would cause.
func (s *gitSession) restoreSnapshotUntracked(snapshot WorkSnapshot) error {
	untrackedCommit, err := s.snapshotUntrackedCommit(snapshot.Hash)
	if err != nil {
		return err
	}
	if untrackedCommit == "" {
		return nil
	}
	untracked, err := s.gitText("rev-parse", "--verify", untrackedCommit+"^{tree}")
	if err != nil {
		return fmt.Errorf("saved untracked parent is unavailable: %w", err)
	}
	untrackedTree := strings.TrimSpace(untracked)
	listing, err := s.cmd.New([]string{"git", "ls-tree", "-r", "--name-only", "-z", untrackedTree}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("read saved untracked files: %w: %s", err, strings.TrimSpace(listing))
	}
	if listing == "" {
		return nil
	}
	name, removePaths, err := s.temporaryFile("lazygitgo-journal-untracked-", []byte(listing), 0o600)
	if err != nil {
		return fmt.Errorf("prepare saved untracked files: %w", err)
	}
	defer removePaths()
	output, err := s.cmd.New([]string{
		"git", "--literal-pathspecs", "restore", "--source=" + untrackedTree, "--worktree",
		"--pathspec-from-file=" + name, "--pathspec-file-nul",
	}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore saved untracked files: %w: %s", err, strings.TrimSpace(output))
	}
	paths, err := workspacePathsFromNul(listing)
	if err != nil {
		return err
	}
	return s.restoreRawWorkspacePaths(snapshot.Hash, paths)
}

// restoreResetWorkSnapshot restores tracked index/worktree state separately
// from the pure-untracked parent. Reset can overwrite or remove an untracked
// file when the target tracks the same path, so that parent is still required.
func (s *gitSession) restoreResetWorkSnapshot(ctx context.Context, snapshot WorkSnapshot, expectedAfter ...*WorkSnapshot) error {
	if len(expectedAfter) > 1 {
		return fmt.Errorf("reset recovery has multiple reviewed post states")
	}
	if len(expectedAfter) == 1 {
		handled, err := s.restoreCompleteWorkspaceSnapshot(ctx, snapshot, expectedAfter[0])
		if handled || err != nil {
			return err
		}
	}
	// Legacy snapshots retain the stash-aware path. The public RestoreWorkSnapshot
	// overlap guard must not reject pre-existing untracked work which was part
	// of the exact after-state already checked by the journal CAS.
	if err := s.reconcileWorkspacePaths(snapshot.Hash); err != nil {
		return err
	}
	tracked, err := s.trackedSnapshotWithoutUntracked(snapshot)
	if err != nil {
		return err
	}
	if err := s.applyWorkSnapshot(tracked); err != nil {
		return err
	}
	if err := s.restoreSnapshotUntracked(snapshot); err != nil {
		return err
	}
	if err := s.restoreWorkspaceRepositoryArchive(snapshot.Hash); err != nil {
		return err
	}
	return s.restoreWorkspacePermissions(snapshot.Hash)
}

// restoreJournalWorkSnapshot undoes discard after its exact after-state has
// matched. Tracked state uses stash's index-aware apply; surviving untracked
// files are then overwritten only where the saved tree has a version.
func (s *gitSession) restoreJournalWorkSnapshot(snapshot WorkSnapshot, expectedAfter ...*WorkSnapshot) error {
	return s.restoreJournalWorkSnapshotContext(context.Background(), snapshot, expectedAfter...)
}

func (s *gitSession) restoreJournalWorkSnapshotContext(ctx context.Context, snapshot WorkSnapshot, expectedAfter ...*WorkSnapshot) error {
	if len(expectedAfter) > 1 {
		return fmt.Errorf("workspace recovery has multiple reviewed post states")
	}
	if len(expectedAfter) == 1 {
		handled, err := s.restoreCompleteWorkspaceSnapshot(ctx, snapshot, expectedAfter[0])
		if handled || err != nil {
			return err
		}
	}
	// Legacy snapshots retain the stash-aware apply and untracked parent path.
	if err := s.reconcileWorkspacePaths(snapshot.Hash); err != nil {
		return err
	}
	tracked, err := s.trackedSnapshotWithoutUntracked(snapshot)
	if err != nil {
		return err
	}
	if err := s.applyWorkSnapshot(tracked); err != nil {
		return err
	}
	if err := s.restoreSnapshotUntracked(snapshot); err != nil {
		return err
	}
	if err := s.restoreWorkspaceRepositoryArchive(snapshot.Hash); err != nil {
		return err
	}
	return s.restoreWorkspacePermissions(snapshot.Hash)
}

// UndoOperation performs only the operation-specific plan stored with id.
// It never replays an older reflog entry by accident and refuses stale state.
func (s *gitSession) UndoOperation(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	name, err := s.operationPath(id)
	if err != nil {
		return err
	}
	record, err := s.readOperationRecord(name)
	if err != nil {
		return err
	}
	if record.ID != id {
		return fmt.Errorf("safety journal operation identity does not match its record")
	}
	if len(record.RemoteEffects) != 0 {
		return fmt.Errorf("undo operation: %w", ErrRemoteCompensationRequired)
	}
	if !record.CanUndo() {
		return fmt.Errorf("this operation has no safe undo available")
	}
	if err := s.requireUnchangedOperation(record); err != nil {
		return fmt.Errorf("undo operation: %w", err)
	}
	if record.WorkspaceGuardBefore != "" {
		if record.AfterSnapshot == nil {
			return fmt.Errorf("undo operation: the post-operation recovery point is missing")
		}
		if err := s.validateJournalSnapshot(record.AfterSnapshot); err != nil {
			return fmt.Errorf("undo operation: validate the post-operation recovery point: %w", err)
		}
		afterScope, present, err := storedWorkspaceScope(s, record.AfterSnapshot)
		if err != nil || !present {
			return fmt.Errorf("undo operation: the post-operation capture policy is incomplete")
		}
		if afterScope.GitStateGuard != record.GitStateAfter || afterScope.WorkspaceGuard != record.WorkspaceGuardAfter {
			return fmt.Errorf("undo operation: the post-operation recovery point does not match its journal entry")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.WorkspaceGuardBefore != "" {
		record.UndoStartedAt = time.Now().Unix()
		record.Status = OperationUndoing
		if err := s.writeOperationRecord(name, record); err != nil {
			return fmt.Errorf("undo operation: could not record its start; no recovery writes were made: %w", err)
		}
	}

	switch record.UndoKind {
	case UndoRestoreWork:
		if record.Snapshot == nil {
			return fmt.Errorf("undo operation: its recovery snapshot is missing")
		}
		// requireUnchangedOperation has already matched the complete worktree
		// against this operation's final state. Unselected pre-existing changes
		// therefore are not newer conflicts: they are part of the exact state
		// being replaced by the saved snapshot.
		if err := s.restoreJournalWorkSnapshotContext(ctx, *record.Snapshot, record.AfterSnapshot); err != nil {
			return s.failJournalOperation(record, err)
		}
	case UndoResetHead:
		if record.HeadBefore == "" {
			return fmt.Errorf("undo operation: the previous HEAD is unavailable")
		}
		if _, err := s.gitText("rev-parse", "--verify", record.RefName); record.RefName == "" || err != nil {
			return fmt.Errorf("undo operation: the previous HEAD is no longer pinned")
		}
		if record.WorkspaceGuardBefore != "" && record.Snapshot != nil {
			// New checkpoints already contain the raw target tree, index,
			// operation metadata and local refs. Restore them without a hard reset.
			if err := s.restoreResetWorkSnapshot(ctx, *record.Snapshot, record.AfterSnapshot); err != nil {
				return s.failJournalOperation(record, fmt.Errorf("HEAD was restored, but saved work still needs recovery: %w", err))
			}
			if err := s.restoreOperationHead(record); err != nil {
				return s.failJournalOperation(record, err)
			}
		} else {
			if err := s.git.Commit.ResetToCommit(record.HeadBefore, string(ResetHard), nil); err != nil {
				return fmt.Errorf("restore previous HEAD: %w", err)
			}
			if record.Snapshot != nil {
				if err := s.restoreResetWorkSnapshot(ctx, *record.Snapshot); err != nil {
					return fmt.Errorf("HEAD was restored, but saved work still needs recovery: %w", err)
				}
			}
		}
	case UndoRecreateBranch:
		if record.RefName == "" || record.ObjectID == "" {
			return fmt.Errorf("undo operation: deleted branch information is incomplete")
		}
		if err := s.cmd.New([]string{"git", "show-ref", "--verify", "--quiet", "refs/heads/" + record.Branch}).Run(); err == nil {
			return fmt.Errorf("branch %s already exists; it will not be replaced", record.Branch)
		}
		if record.WorkspaceGuardBefore != "" && record.Snapshot != nil {
			if _, err := s.restoreCompleteWorkspaceSnapshot(ctx, *record.Snapshot, record.AfterSnapshot); err != nil {
				return s.failJournalOperation(record, err)
			}
		} else if err := s.git.Branch.New(record.Branch, record.RefName); err != nil {
			return fmt.Errorf("recreate branch: %w", err)
		}
	case UndoRestoreStash:
		if record.RefName == "" || record.ObjectID == "" {
			return fmt.Errorf("undo operation: dropped stash information is incomplete")
		}
		message := record.Message
		if message == "" {
			message = record.Summary
		}
		output, err := s.cmd.New([]string{"git", "stash", "store", "-m", message, record.RefName}).RunWithOutput()
		if err != nil {
			return fmt.Errorf("restore dropped stash: %w: %s", err, strings.TrimSpace(output))
		}
	case UndoRestoreIndex:
		if err := s.undoIndexOperation(record); err != nil {
			return s.failJournalOperation(record, err)
		}
		return s.markOperationUndone(record)
	case UndoRestoreFile:
		if err := s.undoFileOperation(record); err != nil {
			return s.failJournalOperation(record, err)
		}
		return s.markOperationUndone(record)
	case UndoRestoreRef:
		if err := s.undoRefOperation(record); err != nil {
			return s.failJournalOperation(record, err)
		}
		return s.markOperationUndone(record)
	case UndoReversePatch:
		patchName := s.joinPath(s.journalDir(), record.ID+".patch")
		data, err := s.fileService().ReadFile(patchName)
		if err != nil {
			return fmt.Errorf("undo operation: read saved patch: %w", err)
		}
		if err := s.applyWorkingTreePatch(ctx, string(data), true); err != nil {
			return err
		}
	default:
		return fmt.Errorf("undo operation: unsupported recovery plan")
	}
	if err := s.verifyWorkspaceUndo(record); err != nil {
		return err
	}
	return s.markOperationUndone(record)
}

func normalizedOperationType(type_ OperationType) OperationType {
	if type_ == OperationTypePullRebase {
		return OperationTypeRebase
	}
	return type_
}

// finishStartedJournalOperation distinguishes a conflict pause from a failed
// command. The original Git error is retained for the caller, while the
// journal stays active and cannot offer Undo at a transient detached HEAD.
func (s *gitSession) finishStartedJournalOperation(record OperationRecord, type_ OperationType, actionErr error) error {
	if actionErr != nil {
		state, stateErr := s.OperationState(context.Background())
		if stateErr == nil && normalizedOperationType(state.Type) == normalizedOperationType(type_) && state.OrigSHA == record.HeadBefore {
			record.ActiveOperation = type_
			record.RebasePaused = normalizedOperationType(type_) == OperationTypeRebase
			if finishErr := s.completeJournalOperation(record, nil); finishErr != nil {
				return errors.Join(actionErr, finishErr)
			}
			return actionErr
		}
	}
	return s.completeJournalOperation(record, actionErr)
}

// finalizeActiveOperationJournal closes only a record matching the operation
// which just left Git metadata. Continue and skip finalize successful recovery;
// abort leaves an interrupted, non-undoable record.
func (s *gitSession) finalizeActiveOperationJournal(ctx context.Context, finished OperationType, action string) error {
	records, err := s.Operations(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		active := record.ActiveOperation
		if active == OperationTypeNone && record.RebasePaused {
			active = OperationTypeRebase
		}
		if active == OperationTypeNone || normalizedOperationType(active) != normalizedOperationType(finished) {
			continue
		}
		record.ActiveOperation = OperationTypeNone
		record.RebasePaused = false
		switch action {
		case "continue", "skip":
			branch, _, identityErr := s.currentOperationIdentity()
			if identityErr != nil {
				return fmt.Errorf("finish operation recovery: %w", identityErr)
			}
			if record.Branch != "" && branch != record.Branch {
				return fmt.Errorf("operation finished on %s instead of the original branch %s", branch, record.Branch)
			}
			record.Error = ""
			// Completion is no longer the paused state. Refresh the durable
			// post snapshot and both guard fingerprints before offering Undo.
			return s.completeJournalOperation(record, nil)
		case "abort":
			record.Status = OperationInterrupted
			record.Message = fmt.Sprintf("The active %s was aborted.", active)
			record.FinishedAt = time.Now().Unix()
		default:
			return fmt.Errorf("unsupported active-operation recovery action %q", action)
		}
		name, pathErr := s.operationPath(record.ID)
		if pathErr != nil {
			return pathErr
		}
		if err := s.writeOperationRecord(name, record); err != nil {
			return fmt.Errorf("finish operation recovery: %w", err)
		}
		return nil
	}
	return nil
}
