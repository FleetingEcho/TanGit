package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/stefanhaller/git-todo-parser/todo"
)

// CommitMove is a one-position move in newest-first history.
type CommitMove int

const (
	MoveCommitNewer CommitMove = -1
	MoveCommitOlder CommitMove = 1
)

// CombineMode chooses how a commit is folded into its older neighbor.
type CombineMode uint8

const (
	CombineSquash CombineMode = iota
	CombineFixup
)

// CommitMessage is the editable form of a commit message. Git treats everything
// after the subject and its following blank line as the description.
type CommitMessage struct {
	Summary     string
	Description string
}

// splitCommitMessage separates a full Git commit message into the fields used
// by the native message form. Keeping this independent of CommitMessage's Git
// read makes the parsing rules directly testable.
func splitCommitMessage(message string) CommitMessage {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.TrimSpace(message)
	summary, description, _ := strings.Cut(message, "\n")
	return CommitMessage{
		Summary:     strings.TrimSpace(summary),
		Description: strings.TrimSpace(description),
	}
}

// CommitMessage reads the complete message rather than only the subject carried
// by history rows, so opening the reword form cannot silently discard a body.
func (s *gitSession) CommitMessage(ctx context.Context, commit Commit) (CommitMessage, error) {
	if err := ctx.Err(); err != nil {
		return CommitMessage{}, err
	}
	message, err := s.git.Commit.GetCommitMessage(commit.Hash)
	if err != nil {
		return CommitMessage{}, fmt.Errorf("read commit message: %w", err)
	}
	return splitCommitMessage(message), nil
}

