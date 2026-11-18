package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// workspaceArchiveScope is immutable capture policy, not a guess based on the
// current ignore rules at restore time. Ordinary snapshots include ignored
// files; hard reset restores only its tracked/ignored collision paths.
type workspaceArchiveScope struct {
	Version        int      `json:"version"`
	AllIgnored     bool     `json:"all_ignored"`
	IgnoredPaths   []string `json:"ignored_paths,omitempty"`
	GitStateGuard  string   `json:"git_state_guard"`
	WorkspaceGuard string   `json:"workspace_guard"`
}

func (s *gitSession) writeWorkspaceScope(root *workspaceTreeNode, scope workspaceArchiveScope) (workspaceArchiveScope, error) {
	guard, err := s.workspaceGitStateDigest()
	if err != nil {
		return scope, err
	}
	scope.Version = 1
	scope.GitStateGuard = guard
	scope.WorkspaceGuard = ""
	scope.IgnoredPaths = append([]string(nil), scope.IgnoredPaths...)
	sort.Strings(scope.IgnoredPaths)
	policy, err := json.Marshal(scope)
	if err != nil {
		return scope, err
	}
	contentTree, err := s.writeWorkspaceTree(root)
	if err != nil {
		return scope, err
	}
	protected := sha256.Sum256([]byte(contentTree + "\x00" + string(policy)))
	scope.WorkspaceGuard = fmt.Sprintf("%x", protected)
	payload, err := json.Marshal(scope)
	if err != nil {
		return scope, err
	}
	oid, err := workspaceMetadataBlob(s, payload)
	if err != nil {
		return scope, err
	}
	root.insert("scope.json", workspaceTreeEntry{mode: "100644", oid: oid})
	return scope, nil
}

func decodeWorkspaceScope(payload []byte) (workspaceArchiveScope, error) {
	var scope workspaceArchiveScope
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scope); err != nil {
		return scope, fmt.Errorf("workspace capture policy is invalid: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return scope, fmt.Errorf("workspace capture policy contains trailing data")
	}
	if scope.Version != 1 || len(scope.GitStateGuard) != sha256.Size*2 || len(scope.WorkspaceGuard) != sha256.Size*2 {
		return scope, fmt.Errorf("workspace capture policy or protection version is unsupported")
	}
	for _, digest := range []string{scope.GitStateGuard, scope.WorkspaceGuard} {
		if _, err := hex.DecodeString(digest); err != nil {
			return scope, fmt.Errorf("workspace protection fingerprint is invalid")
		}
	}
	for _, name := range scope.IgnoredPaths {
		if err := validateFileRecoveryPath(name); err != nil {
			return scope, fmt.Errorf("workspace ignored-path policy is invalid: %w", err)
		}
	}
	return scope, nil
}

func (s *gitSession) currentWorkspaceArchive(scope workspaceArchiveScope) (*workspaceArchive, error) {
	ignored := append([]string(nil), scope.IgnoredPaths...)
	if scope.AllIgnored {
		var err error
		ignored, err = s.ignoredWorkspacePaths()
		if err != nil {
			return nil, err
		}
	}
	scope.IgnoredPaths = ignored
	listing, err := s.recoveryUntrackedListing(ignored)
	if err != nil {
		return nil, err
	}
	return s.captureWorkspaceArchive(listing, scope)
}

func (s *gitSession) currentWorkspaceScope(scope workspaceArchiveScope) (workspaceArchiveScope, error) {
	archive, err := s.currentWorkspaceArchive(scope)
	if err != nil {
		return workspaceArchiveScope{}, err
	}
	return archive.Scope, nil
}

// checkWorkspaceIndexLock refuses to capture or mutate through an existing
// Git index lock. The lock belongs to another writer and is never removed or
// replaced by TanGit's recovery machinery.
func (s *gitSession) checkWorkspaceIndexLock() error {
	index, err := s.cmd.New([]string{"git", "rev-parse", "--path-format=absolute", "--git-path", "index"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("resolve Git index for recovery: %w: %s", err, strings.TrimSpace(index))
	}
	name := strings.TrimSuffix(index, "\n")
	if name == "" {
		return fmt.Errorf("resolve Git index for recovery: empty index path")
	}
	lock := name + ".lock"
	if _, err := s.fileService().Lstat(lock); err == nil {
		return fmt.Errorf("cannot capture or modify the workspace while Git's %s is held by another process", lock)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect Git index lock %s: %w", lock, err)
	}
	return nil
}

// workspaceGitStateDigest protects everything the metadata restore can alter:
// semantic index stages, whitelisted operation files and their modes, and
// operation-local expected-old-value refs. It deliberately excludes raw
// worktree bytes, which patchRepositoryState hashes separately.
func (s *gitSession) workspaceGitStateDigest() (string, error) {
	_, index, err := s.captureWorkspaceIndex()
	if err != nil {
		return "", err
	}
	manifest, files, err := s.collectRepositoryOperationState()
	if err != nil {
		return "", err
	}
	refs, err := s.currentWorkspaceRefs()
	if err != nil {
		return "", err
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "tangit-repository-v1\x00index\x00%d\x00%s", len(index), index)
	fmt.Fprintf(hash, "head\x00%s\x00branch\x00%s\x00", head, branch)
	for _, entry := range manifest.Directories {
		fmt.Fprintf(hash, "directory\x00%s\x00%d\x00", entry.Path, entry.Mode)
	}
	for _, entry := range manifest.FileModes {
		fmt.Fprintf(hash, "mode\x00%s\x00%d\x00", entry.Path, entry.Mode)
	}
	for _, name := range sortedWorkspacePayloadPaths(files) {
		content := files[name]
		fmt.Fprintf(hash, "state\x00%s\x00%d\x00", name, len(content))
		hash.Write(content)
		hash.Write([]byte{0})
	}
	var names []string
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(hash, "ref\x00%s\x00%s\x00", name, refs[name])
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
