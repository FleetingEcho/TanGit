package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

const workspaceMetadataMarker = "Tangit-Workspace-Metadata: 1\n"

type workspaceTreeEntry struct {
	mode string
	oid  string
}

type workspaceTreeNode struct {
	entries map[string]workspaceTreeEntry
	dirs    map[string]*workspaceTreeNode
}

func newWorkspaceTreeNode() *workspaceTreeNode {
	return &workspaceTreeNode{entries: make(map[string]workspaceTreeEntry), dirs: make(map[string]*workspaceTreeNode)}
}

func (n *workspaceTreeNode) insert(name string, entry workspaceTreeEntry) {
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		if n.dirs[part] == nil {
			n.dirs[part] = newWorkspaceTreeNode()
		}
		n = n.dirs[part]
	}
	n.entries[parts[len(parts)-1]] = entry
}

func (s *gitSession) writeWorkspaceTree(node *workspaceTreeNode) (string, error) {
	names := make([]string, 0, len(node.entries)+len(node.dirs))
	for name := range node.dirs {
		names = append(names, name)
	}
	for name := range node.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var input strings.Builder
	for _, name := range names {
		entry, found := node.entries[name]
		if !found {
			tree, err := s.writeWorkspaceTree(node.dirs[name])
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&input, "040000 tree %s\t%s\x00", tree, name)
			continue
		}
		kind := "blob"
		if entry.mode == "160000" {
			kind = "commit"
		} else if entry.mode == "040000" {
			kind = "tree"
		}
		fmt.Fprintf(&input, "%s %s %s\t%s\x00", entry.mode, kind, entry.oid, name)
	}
	tree, err := s.cmd.New([]string{"git", "mktree", "-z", "--missing"}).SetStdin(input.String()).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("write workspace metadata tree: %w: %s", err, strings.TrimSpace(tree))
	}
	return strings.TrimSpace(tree), nil
}

type workspaceIndexEntry struct {
	Mode  string
	OID   string
	Stage int
	Path  string
	Raw   string
}

func (s *gitSession) parseWorkspaceIndex(input string) ([]workspaceIndexEntry, error) {
	var entries []workspaceIndexEntry
	seen := make(map[string]struct{})
	for _, record := range strings.Split(strings.TrimSuffix(input, "\x00"), "\x00") {
		if record == "" {
			continue
		}
		tab := strings.IndexByte(record, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("saved index contains a malformed entry")
		}
		fields := strings.Fields(record[:tab])
		if len(fields) != 3 || !fileRecoveryOID(fields[1]) {
			return nil, fmt.Errorf("saved index contains a malformed entry")
		}
		stage, err := strconv.Atoi(fields[2])
		if err != nil || stage < 0 || stage > 3 {
			return nil, fmt.Errorf("saved index contains an invalid stage")
		}
		switch fields[0] {
		case "100644", "100755", "120000", "160000":
		default:
			return nil, fmt.Errorf("saved index contains unsupported mode %q", fields[0])
		}
		name := record[tab+1:]
		if err := validateFileRecoveryPath(name); err != nil {
			return nil, fmt.Errorf("saved index path is invalid: %w", err)
		}
		if fields[0] == "160000" && stage != 0 {
			return nil, fmt.Errorf("conflicted submodules are not supported for workspace Undo")
		}
		key := fmt.Sprintf("%s\x00%d", name, stage)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("saved index contains duplicate stage %d for %q", stage, name)
		}
		seen[key] = struct{}{}
		if stage == 0 || fields[0] != "160000" {
			kind, err := s.gitText("cat-file", "-t", fields[1])
			if err != nil || strings.TrimSpace(kind) != "blob" {
				return nil, fmt.Errorf("saved index payload for %q is missing or not a blob", name)
			}
		}
		entries = append(entries, workspaceIndexEntry{Mode: fields[0], OID: fields[1], Stage: stage, Path: name, Raw: record + "\x00"})
	}
	return entries, nil
}

