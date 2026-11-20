package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ContinueSource is an immutable read of the exact operation and checkout a
// Continue preflight reviewed. IndexHash covers every index mode, object ID,
// stage and path. WorkingTreeHash covers actual filesystem bytes and file
// types, including ordinary untracked files; display line counts and lengths
// are never used as identity. The per-path records remain private so callers
// compare sources through CompareContinueSources rather than approximating a
// status check of their own.
type ContinueSource struct {
	Operation       OperationState
	IndexHash       string
	WorkingTreeHash string
	files           []continueFileFingerprint
}

type continueFileFingerprint struct {
	Path        string
	Index       string
	WorkingTree string
}

// ContinueSourceDelta identifies which reviewed inputs changed. ChangedFiles
// is derived from canonical per-path hashes, not from status count or a patch's
// textual length. OperationChanged requires a fresh review; content changes
// require a checkpoint before an otherwise eligible Continue.
type ContinueSourceDelta struct {
	OperationChanged   bool
	IndexChanged       bool
	WorkingTreeChanged bool
	ChangedFiles       []string
}

// ContentChanged reports either canonical checkout component changing.
func (d ContinueSourceDelta) ContentChanged() bool {
	return d.IndexChanged || d.WorkingTreeChanged
}

// CompareContinueSources compares two complete, content-addressed reads.
func CompareContinueSources(before, after ContinueSource) ContinueSourceDelta {
	delta := ContinueSourceDelta{
		OperationChanged: before.Operation.Type != after.Operation.Type ||
			before.Operation.Fingerprint != after.Operation.Fingerprint,
		IndexChanged:       before.IndexHash != after.IndexHash,
		WorkingTreeChanged: before.WorkingTreeHash != after.WorkingTreeHash,
	}
	type fingerprints struct{ index, workingTree string }
	beforeFiles := make(map[string]fingerprints, len(before.files))
	for _, file := range before.files {
		beforeFiles[file.Path] = fingerprints{index: file.Index, workingTree: file.WorkingTree}
	}
	seen := make(map[string]bool, len(before.files)+len(after.files))
	for _, file := range after.files {
		previous := beforeFiles[file.Path]
		if previous.index != file.Index || previous.workingTree != file.WorkingTree {
			delta.ChangedFiles = append(delta.ChangedFiles, file.Path)
		}
		seen[file.Path] = true
	}
	for _, file := range before.files {
		if seen[file.Path] {
			continue
		}
		// A path present only in the old manifest was deleted (or ceased to be
		// an ordinary untracked file), which is just as relevant as an edit.
		delta.ChangedFiles = append(delta.ChangedFiles, file.Path)
	}
	sort.Strings(delta.ChangedFiles)
	return delta
}

// ContinueReviewSession is deliberately an optional extension. The very broad
// Session interface does not grow for one operation-specific workflow, and
// legacy callers cannot accidentally substitute Snapshot line counts for a
// real source capture.
type ContinueReviewSession interface {
	CaptureContinueSource(context.Context) (ContinueSource, error)
}

func continueDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func nulRecords(output string) []string {
	if output == "" {
		return nil
	}
	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	result := records[:0]
	for _, record := range records {
		if record != "" {
			result = append(result, record)
		}
	}
	return result
}

func continuePathDigest(kind string, mode fs.FileMode, content []byte) (string, error) {
	contentDigest := sha256.Sum256(content)
	return continueDigest(struct {
		Kind    string
		Mode    uint32
		Content string
	}{Kind: kind, Mode: uint32(mode), Content: hex.EncodeToString(contentDigest[:])})
}

// fingerprintContinueDirectory handles a gitlink (or a path replaced by a
// directory) without following symlinks or hashing Git's own administrative
// files. It is intentionally content based too: submodule dirtiness labels or
// file sizes would miss a same-length edit just as they would in the parent.
func fingerprintContinueDirectory(root string) (string, error) {
	entries := []string{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == root {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var content []byte
		kind := "other"
		switch {
		case info.Mode().IsRegular():
			kind = "file"
			content, err = os.ReadFile(name)
		case info.Mode()&os.ModeSymlink != 0:
			kind = "symlink"
			var target string
			target, err = os.Readlink(name)
			content = []byte(target)
		case info.IsDir():
			kind = "directory"
		}
		if err != nil {
			return err
		}
		digest, err := continuePathDigest(kind, info.Mode(), content)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(relative)+"\x00"+digest)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	return continueDigest(entries)
}

func fingerprintContinuePath(root, path string) (string, error) {
	name := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return continuePathDigest("missing", 0, nil)
	}
	if err != nil {
		return "", fmt.Errorf("inspect Continue source %s: %w", path, err)
	}
	var content []byte
	kind := "other"
	switch {
	case info.Mode().IsRegular():
		kind = "file"
		content, err = os.ReadFile(name)
	case info.Mode()&os.ModeSymlink != 0:
		kind = "symlink"
		var target string
		target, err = os.Readlink(name)
		content = []byte(target)
	case info.IsDir():
		return fingerprintContinueDirectory(name)
	}
	if err != nil {
		return "", fmt.Errorf("read Continue source %s: %w", path, err)
	}
	digest, err := continuePathDigest(kind, info.Mode(), content)
	if err != nil {
		return "", fmt.Errorf("hash Continue source %s: %w", path, err)
	}
	return digest, nil
}

