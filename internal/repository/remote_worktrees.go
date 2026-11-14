package repository

import (
	"fmt"
	"os"
	pathpkg "path"
	"strconv"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/jesseduffield/lazygit/pkg/utils"
)

// sessionWorktrees keeps local behavior unchanged and replaces every local
// stat/read in Lazygit's worktree loader when the repository is over SSH.
func (s *gitSession) sessionWorktrees() ([]*models.Worktree, map[string]worktreeMetadata, error) {
	if s.sshClient == nil {
		worktrees, err := s.git.Loaders.Worktrees.GetWorktrees()
		if err != nil {
			return nil, nil, err
		}
		metadata, err := s.sessionWorktreeMetadata()
		return worktrees, metadata, err
	}
	output, err := s.cmd.New([]string{"git", "worktree", "list", "--porcelain"}).DontLog().RunWithOutput()
	if err != nil {
		return nil, nil, err
	}
	worktrees := parseSessionWorktrees(output)
	metadata := parseWorktreeMetadata(output)
	for _, worktree := range worktrees {
		if _, statErr := s.fileService().Stat(worktree.Path); statErr != nil {
			worktree.IsPathMissing = os.IsNotExist(statErr)
		}
		if worktree.IsPathMissing {
			continue
		}
		gitDir, gitDirErr := s.cmd.New([]string{"git", "-C", worktree.Path, "rev-parse", "--absolute-git-dir"}).DontLog().RunWithOutput()
		if gitDirErr == nil {
			worktree.GitDir = strings.TrimSpace(gitDir)
		}
	}
	for _, worktree := range worktrees {
		if worktree.GitDir != "" {
			worktree.IsCurrent = worktree.GitDir == s.git.RepoPaths.WorktreeGitDirPath()
			worktree.IsMain = worktree.GitDir == s.git.RepoPaths.RepoGitDirPath()
		} else {
			worktree.IsCurrent = worktree.Path == s.git.RepoPaths.WorktreePath()
			worktree.IsMain = worktree.Path == s.git.RepoPaths.RepoPath()
		}
	}
	uniqueSessionWorktreeNames(worktrees)
	for index, worktree := range worktrees {
		if worktree.IsCurrent {
			copy(worktrees[1:index+1], worktrees[0:index])
			worktrees[0] = worktree
			break
		}
	}
	for _, worktree := range worktrees {
		if worktree.Branch != "" || worktree.GitDir == "" {
			continue
		}
		for _, dir := range []string{"rebase-merge", "rebase-apply"} {
			content, readErr := s.fileService().ReadFile(s.joinPath(worktree.GitDir, dir, "head-name"))
			if readErr == nil {
				worktree.Branch = strings.TrimPrefix(strings.TrimSpace(string(content)), "refs/heads/")
				worktree.IsRebasingOrBisecting = true
				break
			}
		}
		if worktree.Branch == "" {
			if content, readErr := s.fileService().ReadFile(s.joinPath(worktree.GitDir, "BISECT_START")); readErr == nil {
				worktree.Branch = strings.TrimSpace(string(content))
				worktree.IsRebasingOrBisecting = true
			}
		}
	}
	return worktrees, metadata, nil
}

