package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// FileRecoveryManifestVersion is deliberately independent of the V1 safety
	// journal. V2 records are neither migrated nor exposed as V1 operations.
	FileRecoveryManifestVersion = 2

	FileRecoveryReady       = "ready"
	FileRecoveryRestoring   = "restoring"
	FileRecoveryInterrupted = "interrupted"
	FileRecoveryRestored    = "restored"

	FileRecoveryPending          = "pending"
	FileRecoveryWorktreeRestored = "worktree-restored"
	FileRecoveryIndexRestored    = "index-restored"

	fileRecoveryEventArchived         = "archived"
	fileRecoveryEventRestoreStarted   = "restore-started"
	fileRecoveryEventRetryStarted     = "retry-started"
	fileRecoveryEventWorktreeRestored = "worktree-restored"
	fileRecoveryEventWorktreeVerified = "worktree-verified"
	fileRecoveryEventIndexRestored    = "index-restored"
	fileRecoveryEventIndexVerified    = "index-verified"
	fileRecoveryEventInterrupted      = "interrupted"
)

// ErrFileRecoveryUnsupported marks file kinds whose exact filesystem semantics
// cannot be restored by this module. In particular, gitlinks/submodules are not
// silently replaced with their checked-out directories.
var ErrFileRecoveryUnsupported = errors.New("unsupported file recovery state")

// ErrFileRecoveryAfterStateChanged marks the per-path compare-and-swap refusal.
// Changes in unrelated files never participate in this decision.
var ErrFileRecoveryAfterStateChanged = errors.New("file recovery after-state changed")

// FileRecoveryConflictError says which component stopped the restore. The saved
// operation remains available after the caller preserves or resolves the work.
type FileRecoveryConflictError struct {
	Path      string
	Component string
}

func (e *FileRecoveryConflictError) Error() string {
	return fmt.Sprintf("restore %s stopped because its %s no longer matches the saved after-state", e.Path, e.Component)
}

func (e *FileRecoveryConflictError) Unwrap() error { return ErrFileRecoveryAfterStateChanged }

// FileRecoverySession is an optional extension of Session. Keeping it separate
// avoids forcing every existing Session fake to implement V2 file recovery.
// Archive does not mutate the worktree or index: After describes the exact state
// produced by the caller's subsequent operation, and Restore CASes against it.
// TanGit serializes its own mutations and rechecks before each phase; callers
// must still coordinate external Git processes for a cross-process transaction.
type FileRecoverySession interface {
	ArchiveFileRecovery(context.Context, FileRecoveryArchive) (FileRecoveryRecord, error)
	RestoreFileRecovery(context.Context, string) error
	ReadFileRecovery(context.Context, string) (FileRecoveryRecord, error)
}

// FileRecoveryArchive identifies one file and the complete per-path state that
// must still match when recovery starts. For a present after-worktree file,
// Content holds the expected bytes (nil is empty); index content comes from its
// verified Git blob OID.
type FileRecoveryArchive struct {
	Path  string
	After FileRecoveryState
}

// FileRecoveryWorktree stores raw file metadata and its SHA-256 payload digest.
// Content is an archive input / internal snapshot and is never embedded in JSON.
// Present distinguishes deletion from an empty file. Mode is 100644 or 100755.
type FileRecoveryWorktree struct {
	Present bool   `json:"present"`
	Mode    string `json:"mode,omitempty"`
	Size    int64  `json:"size"`
	Digest  string `json:"digest,omitempty"`
	Content []byte `json:"-"`
}

// FileRecoveryIndexEntry is an exact Git index entry. Stage is 0, 1, 2 or 3.
// Present distinguishes an absent side from an empty blob. OID and Content both
// identify the blob; the raw payload also lets Restore recreate a pruned object.
type FileRecoveryIndexEntry struct {
	Stage   int    `json:"stage"`
	Present bool   `json:"present"`
	Mode    string `json:"mode,omitempty"`
	OID     string `json:"oid,omitempty"`
	Size    int64  `json:"size"`
	Digest  string `json:"digest,omitempty"`
	Content []byte `json:"-"`
}

