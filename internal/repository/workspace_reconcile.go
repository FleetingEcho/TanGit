package repository

import (
	"fmt"
	"os"
	"strings"
)

// reconcileWorkspacePaths removes files absent from the target's complete raw
// tree, not just files tracked by a patch. The operation journal checks the
// exact current/post state before entering here. This is what removes a file
// created by an operation while leaving unrelated pre-existing files alone.
// Directories and special files are never recursively removed implicitly.
func (s *gitSession) reconcileWorkspacePaths(snapshotHash string) error {
	metadata, present, err := workspaceMetadataLocation(s, snapshotHash)
	if err != nil || !present {
		return err // Legacy snapshots retain their older, narrow behavior.
	}
	saved, present, err := readWorkspaceRepositoryArchive(s, snapshotHash)
	if err != nil || !present {
		return err
	}
	ignored := append([]string(nil), saved.Scope.IgnoredPaths...)
	if saved.Scope.AllIgnored {
		ignored, err = s.ignoredWorkspacePaths()
		if err != nil {
			return err
		}
	}
	untracked, err := s.recoveryUntrackedListing(ignored)
	if err != nil {
		return err
	}
	tracked, err := s.cmd.New([]string{"git", "ls-files", "-z", "--"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("compare workspace paths: %w", err)
	}
	listing, err := s.cmd.New([]string{"git", "ls-tree", "-r", "-z", metadata + ":worktree"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("read target workspace paths: %w", err)
	}
	objects, err := decodeWorkspaceTree(listing)
	if err != nil {
		return err
	}
	target := make(map[string]bool, len(objects))
	for _, object := range objects {
		if err := snapshotRepositoryPath(object.Path); err != nil {
			return err
		}
		target[object.Path] = true
	}
	current := make(map[string]bool)
	for _, name := range strings.Split(strings.TrimSuffix(tracked+untracked, "\x00"), "\x00") {
		if name != "" {
			current[name] = true
		}
	}
	for _, name := range sortedWorkspacePaths(current) {
		if target[name] {
			continue
		}
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), name)
		if err != nil {
			return err
		}
		info, err := s.fileService().Lstat(filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("restore would leave extra directory or special file %q; review it explicitly", name)
		}
		if err := s.fileService().Remove(filename); err != nil {
			return fmt.Errorf("remove extra file %q while restoring workspace: %w", name, err)
		}
	}
	return nil
}
