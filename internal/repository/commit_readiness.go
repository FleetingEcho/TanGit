package repository

import (
	"fmt"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// commitReadiness performs fresh, read-only checks immediately before every
// commit attempt. Failures happen before Git starts a hook or changes a ref.
// commandBlockingCommit names the operation that must finish or abort before
// an ordinary commit. Squash merging alone instead needs that commit to finish.
func commandBlockingCommit(state models.WorkingTreeState) string {
	state.SquashMerging = false
	return state.Effective().CommandName()
}

func (s *gitSession) commitReadiness(amend bool) error {
	output, err := s.cmd.New([]string{"git", "ls-files", "-u", "-z"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("check unresolved conflicts: %w: %s", err, strings.TrimSpace(output))
	}
	if output != "" {
		return fmt.Errorf("resolve merge conflicts before committing")
	}
	if command := commandBlockingCommit(s.workingTreeState()); command != "" {
		// An interactive rebase edit-stop is deliberately amendable at HEAD;
		// continue with the ordinary staged changes. Other interrupted
		// operations still must be finished or aborted first.
		if !amend || command != "rebase" {
			return fmt.Errorf("finish or abort the %s in progress first", command)
		}
	}
	for _, identity := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		output, err := s.cmd.New([]string{"git", "var", identity}).RunWithOutput()
		if err != nil {
			return fmt.Errorf("configure Git author name and email before committing: %w: %s", err, strings.TrimSpace(output))
		}
	}
	if !amend {
		if err := s.requireStagedChanges(); err != nil {
			return err
		}
	}
	return nil
}