func (s *gitSession) captureWorkspaceIndex() ([]workspaceIndexEntry, string, error) {
	output, err := s.cmd.New([]string{"git", "--literal-pathspecs", "ls-files", "--stage", "-z", "--"}).RunWithOutput()
	if err != nil {
		return nil, "", fmt.Errorf("capture workspace index: %w: %s", err, strings.TrimSpace(output))
	}
	entries, err := s.parseWorkspaceIndex(output)
	if err != nil {
		return nil, "", err
	}
	var raw strings.Builder
	for _, entry := range entries {
		raw.WriteString(entry.Raw)
	}
	return entries, raw.String(), nil
}

func (s *gitSession) restoreWorkspaceIndex(entries []workspaceIndexEntry) error {
	if err := s.cmd.New([]string{"git", "read-tree", "--empty"}).Run(); err != nil {
		return fmt.Errorf("restore workspace index: %w", err)
	}
	var input strings.Builder
	for _, entry := range entries {
		input.WriteString(entry.Raw)
	}
	output, err := s.cmd.New([]string{"git", "update-index", "-z", "--index-info"}).SetStdin(input.String()).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore workspace index: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (s *gitSession) captureWorkspaceTreeFiles(listing string, entries []workspaceIndexEntry) (*workspaceTreeNode, *workspaceTreeNode, error) {
	tracked, err := s.cmd.New([]string{"git", "ls-files", "-z", "--"}).RunWithOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("list workspace files: %w", err)
	}
	index := make(map[string]workspaceIndexEntry)
	for _, entry := range entries {
		if entry.Stage == 0 {
			index[entry.Path] = entry
		}
	}
	paths := make(map[string]bool)
	for _, name := range strings.Split(strings.TrimSuffix(tracked+listing, "\x00"), "\x00") {
		if name != "" {
			if err := validateFileRecoveryPath(name); err != nil {
				return nil, nil, fmt.Errorf("capture raw workspace path %q: %w", name, err)
			}
			paths[name] = false
		}
	}
	// Keep the membership test exact even for paths that are substrings of one
	// another, and for a file which also occurs in tracked deletions.
	untracked := make(map[string]bool)
	for _, name := range strings.Split(strings.TrimSuffix(listing, "\x00"), "\x00") {
		if name != "" {
			untracked[name] = true
		}
	}
	for name := range paths {
		paths[name] = untracked[name]
	}
	working, extra := newWorkspaceTreeNode(), newWorkspaceTreeNode()
	for _, name := range sortedWorkspacePaths(paths) {
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), name)
		if err != nil {
			return nil, nil, err
		}
		info, err := s.fileService().Lstat(filename)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, err
		}
		entry, err := s.captureRawWorkspaceFile(name, filename, info, index[name])
		if err != nil {
			return nil, nil, err
		}
		working.insert(name, entry)
		if paths[name] {
			extra.insert(name, entry)
		}
	}
	return working, extra, nil
}

func (s *gitSession) captureRawWorkspaceFile(name, filename string, info os.FileInfo, indexed workspaceIndexEntry) (workspaceTreeEntry, error) {
	mode := "100644"
	if info.Mode().IsDir() {
		if indexed.Mode != "160000" || indexed.Stage != 0 {
			return workspaceTreeEntry{}, fmt.Errorf("workspace Undo does not support non-file path %q without a clean submodule index", name)
		}
		head, err := s.cmd.New([]string{"git", "-C", filename, "rev-parse", "--verify", "HEAD"}).RunWithOutput()
		if err != nil || strings.TrimSpace(head) != indexed.OID {
			return workspaceTreeEntry{}, fmt.Errorf("workspace Undo does not support a changed submodule at %q", name)
		}
		state, err := s.cmd.New([]string{"git", "-C", filename, "status", "--porcelain", "-z", "--ignored=matching", "--untracked-files=all", "--"}).RunWithOutput()
		if err != nil {
			return workspaceTreeEntry{}, fmt.Errorf("inspect submodule %q: %w", name, err)
		}
		if state != "" {
			return workspaceTreeEntry{}, fmt.Errorf("workspace Undo does not support a dirty submodule at %q", name)
		}
		return workspaceTreeEntry{mode: "160000", oid: indexed.OID}, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := s.fileService().Readlink(filename)
		if err != nil {
			return workspaceTreeEntry{}, err
		}
		oid, err := s.cmd.New([]string{"git", "hash-object", "-w", "--stdin"}).SetStdin(target).RunWithOutput()
		if err != nil {
			return workspaceTreeEntry{}, fmt.Errorf("capture symbolic link %q: %w", name, err)
		}
		return workspaceTreeEntry{mode: "120000", oid: strings.TrimSpace(oid)}, nil
	}
	if !info.Mode().IsRegular() {
		return workspaceTreeEntry{}, fmt.Errorf("workspace Undo does not support special file %q", name)
	}
	// --no-filters is essential: the index blob may be an LFS pointer or a
	// normalized clean representation, while restore must reproduce these bytes.
	oid, err := s.cmd.New([]string{"git", "hash-object", "-w", "--no-filters", filename}).RunWithOutput()
	if err != nil {
		return workspaceTreeEntry{}, fmt.Errorf("capture raw worktree file %q: %w: %s", name, err, strings.TrimSpace(oid))
	}
	if info.Mode().Perm()&0o111 != 0 {
		mode = "100755"
	}
	return workspaceTreeEntry{mode: mode, oid: strings.TrimSpace(oid)}, nil
}