func (s *gitSession) captureContinueSourceOnce(ctx context.Context) (ContinueSource, error) {
	if err := ctx.Err(); err != nil {
		return ContinueSource{}, err
	}
	operation, err := s.OperationState(ctx)
	if err != nil {
		return ContinueSource{}, err
	}
	indexOutput, err := s.cmd.New([]string{"git", "ls-files", "--stage", "-z", "--"}).RunWithOutput()
	if err != nil {
		return ContinueSource{}, fmt.Errorf("read Continue index: %w: %s", err, strings.TrimSpace(indexOutput))
	}
	records := nulRecords(indexOutput)
	indexRecords := append([]string(nil), records...)
	sort.Strings(indexRecords)
	indexHash, err := continueDigest(indexRecords)
	if err != nil {
		return ContinueSource{}, fmt.Errorf("hash Continue index: %w", err)
	}
	indexByPath := make(map[string][]string)
	for _, record := range records {
		_, path, ok := strings.Cut(record, "\t")
		if !ok || path == "" {
			return ContinueSource{}, fmt.Errorf("parse Continue index record %q", record)
		}
		indexByPath[path] = append(indexByPath[path], record)
	}

	listing, err := s.cmd.New([]string{"git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}).RunWithOutput()
	if err != nil {
		return ContinueSource{}, fmt.Errorf("list Continue working tree: %w: %s", err, strings.TrimSpace(listing))
	}
	pathSet := make(map[string]bool, len(indexByPath))
	for path := range indexByPath {
		pathSet[path] = true
	}
	for _, path := range nulRecords(listing) {
		pathSet[path] = true
	}
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]continueFileFingerprint, 0, len(paths))
	workingTreeRecords := make([]string, 0, len(paths))
	root := s.git.RepoPaths.WorktreePath()
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return ContinueSource{}, err
		}
		pathIndex, err := continueDigest(indexByPath[path])
		if err != nil {
			return ContinueSource{}, err
		}
		workingTree, err := fingerprintContinuePath(root, path)
		if err != nil {
			return ContinueSource{}, err
		}
		files = append(files, continueFileFingerprint{Path: path, Index: pathIndex, WorkingTree: workingTree})
		workingTreeRecords = append(workingTreeRecords, path+"\x00"+workingTree)
	}
	workingTreeHash, err := continueDigest(workingTreeRecords)
	if err != nil {
		return ContinueSource{}, fmt.Errorf("hash Continue working tree: %w", err)
	}
	return ContinueSource{
		Operation: operation, IndexHash: indexHash, WorkingTreeHash: workingTreeHash, files: files,
	}, nil
}

// CaptureContinueSource takes two complete reads and rejects a checkout that
// changed between them. This avoids blessing an index from one instant and
// working-tree bytes from another. It performs no stage, commit or restore.
func (s *gitSession) CaptureContinueSource(ctx context.Context) (ContinueSource, error) {
	if s.sshClient != nil {
		return ContinueSource{}, fmt.Errorf("%w: Continue checks use fully trusted local shell commands and are disabled for SSH workspaces", ErrOperationUnsupported)
	}
	first, err := s.captureContinueSourceOnce(ctx)
	if err != nil {
		return ContinueSource{}, err
	}
	second, err := s.captureContinueSourceOnce(ctx)
	if err != nil {
		return ContinueSource{}, err
	}
	if delta := CompareContinueSources(first, second); delta.OperationChanged || delta.ContentChanged() {
		return ContinueSource{}, fmt.Errorf("%w: operation, index, or working tree changed while capturing the Continue source", ErrOperationChanged)
	}
	return second, nil
}
