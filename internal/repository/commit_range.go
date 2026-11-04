package repository

import (
	"context"
	"fmt"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/jesseduffield/lazygit/pkg/utils"
	"github.com/stefanhaller/git-todo-parser/todo"
)

// CommitRangeAction chooses a single rebase operation over a linear selection.
type CommitRangeAction string

const (
	RangeSquash    CommitRangeAction = "squash"
	RangeFixup     CommitRangeAction = "fixup"
	RangeMoveNewer CommitRangeAction = "newer"
	RangeMoveOlder CommitRangeAction = "older"
	RangeDrop      CommitRangeAction = "drop"
)

// CherryPickCommits takes newest-first history and lets Git sequence the whole
// selection oldest-first, retaining the remaining sequence after a conflict.
func (s *gitSession) CherryPickCommits(ctx context.Context, selected []Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(selected) == 0 {
		return fmt.Errorf("select commits first")
	}
	if state := s.git.Status.WorkingTreeState(); state.Any() {
		return fmt.Errorf("finish or abort the %s in progress first", state.CommandName())
	}
	commits := make([]*models.Commit, 0, len(selected))
	seen := map[string]bool{}
	for _, c := range selected {
		if c.Hash == "" || seen[c.Hash] {
			return fmt.Errorf("invalid or duplicate commit")
		}
		seen[c.Hash] = true
		commits = append(commits, models.NewCommit(&utils.StringPool{}, models.NewCommitOpts{Hash: c.Hash, Parents: append([]string(nil), c.Parents...)}))
	}
	return s.git.Rebase.CherryPickCommits(commits)
}

// EditCommitRange resolves hashes against fresh HEAD history. A visible filtered
// range may hide commits, so adjacency must be checked here before rewriting.
func (s *gitSession) EditCommitRange(ctx context.Context, selected []Commit, action CommitRangeAction) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(selected) < 2 {
		return fmt.Errorf("select at least two commits")
	}
	commits, _, err := s.editableHeadHistory(selected[0].Hash)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, c := range selected {
		if wanted[c.Hash] {
			return fmt.Errorf("duplicate commit")
		}
		wanted[c.Hash] = true
	}
	first, last := -1, -1
	for i, c := range commits {
		if wanted[c.Hash()] {
			if first < 0 {
				first = i
			}
			last = i
			delete(wanted, c.Hash())
		}
	}
	if len(wanted) > 0 || first < 0 || last-first+1 != len(selected) {
		return fmt.Errorf("select consecutive commits in current HEAD history")
	}
	// Ranges must also form a linear chain; date-order neighbors can be siblings.
	for i := first; i <= last; i++ {
		if commits[i].IsMerge() {
			return fmt.Errorf("merge commits cannot be edited as a range")
		}
		if i < last && (len(commits[i].Parents()) == 0 || commits[i].Parents()[0] != commits[i+1].Hash()) {
			return fmt.Errorf("select a linear commit range")
		}
	}
	switch action {
	case RangeSquash, RangeFixup:
		cmd := todo.Squash
		if action == RangeFixup {
			cmd = todo.Fixup
		}
		// Retain the oldest selected commit, folding only its selected successors.
		return s.git.Rebase.InteractiveRebase(commits, first, last-1, cmd, "")
	case RangeDrop:
		return s.git.Rebase.InteractiveRebase(commits, first, last, todo.Drop, "")
	case RangeMoveNewer:
		if first == 0 {
			return fmt.Errorf("selection is already at HEAD")
		}
		if commits[first-1].IsMerge() {
			return fmt.Errorf("cannot move across a merge commit")
		}
		return s.git.Rebase.MoveCommitsUp(commits, first, last)
	case RangeMoveOlder:
		if last == len(commits)-1 {
			return fmt.Errorf("selection includes the root commit")
		}
		if commits[last+1].IsMerge() {
			return fmt.Errorf("cannot move across a merge commit")
		}
		return s.git.Rebase.MoveCommitsDown(commits, first, last)
	default:
		return fmt.Errorf("unknown range action %q", action)
	}
}
