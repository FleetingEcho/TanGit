package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// captureRawWorkSnapshot constructs stash-shaped parents without asking git
// add to resolve an unmerged index or requiring a tracked change. The real
// index is untouched. An unborn symbolic HEAD gets a synthetic empty base;
// staged files and raw payloads are still retained by their explicit trees.
func (s *gitSession) captureRawWorkSnapshot(ctx context.Context, name string, archive *workspaceArchive) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	env := []string{"GIT_AUTHOR_NAME=lazygitGo", "GIT_AUTHOR_EMAIL=recovery@localhost", "GIT_COMMITTER_NAME=lazygitGo", "GIT_COMMITTER_EMAIL=recovery@localhost"}
	base, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		symbolic, symbolicErr := s.gitText("symbolic-ref", "--quiet", "HEAD")
		if symbolicErr != nil || symbolic == "" {
			return "", fmt.Errorf("capture workspace without a usable HEAD: %w", err)
		}
		var emptyTree string
		emptyTree, err = s.writeWorkspaceTree(newWorkspaceTreeNode())
		if err == nil {
			base, err = s.cmd.New([]string{"git", "commit-tree", emptyTree, "-m", "lazygitGo empty recovery base"}).AddEnvVars(env...).RunWithOutput()
		}
		if err != nil {
			return "", fmt.Errorf("capture unborn workspace: %w", err)
		}
	}
	base = strings.TrimSpace(base)
	indexName, removeIndex, err := s.temporaryFile("lazygitgo-workspace-index-", nil, 0o600)
	if err != nil {
		return "", err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	env = append([]string{"GIT_INDEX_FILE=" + indexName}, env...)
	var stageZero strings.Builder
	for _, entry := range archive.Index {
		if entry.Stage == 0 {
			stageZero.WriteString(entry.Raw)
		}
	}
	if err := s.cmd.New([]string{"git", "update-index", "-z", "--index-info"}).AddEnvVars(env...).SetStdin(stageZero.String()).Run(); err != nil {
		return "", fmt.Errorf("capture conflict index: %w", err)
	}
	indexTree, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("capture conflict index tree: %w: %s", err, strings.TrimSpace(indexTree))
	}
	indexTree = strings.TrimSpace(indexTree)
	indexCommit, err := s.cmd.New([]string{"git", "commit-tree", indexTree, "-p", base, "-m", "lazygitGo conflicted index snapshot"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("capture conflict index commit: %w: %s", err, strings.TrimSpace(indexCommit))
	}
	workingTree, err := s.writeWorkspaceTree(archive.Worktree)
	if err != nil {
		return "", err
	}
	parents := []string{base, strings.TrimSpace(indexCommit)}
	if len(archive.Untracked.entries) > 0 || len(archive.Untracked.dirs) > 0 {
		untrackedTree, err := s.writeWorkspaceTree(archive.Untracked)
		if err != nil {
			return "", err
		}
		untrackedCommit, err := s.cmd.New([]string{"git", "commit-tree", untrackedTree, "-m", "lazygitGo untracked snapshot"}).AddEnvVars(env...).RunWithOutput()
		if err != nil {
			return "", fmt.Errorf("capture conflict untracked snapshot: %w: %s", err, strings.TrimSpace(untrackedCommit))
		}
		parents = append(parents, strings.TrimSpace(untrackedCommit))
	}
	args := []string{"git", "commit-tree", workingTree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	hash, err := s.cmd.New(args).AddEnvVars(env...).SetStdin(name).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("capture conflicted worktree: %w: %s", err, strings.TrimSpace(hash))
	}
	return strings.TrimSpace(hash), nil
}
