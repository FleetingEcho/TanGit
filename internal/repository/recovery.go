package repository

import (
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	RecoverySnapshotKind   = "recovery"
	CheckpointSnapshotKind = "checkpoint"
	recoveryRefPrefix      = "refs/tangit/recovery/"
	checkpointRefPrefix    = "refs/tangit/checkpoints/"
	recoveryKeep           = 20
	recoveryLifetime       = 7 * 24 * time.Hour
)

// WorkSnapshot is a repository-local recovery point. Its ID is the immutable
// stash-shaped commit ID, not a mutable stash index or a user-provided ref.
type WorkSnapshot struct {
	ID        string
	Hash      string
	Kind      string
	Name      string
	CreatedAt int64
}

// RestoreConflictError rejects a recovery before Git changes anything when a
// saved path has gained new work. The entry remains available for a later try.
type RestoreConflictError struct {
	Paths []string
}

func (e *RestoreConflictError) Error() string {
	return fmt.Sprintf("restore would overlap new changes in %s", strings.Join(e.Paths, ", "))
}

func workSnapshotRef(kind, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("snapshot id is required")
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", fmt.Errorf("invalid snapshot id")
		}
	}
	switch kind {
	case RecoverySnapshotKind:
		return recoveryRefPrefix + id, nil
	case CheckpointSnapshotKind:
		return checkpointRefPrefix + id, nil
	default:
		return "", fmt.Errorf("unknown snapshot kind %q", kind)
	}
}

// captureWorkSnapshot writes the same W/I/U object shape git stash uses, but
// publishes it under a hidden lazygitGo ref instead of refs/stash and never cleans
// the working tree. A failed capture therefore cannot destroy user work.
func (s *gitSession) captureWorkSnapshot(ctx context.Context, kind, name string) (WorkSnapshot, error) {
	ignored, err := s.ignoredWorkspacePaths()
	if err != nil {
		return WorkSnapshot{}, err
	}
	return s.captureWorkSnapshotWithIgnored(ctx, kind, name, ignored, workspaceArchiveScope{AllIgnored: true})
}

// captureWorkSnapshotWithIgnored also saves selected ignored paths. Ordinary
// snapshots include ignored files as well; destructive reset passes only the
// ignored paths its target can overwrite.
func (s *gitSession) captureWorkSnapshotWithIgnored(ctx context.Context, kind, name string, ignored []string, policies ...workspaceArchiveScope) (WorkSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return WorkSnapshot{}, err
	}
	if err := s.checkWorkspaceIndexLock(); err != nil {
		return WorkSnapshot{}, err
	}
	listing, err := s.recoveryUntrackedListing(ignored)
	if err != nil {
		return WorkSnapshot{}, err
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = "lazygitGo snapshot"
	}
	policy := workspaceArchiveScope{IgnoredPaths: append([]string(nil), ignored...)}
	if len(policies) > 1 {
		return WorkSnapshot{}, fmt.Errorf("workspace snapshot has multiple capture policies")
	}
	if len(policies) == 1 {
		policy = policies[0]
		policy.IgnoredPaths = append(policy.IgnoredPaths[:0], ignored...)
	}
	archive, err := s.captureWorkspaceArchive(listing, policy)
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("capture workspace archive: %w", err)
	}
	var hash string
	trackedChanges := true
	if !archive.HasUnmerged {
		trackedChanges, err = s.hasTrackedWorkspaceChanges()
		if err != nil {
			return WorkSnapshot{}, fmt.Errorf("inspect recovery worktree: %w", err)
		}
	}
	if archive.HasUnmerged || !trackedChanges {
		hash, err = s.captureRawWorkSnapshot(ctx, name, archive)
	} else {
		hash, err = s.gitText("stash", "create", name)
		if err != nil {
			// Fail closed on an actual capture failure. A raw fallback is not
			// a licence to continue after a failed or damaged Git capture.
			return WorkSnapshot{}, fmt.Errorf("capture recovery: %w", err)
		} else {
			hash = strings.TrimSpace(hash)
			if hash == "" && listing == "" {
				hash, err = s.captureRawWorkSnapshot(ctx, name, archive)
			} else {
				if hash == "" {
					hash, err = s.untrackedOnlySnapshotBase(name, listing)
					if err != nil {
						return WorkSnapshot{}, err
					}
				}
				hash, err = s.addUntrackedSnapshotParent(hash, name, listing)
			}
		}
	}
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("capture recovery: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return WorkSnapshot{}, err
	}
	hash, err = s.attachWorkspacePermissions(hash, name, listing, archive.MetadataCommit)
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("capture workspace permissions: %w", err)
	}
	ref, err := workSnapshotRef(kind, hash)
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("prepare recovery ref for %q: %w", hash, err)
	}
	output, err := s.cmd.New([]string{"git", "update-ref", "-m", name, ref, hash}).RunWithOutput()
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("publish recovery: %w: %s", err, strings.TrimSpace(output))
	}
	return WorkSnapshot{ID: hash, Hash: hash, Kind: kind, Name: name, CreatedAt: time.Now().Unix()}, nil
}