func sortedWorkspacePaths(paths map[string]bool) []string {
	result := make([]string, 0, len(paths))
	for name := range paths {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

type workspaceArchive struct {
	MetadataCommit string
	Index          []workspaceIndexEntry
	Worktree       *workspaceTreeNode
	Untracked      *workspaceTreeNode
	HasUnmerged    bool
	Scope          workspaceArchiveScope
	Permissions    workspacePermissionManifest
}

func hasUnmergedWorkspaceEntries(entries []workspaceIndexEntry) bool {
	for _, entry := range entries {
		if entry.Stage != 0 {
			return true
		}
	}
	return false
}

func (s *gitSession) captureWorkspaceArchive(listing string, scope workspaceArchiveScope) (*workspaceArchive, error) {
	entries, rawIndex, err := s.captureWorkspaceIndex()
	if err != nil {
		return nil, err
	}
	working, untracked, err := s.captureWorkspaceTreeFiles(listing, entries)
	if err != nil {
		return nil, err
	}
	root := newWorkspaceTreeNode()
	root.dirs["worktree"] = working
	root.dirs["untracked"] = untracked
	indexOID, err := workspaceMetadataBlob(s, []byte(rawIndex))
	if err != nil {
		return nil, err
	}
	root.insert("index/stages", workspaceTreeEntry{mode: "100644", oid: indexOID})
	for _, entry := range entries {
		if entry.Mode != "160000" {
			root.insert("index/objects/"+entry.OID, workspaceTreeEntry{mode: "100644", oid: entry.OID})
		}
	}
	permissions, err := s.captureWorkspacePermissions(listing)
	if err != nil {
		return nil, err
	}
	permissionPayload, err := json.Marshal(permissions)
	if err != nil {
		return nil, err
	}
	permissionOID, err := workspaceMetadataBlob(s, permissionPayload)
	if err != nil {
		return nil, err
	}
	root.insert("permissions.json", workspaceTreeEntry{mode: "100644", oid: permissionOID})
	if err := s.archiveRepositoryOperationState(root); err != nil {
		return nil, err
	}
	finalScope, err := s.writeWorkspaceScope(root, scope)
	if err != nil {
		return nil, err
	}
	metadataTree, err := s.writeWorkspaceTree(root)
	if err != nil {
		return nil, err
	}
	metadataCommit, err := s.commitWorkspaceMetadataWithRefs(metadataTree, root)
	if err != nil {
		return nil, err
	}
	return &workspaceArchive{MetadataCommit: metadataCommit, Index: entries, Worktree: working, Untracked: untracked, HasUnmerged: hasUnmergedWorkspaceEntries(entries), Scope: finalScope, Permissions: permissions}, nil
}

func workspaceMetadataBlob(s *gitSession, data []byte) (string, error) {
	oid, err := s.cmd.New([]string{"git", "hash-object", "-w", "--stdin"}).SetStdin(string(data)).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("write workspace metadata: %w: %s", err, strings.TrimSpace(oid))
	}
	return strings.TrimSpace(oid), nil
}

func (s *gitSession) restoreRawWorkspacePaths(snapshotHash string, paths []string) error {
	metadata, present, err := workspaceMetadataLocation(s, snapshotHash)
	if err != nil || !present {
		return err
	}
	for _, name := range paths {
		if err := validateFileRecoveryPath(name); err != nil {
			return err
		}
		line, err := s.cmd.New([]string{"git", "--literal-pathspecs", "ls-tree", "-z", metadata, "--", "worktree/" + name}).RunWithOutput()
		if err != nil {
			return fmt.Errorf("read raw workspace file %q: %w", name, err)
		}
		line = strings.TrimSuffix(line, "\x00")
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return fmt.Errorf("raw workspace file %q has an invalid entry", name)
		}
		if fields[0] == "120000" || fields[0] == "160000" {
			// Git restores symbolic links and clean submodules from the
			// snapshot tree. Only regular files need unfiltered bytes here.
			continue
		}
		if fields[0] != "100644" && fields[0] != "100755" || fields[1] != "blob" {
			return fmt.Errorf("raw workspace file %q has an invalid mode", name)
		}
		payload, err := s.readFileRecoveryBlob(fields[2])
		if err != nil {
			return err
		}
		filename, err := s.worktreeFilePath(s.git.RepoPaths.WorktreePath(), name)
		if err != nil {
			return err
		}
		if err := s.fileService().WriteFileAtomic(filename, payload, 0o600); err != nil {
			return fmt.Errorf("restore raw workspace file %q: %w", name, err)
		}
	}
	return nil
}