func parseSessionWorktrees(output string) []*models.Worktree {
	var worktrees []*models.Worktree
	var current *models.Worktree
	for _, line := range strings.Split(utils.NormalizeLinefeeds(output), "\n") {
		if line == "" && current != nil {
			worktrees = append(worktrees, current)
			current = nil
			continue
		}
		if line == "bare" {
			current = nil
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = &models.Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case current != nil && strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case current != nil && strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if current != nil {
		worktrees = append(worktrees, current)
	}
	return worktrees
}

type worktreeMetadata struct {
	Locked      bool
	LockReason  string
	Prunable    bool
	PruneReason string
}

type worktreeRuntimeStatus struct {
	Staged       int
	Modified     int
	Untracked    int
	Ahead        int
	Behind       int
	LastCommitAt int64
	LastSubject  string
}

func (s *gitSession) sessionWorktreeStatuses(worktrees []*models.Worktree) map[string]worktreeRuntimeStatus {
	statuses := make(map[string]worktreeRuntimeStatus, len(worktrees))
	for _, worktree := range worktrees {
		if worktree.IsPathMissing || strings.TrimSpace(worktree.Path) == "" {
			continue
		}
		var status worktreeRuntimeStatus
		stdout, _, err := s.cmd.New([]string{
			"git", "-C", worktree.Path, "status", "--porcelain=v1", "--branch", "--untracked-files=all",
		}).DontLog().RunWithOutputs()
		if err == nil {
			parseWorktreeStatus(stdout, &status)
		}
		stdout, _, err = s.cmd.New([]string{
			"git", "-C", worktree.Path, "log", "-1", "--format=%ct%x00%s",
		}).DontLog().RunWithOutputs()
		if err == nil {
			parts := strings.SplitN(strings.TrimSpace(stdout), "\x00", 2)
			if len(parts) > 0 {
				status.LastCommitAt, _ = strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			}
			if len(parts) == 2 {
				status.LastSubject = strings.TrimSpace(parts[1])
			}
		}
		statuses[worktree.Path] = status
	}
	return statuses
}

func parseWorktreeStatus(output string, status *worktreeRuntimeStatus) {
	for _, line := range strings.Split(utils.NormalizeLinefeeds(output), "\n") {
		if strings.HasPrefix(line, "## ") {
			if bracket := strings.IndexByte(line, '['); bracket >= 0 {
				line = line[bracket+1:]
			}
			for _, part := range strings.Split(line, ",") {
				part = strings.Trim(part, " []")
				if value := strings.TrimPrefix(part, "ahead "); value != part {
					status.Ahead, _ = strconv.Atoi(value)
				}
				if value := strings.TrimPrefix(part, "behind "); value != part {
					status.Behind, _ = strconv.Atoi(value)
				}
			}
			continue
		}
		if len(line) < 2 {
			continue
		}
		if line[:2] == "??" {
			status.Untracked++
			continue
		}
		if line[0] != ' ' {
			status.Staged++
		}
		if line[1] != ' ' {
			status.Modified++
		}
	}
}

func (s *gitSession) sessionWorktreeMetadata() (map[string]worktreeMetadata, error) {
	output, err := s.cmd.New([]string{"git", "worktree", "list", "--porcelain"}).DontLog().RunWithOutput()
	if err != nil {
		return nil, err
	}
	return parseWorktreeMetadata(output), nil
}

func parseWorktreeMetadata(output string) map[string]worktreeMetadata {
	metadata := map[string]worktreeMetadata{}
	path := ""
	current := worktreeMetadata{}
	flush := func() {
		if path != "" {
			metadata[path] = current
		}
		path = ""
		current = worktreeMetadata{}
	}
	for _, line := range strings.Split(utils.NormalizeLinefeeds(output), "\n") {
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			path = strings.TrimPrefix(line, "worktree ")
		case line == "locked":
			current.Locked = true
		case strings.HasPrefix(line, "locked "):
			current.Locked = true
			current.LockReason = strings.TrimPrefix(line, "locked ")
		case line == "prunable":
			current.Prunable = true
		case strings.HasPrefix(line, "prunable "):
			current.Prunable = true
			current.PruneReason = strings.TrimPrefix(line, "prunable ")
		}
	}
	flush()
	return metadata
}

func uniqueSessionWorktreeNames(worktrees []*models.Worktree) {
	used := map[string]int{}
	for _, worktree := range worktrees {
		base := pathpkg.Base(pathpkg.Clean(worktree.Path))
		if base == "." || base == "/" || base == "" {
			base = "worktree"
		}
		used[base]++
		name := base
		if used[base] > 1 {
			name = fmt.Sprintf("%s-%d", base, used[base])
		}
		worktree.Name = name
	}
}
