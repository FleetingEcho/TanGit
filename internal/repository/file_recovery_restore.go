package repository

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// readFileRecoveryBlob returns bytes, never display text, and verifies that the
// object ID really names those bytes under this repository's hash algorithm.
func (s *gitSession) readFileRecoveryBlob(oid string) ([]byte, error) {
	if !fileRecoveryOID(oid) {
		return nil, fmt.Errorf("invalid blob object id")
	}
	output, err := s.cmd.New([]string{"git", "cat-file", "blob", oid}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w: %s", oid, err, strings.TrimSpace(output))
	}
	hash, err := s.cmd.New([]string{"git", "hash-object", "--stdin"}).SetStdin(output).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("verify blob %s: %w: %s", oid, err, strings.TrimSpace(hash))
	}
	if strings.TrimSpace(hash) != oid {
		return nil, fmt.Errorf("blob %s content does not match its object id", oid)
	}
	return []byte(output), nil
}

func (s *gitSession) fileRecoveryWorktreePath(relativePath string) string {
	if s.sshClient != nil {
		return s.joinPath(s.git.RepoPaths.WorktreePath(), relativePath)
	}
	return s.joinPath(s.git.RepoPaths.WorktreePath(), filepath.FromSlash(relativePath))
}

func (s *gitSession) captureFileRecoveryWorktree(relativePath string) (FileRecoveryWorktree, error) {
	name := s.fileRecoveryWorktreePath(relativePath)
	info, err := s.fileService().Lstat(name)
	if os.IsNotExist(err) {
		return FileRecoveryWorktree{}, nil
	}
	if err != nil {
		return FileRecoveryWorktree{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return FileRecoveryWorktree{}, fmt.Errorf("%w: symbolic-link worktree path %s", ErrFileRecoveryUnsupported, relativePath)
	}
	if !info.Mode().IsRegular() {
		return FileRecoveryWorktree{}, fmt.Errorf("%w: non-regular worktree path %s", ErrFileRecoveryUnsupported, relativePath)
	}
	content, err := s.fileService().ReadFile(name)
	if err != nil {
		return FileRecoveryWorktree{}, err
	}
	mode := "100644"
	if info.Mode().Perm()&0o111 != 0 {
		mode = "100755"
	}
	return FileRecoveryWorktree{Present: true, Mode: mode, Content: content}, nil
}

func (s *gitSession) captureFileRecoveryIndex(relativePath string) (FileRecoveryIndex, error) {
	output, err := s.cmd.New([]string{
		"git", "--literal-pathspecs", "ls-files", "--stage", "-z", "--", relativePath,
	}).RunWithOutput()
	if err != nil {
		return FileRecoveryIndex{}, fmt.Errorf("list index stages: %w: %s", err, strings.TrimSpace(output))
	}
	var entries [4]FileRecoveryIndexEntry
	for stage := range entries {
		entries[stage].Stage = stage
	}
	for record := range strings.SplitSeq(output, "\x00") {
		if record == "" {
			continue
		}
		metadata, name, ok := strings.Cut(record, "\t")
		if !ok || name != relativePath {
			return FileRecoveryIndex{}, fmt.Errorf("index path %q does not exactly match %q", name, relativePath)
		}
		fields := strings.Fields(metadata)
		if len(fields) != 3 {
			return FileRecoveryIndex{}, fmt.Errorf("malformed index entry for %s", relativePath)
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil || stage < 0 || stage > 3 {
			return FileRecoveryIndex{}, fmt.Errorf("invalid index stage for %s", relativePath)
		}
		if entries[stage].Present {
			return FileRecoveryIndex{}, fmt.Errorf("duplicate index stage %d for %s", stage, relativePath)
		}
		mode, oid := fields[0], strings.ToLower(fields[1])
		if err := fileRecoveryPresentMode(mode); err != nil {
			return FileRecoveryIndex{}, fmt.Errorf("path %s stage %d: %w", relativePath, stage, err)
		}
		if !fileRecoveryOID(oid) {
			return FileRecoveryIndex{}, fmt.Errorf("invalid object id at path %s stage %d", relativePath, stage)
		}
		entries[stage] = FileRecoveryIndexEntry{Stage: stage, Present: true, Mode: mode, OID: oid}
	}
	if entries[0].Present && (entries[1].Present || entries[2].Present || entries[3].Present) {
		return FileRecoveryIndex{}, fmt.Errorf("path %s has both stage 0 and unmerged entries", relativePath)
	}
	return fileRecoveryIndexFromEntries(entries), nil
}

func (s *gitSession) captureFileRecoveryState(relativePath string) (FileRecoveryState, error) {
	// Inspect the index first so a gitlink has a precise unsupported error even
	// though its worktree representation is a directory.
	index, err := s.captureFileRecoveryIndex(relativePath)
	if err != nil {
		return FileRecoveryState{}, err
	}
	worktree, err := s.captureFileRecoveryWorktree(relativePath)
	if err != nil {
		return FileRecoveryState{}, err
	}
	return FileRecoveryState{Worktree: worktree, Index: index}, nil
}

func (s *gitSession) capturePreparedFileRecoveryState(relativePath string) (FileRecoveryState, error) {
	state, err := s.captureFileRecoveryState(relativePath)
	if err != nil {
		return FileRecoveryState{}, err
	}
	state.Worktree, err = prepareFileRecoveryWorktree(state.Worktree)
	if err != nil {
		return FileRecoveryState{}, err
	}
	state.Index, err = s.prepareFileRecoveryIndex(state.Index)
	if err != nil {
		return FileRecoveryState{}, err
	}
	return state, nil
}

func (s *gitSession) readFileRecoveryPayload(digest string, size int64) ([]byte, error) {
	name, err := s.fileRecoveryPayloadPath(digest)
	if err != nil {
		return nil, err
	}
	content, err := s.fileService().ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read file recovery payload: %w", err)
	}
	if int64(len(content)) != size || fileRecoveryDigest(content) != digest {
		return nil, fmt.Errorf("file recovery payload digest or size does not match its manifest")
	}
	return content, nil
}

func (s *gitSession) loadFileRecoveryInitialState(state FileRecoveryState) (FileRecoveryState, error) {
	var err error
	if state.Worktree.Present {
		state.Worktree.Content, err = s.readFileRecoveryPayload(state.Worktree.Digest, state.Worktree.Size)
		if err != nil {
			return FileRecoveryState{}, err
		}
	}
	entries := state.Index.entries()
	for stage := range entries {
		if !entries[stage].Present {
			continue
		}
		entries[stage].Content, err = s.readFileRecoveryPayload(entries[stage].Digest, entries[stage].Size)
		if err != nil {
			return FileRecoveryState{}, fmt.Errorf("load stage %d payload: %w", stage, err)
		}
	}
	state.Index = fileRecoveryIndexFromEntries(entries)
	return state, nil
}

func (s *gitSession) ensureFileRecoveryIndexBlobs(index FileRecoveryIndex) error {
	for _, entry := range index.entries() {
		if !entry.Present {
			continue
		}
		content, err := s.readFileRecoveryBlob(entry.OID)
		if err == nil {
			if !bytes.Equal(content, entry.Content) {
				return fmt.Errorf("stage %d payload differs from its Git blob", entry.Stage)
			}
			continue
		}
		// The payload makes recovery independent of reflogs and object retention.
		// hash-object -w changes only object storage, never .git/index.
		output, writeErr := s.cmd.New([]string{"git", "hash-object", "-w", "--stdin"}).SetStdin(string(entry.Content)).RunWithOutput()
		if writeErr != nil {
			return fmt.Errorf("recreate stage %d blob: %w: %s", entry.Stage, writeErr, strings.TrimSpace(output))
		}
		if strings.TrimSpace(output) != entry.OID {
			return fmt.Errorf("stage %d payload hashes to an unexpected object id", entry.Stage)
		}
	}
	return nil
}

func (s *gitSession) ensureFileRecoveryParent(relativePath string) error {
	root := s.git.RepoPaths.WorktreePath()
	parent := path.Dir(relativePath)
	if parent == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(parent, "/") {
		current = s.joinPath(current, component)
		info, err := s.fileService().Lstat(current)
		if os.IsNotExist(err) {
			if err := s.fileService().MkdirAll(current, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: unsafe parent component in %s", ErrFileRecoveryUnsupported, relativePath)
		}
	}
	return nil
}

func (s *gitSession) writeFileRecoveryWorktree(relativePath string, worktree FileRecoveryWorktree) error {
	name := s.fileRecoveryWorktreePath(relativePath)
	if !worktree.Present {
		if err := s.fileService().Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove recovered worktree file: %w", err)
		}
		if s.sshClient != nil {
			return s.syncRemotePath(path.Dir(name))
		}
		return s.syncDirectory(filepath.Dir(name))
	}
	if err := s.ensureFileRecoveryParent(relativePath); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if worktree.Mode == "100755" {
		mode = 0o755
	}
	if err := s.writeFileRecoveryAtomic(name, worktree.Content, mode); err != nil {
		return fmt.Errorf("write recovered worktree file: %w", err)
	}
	return nil
}

func (s *gitSession) recoverFileRecoveryIndex(relativePath string, index FileRecoveryIndex) error {
	emptyOID, err := s.cmd.New([]string{"git", "hash-object", "--stdin"}).SetStdin("").RunWithOutput()
	if err != nil {
		return fmt.Errorf("determine Git object format: %w: %s", err, strings.TrimSpace(emptyOID))
	}
	zeroOID := strings.Repeat("0", len(strings.TrimSpace(emptyOID)))
	var input strings.Builder
	// Mode zero removes every old stage for exactly this literal path. Adding
	// entries in the same update-index invocation keeps the real index atomic.
	fmt.Fprintf(&input, "0 %s 0\t%s\x00", zeroOID, relativePath)
	for _, entry := range index.entries() {
		if entry.Present {
			fmt.Fprintf(&input, "%s %s %d\t%s\x00", entry.Mode, entry.OID, entry.Stage, relativePath)
		}
	}
	output, err := s.cmd.New([]string{"git", "update-index", "-z", "--index-info"}).SetStdin(input.String()).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore index through update-index: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

type fileRecoveryRestoreHooks struct {
	beforeIndex func() error
	afterIndex  func() error
}

func fileRecoveryConflict(record FileRecoveryRecord, component string) error {
	return &FileRecoveryConflictError{Path: record.Path, Component: component}
}

func (s *gitSession) interruptFileRecovery(record FileRecoveryRecord, cause error) (FileRecoveryRecord, error) {
	record.Status = FileRecoveryInterrupted
	record.Error = RedactSecrets(cause.Error())
	appendFileRecoveryJournal(&record, fileRecoveryEventInterrupted, cause.Error())
	if err := s.writeFileRecoveryRecord(record); err != nil {
		return record, fmt.Errorf("%w (also could not journal the interrupted file recovery: %v)", cause, err)
	}
	stripFileRecoveryRecordContent(&record)
	return record, cause
}

// restoreFileRecovery has an internal seam for crash tests. The public method
// passes no hooks; production progress transitions are exactly the tested ones.
func (s *gitSession) restoreFileRecovery(ctx context.Context, id string, hooks *fileRecoveryRestoreHooks) (FileRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	record, err := s.readFileRecoveryRecord(id, true)
	if err != nil {
		return FileRecoveryRecord{}, err
	}
	initial, err := s.loadFileRecoveryInitialState(record.Initial)
	if err != nil {
		return s.interruptFileRecovery(record, err)
	}
	record.Initial = initial
	current, err := s.capturePreparedFileRecoveryState(record.Path)
	if err != nil {
		return s.interruptFileRecovery(record, fmt.Errorf("capture current file state: %w", err))
	}

	// CAS accepts either the saved after-state (not restored yet) or the exact
	// initial state (a completed write whose journal transition was interrupted).
	// Once file progress is durable, an after-state file must not be overwritten.
	if !fileRecoveryWorktreeEqual(current.Worktree, initial.Worktree) {
		if record.Progress == FileRecoveryWorktreeRestored || record.Progress == FileRecoveryIndexRestored ||
			!fileRecoveryWorktreeEqual(current.Worktree, record.After.Worktree) {
			return s.interruptFileRecovery(record, fileRecoveryConflict(record, "worktree"))
		}
	}
	if !fileRecoveryIndexEqual(current.Index, initial.Index) &&
		!fileRecoveryIndexEqual(current.Index, record.After.Index) {
		return s.interruptFileRecovery(record, fileRecoveryConflict(record, "index"))
	}
	if record.Status == FileRecoveryRestored && fileRecoveryStateEqual(current, initial) {
		stripFileRecoveryRecordContent(&record)
		return record, nil
	}
	if err := ctx.Err(); err != nil {
		return s.interruptFileRecovery(record, err)
	}

	previousStatus := record.Status
	record.Status = FileRecoveryRestoring
	record.Error = ""
	record.Attempts++
	event := fileRecoveryEventRestoreStarted
	if previousStatus == FileRecoveryInterrupted || record.Attempts > 1 {
		event = fileRecoveryEventRetryStarted
	}
	appendFileRecoveryJournal(&record, event, "")
	if err := s.writeFileRecoveryRecord(record); err != nil {
		return FileRecoveryRecord{}, err
	}

	if record.Progress == FileRecoveryPending {
		// Recheck immediately before the atomic file write. The worktree-only read
		// avoids mutating or rejecting because of any unrelated path.
		latest, captureErr := s.capturePreparedFileRecoveryState(record.Path)
		if captureErr != nil {
			return s.interruptFileRecovery(record, captureErr)
		}
		event := fileRecoveryEventWorktreeRestored
		switch {
		case fileRecoveryWorktreeEqual(latest.Worktree, initial.Worktree):
			event = fileRecoveryEventWorktreeVerified
		case fileRecoveryWorktreeEqual(latest.Worktree, record.After.Worktree):
			if err := s.writeFileRecoveryWorktree(record.Path, initial.Worktree); err != nil {
				return s.interruptFileRecovery(record, err)
			}
		default:
			return s.interruptFileRecovery(record, fileRecoveryConflict(record, "worktree"))
		}
		record.Progress = FileRecoveryWorktreeRestored
		appendFileRecoveryJournal(&record, event, "")
		if err := s.writeFileRecoveryRecord(record); err != nil {
			return s.interruptFileRecovery(record, err)
		}
	}

	if hooks != nil && hooks.beforeIndex != nil {
		if err := hooks.beforeIndex(); err != nil {
			return s.interruptFileRecovery(record, err)
		}
	}
	latest, err := s.capturePreparedFileRecoveryState(record.Path)
	if err != nil {
		return s.interruptFileRecovery(record, err)
	}
	if !fileRecoveryWorktreeEqual(latest.Worktree, initial.Worktree) {
		return s.interruptFileRecovery(record, fileRecoveryConflict(record, "worktree after the file step"))
	}
	indexEvent := fileRecoveryEventIndexRestored
	switch {
	case fileRecoveryIndexEqual(latest.Index, initial.Index):
		// The command completed before a crash, but its final journal write did not.
		indexEvent = fileRecoveryEventIndexVerified
	case fileRecoveryIndexEqual(latest.Index, record.After.Index):
		if err := s.ensureFileRecoveryIndexBlobs(initial.Index); err != nil {
			return s.interruptFileRecovery(record, err)
		}
		if err := s.recoverFileRecoveryIndex(record.Path, initial.Index); err != nil {
			return s.interruptFileRecovery(record, err)
		}
		if hooks != nil && hooks.afterIndex != nil {
			if err := hooks.afterIndex(); err != nil {
				return s.interruptFileRecovery(record, err)
			}
		}
		verification, err := s.capturePreparedFileRecoveryState(record.Path)
		if err != nil {
			return s.interruptFileRecovery(record, err)
		}
		if !fileRecoveryStateEqual(verification, initial) {
			return s.interruptFileRecovery(record, fmt.Errorf("verify restored file recovery: final state differs for %s", record.Path))
		}
	default:
		return s.interruptFileRecovery(record, fileRecoveryConflict(record, "index"))
	}
	record.Progress = FileRecoveryIndexRestored
	record.Status = FileRecoveryRestored
	appendFileRecoveryJournal(&record, indexEvent, "")
	if err := s.writeFileRecoveryRecord(record); err != nil {
		return s.interruptFileRecovery(record, err)
	}
	stripFileRecoveryRecordContent(&record)
	return record, nil
}

// RestoreFileRecovery explicitly starts or retries one operation. Interrupted
// progress is resumed only after CAS and payload verification, never by blindly
// replaying both steps. Git itself owns .git/index through update-index.
func (s *gitSession) RestoreFileRecovery(ctx context.Context, id string) error {
	_, err := s.restoreFileRecovery(ctx, id, nil)
	return err
}

// The optional interface remains useful to app code without changing Session
// or any of its lightweight fakes.
var _ FileRecoverySession = (*gitSession)(nil)