// hasTrackedWorkspaceChanges distinguishes a genuinely clean tracked state
// from a failed write. Untracked and ignored files are already in the raw
// archive and do not require a stash write of their own. A symbolic unborn
// HEAD is valid, but a broken or unreadable detached HEAD remains an error.
func (s *gitSession) hasTrackedWorkspaceChanges() (bool, error) {
	stdout, stderr, err := s.cmd.New([]string{"git", "diff", "--quiet", "--no-ext-diff", "--no-textconv", "HEAD", "--"}).RunWithOutputs()
	if err == nil {
		return false, nil
	}
	if code, ok := commandExitCode(err); ok && code == 1 {
		return true, nil
	}
	if head, headErr := s.gitText("symbolic-ref", "--quiet", "HEAD"); headErr == nil && head != "" {
		return false, nil
	}
	return false, fmt.Errorf("compare tracked workspace: %w: %s", err, strings.TrimSpace(stdout+stderr))
}

func (s *gitSession) recoveryUntrackedListing(ignored []string) (string, error) {
	listing, err := s.cmd.New([]string{"git", "ls-files", "--others", "--exclude-standard", "-z"}).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("list untracked files: %w: %s", err, strings.TrimSpace(listing))
	}
	for _, name := range ignored {
		listing += name + "\x00"
	}
	return listing, nil
}

// captureRefMutationSnapshot limits recovery to ignored collisions that the
// requested tracked transition can actually affect. The policy is published
// with the restore point, so post-capture and Undo never need to enumerate an
// unrelated ignored tree. Unsupported paths are still rejected on collision.
func (s *gitSession) captureRefMutationSnapshot(ctx context.Context, summary, target string) (WorkSnapshot, error) {
	if strings.TrimSpace(target) == "" {
		return WorkSnapshot{}, fmt.Errorf("merge target is required")
	}
	_, before, err := s.currentOperationIdentity()
	if err != nil {
		return WorkSnapshot{}, err
	}
	after, err := s.gitText("rev-parse", "--verify", target+"^{commit}")
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("prepare merge recovery target: %w", err)
	}
	ignored, err := s.resetIgnoredPaths(before, after)
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("prepare merge recovery collisions: %w", err)
	}
	if len(ignored) > 0 {
		// A real overwrite candidate keeps the original full recovery
		// contract, including failing closed on paths it cannot safely store.
		return s.captureWorkSnapshot(ctx, RecoverySnapshotKind, summary)
	}
	return s.captureWorkSnapshotWithIgnored(ctx, RecoverySnapshotKind, summary, nil, workspaceArchiveScope{AllIgnored: false})
}

