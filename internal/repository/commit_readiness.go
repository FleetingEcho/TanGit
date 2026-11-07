package repository

import (
	"fmt"
	"strings"
)

// commitReadiness performs fresh, read-only checks immediately before every
// commit attempt. Failures happen before Git starts a hook or changes a ref.
func (s *gitSession) commitReadiness(amend bool) error {
	output, err := s.cmd.New([]string{"git", "ls-files", "-u", "-z"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("check unresolved conflicts: %w: %s", err, strings.TrimSpace(output))
	}
	if output != "" {
		return fmt.Errorf("resolve merge conflicts before committing")
	}
	if state := s.git.Status.WorkingTreeState(); state.Any() {
		return fmt.Errorf("finish or abort the %s in progress first", state.CommandName())
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
