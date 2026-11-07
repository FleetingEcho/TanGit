package repository

import (
	"context"
	"fmt"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// CommitFilePatch returns the same first-parent, single-file comparison as
// CommitFileDiff, but with stable settings suitable for building and applying a
// patch. Display whitespace and context preferences deliberately do not apply.
func (s *gitSession) CommitFilePatch(ctx context.Context, commit Commit, file CommitFile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	from := models.EmptyTreeCommitHash
	if len(commit.Parents) > 0 {
		from = commit.Parents[0]
	}
	args := []string{
		"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv",
		"--binary", "--full-index", "--unified=3", "--submodule",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
		from, commit.Hash, "--", file.Path,
	}
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		args = append(args, file.PreviousPath)
	}
	patch, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("load canonical commit file patch: %w", err)
	}
	return patch, nil
}