func (s *gitSession) commitWorkspaceMetadataWithRefs(tree string, root *workspaceTreeNode) (string, error) {
	// Ref trees are complete object references, but making referenced commits
	// parents of the metadata commit retains them even after refs are removed.
	var parents []string
	if root.dirs["refs"] != nil || root.dirs["objects"] != nil {
		entries, err := s.cmd.New([]string{"git", "ls-tree", "-r", "-z", tree, "--", "refs", "objects"}).RunWithOutput()
		if err != nil {
			return "", fmt.Errorf("read workspace ref pins: %w", err)
		}
		unique := make(map[string]bool)
		for _, line := range strings.Split(strings.TrimSuffix(entries, "\x00"), "\x00") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[1] == "commit" && !unique[fields[2]] {
				unique[fields[2]] = true
				parents = append(parents, fields[2])
			}
		}
		sort.Strings(parents)
	}
	args := []string{"git", "commit-tree", tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	commit, err := s.cmd.New(args).SetStdin("TanGit workspace metadata\n").RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("write workspace metadata commit: %w: %s", err, strings.TrimSpace(commit))
	}
	return strings.TrimSpace(commit), nil
}

func workspaceMetadataLocation(s *gitSession, hash string) (string, bool, error) {
	object, err := s.gitText("cat-file", "commit", hash)
	if err != nil {
		return "", false, err
	}
	_, message, _ := strings.Cut(object, "\n\n")
	marker := "\n" + strings.TrimSuffix(workspaceMetadataMarker, "\n")
	if !strings.Contains(message, marker) {
		if strings.Contains(message, "\nTangit-Workspace-Metadata:") {
			return "", false, fmt.Errorf("saved workspace metadata uses an unsupported version")
		}
		return "", false, nil
	}
	header := strings.SplitN(object, "\n\n", 2)[0]
	var parents []string
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "parent ") && fileRecoveryOID(strings.TrimSpace(strings.TrimPrefix(line, "parent "))) {
			parents = append(parents, strings.TrimSpace(strings.TrimPrefix(line, "parent ")))
		}
	}
	if len(parents) < 3 {
		return "", false, fmt.Errorf("saved workspace metadata parent is missing")
	}
	return parents[len(parents)-1], true, nil
}

type workspaceArchivedObject struct {
	Mode string
	Kind string
	OID  string
	Path string
}

