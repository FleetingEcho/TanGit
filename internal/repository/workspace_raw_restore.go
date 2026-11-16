package repository

import (
	"context"
	"fmt"
	"os"
)

// restoreWorkspaceRawDelta compares the complete raw target tree with the
// current complete raw tree, not merely committed HEAD differences. This is
// what restores unchanged-at-HEAD bytes after apply/pop/checkout. The caller
// chooses the reviewed target and supplies its approved post-operation point.
func (s *gitSession) restoreWorkspaceRawDelta(ctx context.Context, target WorkSnapshot, expectedAfter *WorkSnapshot) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	metadata, present, err := workspaceMetadataLocation(s, target.Hash)
	if err != nil || !present {
		return false, err // Older snapshots retain their narrow restore path.
	}
	targetArchive, present, err := readWorkspaceRepositoryArchive(s, target.Hash)
	if err != nil || !present {
		return true, err
	}
	if _, err := s.readWorkspacePermissions(target.Hash); err != nil {
		return true, err
	}
	currentArchive, err := s.currentWorkspaceArchive(targetArchive.Scope)
	if err != nil {
		return true, err
	}
	if expectedAfter != nil {
		expected, present, err := storedWorkspaceScope(s, expectedAfter)
		if err != nil || !present {
			return true, fmt.Errorf("reviewed post-operation recovery metadata is incomplete")
		}
		if currentArchive.Scope.GitStateGuard != expected.GitStateGuard || currentArchive.Scope.WorkspaceGuard != expected.WorkspaceGuard {
			return true, fmt.Errorf("workspace changed while preparing recovery; no files were written")
		}
	}
	listing, err := s.cmd.New([]string{"git", "ls-tree", "-r", "-z", metadata + ":worktree"}).RunWithOutput()
	if err != nil {
		return true, fmt.Errorf("read raw target tree: %w", err)
	}
	objects, err := decodeWorkspaceTree(listing)
	if err != nil {
		return true, err
	}
	targetFiles := make(map[string]workspaceArchivedObject, len(objects))
	for _, object := range objects {
		if err := snapshotRepositoryPath(object.Path); err != nil {
			return true, err
		}
		targetFiles[object.Path] = object
	}
	current := make(map[string]workspaceArchivedObject)
	flattenCurrentWorkspaceTree("", currentArchive.Worktree, current)
	type fileRestore struct {
		name    string
		object  workspaceArchivedObject
		payload []byte
	}
	var removals []string
	var restores []fileRestore
	candidates := make(map[string]bool, len(current)+len(targetFiles))
	for name := range current {
		candidates[name] = true
	}
	for name := range targetFiles {
		candidates[name] = true
	}
	for _, name := range sortedWorkspacePaths(candidates) {
		existing := current[name]
		desired, keep := targetFiles[name]
		if keep && desired == existing {
			continue
		}
		if existing.Kind == "commit" || desired.Kind == "commit" {
			return true, fmt.Errorf("restoring changed submodule %q requires an explicit submodule-safe plan", name)
		}
		if !keep {
			removals = append(removals, name)
			continue
		}
		if desired.Kind != "blob" || desired.Mode != "100644" && desired.Mode != "100755" && desired.Mode != "120000" {
			return true, fmt.Errorf("raw target %q has unsupported mode %s/%s", name, desired.Kind, desired.Mode)
		}
		if err := snapshotRepositoryPath(name); err != nil {
			return true, err
		}
		// Read every changed payload before making the first filesystem write.
		// A deleted or corrupt object cannot leave a partially restored tree.
		payload, err := s.readFileRecoveryBlob(desired.OID)
		if err != nil {
			return true, fmt.Errorf("prepare raw restore for %q: %w", name, err)
		}
		restores = append(restores, fileRestore{name: name, object: desired, payload: payload})
	}
	sortedRemovals := sortedWorkspacePathNames(removals)
	for _, name := range sortedRemovals {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), name)
		if err != nil {
			return true, err
		}
		info, err := s.fileService().Lstat(filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return true, err
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return true, fmt.Errorf("extra path %q is a directory or special file; review it instead of deleting it recursively", name)
		}
		if err := s.fileService().Remove(filename); err != nil {
			return true, fmt.Errorf("remove extra path %q during recovery: %w", name, err)
		}
	}
	for _, restore := range restores {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), restore.name)
		if err != nil {
			return true, err
		}
		if restore.object.Mode == "120000" {
			if info, err := s.fileService().Lstat(filename); err == nil {
				if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
					return true, fmt.Errorf("symlink destination %q is not a regular file or symlink", restore.name)
				}
				if err := s.fileService().Remove(filename); err != nil {
					return true, fmt.Errorf("replace symbolic link %q: %w", restore.name, err)
				}
			} else if !os.IsNotExist(err) {
				return true, err
			}
			if err := s.fileService().Symlink(string(restore.payload), filename); err != nil {
				return true, fmt.Errorf("restore symbolic link %q: %w", restore.name, err)
			}
			continue
		}
		if err := s.fileService().WriteFileAtomic(filename, restore.payload, 0o600); err != nil {
			return true, fmt.Errorf("restore raw file %q: %w", restore.name, err)
		}
	}
	return true, nil
}

func flattenCurrentWorkspaceTree(prefix string, node *workspaceTreeNode, files map[string]workspaceArchivedObject) {
	for name, directory := range node.dirs {
		flattenCurrentWorkspaceTree(prefix+name+"/", directory, files)
	}
	for name, entry := range node.entries {
		kind := "blob"
		if entry.mode == "160000" {
			kind = "commit"
		}
		files[prefix+name] = workspaceArchivedObject{Mode: entry.mode, Kind: kind, OID: entry.oid, Path: prefix + name}
	}
}

func sortedWorkspacePathNames(paths []string) []string {
	values := make(map[string]bool, len(paths))
	for _, path := range paths {
		values[path] = true
	}
	return sortedWorkspacePaths(values)
}

// restoreCompleteWorkspaceSnapshot uses raw worktree deltas for new archives.
// Legacy snapshots continue through their stash-aware apply path below.
func (s *gitSession) restoreCompleteWorkspaceSnapshot(ctx context.Context, snapshot WorkSnapshot, expectedAfter *WorkSnapshot) (bool, error) {
	handled, err := s.restoreWorkspaceRawDelta(ctx, snapshot, expectedAfter)
	if err != nil || !handled {
		return handled, err
	}
	if err := s.restoreWorkspaceRepositoryArchive(snapshot.Hash); err != nil {
		return true, err
	}
	return true, s.restoreWorkspacePermissions(snapshot.Hash)
}
