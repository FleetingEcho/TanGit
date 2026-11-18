package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"strings"
	"unicode/utf8"
)

// ErrConflictResultChanged rejects a reviewed Result whose source worktree,
// unmerged index stages, or interrupted Git operation changed after preview.
var ErrConflictResultChanged = errors.New("conflict result source changed")

// ErrConflictResultBinary keeps binary conflict content out of the text-only
// conflict assistant. Existing whole-file conflict actions remain available.
var ErrConflictResultBinary = errors.New("binary conflict results are unsupported")

// ConflictResultSession is an optional narrow extension used by Conflict
// Assistant. It deliberately has no staging, operation-driving, hook, or commit
// parameter: the only repository mutation is the reviewed worktree Result.
type ConflictResultSession interface {
	Session
	WriteConflictResult(context.Context, ConflictResultRequest) (FileRecoveryRecord, error)
}

// ConflictResultRequest binds the exact approved Result to the state from which
// its region preview was built. Digest strings use the sha256:<hex> form.
type ConflictResultRequest struct {
	Path                 string
	WorktreeDigest       string
	IndexDigest          string
	OperationFingerprint string
	Result               []byte
}

// ConflictWorktreeDigest identities raw file bytes, not normalized editor text.
func ConflictWorktreeDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func conflictDigestString(digest hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write([]byte(value))
}

// ConflictStagesDigest identities every unmerged stage's presence and exact
// bytes. Mode/OID changes are additionally covered by OperationState's raw
// ls-files fingerprint, so an index edit cannot be reduced to display text.
func ConflictStagesDigest(stages ConflictStages) string {
	digest := sha256.New()
	conflictDigestString(digest, "tangit-conflict-index-v1")
	for _, item := range []struct {
		name  string
		stage ConflictStage
	}{{"base", stages.Base}, {"current", stages.Current}, {"incoming", stages.Incoming}} {
		conflictDigestString(digest, item.name)
		if item.stage.Present {
			conflictDigestString(digest, "present")
		} else {
			conflictDigestString(digest, "absent")
		}
		conflictDigestString(digest, item.stage.Content)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func conflictStagesFromRecovery(index FileRecoveryIndex) ConflictStages {
	result := ConflictStages{}
	for stage, entry := range index.entries() {
		if !entry.Present {
			continue
		}
		value := ConflictStage{Present: true, Content: string(entry.Content)}
		switch stage {
		case 1:
			result.Base = value
		case 2:
			result.Current = value
		case 3:
			result.Incoming = value
		}
	}
	return result
}

func conflictResultChanged(component string) error {
	return fmt.Errorf("%w: %s does not match the reviewed source; request advice again", ErrConflictResultChanged, component)
}

// WriteConflictResult archives the exact before/after per-path state with V2
// recovery, then atomically replaces only the worktree Result. It never stages,
// continues, commits, or invokes a hook. All validation and publication happen
// under TanGit's existing worktree mutation lock.
func (s *gitSession) WriteConflictResult(ctx context.Context, request ConflictResultRequest) (FileRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	if err := validateFileRecoveryPath(request.Path); err != nil {
		return FileRecoveryRecord{}, err
	}
	if strings.TrimSpace(request.WorktreeDigest) == "" || strings.TrimSpace(request.IndexDigest) == "" {
		return FileRecoveryRecord{}, fmt.Errorf("conflict Result requires raw worktree and index digests")
	}
	if !utf8.Valid(request.Result) || strings.IndexByte(string(request.Result), 0) >= 0 {
		return FileRecoveryRecord{}, ErrConflictResultBinary
	}

	unlock := s.lockWorktreeMutation()
	defer unlock()
	initial, err := s.capturePreparedFileRecoveryState(request.Path)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("capture conflict Result source: %w", err)
	}
	if !initial.Worktree.Present || !utf8.Valid(initial.Worktree.Content) || strings.IndexByte(string(initial.Worktree.Content), 0) >= 0 {
		return FileRecoveryRecord{}, ErrConflictResultBinary
	}
	if ConflictWorktreeDigest(initial.Worktree.Content) != request.WorktreeDigest {
		return FileRecoveryRecord{}, conflictResultChanged("working-tree bytes")
	}
	indexDigest := ConflictStagesDigest(conflictStagesFromRecovery(initial.Index))
	if indexDigest != request.IndexDigest {
		return FileRecoveryRecord{}, conflictResultChanged("unmerged index stages")
	}
	if initial.Index.Stage0.Present || (!initial.Index.Stage1.Present && !initial.Index.Stage2.Present && !initial.Index.Stage3.Present) {
		return FileRecoveryRecord{}, fmt.Errorf("write conflict Result: %s is not an unmerged index path", request.Path)
	}
	operation, err := s.OperationState(ctx)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("validate conflict operation fingerprint: %w", err)
	}
	if operation.Fingerprint != request.OperationFingerprint {
		return FileRecoveryRecord{}, conflictResultChanged("operation fingerprint")
	}

	after := initial
	after.Worktree.Content = append([]byte(nil), request.Result...)
	after.Worktree.Present = true
	record, err := s.archivePreparedFileRecovery(ctx, request.Path, initial, after)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("save conflict Result recovery: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	mode := 0o644
	if initial.Worktree.Mode == "100755" {
		mode = 0o755
	}
	name := s.fileRecoveryWorktreePath(request.Path)
	if err := s.writeFileRecoveryAtomic(name, request.Result, os.FileMode(mode)); err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("write conflict Result only: %w", err)
	}
	return record, nil
}
