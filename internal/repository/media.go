package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxMediaSize int64 = 10 << 20

// MediaSource identifies the two versions a binary diff compares.
type MediaSource uint8

const (
	MediaWorkingTree MediaSource = iota
	MediaCommit
	MediaStash
)

// MediaRequest names a change without resolving it through mutable app state.
// File is used by MediaWorkingTree, Commit and CommitFile by MediaCommit, and
// StashIndex and CommitFile by MediaStash.
type MediaRequest struct {
	Source     MediaSource
	File       File
	Commit     Commit
	CommitFile CommitFile
	StashIndex int
}

// MediaBlob is one side of an image comparison. Present distinguishes an
// addition/deletion from an unreadable blob. Unavailable explains content that
// exists but cannot be shown, such as an LFS object absent from this machine.
type MediaBlob struct {
	Present     bool
	Data        []byte
	Unavailable string
}

type MediaPair struct {
	Old MediaBlob
	New MediaBlob
}

// Media reads raw blobs only. In particular it never runs textconv, clean or
// smudge filters, git-lfs, or any command that can fetch content from a remote.
func (s *gitSession) Media(ctx context.Context, request MediaRequest) (MediaPair, error) {
	if err := ctx.Err(); err != nil {
		return MediaPair{}, err
	}
	switch request.Source {
	case MediaWorkingTree:
		return s.workingTreeMedia(request.File)
	case MediaCommit:
		return s.commitMedia(request.Commit, request.CommitFile)
	case MediaStash:
		return s.stashMedia(request.StashIndex, request.CommitFile)
	default:
		return MediaPair{}, fmt.Errorf("read media: unknown source")
	}
}

func (s *gitSession) workingTreeMedia(file File) (MediaPair, error) {
	if file.Path == "" {
		return MediaPair{}, fmt.Errorf("read media: empty path")
	}
	oldPath := file.PreviousPath
	if oldPath == "" {
		oldPath = file.Path
	}
	var pair MediaPair
	var err error
	if file.Staged {
		if file.Tracked && !file.Added {
			pair.Old, err = s.gitBlobMedia("HEAD:" + oldPath)
			if err != nil {
				return MediaPair{}, err
			}
		}
		if !file.Deleted {
			pair.New, err = s.gitBlobMedia(":0:" + file.Path)
			if err != nil {
				return MediaPair{}, err
			}
		}
		return pair, nil
	}
	if file.Tracked {
		pair.Old, err = s.gitBlobMedia(":0:" + oldPath)
		if err != nil {
			return MediaPair{}, err
		}
	}
	if file.Deleted {
		return pair, nil
	}
	root, err := s.gitText("rev-parse", "--show-toplevel")
	if err != nil {
		return MediaPair{}, fmt.Errorf("locate working tree for media: %w", err)
	}
	path, err := worktreeFilePath(root, file.Path)
	if err != nil {
		return MediaPair{}, err
	}
	pair.New, err = readLocalMedia(path)
	if err != nil {
		return MediaPair{}, err
	}
	if pair.New.Present && pair.New.Unavailable == "" {
		pair.New, err = s.resolveLocalLFS(pair.New)
	}
	return pair, err
}

func (s *gitSession) commitMedia(commit Commit, file CommitFile) (MediaPair, error) {
	if commit.Hash == "" || file.Path == "" {
		return MediaPair{}, fmt.Errorf("read commit media: missing commit or path")
	}
	oldPath := file.PreviousPath
	if oldPath == "" {
		oldPath = file.Path
	}
	var pair MediaPair
	var err error
	if file.Status != "A" && len(commit.Parents) > 0 {
		pair.Old, err = s.gitBlobMedia(commit.Parents[0] + ":" + oldPath)
		if err != nil {
			return MediaPair{}, err
		}
	}
	if file.Status != "D" {
		pair.New, err = s.gitBlobMedia(commit.Hash + ":" + file.Path)
	}
	return pair, err
}

func (s *gitSession) stashMedia(index int, file CommitFile) (MediaPair, error) {
	if index < 0 || file.Path == "" {
		return MediaPair{}, fmt.Errorf("read stash media: invalid stash or path")
	}
	ref := fmt.Sprintf("refs/stash@{%d}", index)
	oldPath := file.PreviousPath
	if oldPath == "" {
		oldPath = file.Path
	}
	var pair MediaPair
	var err error
	if file.Status != "A" {
		pair.Old, err = s.gitBlobMedia(ref + "^1:" + oldPath)
		if err != nil {
			return MediaPair{}, err
		}
	}
	if file.Status == "D" {
		return pair, nil
	}
	pair.New, err = s.gitBlobMedia(ref + ":" + file.Path)
	if err != nil || pair.New.Present || file.Status != "A" {
		return pair, err
	}
	// --include-untracked stores additions in the stash's third parent.
	pair.New, err = s.gitBlobMedia(ref + "^3:" + file.Path)
	return pair, err
}

