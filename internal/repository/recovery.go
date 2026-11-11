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
// publishes it under a hidden TanGit ref instead of refs/stash and never cleans
// the working tree. A failed capture therefore cannot destroy user work.
func (s *gitSession) captureWorkSnapshot(ctx context.Context, kind, name string) (WorkSnapshot, error) {
	return s.captureWorkSnapshotWithIgnored(ctx, kind, name, nil)
}

// captureWorkSnapshotWithIgnored also saves selected ignored paths. Ordinary
// snapshots omit dependency and build trees; destructive reset passes only the
// ignored paths its target can overwrite.
func (s *gitSession) captureWorkSnapshotWithIgnored(ctx context.Context, kind, name string, ignored []string) (WorkSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return WorkSnapshot{}, err
	}
	listing, err := s.recoveryUntrackedListing(ignored)
	if err != nil {
		return WorkSnapshot{}, err
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = "TanGit snapshot"
	}
	hash, err := s.gitText("stash", "create", name)
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("capture recovery: %w", err)
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		hash, err = s.untrackedOnlySnapshotBase(name, listing)
		if err != nil {
			return WorkSnapshot{}, err
		}
	}
	hash, err = s.addUntrackedSnapshotParent(hash, name, listing)
	if err != nil {
		return WorkSnapshot{}, fmt.Errorf("capture untracked recovery: %w", err)
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

// resetIgnoredPaths finds ignored files that a hard reset between before and
// after can replace. Only added target paths matter: a path tracked on both
// sides cannot also be ignored, and unrelated ignored trees need no hashing.
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
	env := []string{"GIT_AUTHOR_NAME=TanGit", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=TanGit", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	base, baseErr := s.gitText("rev-parse", "--verify", "HEAD")
	if baseErr != nil {
		output, err := s.cmd.New([]string{"git", "commit-tree", "4b825dc642cb6eb9a060e54bf8d69288fbee4904", "-m", "TanGit recovery base"}).AddEnvVars(env...).RunWithOutput()
		if err != nil {
			return "", fmt.Errorf("create unborn recovery base: %w: %s", err, strings.TrimSpace(output))
		}
		base = strings.TrimSpace(output)
	}
	tree, err := s.gitText("rev-parse", base+"^{tree}")
	if err != nil {
		return "", err
	}
	indexCommit, err := s.cmd.New([]string{"git", "commit-tree", tree, "-p", base, "-m", "TanGit index snapshot"}).AddEnvVars(env...).RunWithOutput()
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
	indexName, removeIndex, err := s.temporaryFile("tangit-recovery-index-", nil, 0o600)
	if err != nil {
		return "", err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	pathsName, removePaths, err := s.temporaryFile("tangit-recovery-paths-", []byte(listing), 0o600)
	if err != nil {
		return "", err
	}
	defer removePaths()
	env := []string{"GIT_INDEX_FILE=" + indexName, "GIT_AUTHOR_NAME=TanGit", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=TanGit", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	output, err := s.cmd.New([]string{"git", "add", "--all", "--force", "--pathspec-from-file=" + pathsName, "--pathspec-file-nul"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("index untracked files: %w: %s", err, strings.TrimSpace(output))
	}
	tree, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("write untracked tree: %w: %s", err, strings.TrimSpace(output))
	}
	untracked, err := s.cmd.New([]string{"git", "commit-tree", strings.TrimSpace(tree), "-m", "TanGit untracked snapshot"}).AddEnvVars(env...).RunWithOutput()
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

// savedSnapshotPaths is the set a stash apply may touch, including renames and
// the third-parent untracked tree. Any later work on one makes restore unsafe.
func (s *gitSession) savedSnapshotPaths(hash string) ([]string, error) {
	output, err := s.cmd.New([]string{"git", "stash", "show", "--name-status", "-z", "-u", hash}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read recovery paths: %w: %s", err, strings.TrimSpace(output))
	}
	files, err := parseNameStatus(output)
	if err != nil {
		return nil, fmt.Errorf("read recovery paths: %w", err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.Path)
		if file.PreviousPath != "" {
			paths = append(paths, file.PreviousPath)
		}
	}
	return paths, nil
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
func (s *gitSession) applyWorkSnapshot(snapshot WorkSnapshot) error {
	output, err := s.cmd.New([]string{"git", "stash", "apply", "--index", snapshot.Hash}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("restore recovery: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
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
		if record.CanUndo() && record.Snapshot != nil {
			protected[record.Snapshot.ID] = true
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
