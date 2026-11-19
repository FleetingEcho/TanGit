package repository

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

type workspaceRepositoryArchive struct {
	Manifest workspaceStateManifest
	Scope    workspaceArchiveScope
	Files    map[string][]byte
	Refs     map[string]string
	Index    []workspaceIndexEntry
}

func readWorkspaceRepositoryArchive(s *gitSession, snapshotHash string) (*workspaceRepositoryArchive, bool, error) {
	metadata, present, err := workspaceMetadataLocation(s, snapshotHash)
	if err != nil || !present {
		return nil, present, err
	}
	listing, err := s.cmd.New([]string{"git", "ls-tree", "-r", "-z", metadata, "--", "repository.json", "scope.json", "state", "refs", "index/stages"}).RunWithOutput()
	if err != nil {
		return nil, true, fmt.Errorf("read workspace operation archive: %w", err)
	}
	objects, err := decodeWorkspaceTree(listing)
	if err != nil {
		return nil, true, err
	}
	result := &workspaceRepositoryArchive{Files: make(map[string][]byte), Refs: make(map[string]string), Manifest: workspaceStateManifest{}}
	var manifest, scope []byte
	for _, object := range objects {
		if object.Kind != "blob" && !strings.HasPrefix(object.Path, "refs/") || object.Mode != "160000" && strings.HasPrefix(object.Path, "refs/") {
			return nil, true, fmt.Errorf("workspace operation archive contains an unexpected object")
		}
		payload, err := s.readFileRecoveryBlob(object.OID)
		if err != nil && object.Kind == "blob" {
			return nil, true, fmt.Errorf("read workspace operation archive %q: %w", object.Path, err)
		}
		switch {
		case object.Path == "repository.json":
			if object.Mode != "100644" || manifest != nil {
				return nil, true, fmt.Errorf("workspace operation manifest is missing or duplicated")
			}
			manifest = payload
		case object.Path == "scope.json":
			if object.Mode != "100644" || scope != nil {
				return nil, true, fmt.Errorf("workspace capture policy is missing or duplicated")
			}
			scope = payload
		case object.Path == "index/stages":
			if object.Mode != "100644" {
				return nil, true, fmt.Errorf("workspace operation index has an invalid object mode")
			}
			result.Index, err = s.parseWorkspaceIndex(string(payload))
			if err != nil {
				return nil, true, err
			}
		case strings.HasPrefix(object.Path, "state/"):
			name := strings.TrimPrefix(object.Path, "state/")
			if (object.Mode != "100644" && object.Mode != "100755") || !repositoryStateFile(name) && !strings.Contains(name, "/") || !validWorkspaceRepositoryPath(name) {
				return nil, true, fmt.Errorf("workspace operation archive path %q is invalid", name)
			}
			result.Files[name] = payload
		case strings.HasPrefix(object.Path, "refs/"):
			refName := strings.TrimPrefix(object.Path, "refs/")
			if refName != "refs/stash" && !strings.HasPrefix(refName, "refs/bisect/") && !strings.HasPrefix(refName, "refs/rewritten/") && !strings.HasPrefix(refName, "refs/heads/") || object.Kind != "commit" {
				return nil, true, fmt.Errorf("workspace operation archive ref %q is invalid", refName)
			}
			if err := s.cmd.New([]string{"git", "check-ref-format", refName}).Run(); err != nil {
				return nil, true, fmt.Errorf("workspace operation archive ref %q is invalid", refName)
			}
			kind, err := s.gitText("cat-file", "-t", object.OID)
			if err != nil || strings.TrimSpace(kind) != "commit" {
				return nil, true, fmt.Errorf("workspace operation ref target is unavailable")
			}
			result.Refs[refName] = object.OID
		default:
			return nil, true, fmt.Errorf("workspace operation archive contains unexpected path %q", object.Path)
		}
	}
	if manifest == nil || scope == nil {
		return nil, true, fmt.Errorf("workspace operation manifest or capture policy is missing")
	}
	result.Scope, err = decodeWorkspaceScope(scope)
	if err != nil {
		return nil, true, err
	}
	decoder := json.NewDecoder(bytes.NewReader(manifest))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.Manifest); err != nil {
		return nil, true, fmt.Errorf("workspace operation manifest is invalid: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, true, fmt.Errorf("workspace operation manifest contains trailing data")
	}
	if result.Manifest.Version != 1 {
		return nil, true, fmt.Errorf("workspace operation manifest uses an unsupported version")
	}
	seen := make(map[string]bool)
	for _, group := range [][]workspaceStatePathMode{result.Manifest.Directories, result.Manifest.FileModes} {
		for _, entry := range group {
			if !validWorkspaceRepositoryPath(entry.Path) || entry.Mode&^uint32(os.ModePerm) != 0 || seen[entry.Path] {
				return nil, true, fmt.Errorf("workspace operation metadata path %q is invalid", entry.Path)
			}
			seen[entry.Path] = true
		}
	}
	for name := range result.Files {
		if !seen[name] {
			return nil, true, fmt.Errorf("workspace operation file %q has no recorded permissions", name)
		}
	}
	// Ref paths are represented as Git objects, not separately edited loose
	// files. Reject a mixed representation instead of overriding ref locks.
	for name := range result.Files {
		if strings.HasPrefix(name, "refs/") {
			return nil, true, fmt.Errorf("workspace operation archive mixes raw and Git-managed refs")
		}
	}
	return result, true, nil
}