// gitBlobMedia reads an object without invoking Git's text conversion. Missing
// parents and paths are ordinary addition/deletion states; damaged revisions
// and other Git failures remain errors instead of being rendered as absence.
func (s *gitSession) gitBlobMedia(spec string) (MediaBlob, error) {
	output, err := s.cmd.New([]string{"git", "cat-file", "-s", spec}).RunWithOutput()
	if err != nil {
		if missingGitMediaObject(output) {
			return MediaBlob{}, nil
		}
		return MediaBlob{}, fmt.Errorf("read media object: %w: %s", err, strings.TrimSpace(output))
	}
	size, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil || size < 0 {
		return MediaBlob{}, fmt.Errorf("read media object size: %q", strings.TrimSpace(output))
	}
	if size > maxMediaSize {
		return MediaBlob{Present: true, Unavailable: "Image is larger than 10 MiB"}, nil
	}
	data, err := s.cmd.New([]string{"git", "cat-file", "blob", spec}).RunWithOutput()
	if err != nil {
		return MediaBlob{}, fmt.Errorf("read media object: %w: %s", err, strings.TrimSpace(data))
	}
	return s.resolveLocalLFS(MediaBlob{Present: true, Data: []byte(data)})
}

func missingGitMediaObject(output string) bool {
	output = strings.ToLower(output)
	for _, text := range []string{
		"not a valid object name", "invalid object name", "does not exist in",
		"exists on disk, but not in", "path not in",
	} {
		if strings.Contains(output, text) {
			return true
		}
	}
	return false
}

func readLocalMedia(path string) (MediaBlob, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return MediaBlob{}, nil
	}
	if err != nil {
		return MediaBlob{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxMediaSize+1))
	if err != nil {
		return MediaBlob{}, err
	}
	if int64(len(data)) > maxMediaSize {
		return MediaBlob{Present: true, Unavailable: "Image is larger than 10 MiB"}, nil
	}
	return MediaBlob{Present: true, Data: data}, nil
}

type lfsPointer struct {
	oid  string
	size int64
}

// parseLFSPointer recognizes only the small, canonical pointer format. An
// ordinary image that happens to contain similar text is left to the decoder.
func parseLFSPointer(data []byte) (lfsPointer, bool) {
	if len(data) == 0 || len(data) > 4096 {
		return lfsPointer{}, false
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) < 3 || lines[0] != "version https://git-lfs.github.com/spec/v1" {
		return lfsPointer{}, false
	}
	var pointer lfsPointer
	for _, line := range lines[1:] {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			return lfsPointer{}, false
		}
		switch key {
		case "oid":
			if !strings.HasPrefix(value, "sha256:") || len(value) != 71 {
				return lfsPointer{}, false
			}
			if _, err := hex.DecodeString(value[7:]); err != nil {
				return lfsPointer{}, false
			}
			pointer.oid = value[7:]
		case "size":
			size, err := strconv.ParseInt(value, 10, 64)
			if err != nil || size < 0 {
				return lfsPointer{}, false
			}
			pointer.size = size
		}
	}
	return pointer, pointer.oid != ""
}

// resolveLocalLFS resolves a pointer strictly through .git/lfs/objects. It
// verifies both size and digest before returning bytes, and never invokes an
// LFS command (whose filters or transfer defaults could access the network).
func (s *gitSession) resolveLocalLFS(blob MediaBlob) (MediaBlob, error) {
	pointer, ok := parseLFSPointer(blob.Data)
	if !ok {
		return blob, nil
	}
	if pointer.size > maxMediaSize {
		return MediaBlob{Present: true, Unavailable: "Image is larger than 10 MiB"}, nil
	}
	relative := filepath.Join("lfs", "objects", pointer.oid[:2], pointer.oid[2:4], pointer.oid)
	path, err := s.gitText("rev-parse", "--git-path", filepath.ToSlash(relative))
	if err != nil {
		return MediaBlob{}, fmt.Errorf("locate local Git LFS object: %w", err)
	}
	if !filepath.IsAbs(path) {
		root, rootErr := s.gitText("rev-parse", "--show-toplevel")
		if rootErr != nil {
			return MediaBlob{}, fmt.Errorf("locate local Git LFS object: %w", rootErr)
		}
		path = filepath.Join(root, path)
	}
	local, err := readLocalMedia(path)
	if os.IsNotExist(err) || !local.Present {
		return MediaBlob{Present: true, Unavailable: "Git LFS object is not available locally"}, nil
	}
	if err != nil {
		return MediaBlob{}, fmt.Errorf("read local Git LFS object: %w", err)
	}
	if local.Unavailable != "" {
		local.Present = true
		return local, nil
	}
	if int64(len(local.Data)) != pointer.size {
		return MediaBlob{Present: true, Unavailable: "Local Git LFS object has the wrong size"}, nil
	}
	digest := sha256.Sum256(local.Data)
	if hex.EncodeToString(digest[:]) != pointer.oid {
		return MediaBlob{Present: true, Unavailable: "Local Git LFS object failed its checksum"}, nil
	}
	return local, nil
}
