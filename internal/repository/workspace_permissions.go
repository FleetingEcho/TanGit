package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	workspacePermissionVersion = 1
	workspacePermissionMarker  = "Tangit-Workspace-Permissions: 1\n"
)

type workspacePermissionManifest struct {
	Version int                        `json:"version"`
	Files   []workspacePermissionEntry `json:"files"`
}

type workspacePermissionEntry struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

// attachWorkspacePermissions keeps POSIX permissions in the immutable
// snapshot commit message. Git trees retain executable/symlink modes, not the
// other permission bits. The snapshot ref reaches both trees and this metadata;
// there is no sidecar that garbage collection could leave behind or expire.
func (s *gitSession) attachWorkspacePermissions(hash, message, untracked string) (string, error) {
	tracked, err := s.cmd.New([]string{"git", "ls-files", "-z", "--"}).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("capture workspace permissions: list tracked files: %w", err)
	}
	paths := make(map[string]struct{})
	for _, name := range strings.Split(strings.TrimSuffix(tracked+untracked, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		if err := validateFileRecoveryPath(name); err != nil {
			return "", fmt.Errorf("capture workspace permissions for %q: %w", name, err)
		}
		paths[name] = struct{}{}
	}
	manifest := workspacePermissionManifest{Version: workspacePermissionVersion}
	files := s.fileService()
	for name := range paths {
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), name)
		if err != nil {
			return "", fmt.Errorf("capture workspace permissions for %q: %w", name, err)
		}
		info, err := files.Lstat(filename)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("capture workspace permissions for %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		manifest.Files = append(manifest.Files, workspacePermissionEntry{Path: name, Mode: uint32(info.Mode().Perm())})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	payload, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("capture workspace permissions: %w", err)
	}
	return s.commitWorkspaceMetadata(hash, message+"\n\n"+workspacePermissionMarker+string(payload))
}

// commitWorkspaceMetadata preserves the exact tree and parent sequence of the
// stash-shaped work snapshot, changing only its message and resulting ID.
func (s *gitSession) commitWorkspaceMetadata(hash, message string) (string, error) {
	object, err := s.gitText("cat-file", "commit", hash)
	if err != nil {
		return "", fmt.Errorf("read workspace snapshot: %w", err)
	}
	header, _, found := strings.Cut(object, "\n\n")
	if !found {
		return "", fmt.Errorf("read workspace snapshot: commit has no message")
	}
	var tree string
	var parents []string
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "tree ") {
			tree = strings.TrimSpace(strings.TrimPrefix(line, "tree "))
			continue
		}
		if strings.HasPrefix(line, "parent ") {
			parents = append(parents, strings.TrimSpace(strings.TrimPrefix(line, "parent ")))
		}
	}
	if tree == "" {
		return "", fmt.Errorf("read workspace snapshot: commit has no tree")
	}
	args := []string{"git", "commit-tree", tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	id, err := s.cmd.New(args).SetStdin(message).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("write workspace permissions: %w: %s", err, strings.TrimSpace(id))
	}
	return strings.TrimSpace(id), nil
}

// readWorkspacePermissions also serves as a fail-closed integrity check before
// journal Undo. Legacy snapshots without the new marker retain their original
// executable/symlink restoration behavior.
func (s *gitSession) readWorkspacePermissions(hash string) ([]workspacePermissionEntry, error) {
	object, err := s.gitText("cat-file", "commit", hash)
	if err != nil {
		return nil, fmt.Errorf("read workspace permissions: %w", err)
	}
	_, message, found := strings.Cut(object, "\n\n")
	if !found {
		return nil, fmt.Errorf("saved workspace snapshot has no message")
	}
	marker := "\n" + workspacePermissionMarker
	start := strings.Index(message, marker)
	if start < 0 {
		if strings.Contains(message, "\nTangit-Workspace-Permissions:") {
			return nil, fmt.Errorf("saved workspace permissions use an unsupported version")
		}
		return nil, nil
	}
	payload := message[start+len(marker):]
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.DisallowUnknownFields()
	var manifest workspacePermissionManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("saved workspace permissions are invalid: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("saved workspace permissions contain trailing data")
	}
	if manifest.Version != workspacePermissionVersion {
		return nil, fmt.Errorf("saved workspace permissions use an unsupported version")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, entry := range manifest.Files {
		if err := validateFileRecoveryPath(entry.Path); err != nil {
			return nil, fmt.Errorf("saved workspace permission path %q is invalid: %w", entry.Path, err)
		}
		if entry.Mode&^uint32(os.ModePerm) != 0 {
			return nil, fmt.Errorf("saved workspace permissions for %q are invalid", entry.Path)
		}
		if _, duplicate := seen[entry.Path]; duplicate {
			return nil, fmt.Errorf("saved workspace permissions contain duplicate path %q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
		if _, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), entry.Path); err != nil {
			return nil, fmt.Errorf("saved workspace permission path %q is invalid: %w", entry.Path, err)
		}
	}
	return manifest.Files, nil
}

func (s *gitSession) restoreWorkspacePermissions(hash string) error {
	entries, err := s.readWorkspacePermissions(hash)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), entry.Path)
		if err != nil {
			return err
		}
		info, err := s.fileService().Lstat(filename)
		if err != nil {
			return fmt.Errorf("restore permissions for %q: %w", entry.Path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("restore permissions for %q: restored path is no longer a regular file", entry.Path)
		}
		if err := s.fileService().Chmod(filename, os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("restore permissions for %q: %w", entry.Path, err)
		}
	}
	return nil
}