func validWorkspaceRepositoryPath(name string) bool {
	return name != "" && path.Clean(name) == name && !path.IsAbs(name) && name != ".." && !strings.HasPrefix(name, "../") && repositoryStatePath(name)
}

func (s *gitSession) restoreWorkspaceRepositoryArchive(snapshotHash string) error {
	saved, present, err := readWorkspaceRepositoryArchive(s, snapshotHash)
	if err != nil || !present {
		return err
	}
	current, _, err := s.collectRepositoryOperationState()
	if err != nil {
		return err
	}
	savedFiles := make(map[string]workspaceStatePathMode, len(saved.Manifest.FileModes))
	for _, entry := range saved.Manifest.FileModes {
		savedFiles[entry.Path] = entry
	}
	savedDirs := make(map[string]workspaceStatePathMode, len(saved.Manifest.Directories))
	for _, entry := range saved.Manifest.Directories {
		savedDirs[entry.Path] = entry
	}
	gitDir := s.git.RepoPaths.WorktreeGitDirPath()
	for _, entry := range current.FileModes {
		if strings.HasPrefix(entry.Path, "refs/") {
			continue
		}
		if _, keep := savedFiles[entry.Path]; keep {
			continue
		}
		if err := s.fileService().Remove(s.joinPath(gitDir, entry.Path)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove replaced operation metadata %q: %w", entry.Path, err)
		}
	}
	var removeDirs []string
	for _, entry := range current.Directories {
		if _, keep := savedDirs[entry.Path]; !keep && !strings.HasPrefix(entry.Path, "refs/") {
			removeDirs = append(removeDirs, entry.Path)
		}
	}
	sort.Slice(removeDirs, func(i, j int) bool { return len(removeDirs[i]) > len(removeDirs[j]) })
	for _, name := range removeDirs {
		if err := s.fileService().RemoveAll(s.joinPath(gitDir, name)); err != nil {
			return fmt.Errorf("remove replaced operation directory %q: %w", name, err)
		}
	}
	var dirs []workspaceStatePathMode
	for _, entry := range saved.Manifest.Directories {
		if !strings.HasPrefix(entry.Path, "refs/") {
			dirs = append(dirs, entry)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Path < dirs[j].Path })
	for _, entry := range dirs {
		if err := s.fileService().MkdirAll(s.joinPath(gitDir, entry.Path), os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("restore operation directory %q: %w", entry.Path, err)
		}
	}
	for _, entry := range saved.Manifest.FileModes {
		if strings.HasPrefix(entry.Path, "refs/") {
			continue
		}
		if err := s.fileService().WriteFileAtomic(s.joinPath(gitDir, entry.Path), saved.Files[entry.Path], os.FileMode(entry.Mode)); err != nil {
			return fmt.Errorf("restore operation metadata %q: %w", entry.Path, err)
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := s.fileService().Chmod(s.joinPath(gitDir, dirs[i].Path), os.FileMode(dirs[i].Mode)); err != nil {
			return fmt.Errorf("restore operation directory permissions %q: %w", dirs[i].Path, err)
		}
	}
	if err := s.updateWorkspaceRefs(saved.Refs); err != nil {
		return err
	}
	if err := s.restoreWorkspaceIndex(saved.Index); err != nil {
		return err
	}
	return nil
}

func workspacePathsFromNul(input string) ([]string, error) {
	if input == "" {
		return nil, nil
	}
	var paths []string
	for _, name := range strings.Split(strings.TrimSuffix(input, "\x00"), "\x00") {
		if err := snapshotRepositoryPath(name); err != nil {
			return nil, err
		}
		paths = append(paths, name)
	}
	return paths, nil
}

func (s *gitSession) currentWorkspaceRefs() (map[string]string, error) {
	output, err := s.cmd.New([]string{"git", "for-each-ref", "--format=%(refname)%00%(objectname)%00%(objecttype)%00%(symref)", "refs/stash", "refs/bisect", "refs/rewritten", "refs/heads"}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read operation refs: %w", err)
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 || fields[2] != "commit" || !fileRecoveryOID(fields[1]) || fields[3] != "" {
			return nil, fmt.Errorf("unsupported current operation ref")
		}
		refs[fields[0]] = fields[1]
	}
	return refs, nil
}

func (s *gitSession) updateWorkspaceRefs(saved map[string]string) error {
	current, err := s.currentWorkspaceRefs()
	if err != nil {
		return err
	}
	var input strings.Builder
	input.WriteString("start\noption no-deref\n")
	for ref, oid := range current {
		if _, keep := saved[ref]; !keep {
			fmt.Fprintf(&input, "delete %s %s\n", ref, oid)
		}
	}
	for ref, oid := range saved {
		expected := current[ref]
		if expected == oid {
			continue
		}
		if expected == "" {
			expected = strings.Repeat("0", len(oid))
		}
		fmt.Fprintf(&input, "update %s %s %s\n", ref, oid, expected)
	}
	if input.Len() == len("start\noption no-deref\n") {
		return nil
	}
	input.WriteString("prepare\ncommit\n")
	output, err := s.cmd.New([]string{"git", "update-ref", "--stdin"}).SetStdin(input.String()).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore operation refs without overwriting newer references: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}