func decodeWorkspaceTree(input string) ([]workspaceArchivedObject, error) {
	var result []workspaceArchivedObject
	for _, record := range strings.Split(strings.TrimSuffix(input, "\x00"), "\x00") {
		if record == "" {
			continue
		}
		metadata, name, found := strings.Cut(record, "\t")
		fields := strings.Fields(metadata)
		if !found || len(fields) != 3 || !fileRecoveryOID(fields[2]) || name == "" || path.Clean(name) != name {
			return nil, fmt.Errorf("workspace metadata contains a malformed tree entry")
		}
		result = append(result, workspaceArchivedObject{Mode: fields[0], Kind: fields[1], OID: fields[2], Path: name})
	}
	return result, nil
}

type workspaceStatePathMode struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

type workspaceStateManifest struct {
	Version     int                      `json:"version"`
	Directories []workspaceStatePathMode `json:"directories"`
	FileModes   []workspaceStatePathMode `json:"file_modes"`
}

func repositoryStateDirectory(name string) bool {
	switch name {
	case "rebase-merge", "rebase-apply", "sequencer", "refs/bisect", "refs/rewritten":
		return true
	}
	return false
}

func repositoryStateFile(name string) bool {
	switch name {
	case "MERGE_HEAD", "MERGE_MSG", "MERGE_MODE", "AUTO_MERGE", "CHERRY_PICK_HEAD", "REVERT_HEAD", "logs/refs/stash",
		"config/common", "config/worktree/config", "config/worktree/config.worktree":
		return true
	}
	return strings.HasPrefix(name, "BISECT_") && !strings.Contains(name, "/")
}

func repositoryStatePath(name string) bool {
	if repositoryStateFile(name) || repositoryStateDirectory(name) {
		return true
	}
	for _, directory := range []string{"rebase-merge/", "rebase-apply/", "sequencer/", "refs/bisect/", "refs/rewritten/"} {
		if strings.HasPrefix(name, directory) {
			return true
		}
	}
	return false
}

