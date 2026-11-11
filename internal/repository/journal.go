package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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
	// Partial marks a failed multi-file discard after at least one file was
	// completely discarded. Its recorded final state and snapshot still support
	// the same revalidated Undo as a successful discard.
	Partial     bool          `json:"partial,omitempty"`
	CreatedAt   int64         `json:"created_at"`
	FinishedAt  int64         `json:"finished_at,omitempty"`
	UndoneAt    int64         `json:"undone_at,omitempty"`
	Worktree    string        `json:"worktree"`
	Branch      string        `json:"branch_before,omitempty"`
	BranchAfter string        `json:"branch_after,omitempty"`
	HeadBefore  string        `json:"head_before,omitempty"`
	HeadAfter   string        `json:"head_after,omitempty"`
	BeforeState string        `json:"before_state,omitempty"`
	AfterState  string        `json:"after_state,omitempty"`
	StashAfter  string        `json:"stash_after,omitempty"`
	ObjectID    string        `json:"object_id,omitempty"`
	RefName     string        `json:"ref_name,omitempty"`
	Message     string        `json:"message,omitempty"`
	UndoKind    string        `json:"undo_kind,omitempty"`
	Snapshot    *WorkSnapshot `json:"snapshot,omitempty"`
	Error       string        `json:"error,omitempty"`

	patchFile string
}

// CanUndo is only a UI hint. Repository preflights remain authoritative.
func (r OperationRecord) CanUndo() bool {
	recoverable := r.Status == OperationSucceeded || (r.Status == OperationFailed && r.Partial)
	return recoverable && r.UndoneAt == 0 && r.UndoKind != ""
}

func (s *gitSession) journalDir() string {
	return filepath.Join(s.git.RepoPaths.WorktreeGitDirPath(), "tangit", "journal")
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
	return filepath.Join(s.journalDir(), id+".json"), nil
}

