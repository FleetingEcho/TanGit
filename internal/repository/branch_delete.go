package repository

import (
	"fmt"
	"strings"
)

// deleteBranchAtTip preserves git branch -d's checked-out and merged checks,
// then deletes with an expected-old-value update. A branch advanced after the
// identity read cannot be deleted while the journal pins an older tip.
func (s *gitSession) deleteBranchAtTip(name, tip string, force bool) error {
	ref := "refs/heads/" + name
	listing, err := s.cmd.New([]string{"git", "worktree", "list", "--porcelain"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("check checked-out branches: %w: %s", err, strings.TrimSpace(listing))
	}
	for _, line := range strings.Split(listing, "\n") {
		if line == "branch "+ref {
			return fmt.Errorf("branch %s is checked out in a worktree", name)
		}
	}
	if !force && !s.branchTipMerged(tip, "HEAD") {
		upstream, upstreamErr := s.gitText("rev-parse", "--verify", ref+"@{upstream}")
		if upstreamErr != nil || !s.branchTipMerged(tip, strings.TrimSpace(upstream)) {
			return fmt.Errorf("branch %s is not fully merged; use force deletion to discard its commits", name)
		}
	}
	output, err := s.cmd.New([]string{"git", "update-ref", "-d", ref, tip}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("delete exact branch tip: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (s *gitSession) branchTipMerged(tip, base string) bool {
	return base != "" && s.cmd.New([]string{"git", "merge-base", "--is-ancestor", tip, base}).Run() == nil
}