func (s *gitSession) collectRepositoryOperationState() (workspaceStateManifest, map[string][]byte, error) {
	manifest := workspaceStateManifest{Version: 1}
	payloads := make(map[string][]byte)
	root := s.git.RepoPaths.WorktreeGitDirPath()
	rootEntries, err := s.fileService().ReadDir(root)
	if err != nil {
		return manifest, nil, fmt.Errorf("read operation state: %w", err)
	}
	var names []string
	for _, entry := range rootEntries {
		if repositoryStateFile(entry.Name()) || repositoryStateDirectory(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	names = append(names, "refs/bisect", "refs/rewritten", "logs/refs/stash", "config/common", "config/worktree/config", "config/worktree/config.worktree")
	sort.Strings(names)
	var walk func(string) error
	walk = func(name string) error {
		if !repositoryStatePath(name) || path.Clean(name) != name {
			return fmt.Errorf("unsupported Git operation metadata path %q", name)
		}
		filename, err := s.repositoryStateFilePath(name)
		if err != nil {
			return err
		}
		info, err := s.fileService().Lstat(filename)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			manifest.Directories = append(manifest.Directories, workspaceStatePathMode{Path: name, Mode: uint32(info.Mode().Perm())})
			entries, err := s.fileService().ReadDir(filename)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err := walk(s.joinPath(name, entry.Name())); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() || !repositoryStateFile(name) && !strings.Contains(name, "/") {
			return fmt.Errorf("Git operation metadata path %q is not a regular file", name)
		}
		if strings.HasPrefix(name, "refs/") {
			// Refs are restored through update-ref with expected-old-value
			// transactions, including packed refs, not by editing loose files.
			return nil
		}
		payload, err := s.fileService().ReadFile(filename)
		if err != nil {
			return err
		}
		payloads[name] = payload
		manifest.FileModes = append(manifest.FileModes, workspaceStatePathMode{Path: name, Mode: uint32(info.Mode().Perm())})
		return nil
	}
	for _, name := range names {
		if err := walk(name); err != nil {
			return manifest, nil, fmt.Errorf("capture Git operation metadata: %w", err)
		}
	}
	return manifest, payloads, nil
}

func (s *gitSession) archiveRepositoryOperationState(root *workspaceTreeNode) error {
	manifest, payloads, err := s.collectRepositoryOperationState()
	if err != nil {
		return err
	}
	for _, name := range sortedWorkspacePayloadPaths(payloads) {
		oid, err := workspaceMetadataBlob(s, payloads[name])
		if err != nil {
			return err
		}
		root.insert("state/"+name, workspaceTreeEntry{mode: "100644", oid: oid})
		if err := s.pinWorkspaceCommit(root, name, payloads[name]); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	oid, err := workspaceMetadataBlob(s, encoded)
	if err != nil {
		return err
	}
	root.insert("repository.json", workspaceTreeEntry{mode: "100644", oid: oid})
	return s.archiveWorkspaceRefs(root)
}

func sortedWorkspacePayloadPaths(paths map[string][]byte) []string {
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *gitSession) pinWorkspaceCommit(root *workspaceTreeNode, name string, payload []byte) error {
	if name == "AUTO_MERGE" {
		oid := strings.TrimSpace(string(payload))
		kind, err := s.gitText("cat-file", "-t", oid)
		if err != nil || strings.TrimSpace(kind) != "tree" || !fileRecoveryOID(oid) {
			return fmt.Errorf("cannot pin Git AUTO_MERGE tree")
		}
		root.insert("trees/"+oid, workspaceTreeEntry{mode: "040000", oid: oid})
		return nil
	}
	var candidates []string
	switch {
	case name == "MERGE_HEAD", name == "CHERRY_PICK_HEAD", name == "REVERT_HEAD",
		name == "rebase-merge/orig-head", name == "rebase-merge/onto", name == "rebase-merge/stopped-sha",
		name == "rebase-apply/original-commit":
		candidates = strings.Fields(string(payload))
	case name == "rebase-merge/git-rebase-todo", name == "rebase-merge/done", name == "sequencer/todo":
		for _, line := range strings.Split(string(payload), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if fields[0] == "update-ref" && (name == "rebase-merge/git-rebase-todo" || name == "rebase-merge/done") && !strings.HasPrefix(fields[1], "refs/heads/") {
					return fmt.Errorf("workspace Undo does not support rebase ref updates outside local branches")
				}
				switch fields[0] {
				case "pick", "p", "reword", "r", "edit", "e", "squash", "s", "fixup", "f", "merge", "m":
					index := 1
					if (fields[0] == "fixup" || fields[0] == "merge" || fields[0] == "m") && len(fields) >= 3 && (fields[1] == "-C" || fields[1] == "-c") {
						index = 2
					}
					candidates = append(candidates, fields[index])
				}
			}
		}
	case name == "logs/refs/stash":
		for _, line := range strings.Split(string(payload), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				candidates = append(candidates, fields[0], fields[1])
			}
		}
	}
	for _, candidate := range candidates {
		if name == "logs/refs/stash" && (len(candidate) == 40 || len(candidate) == 64) && strings.Trim(candidate, "0") == "" {
			continue
		}
		oid, err := s.gitText("rev-parse", "--verify", candidate+"^{commit}")
		if err != nil {
			return fmt.Errorf("cannot pin Git operation commit from %s: %w", name, err)
		}
		oid = strings.TrimSpace(oid)
		if !fileRecoveryOID(oid) {
			return fmt.Errorf("invalid operation commit %q in %s", oid, name)
		}
		root.insert("objects/"+oid, workspaceTreeEntry{mode: "160000", oid: oid})
	}
	return nil
}

func (s *gitSession) archiveWorkspaceRefs(root *workspaceTreeNode) error {
	// These are operation-local refs, not all repository refs. HEAD and its
	// branch remain protected by OperationRecord's exact before/after pin.
	refs, err := s.currentWorkspaceRefs()
	if err != nil {
		return err
	}
	var names []string
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name != "refs/stash" && !strings.HasPrefix(name, "refs/bisect/") && !strings.HasPrefix(name, "refs/rewritten/") && !strings.HasPrefix(name, "refs/heads/") {
			return fmt.Errorf("unexpected operation reference %q", name)
		}
		root.insert("refs/"+name, workspaceTreeEntry{mode: "160000", oid: refs[name]})
	}
	return nil
}