// syncDirectory persists a rename's directory entry on Unix filesystems.
// Directory fsync is not supported by Windows, where the file sync before the
// rename remains the best available durability boundary.
func syncDirectory(name string) error {
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

func writeOperationRecord(name string, record OperationRecord) error {
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
	if err := syncDirectory(filepath.Dir(name)); err != nil {
		return fmt.Errorf("sync safety journal: %w", err)
	}
	return nil
}

func readOperationRecord(name string) (OperationRecord, error) {
	data, err := os.ReadFile(name)
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
	state, err := s.patchRepositoryState()
	if err != nil {
		return OperationRecord{}, fmt.Errorf("record operation start: %w", err)
	}
	record := OperationRecord{
		Version: journalVersion, ID: hex.EncodeToString(random[:]), Kind: kind,
		Summary: RedactSecrets(strings.TrimSpace(summary)), Status: OperationPending,
		CreatedAt: time.Now().Unix(), Worktree: s.git.RepoPaths.WorktreePath(),
		Branch: branch, HeadBefore: head, BeforeState: state,
		UndoKind: undoKind, Snapshot: snapshot,
	}
	name, _ := s.operationPath(record.ID)
	if err := writeOperationRecord(name, record); err != nil {
		return OperationRecord{}, err
	}
	return record, nil
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
	if err := writeOperationRecord(name, record); err != nil {
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
	var stashErr error
	if actionErr == nil && record.UndoKind == UndoRestoreStash {
		record.StashAfter, stashErr = s.stashOperationState()
	}
	record.FinishedAt = time.Now().Unix()
	record.Status = OperationSucceeded
	if actionErr != nil {
		record.Status = OperationFailed
		record.Error = RedactSecrets(actionErr.Error())
		if !record.Partial {
			record.UndoKind = ""
		}
	}
	if identityErr != nil || stateErr != nil || stashErr != nil {
		record.Status = OperationInterrupted
		record.UndoKind = ""
	}
	name, _ := s.operationPath(record.ID)
	if err := writeOperationRecord(name, record); err != nil {
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
	entries, err := os.ReadDir(s.journalDir())
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
		record, readErr := readOperationRecord(filepath.Join(s.journalDir(), entry.Name()))
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
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt > records[j].CreatedAt
	})
	return records, nil
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
	return nil
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
	if record.Worktree != s.git.RepoPaths.WorktreePath() {
		return fmt.Errorf("operation belongs to a different worktree")
	}
	if state := s.git.Status.WorkingTreeState(); state.Any() {
		return fmt.Errorf("cannot undo while %s is in progress", state.CommandName())
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
	if record.UndoKind == UndoRestoreStash {
		stashState, err := s.stashOperationState()
		if err != nil {
			return err
		}
		if stashState != record.StashAfter {
			return fmt.Errorf("the stash list changed after this operation; preserve the newer stash state instead of undoing")
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
	record.UndoneAt = time.Now().Unix()
	name, _ := s.operationPath(record.ID)
	if err := writeOperationRecord(name, record); err != nil {
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
	env := []string{"GIT_AUTHOR_NAME=TanGit", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=TanGit", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	hash, err := s.cmd.New([]string{"git", "commit-tree", strings.TrimSpace(tree), "-p", strings.TrimSpace(base), "-p", strings.TrimSpace(index), "-m", "TanGit journal recovery"}).AddEnvVars(env...).RunWithOutput()
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
	untracked, err := s.gitText("rev-parse", "--verify", "--quiet", snapshot.Hash+"^3^{tree}")
	if err != nil {
		// Snapshots without an untracked parent have only tracked work to undo.
		return nil
	}
	untrackedTree := strings.TrimSpace(untracked)
	listing, err := s.cmd.New([]string{"git", "ls-tree", "-r", "--name-only", "-z", untrackedTree}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("read saved untracked files: %w: %s", err, strings.TrimSpace(listing))
	}
	if listing == "" {
		return nil
	}
	paths, err := os.CreateTemp("", "tangit-journal-untracked-")
	if err != nil {
		return fmt.Errorf("prepare saved untracked files: %w", err)
	}
	name := paths.Name()
	defer os.Remove(name)
	if _, err := paths.WriteString(listing); err != nil {
		paths.Close()
		return fmt.Errorf("prepare saved untracked files: %w", err)
	}
	if err := paths.Close(); err != nil {
		return fmt.Errorf("prepare saved untracked files: %w", err)
	}
	output, err := s.cmd.New([]string{
		"git", "--literal-pathspecs", "restore", "--source=" + untrackedTree, "--worktree",
		"--pathspec-from-file=" + name, "--pathspec-file-nul",
	}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore saved untracked files: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// restoreResetWorkSnapshot restores tracked index/worktree state separately
// from the pure-untracked parent. Reset can overwrite or remove an untracked
// file when the target tracks the same path, so that parent is still required.
func (s *gitSession) restoreResetWorkSnapshot(ctx context.Context, snapshot WorkSnapshot) error {
	tracked, err := s.trackedSnapshotWithoutUntracked(snapshot)
	if err != nil {
		return err
	}
	if err := s.RestoreWorkSnapshot(ctx, tracked); err != nil {
		return err
	}
	return s.restoreSnapshotUntracked(snapshot)
}

// restoreJournalWorkSnapshot undoes discard after its exact after-state has
// matched. Tracked state uses stash's index-aware apply; surviving untracked
// files are then overwritten only where the saved tree has a version.
func (s *gitSession) restoreJournalWorkSnapshot(snapshot WorkSnapshot) error {
	tracked, err := s.trackedSnapshotWithoutUntracked(snapshot)
	if err != nil {
		return err
	}
	if err := s.applyWorkSnapshot(tracked); err != nil {
		return err
	}
	return s.restoreSnapshotUntracked(snapshot)
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
	record, err := readOperationRecord(name)
	if err != nil {
		return err
	}
	if record.ID != id {
		return fmt.Errorf("safety journal operation identity does not match its record")
	}
	if !record.CanUndo() {
		return fmt.Errorf("this operation has no safe undo available")
	}
	if err := s.requireUnchangedOperation(record); err != nil {
		return fmt.Errorf("undo operation: %w", err)
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
		if err := s.restoreJournalWorkSnapshot(*record.Snapshot); err != nil {
			return err
		}
	case UndoResetHead:
		if record.HeadBefore == "" {
			return fmt.Errorf("undo operation: the previous HEAD is unavailable")
		}
		if _, err := s.gitText("rev-parse", "--verify", record.RefName); record.RefName == "" || err != nil {
			return fmt.Errorf("undo operation: the previous HEAD is no longer pinned")
		}
		if err := s.git.Commit.ResetToCommit(record.HeadBefore, string(ResetHard), nil); err != nil {
			return fmt.Errorf("restore previous HEAD: %w", err)
		}
		if record.Snapshot != nil {
			if err := s.restoreResetWorkSnapshot(ctx, *record.Snapshot); err != nil {
				return fmt.Errorf("HEAD was restored, but saved work still needs recovery: %w", err)
			}
		}
	case UndoRecreateBranch:
		if record.RefName == "" || record.ObjectID == "" {
			return fmt.Errorf("undo operation: deleted branch information is incomplete")
		}
		if err := s.cmd.New([]string{"git", "show-ref", "--verify", "--quiet", "refs/heads/" + record.Branch}).Run(); err == nil {
			return fmt.Errorf("branch %s already exists; it will not be replaced", record.Branch)
		}
		if err := s.git.Branch.New(record.Branch, record.RefName); err != nil {
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
	case UndoReversePatch:
		patchName := filepath.Join(s.journalDir(), record.ID+".patch")
		data, err := os.ReadFile(patchName)
		if err != nil {
			return fmt.Errorf("undo operation: read saved patch: %w", err)
		}
		if err := s.applyWorkingTreePatch(ctx, string(data), true); err != nil {
			return err
		}
	default:
		return fmt.Errorf("undo operation: unsupported recovery plan")
	}
	return s.markOperationUndone(record)
}
