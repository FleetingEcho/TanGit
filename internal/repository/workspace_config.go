package repository

import (
	"fmt"
	"strings"
)

// Common/worktree configuration can contain upstreams, branch rename data and
// per-worktree behavior. It is part of the operation state, but credentials or
// remote URLs are not exposed through remote compensation plans.
func (s *gitSession) commonGitDirectory() (string, error) {
	output, err := s.cmd.New([]string{"git", "rev-parse", "--path-format=absolute", "--git-common-dir"}).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("locate common Git configuration: %w: %s", err, strings.TrimSpace(output))
	}
	root := strings.TrimSuffix(output, "\n")
	if root == "" {
		return "", fmt.Errorf("common Git directory is empty")
	}
	return root, nil
}

func (s *gitSession) repositoryStateFilePath(name string) (string, error) {
	switch {
	case name == "config/common":
		root, err := s.commonGitDirectory()
		if err != nil {
			return "", err
		}
		return s.joinPath(root, "config"), nil
	case name == "config/worktree/config" || name == "config/worktree/config.worktree":
		return s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), strings.TrimPrefix(name, "config/worktree/")), nil
	default:
		return s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), name), nil
	}
}