// FileRecoveryIndex always has all four stage slots. An ordinary staged file
// has only Stage0 present; an unmerged file has the exact saved 1/2/3 subset.
type FileRecoveryIndex struct {
	Stage0 FileRecoveryIndexEntry `json:"stage0"`
	Stage1 FileRecoveryIndexEntry `json:"stage1"`
	Stage2 FileRecoveryIndexEntry `json:"stage2"`
	Stage3 FileRecoveryIndexEntry `json:"stage3"`
}

func (index FileRecoveryIndex) entries() [4]FileRecoveryIndexEntry {
	return [4]FileRecoveryIndexEntry{index.Stage0, index.Stage1, index.Stage2, index.Stage3}
}

func fileRecoveryIndexFromEntries(entries [4]FileRecoveryIndexEntry) FileRecoveryIndex {
	return FileRecoveryIndex{Stage0: entries[0], Stage1: entries[1], Stage2: entries[2], Stage3: entries[3]}
}

// FileRecoveryState is the complete CAS unit for one path, not a repository-wide
// status digest. Other paths may change before, during, or after recovery.
type FileRecoveryState struct {
	Worktree FileRecoveryWorktree `json:"worktree"`
	Index    FileRecoveryIndex    `json:"index"`
}

// FileRecoveryIdentity pins an operation to both its repository and worktree.
// Digest values also include the local/SSH workspace identity used by TanGit.
type FileRecoveryIdentity struct {
	RepositoryGitDir string `json:"repository_git_dir"`
	WorktreeGitDir   string `json:"worktree_git_dir"`
	Worktree         string `json:"worktree"`
	RepositoryDigest string `json:"repository_digest"`
	WorktreeDigest   string `json:"worktree_digest"`
}

// FileRecoveryJournalEntry is one atomically published progress transition.
// A manifest can therefore distinguish a completed file write from a completed
// index update after the process restarts.
type FileRecoveryJournalEntry struct {
	Time    int64  `json:"time"`
	Attempt int    `json:"attempt"`
	Event   string `json:"event"`
	Detail  string `json:"detail,omitempty"`
}

// FileRecoveryRecord is the durable V2 manifest. Initial and After carry only
// metadata and digests on disk; bytes live in content-addressed payload files.
type FileRecoveryRecord struct {
	Version     int                        `json:"version"`
	OperationID string                     `json:"operation_id"`
	Path        string                     `json:"path"`
	PathDigest  string                     `json:"path_digest"`
	Identity    FileRecoveryIdentity       `json:"identity"`
	Status      string                     `json:"status"`
	Progress    string                     `json:"progress"`
	Attempts    int                        `json:"attempts"`
	CreatedAt   int64                      `json:"created_at"`
	UpdatedAt   int64                      `json:"updated_at"`
	Initial     FileRecoveryState          `json:"initial"`
	After       FileRecoveryState          `json:"after"`
	Journal     []FileRecoveryJournalEntry `json:"journal"`
	Error       string                     `json:"error,omitempty"`
}

func (s *gitSession) fileRecoveryRoot() string {
	return s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "tangit", "file-recovery", "v2")
}

func (s *gitSession) fileRecoveryManifestPath(id string) (string, error) {
	if !validOperationID(id) {
		return "", fmt.Errorf("invalid file recovery operation id")
	}
	return s.joinPath(s.fileRecoveryRoot(), "manifest", id+".json"), nil
}

func fileRecoveryValidDigest(digest string) bool {
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func fileRecoveryDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func (s *gitSession) fileRecoveryPayloadPath(digest string) (string, error) {
	if !fileRecoveryValidDigest(digest) {
		return "", fmt.Errorf("invalid file recovery payload digest")
	}
	// Only the validated digest becomes a filename. Neither the Git path nor a
	// manifest-provided filename can traverse out of the V2 payload directory.
	return s.joinPath(s.fileRecoveryRoot(), "payload", digest+".bin"), nil
}

func fileRecoveryOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	if strings.ToLower(oid) != oid {
		return false
	}
	_, err := hex.DecodeString(oid)
	return err == nil
}