// resetIgnoredPaths finds ignored files that a hard reset between before and
// after can replace. Only added target paths matter: a path tracked on both
// sides cannot also be ignored, and unrelated ignored trees need no hashing.
// ignoredWorkspacePaths lists ignored files without interpreting them as
// tracked changes or flattening symlink entries. Recovery includes them in
// both snapshots and compare-and-swap fingerprints, not only reset collisions.
func (s *gitSession) ignoredWorkspacePaths() ([]string, error) {
	output, err := s.cmd.New([]string{"git", "ls-files", "--others", "--ignored", "--exclude-standard", "-z"}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("list ignored recovery files: %w: %s", err, strings.TrimSpace(output))
	}
	var paths []string
	for _, name := range strings.Split(strings.TrimSuffix(output, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		if err := validateFileRecoveryPath(name); err != nil {
			return nil, fmt.Errorf("unsupported recovery path %q: %w", name, err)
		}
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *gitSession) resetIgnoredPaths(before, after string) ([]string, error) {
	if before == after {
		return nil, nil
	}
	resetTree := func(commit string) string {
		if commit == "" {
			return "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
		}
		return commit
	}
	before, after = resetTree(before), resetTree(after)
	targetSet := make(map[string]bool)
	for _, pair := range [][2]string{{before, after}, {after, before}} {
		output, err := s.cmd.New([]string{"git", "diff", "--no-renames", "--name-only", "--diff-filter=A", "-z", pair[0], pair[1], "--"}).RunWithOutput()
		if err != nil {
			return nil, fmt.Errorf("find reset paths: %w: %s", err, strings.TrimSpace(output))
		}
		for _, name := range strings.Split(strings.TrimSuffix(output, "\x00"), "\x00") {
			if name != "" {
				targetSet[name] = true
			}
		}
	}
	if len(targetSet) == 0 {
		return nil, nil
	}
	targets := make([]string, 0, len(targetSet))
	for name := range targetSet {
		targets = append(targets, name)
	}
	sort.Strings(targets)
	listing, err := s.cmd.New([]string{"git", "ls-files", "--others", "--ignored", "--exclude-standard", "-z"}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("list ignored reset paths: %w: %s", err, strings.TrimSpace(listing))
	}
	var paths []string
	for _, name := range strings.Split(strings.TrimSuffix(listing, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		collision := false
		for parent := name; parent != "."; parent = path.Dir(parent) {
			if targetSet[parent] {
				collision = true
				break
			}
		}
		if !collision {
			prefix := name + "/"
			i := sort.SearchStrings(targets, prefix)
			collision = i < len(targets) && strings.HasPrefix(targets[i], prefix)
		}
		if collision {
			paths = append(paths, name)
		}
	}
	return paths, nil
}

// untrackedOnlySnapshotBase builds the W/I/base part of a stash-shaped commit
// when no tracked file changed (including an unborn HEAD). The object IDs are
// internal recovery data; no ref is published until all parents exist.
func (s *gitSession) untrackedOnlySnapshotBase(name, listing string) (string, error) {
	if listing == "" {
		return "", fmt.Errorf("there are no changes to capture")
	}
	env := []string{"GIT_AUTHOR_NAME=lazygitGo", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=lazygitGo", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	base, baseErr := s.gitText("rev-parse", "--verify", "HEAD")
	if baseErr != nil {
		output, err := s.cmd.New([]string{"git", "commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "-m", "lazygitGo recovery base"}).AddEnvVars(env...).RunWithOutput()
		if err != nil {
			return "", fmt.Errorf("create unborn recovery base: %w: %s", err, strings.TrimSpace(output))
		}
		base = strings.TrimSpace(output)
	}
	tree, err := s.gitText("rev-parse", base+"^{tree}")
	if err != nil {
		return "", err
	}
	indexCommit, err := s.cmd.New([]string{"git", "commit-tree", tree, "-p", base, "-m", "lazygitGo index snapshot"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("create untracked index snapshot: %w: %s", err, strings.TrimSpace(indexCommit))
	}
	commit, err := s.cmd.New([]string{"git", "commit-tree", tree, "-p", base, "-p", strings.TrimSpace(indexCommit), "-m", name}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("create untracked recovery base: %w: %s", err, strings.TrimSpace(commit))
	}
	return strings.TrimSpace(commit), nil
}

// addUntrackedSnapshotParent adds the third-parent tree that git stash create
// itself omits. It works entirely in object storage through a temporary index;
// neither the real index nor any working file is changed.
func (s *gitSession) addUntrackedSnapshotParent(hash, name, listing string) (string, error) {
	if listing == "" {
		return hash, nil
	}
	indexName, removeIndex, err := s.temporaryFile("lazygitgo-recovery-index-", nil, 0o600)
	if err != nil {
		return "", err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	pathsName, removePaths, err := s.temporaryFile("lazygitgo-recovery-paths-", []byte(listing), 0o600)
	if err != nil {
		return "", err
	}
	defer removePaths()
	env := []string{"GIT_INDEX_FILE=" + indexName, "GIT_AUTHOR_NAME=lazygitGo", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=lazygitGo", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	output, err := s.cmd.New([]string{"git", "add", "--all", "--force", "--pathspec-from-file=" + pathsName, "--pathspec-file-nul"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("index untracked files: %w: %s", err, strings.TrimSpace(output))
	}
	tree, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("write untracked tree: %w: %s", err, strings.TrimSpace(output))
	}
	untracked, err := s.cmd.New([]string{"git", "commit-tree", strings.TrimSpace(tree), "-m", "lazygitGo untracked snapshot"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("commit untracked tree: %w: %s", err, strings.TrimSpace(output))
	}
	workingTree, err := s.gitText("rev-parse", hash+"^{tree}")
	if err != nil {
		return "", err
	}
	base, err := s.gitText("rev-parse", hash+"^1")
	if err != nil {
		return "", err
	}
	indexCommit, err := s.gitText("rev-parse", hash+"^2")
	if err != nil {
		return "", err
	}
	commit, err := s.cmd.New([]string{"git", "commit-tree", workingTree, "-p", base, "-p", indexCommit, "-p", strings.TrimSpace(untracked), "-m", name}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("commit complete snapshot: %w: %s", err, strings.TrimSpace(commit))
	}
	return strings.TrimSpace(commit), nil
}

func (s *gitSession) WorkSnapshots(ctx context.Context) ([]WorkSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	format := "%(refname)%00%(objectname)%00%(creatordate:unix)%00%(contents:subject)"
	output, err := s.cmd.New([]string{"git", "for-each-ref", "--format=" + format, recoveryRefPrefix, checkpointRefPrefix}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("list recovery: %w: %s", err, strings.TrimSpace(output))
	}
	var snapshots []WorkSnapshot
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 {
			return nil, fmt.Errorf("list recovery: malformed ref record")
		}
		kind, id := "", ""
		switch {
		case strings.HasPrefix(fields[0], recoveryRefPrefix):
			kind, id = RecoverySnapshotKind, strings.TrimPrefix(fields[0], recoveryRefPrefix)
		case strings.HasPrefix(fields[0], checkpointRefPrefix):
			kind, id = CheckpointSnapshotKind, strings.TrimPrefix(fields[0], checkpointRefPrefix)
		default:
			continue
		}
		created, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("list recovery: malformed creation time")
		}
		snapshots = append(snapshots, WorkSnapshot{ID: id, Hash: fields[1], Kind: kind, Name: fields[3], CreatedAt: created})
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].CreatedAt == snapshots[j].CreatedAt {
			return snapshots[i].ID > snapshots[j].ID
		}
		return snapshots[i].CreatedAt > snapshots[j].CreatedAt
	})
	return snapshots, nil
}

// CreateCheckpoint captures all current work without changing HEAD, the index,
// the working tree or the ordinary stash. Checkpoints persist until deletion.
func (s *gitSession) CreateCheckpoint(ctx context.Context, name string) (WorkSnapshot, error) {
	return s.captureWorkSnapshot(ctx, CheckpointSnapshotKind, strings.TrimSpace(name))
}

// savedSnapshotPaths is the set a restore may touch, including deletions and
// the actual untracked parent. A lazygitGo metadata parent is repository object
// storage, not a list of worktree paths, so do not use `git stash show` here.
func (s *gitSession) savedSnapshotPaths(hash string) ([]string, error) {
	output, err := s.cmd.New([]string{"git", "diff", "--no-renames", "--name-only", "-z", hash + "^1", hash, "--"}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read recovery paths: %w: %s", err, strings.TrimSpace(output))
	}
	paths, err := workspacePathsFromNul(output)
	if err != nil {
		return nil, fmt.Errorf("read recovery paths: %w", err)
	}
	untracked, err := s.snapshotUntrackedCommit(hash)
	if err != nil || untracked == "" {
		return paths, err
	}
	listing, err := s.cmd.New([]string{"git", "ls-tree", "-r", "--name-only", "-z", untracked + "^{tree}"}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read recovery untracked paths: %w", err)
	}
	extra, err := workspacePathsFromNul(listing)
	if err != nil {
		return nil, err
	}
	return append(paths, extra...), nil
}

func (s *gitSession) snapshotUntrackedCommit(hash string) (string, error) {
	metadata, present, err := workspaceMetadataLocation(s, hash)
	if err != nil {
		return "", err
	}
	object, err := s.gitText("cat-file", "commit", hash)
	if err != nil {
		return "", err
	}
	header, _, _ := strings.Cut(object, "\n\n")
	var parents []string
	for _, line := range strings.Split(header, "\n") {
		if strings.HasPrefix(line, "parent ") {
			oid := strings.TrimSpace(strings.TrimPrefix(line, "parent "))
			if !fileRecoveryOID(oid) {
				return "", fmt.Errorf("saved snapshot contains an invalid parent")
			}
			parents = append(parents, oid)
		}
	}
	if present {
		if len(parents) < 3 || parents[len(parents)-1] != metadata {
			return "", fmt.Errorf("saved snapshot metadata parent is inconsistent")
		}
		parents = parents[:len(parents)-1]
	}
	if len(parents) < 3 {
		return "", nil
	}
	return parents[2], nil
}

func (s *gitSession) RestoreWorkSnapshot(ctx context.Context, snapshot WorkSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := workSnapshotRef(snapshot.Kind, snapshot.ID)
	if err != nil {
		return err
	}
	if _, err := s.gitText("rev-parse", "--verify", ref); err != nil {
		return fmt.Errorf("recovery entry no longer exists")
	}
	paths, err := s.savedSnapshotPaths(snapshot.Hash)
	if err != nil {
		return err
	}
	core, err := s.CoreSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("check recovery overlap: %w", err)
	}
	changed := make(map[string]bool, len(core.Files)*2)
	for _, file := range core.Files {
		changed[file.Path] = true
		if file.PreviousPath != "" {
			changed[file.PreviousPath] = true
		}
	}
	var overlap []string
	for _, path := range paths {
		if changed[path] {
			overlap = append(overlap, path)
		}
	}
	if len(overlap) > 0 {
		sort.Strings(overlap)
		return &RestoreConflictError{Paths: overlap}
	}
	return s.applyWorkSnapshot(snapshot)
}

// applyWorkSnapshot performs the write after the caller has completed the
// appropriate validation. Public snapshot restores reject overlaps above;
// journal Undo instead validates the operation's complete exact after-state.
func snapshotRepositoryPath(name string) error {
	if name == ".git" || strings.HasPrefix(name, ".git/") {
		return fmt.Errorf("%w: snapshot points into .git metadata", ErrFileRecoveryUnsupported)
	}
	return validateFileRecoveryPath(name)
}

func (s *gitSession) restoreSnapshotPaths(hash string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	for _, name := range paths {
		if err := snapshotRepositoryPath(name); err != nil {
			return err
		}
	}
	listing := strings.Join(paths, "\x00")
	name, cleanup, err := s.temporaryFile("lazygitgo-recovery-paths-", []byte(listing), 0o600)
	if err != nil {
		return err
	}
	defer cleanup()
	output, err := s.cmd.New([]string{
		"git", "--literal-pathspecs", "restore", "--source=" + hash,
		"--worktree", "--pathspec-from-file=" + name, "--pathspec-file-nul", "--",
	}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore selected snapshot paths: %w: %s", err, strings.TrimSpace(output))
	}
	return s.restoreRawWorkspacePaths(hash, paths)
}

func (s *gitSession) applyWorkSnapshot(snapshot WorkSnapshot) error {
	if _, err := s.gitText("rev-parse", "--verify", snapshot.Hash+"^{tree}"); err != nil {
		return fmt.Errorf("recovery snapshot is not a complete work tree: %w", err)
	}
	var paths, deleted []string
	for _, filter := range []struct {
		filter string
		to     *[]string
	}{{"D", &deleted}, {"AM", &paths}} {
		out, err := s.cmd.New([]string{
			"git", "diff", "--no-renames", "--name-only", "--diff-filter=" + filter.filter,
			"-z", snapshot.Hash + "^1", snapshot.Hash, "--",
		}).RunWithOutput()
		if err != nil {
			return fmt.Errorf("compare recovery to its base tree: %w: %s", err, strings.TrimSpace(out))
		}
		for _, name := range strings.Split(strings.TrimSuffix(out, "\x00"), "\x00") {
			if name == "" {
				continue
			}
			if err := snapshotRepositoryPath(name); err != nil {
				return err
			}
			*filter.to = append(*filter.to, name)
		}
	}
	for _, name := range deleted {
		if err := snapshotRepositoryPath(name); err != nil {
			return err
		}
		file := s.fileRecoveryWorktreePath(name)
		description, err := s.fileService().Lstat(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !description.Mode().IsRegular() && description.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%w: unsupported deletion target %s", ErrFileRecoveryUnsupported, name)
		}
		if err := s.fileService().Remove(file); err != nil {
			return fmt.Errorf("remove deleted snapshot file %s: %w", name, err)
		}
	}
	if err := s.restoreSnapshotPaths(snapshot.Hash, paths); err != nil {
		return err
	}
	if _, err := s.cmd.New([]string{"git", "read-tree", snapshot.Hash + "^2^{tree}"}).RunWithOutput(); err != nil {
		return fmt.Errorf("restore recovery index: %w", err)
	}
	if err := s.restoreSnapshotUntracked(snapshot); err != nil {
		return err
	}
	return s.restoreWorkspacePermissions(snapshot.Hash)
}

func (s *gitSession) DeleteWorkSnapshot(ctx context.Context, snapshot WorkSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref, err := workSnapshotRef(snapshot.Kind, snapshot.ID)
	if err != nil {
		return err
	}
	output, err := s.cmd.New([]string{"git", "update-ref", "-d", ref}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("delete recovery: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// pruneRecovery bounds only automatic recovery not still needed by a journal
// Undo. Named checkpoints and native stashes are never aged out, and no
// aggressive object collection is run.
func (s *gitSession) pruneRecovery(ctx context.Context) error {
	snapshots, err := s.WorkSnapshots(ctx)
	if err != nil {
		return err
	}
	operations, err := s.Operations(ctx)
	if err != nil {
		return err
	}
	protected := make(map[string]bool)
	for _, record := range operations {
		if record.Snapshot != nil && (record.CanUndo() || record.UndoStartedAt != 0) {
			protected[record.Snapshot.ID] = true
		}
		if record.AfterSnapshot != nil && (record.CanUndo() || record.UndoStartedAt != 0) {
			protected[record.AfterSnapshot.ID] = true
		}
	}
	var automatic []WorkSnapshot
	cutoff := time.Now().Add(-recoveryLifetime).Unix()
	for _, snapshot := range snapshots {
		if snapshot.Kind == RecoverySnapshotKind && !protected[snapshot.ID] {
			automatic = append(automatic, snapshot)
		}
	}
	for i, snapshot := range automatic {
		if i >= recoveryKeep || snapshot.CreatedAt < cutoff {
			if err := s.DeleteWorkSnapshot(ctx, snapshot); err != nil {
				return err
			}
		}
	}
	return nil
}