// RewordCommit changes a commit's message without incorporating staged changes.
// Rewriting anything below HEAD uses Lazygit's interactive-rebase commands; its
// early daemon entry point in the TanGit executable edits Git's todo file.
func (s *gitSession) RewordCommit(ctx context.Context, commit Commit, summary, description string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(summary) == "" {
		return fmt.Errorf("commit summary is required")
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	commits, index, err := s.editableHeadHistory(commit.Hash)
	if err != nil {
		return fmt.Errorf("reword commit: %w", err)
	}
	if models.IsHeadCommit(commits, index) {
		if err := s.git.Commit.RewordLastCommit(summary, description).Run(); err != nil {
			return fmt.Errorf("reword HEAD: %w", err)
		}
		return nil
	}
	return s.runHistoryRebase("Reword "+commit.ShortHash, func() error {
		if err := s.rewordCommit(commits, index, summary, description); err != nil {
			return fmt.Errorf("reword %s: %w", commit.ShortHash, err)
		}
		return nil
	})
}

// DropCommit removes the selected commit. Lazygit uses a dedicated todo edit
// for a merge commit because dropping its ordinary pick line would corrupt the
// merge-aware rebase plan.
func (s *gitSession) DropCommit(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	commits, index, err := s.editableHeadHistory(commit.Hash)
	if err != nil {
		return fmt.Errorf("drop commit: %w", err)
	}
	return s.runHistoryRebase("Drop "+commit.ShortHash, func() error {
		if commits[index].IsMerge() {
			err = s.dropMergeCommit(commits, index)
		} else {
			err = s.interactiveRebase(commits, index, index, todo.Drop, "")
		}
		if err != nil {
			return fmt.Errorf("drop %s: %w", commit.ShortHash, err)
		}
		return nil
	})
}

// MoveCommit reorders a non-merge commit by one position. The directions name
// history, not pixels: newer is toward HEAD and older is toward the root.
func (s *gitSession) MoveCommit(ctx context.Context, commit Commit, direction CommitMove) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if direction != MoveCommitNewer && direction != MoveCommitOlder {
		return fmt.Errorf("move commit: invalid direction %d", direction)
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	commits, index, err := s.editableHeadHistory(commit.Hash)
	if err != nil {
		return fmt.Errorf("move commit: %w", err)
	}
	if commits[index].IsMerge() {
		return fmt.Errorf("move commit: merge commits cannot be reordered")
	}
	summary := "Move " + commit.ShortHash + " toward HEAD"
	if direction == MoveCommitOlder {
		summary = "Move " + commit.ShortHash + " toward older history"
	}
	return s.runHistoryRebase(summary, func() error {
		switch direction {
		case MoveCommitNewer:
			if index == 0 {
				return fmt.Errorf("move commit: HEAD cannot move any newer")
			}
			err = s.moveCommits(commits, index, index, -1)
		case MoveCommitOlder:
			if index == len(commits)-1 {
				return fmt.Errorf("move commit: the root commit cannot move any older")
			}
			err = s.moveCommits(commits, index, index, 1)
		}
		if err != nil {
			return fmt.Errorf("move %s: %w", commit.ShortHash, err)
		}
		return nil
	})
}

// CombineCommit folds the selected non-merge commit into its older neighbor.
// Squash keeps both messages for Git to combine; fixup discards the selected
// commit's message.
func (s *gitSession) CombineCommit(ctx context.Context, commit Commit, mode CombineMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if mode != CombineSquash && mode != CombineFixup {
		return fmt.Errorf("combine commit: invalid mode %d", mode)
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	commits, index, err := s.editableHeadHistory(commit.Hash)
	if err != nil {
		return fmt.Errorf("combine commit: %w", err)
	}
	if commits[index].IsMerge() {
		return fmt.Errorf("combine commit: merge commits cannot be squashed or fixed up")
	}
	if index == len(commits)-1 {
		return fmt.Errorf("combine commit: the root commit has no older commit to combine with")
	}
	action := todo.Squash
	if mode == CombineFixup {
		action = todo.Fixup
	}
	summary := "Squash 2 commits"
	if mode == CombineFixup {
		summary = "Fix up 2 commits"
	}
	return s.runHistoryRebase(summary, func() error {
		if err := s.interactiveRebase(commits, index, index, action, ""); err != nil {
			return fmt.Errorf("combine %s: %w", commit.ShortHash, err)
		}
		return nil
	})
}

// AmendCommit folds staged changes into HEAD directly, or into an older commit
// through a fixup and autosquash-style rebase. Unstaged changes are left alone.
func (s *gitSession) AmendCommit(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if err := s.requireStagedChanges(); err != nil {
		return fmt.Errorf("amend commit: %w", err)
	}
	commits, index, err := s.editableHeadHistory(commit.Hash)
	if err != nil {
		return fmt.Errorf("amend commit: %w", err)
	}
	if models.IsHeadCommit(commits, index) {
		if err := s.git.Commit.AmendHead(); err != nil {
			return fmt.Errorf("amend %s: %w", commit.ShortHash, err)
		}
		return nil
	}
	return s.runHistoryRebase("Amend "+commit.ShortHash, func() error {
		if err := s.amendToCommit(commits, index); err != nil {
			return fmt.Errorf("amend %s: %w", commit.ShortHash, err)
		}
		return nil
	})
}

// CreateFixupCommit creates Git's standard fixup! commit for a later explicit
// autosquash, rather than immediately rewriting the selected commit.
func (s *gitSession) CreateFixupCommit(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.requireStagedChanges(); err != nil {
		return fmt.Errorf("create fixup commit: %w", err)
	}
	if _, _, err := s.editableHeadHistory(commit.Hash); err != nil {
		return fmt.Errorf("create fixup commit: %w", err)
	}
	if err := s.git.Commit.CreateFixupCommit(commit.Hash); err != nil {
		return fmt.Errorf("create fixup for %s: %w", commit.ShortHash, err)
	}
	return nil
}

// ApplyFixupCommits performs Git's autosquash from the selected commit through
// HEAD. Selecting the root therefore applies every fixup in the branch.
func (s *gitSession) ApplyFixupCommits(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	commits, index, err := s.editableHeadHistory(commit.Hash)
	if err != nil {
		return fmt.Errorf("apply fixup commits: %w", err)
	}
	return s.runHistoryRebase("Apply fixups from "+commit.ShortHash, func() error {
		if err := s.applyFixupCommits(commits[index]); err != nil {
			return fmt.Errorf("apply fixups from %s: %w", commit.ShortHash, err)
		}
		return nil
	})
}

// runHistoryRebase records the user-facing operation, not just Git's generic
// rebase reflog. Callers hold the worktree lock across preflight and this edit.
// Its before/after HEAD pair lets Undo identify this exact edit.
func (s *gitSession) runHistoryRebase(summary string, run func() error) error {
	record, err := s.beginOperation(OperationRebase, summary, UndoResetHead, nil)
	if err != nil {
		return err
	}
	if record, err = s.pinOperationBefore(record); err != nil {
		return err
	}
	return s.completeJournalOperation(record, run())
}

func (s *gitSession) requireStagedChanges() error {
	output, err := s.cmd.New([]string{"git", "diff", "--cached", "--name-only", "-z"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("inspect staged changes: %w", err)
	}
	if output == "" {
		return fmt.Errorf("stage changes first")
	}
	return nil
}

// editableHeadHistory returns fresh, unfiltered HEAD history and the selected
// commit's index. Lazygit's rebase commands use adjacent entries to calculate
// their base, so a filtered or bounded page could otherwise rewrite the wrong
// range. An unlimited HEAD-only read is deliberate and must not be replaced by
// the paged, all-ref history used by the workspace lists.
func (s *gitSession) editableHeadHistory(hash string) ([]*models.Commit, int, error) {
	if state := s.workingTreeState(); state.Any() {
		return nil, -1, fmt.Errorf("finish or abort the %s in progress first", state.Effective().CommandName())
	}
	if _, err := s.currentBranch(); err != nil {
		return nil, -1, err
	}
	if !s.headHasCommit() {
		return nil, -1, fmt.Errorf("repository has no commits")
	}
	commits, err := s.git.Loaders.CommitLoader.GetCommits(git_commands.GetCommitsOptions{
		RefName:      "HEAD",
		MainBranches: s.commitMainBranches,
		HashPool:     s.hashPool,
	})
	if err != nil {
		return nil, -1, fmt.Errorf("load HEAD history: %w", err)
	}
	for index, candidate := range commits {
		if candidate.Hash() == hash {
			return commits, index, nil
		}
	}
	return nil, -1, fmt.Errorf("commit %s is not in current HEAD history", hash)
}