func validateFileRecoveryPath(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("invalid file recovery path")
	}
	// Git paths are slash-separated. Reject backslash rather than assigning it
	// different identities on Unix and Windows.
	if strings.Contains(name, `\`) || path.IsAbs(name) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return fmt.Errorf("file recovery path must be a portable repository-relative path")
	}
	if path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("file recovery path is not canonical and traversal-free")
	}
	return nil
}

func (s *gitSession) cleanFileRecoveryPath(name string) string {
	if s.sshClient != nil {
		return path.Clean(name)
	}
	return filepath.Clean(name)
}

func (s *gitSession) fileRecoveryLocationIdentity(name string) string {
	name = s.cleanFileRecoveryPath(name)
	if s.sshClient != nil {
		return "ssh\x00" + s.location.ProfileID + "\x00" + name
	}
	return "local\x00" + name
}

func (s *gitSession) fileRecoveryIdentity() FileRecoveryIdentity {
	// Repository identity is shared by linked worktrees through the common git
	// dir; worktree identity additionally includes this specific checkout.
	repository := s.fileRecoveryLocationIdentity(s.git.RepoPaths.RepoGitDirPath())
	worktree := repository + "\x00" + s.fileRecoveryLocationIdentity(s.git.RepoPaths.WorktreePath())
	return FileRecoveryIdentity{
		RepositoryGitDir: s.cleanFileRecoveryPath(s.git.RepoPaths.RepoGitDirPath()),
		WorktreeGitDir:   s.cleanFileRecoveryPath(s.git.RepoPaths.WorktreeGitDirPath()),
		Worktree:         s.cleanFileRecoveryPath(s.git.RepoPaths.WorktreePath()),
		RepositoryDigest: fileRecoveryDigest([]byte(repository)),
		WorktreeDigest:   fileRecoveryDigest([]byte(worktree)),
	}
}

func (s *gitSession) validateFileRecoveryIdentity(identity FileRecoveryIdentity) error {
	if identity != s.fileRecoveryIdentity() {
		return fmt.Errorf("file recovery belongs to a different repository or worktree")
	}
	return nil
}

func fileRecoveryPresentMode(mode string) error {
	switch mode {
	case "160000":
		return fmt.Errorf("%w: gitlink/submodule entries", ErrFileRecoveryUnsupported)
	case "120000":
		return fmt.Errorf("%w: symbolic links", ErrFileRecoveryUnsupported)
	case "100644", "100755":
		return nil
	default:
		return fmt.Errorf("unsupported Git file mode %q", mode)
	}
}

func clearFileRecoveryWorktreeMetadata(worktree *FileRecoveryWorktree) {
	worktree.Size = 0
	worktree.Digest = ""
}

func prepareFileRecoveryWorktree(worktree FileRecoveryWorktree) (FileRecoveryWorktree, error) {
	clearFileRecoveryWorktreeMetadata(&worktree)
	if !worktree.Present {
		worktree.Mode = ""
		worktree.Content = nil
		return worktree, nil
	}
	if err := fileRecoveryPresentMode(worktree.Mode); err != nil {
		return FileRecoveryWorktree{}, err
	}
	worktree.Size = int64(len(worktree.Content))
	worktree.Digest = fileRecoveryDigest(worktree.Content)
	return worktree, nil
}

func validateFileRecoveryWorktree(worktree FileRecoveryWorktree) error {
	if !worktree.Present {
		if worktree.Mode != "" || worktree.Digest != "" || worktree.Size != 0 {
			return fmt.Errorf("absent worktree entry has present-file metadata")
		}
		return nil
	}
	if err := fileRecoveryPresentMode(worktree.Mode); err != nil {
		return err
	}
	if worktree.Size < 0 || !fileRecoveryValidDigest(worktree.Digest) {
		return fmt.Errorf("worktree payload metadata is malformed")
	}
	return nil
}

func (s *gitSession) prepareFileRecoveryIndex(index FileRecoveryIndex) (FileRecoveryIndex, error) {
	entries := index.entries()
	for stage := range entries {
		entry := &entries[stage]
		entry.Stage = stage
		entry.Size = 0
		entry.Digest = ""
		if !entry.Present {
			entry.Mode, entry.OID, entry.Content = "", "", nil
			continue
		}
		entry.OID = strings.ToLower(entry.OID)
		if err := fileRecoveryPresentMode(entry.Mode); err != nil {
			return FileRecoveryIndex{}, err
		}
		if !fileRecoveryOID(entry.OID) {
			return FileRecoveryIndex{}, fmt.Errorf("invalid stage %d object id", stage)
		}
		content, err := s.readFileRecoveryBlob(entry.OID)
		if err != nil {
			return FileRecoveryIndex{}, fmt.Errorf("read stage %d blob: %w", stage, err)
		}
		if entry.Content != nil && !bytes.Equal(entry.Content, content) {
			return FileRecoveryIndex{}, fmt.Errorf("stage %d content does not match its object id", stage)
		}
		entry.Content = content
		entry.Size = int64(len(content))
		entry.Digest = fileRecoveryDigest(content)
	}
	if entries[0].Present && (entries[1].Present || entries[2].Present || entries[3].Present) {
		return FileRecoveryIndex{}, fmt.Errorf("stage 0 cannot coexist with unmerged stages")
	}
	return fileRecoveryIndexFromEntries(entries), nil
}

func validateFileRecoveryIndex(index FileRecoveryIndex) error {
	entries := index.entries()
	for stage, entry := range entries {
		if entry.Stage != stage {
			return fmt.Errorf("index slot %d has stage %d", stage, entry.Stage)
		}
		if !entry.Present {
			if entry.Mode != "" || entry.OID != "" || entry.Digest != "" || entry.Size != 0 {
				return fmt.Errorf("absent stage %d has present-entry metadata", stage)
			}
			continue
		}
		if err := fileRecoveryPresentMode(entry.Mode); err != nil {
			return err
		}
		if !fileRecoveryOID(entry.OID) || entry.Size < 0 || !fileRecoveryValidDigest(entry.Digest) {
			return fmt.Errorf("stage %d payload metadata is malformed", stage)
		}
	}
	if entries[0].Present && (entries[1].Present || entries[2].Present || entries[3].Present) {
		return fmt.Errorf("stage 0 cannot coexist with unmerged stages")
	}
	return nil
}

func (s *gitSession) writeFileRecoveryAtomic(name string, data []byte, mode os.FileMode) error {
	if err := s.fileService().WriteFileAtomic(name, data, mode); err != nil {
		return err
	}
	if s.sshClient != nil {
		if err := s.syncRemotePath(name); err != nil {
			return err
		}
		return s.syncRemotePath(path.Dir(name))
	}
	return s.syncDirectory(filepath.Dir(name))
}

func (s *gitSession) writeFileRecoveryPayload(digest string, content []byte) error {
	if !fileRecoveryValidDigest(digest) || digest != fileRecoveryDigest(content) {
		return fmt.Errorf("file recovery payload digest does not match its content")
	}
	name, err := s.fileRecoveryPayloadPath(digest)
	if err != nil {
		return err
	}
	return s.writeFileRecoveryAtomic(name, content, 0o600)
}

func (s *gitSession) writeFileRecoveryStatePayloads(state FileRecoveryState) error {
	if state.Worktree.Present {
		if err := s.writeFileRecoveryPayload(state.Worktree.Digest, state.Worktree.Content); err != nil {
			return err
		}
	}
	for _, entry := range state.Index.entries() {
		if entry.Present {
			if err := s.writeFileRecoveryPayload(entry.Digest, entry.Content); err != nil {
				return err
			}
		}
	}
	return nil
}

func stripFileRecoveryStateContent(state *FileRecoveryState) {
	state.Worktree.Content = nil
	entries := state.Index.entries()
	for i := range entries {
		entries[i].Content = nil
	}
	state.Index = fileRecoveryIndexFromEntries(entries)
}

func stripFileRecoveryRecordContent(record *FileRecoveryRecord) {
	stripFileRecoveryStateContent(&record.Initial)
	stripFileRecoveryStateContent(&record.After)
}

func appendFileRecoveryJournal(record *FileRecoveryRecord, event, detail string) {
	now := time.Now()
	record.UpdatedAt = now.Unix()
	record.Journal = append(record.Journal, FileRecoveryJournalEntry{
		Time: now.UnixNano(), Attempt: record.Attempts, Event: event, Detail: RedactSecrets(detail),
	})
}

func (s *gitSession) writeFileRecoveryRecord(record FileRecoveryRecord) error {
	stripFileRecoveryRecordContent(&record)
	name, err := s.fileRecoveryManifestPath(record.OperationID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode file recovery manifest: %w", err)
	}
	if err := s.writeFileRecoveryAtomic(name, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write file recovery manifest: %w", err)
	}
	return nil
}

func validateFileRecoveryState(state FileRecoveryState) error {
	if err := validateFileRecoveryWorktree(state.Worktree); err != nil {
		return err
	}
	return validateFileRecoveryIndex(state.Index)
}

func validateFileRecoveryRecord(record FileRecoveryRecord, id string) error {
	if record.Version != FileRecoveryManifestVersion || record.OperationID != id || !validOperationID(id) {
		return fmt.Errorf("unsupported or mismatched file recovery manifest")
	}
	if err := validateFileRecoveryPath(record.Path); err != nil {
		return err
	}
	if record.PathDigest != fileRecoveryDigest([]byte(record.Path)) || !fileRecoveryValidDigest(record.PathDigest) {
		return fmt.Errorf("file recovery path identity digest is invalid")
	}
	if err := validateFileRecoveryState(record.Initial); err != nil {
		return fmt.Errorf("invalid initial file recovery state: %w", err)
	}
	if err := validateFileRecoveryState(record.After); err != nil {
		return fmt.Errorf("invalid file recovery after-state: %w", err)
	}
	switch record.Progress {
	case FileRecoveryPending:
	case FileRecoveryWorktreeRestored:
		if record.Status == FileRecoveryReady || record.Status == FileRecoveryRestored {
			return fmt.Errorf("invalid file recovery status/progress combination")
		}
	case FileRecoveryIndexRestored:
		if record.Status == FileRecoveryReady {
			return fmt.Errorf("invalid file recovery status/progress combination")
		}
	default:
		return fmt.Errorf("unknown file recovery progress %q", record.Progress)
	}
	switch record.Status {
	case FileRecoveryReady, FileRecoveryRestoring, FileRecoveryInterrupted:
		if record.Progress == FileRecoveryIndexRestored && record.Status == FileRecoveryReady {
			return fmt.Errorf("invalid ready file recovery progress")
		}
	case FileRecoveryRestored:
		if record.Progress != FileRecoveryIndexRestored {
			return fmt.Errorf("restored file recovery has incomplete progress")
		}
	default:
		return fmt.Errorf("unknown file recovery status %q", record.Status)
	}
	return nil
}

func (s *gitSession) readFileRecoveryRecord(id string, identify bool) (FileRecoveryRecord, error) {
	name, err := s.fileRecoveryManifestPath(id)
	if err != nil {
		return FileRecoveryRecord{}, err
	}
	data, err := s.fileService().ReadFile(name)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("read file recovery manifest: %w", err)
	}
	var record FileRecoveryRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	// V2 is an exact, versioned schema at every nesting level. Unknown fields
	// fail closed rather than being silently dropped into a restorable record;
	// additive changes require deliberate compatibility handling and tests.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("decode file recovery manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return FileRecoveryRecord{}, fmt.Errorf("decode file recovery manifest: trailing data")
	}
	if err := validateFileRecoveryRecord(record, id); err != nil {
		return FileRecoveryRecord{}, err
	}
	if identify {
		if err := s.validateFileRecoveryIdentity(record.Identity); err != nil {
			return FileRecoveryRecord{}, err
		}
	}
	if record.Status == FileRecoveryRestoring {
		// Reading never repairs or advances a live-looking record. A later explicit
		// Restore call revalidates both components before resuming the saved step.
		record.Status = FileRecoveryInterrupted
		if record.Error == "" {
			record.Error = "Restore was interrupted; an explicit retry is required."
		}
	}
	return record, nil
}

// ReadFileRecovery returns durable metadata without payload bytes. An in-flight
// manifest left by a dead process is reported as interrupted, never auto-retried.
func (s *gitSession) ReadFileRecovery(ctx context.Context, id string) (FileRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	return s.readFileRecoveryRecord(id, true)
}

// ArchiveFileRecovery captures the current initial worktree/index state and the
// caller-provided exact after-state. Payloads are published before the manifest;
// a failed archive may leave unreachable digest files but no restorable record.
func (s *gitSession) ArchiveFileRecovery(ctx context.Context, archive FileRecoveryArchive) (FileRecoveryRecord, error) {
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if err := validateFileRecoveryPath(archive.Path); err != nil {
		return FileRecoveryRecord{}, err
	}
	initial, err := s.capturePreparedFileRecoveryState(archive.Path)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("capture initial file recovery state: %w", err)
	}
	return s.archivePreparedFileRecovery(ctx, archive.Path, initial, archive.After)
}

// archivePreparedFileRecovery persists an exact already-captured initial state.
// The caller holds lockWorktreeMutation, so CAS validation, recovery publication
// and a subsequent approved file mutation all use the same repository view.
func (s *gitSession) archivePreparedFileRecovery(ctx context.Context, path string, initial FileRecoveryState, proposed FileRecoveryState) (FileRecoveryRecord, error) {
	after := proposed
	var err error
	after.Worktree, err = prepareFileRecoveryWorktree(after.Worktree)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("invalid after-state worktree: %w", err)
	}
	after.Index, err = s.prepareFileRecoveryIndex(after.Index)
	if err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("invalid after-state index: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return FileRecoveryRecord{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("create file recovery operation id: %w", err)
	}
	now := time.Now()
	record := FileRecoveryRecord{
		Version: FileRecoveryManifestVersion, OperationID: hex.EncodeToString(random[:]),
		Path: path, PathDigest: fileRecoveryDigest([]byte(path)),
		Identity: s.fileRecoveryIdentity(), Status: FileRecoveryReady, Progress: FileRecoveryPending,
		CreatedAt: now.Unix(), UpdatedAt: now.Unix(), Initial: initial, After: after,
	}
	appendFileRecoveryJournal(&record, fileRecoveryEventArchived, "")
	if err := s.writeFileRecoveryStatePayloads(record.Initial); err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("write initial file recovery payload: %w", err)
	}
	if err := s.writeFileRecoveryStatePayloads(record.After); err != nil {
		return FileRecoveryRecord{}, fmt.Errorf("write after-state payload: %w", err)
	}
	if err := s.writeFileRecoveryRecord(record); err != nil {
		return FileRecoveryRecord{}, err
	}
	stripFileRecoveryRecordContent(&record)
	return record, nil
}

func fileRecoveryWorktreeEqual(left, right FileRecoveryWorktree) bool {
	return left.Present == right.Present && left.Mode == right.Mode && left.Size == right.Size && left.Digest == right.Digest
}

func fileRecoveryIndexEqual(left, right FileRecoveryIndex) bool {
	leftEntries, rightEntries := left.entries(), right.entries()
	for i := range leftEntries {
		l, r := leftEntries[i], rightEntries[i]
		if l.Stage != r.Stage || l.Present != r.Present || l.Mode != r.Mode || l.OID != r.OID || l.Size != r.Size || l.Digest != r.Digest {
			return false
		}
	}
	return true
}

func fileRecoveryStateEqual(left, right FileRecoveryState) bool {
	return fileRecoveryWorktreeEqual(left.Worktree, right.Worktree) && fileRecoveryIndexEqual(left.Index, right.Index)
}
