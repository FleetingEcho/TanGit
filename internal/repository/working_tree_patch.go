package repository

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// WorkingTreeFilePatch returns one canonical, apply-ready patch for the
// selected side of a working-tree file. Display whitespace and context
// preferences deliberately do not affect patch contents or line coordinates.
func (s *gitSession) WorkingTreeFilePatch(ctx context.Context, file File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if file.HasMergeConflicts {
		return "", fmt.Errorf("load canonical working-tree file patch: resolve merge conflicts in %s first", file.Path)
	}

	if !file.Tracked && !file.Staged {
		return s.untrackedFilePatch(file)
	}

	args := []string{
		"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv",
		"--binary", "--full-index", "--unified=3", "--submodule",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
	}
	if file.Staged {
		base := "HEAD"
		if err := s.cmd.New([]string{"git", "rev-parse", "--verify", "--quiet", "HEAD"}).Run(); err != nil {
			// Only a symbolic branch with no commit is an unborn HEAD. A damaged
			// or unreadable HEAD must not silently become an empty-tree diff.
			if symbolicErr := s.cmd.New([]string{"git", "symbolic-ref", "--quiet", "--short", "HEAD"}).Run(); symbolicErr != nil {
				return "", fmt.Errorf("load canonical working-tree file patch: read HEAD: %w", err)
			}
			base = models.EmptyTreeCommitHash
		}
		if !file.Unstaged {
			args = append(args, "--cached")
		}
		// With both sides selected, comparing HEAD (or an unborn branch's
		// empty tree) directly with the worktree includes the index and final
		// working-tree edits without stitching overlapping side patches.
		args = append(args, base)
	}
	args = append(args, "--")
	for _, path := range fileNames(file) {
		if _, err := worktreeFilePath(s.git.RepoPaths.WorktreePath(), path); err != nil {
			return "", fmt.Errorf("load canonical working-tree file patch: %w", err)
		}
		// A literal pathspec keeps wildcard and pathspec-magic characters in
		// real filenames from selecting other files. "--" stops option parsing.
		args = append(args, ":(literal)"+path)
	}
	patch, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("load canonical working-tree file patch: %w: %s", err, strings.TrimSpace(patch))
	}
	return patch, nil
}

// An untracked path has no index side for git diff to find. A no-index
// comparison supplies its /dev/null-to-file addition, including a new-file
// header for an empty file and a literal binary patch for binary contents.
func (s *gitSession) untrackedFilePatch(file File) (string, error) {
	if _, err := worktreeFilePath(s.git.RepoPaths.WorktreePath(), file.Path); err != nil {
		return "", fmt.Errorf("load canonical untracked file patch: %w", err)
	}
	command := backgroundExec(s.cmd.New([]string{
		"git", "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv",
		"--binary", "--full-index", "--unified=3", "--submodule",
		"--", "/dev/null", file.Path,
	}))
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		// Git uses status 1 for an ordinary difference between the two paths.
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			stderr := ""
			if exitError != nil {
				stderr = string(exitError.Stderr)
			}
			return "", fmt.Errorf("load canonical untracked file patch: %w: %s", err, strings.TrimSpace(stderr))
		}
	}
	return string(output), nil
}
