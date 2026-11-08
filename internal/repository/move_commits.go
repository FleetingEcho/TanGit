package repository

import (
	"context"
	"fmt"
	"strings"
)

// MoveUnpushedCommits moves the commits unique to the current branch onto a
// new branch, leaving the current branch pointing at the fork point shared
// with its upstream (or with the repository's default branch). It reports
// how many commits moved.
//
// The working tree must have no staged or unstaged changes; untracked files
// are carried over untouched. The old branch is not deleted or renamed.
func (s *gitSession) MoveUnpushedCommits(ctx context.Context, newBranch string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	name := strings.TrimSpace(newBranch)
	if name == "" {
		return 0, fmt.Errorf("move commits: a branch name is required")
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()

	current, err := s.currentBranch()
	if err != nil {
		return 0, err
	}
	if err := s.cmd.New([]string{"git", "check-ref-format", "--branch", name}).DontLog().Run(); err != nil {
		return 0, fmt.Errorf("move commits: %q is not a valid branch name", name)
	}
	if err := s.cmd.New([]string{"git", "show-ref", "--verify", "--quiet", "refs/heads/" + name}).DontLog().Run(); err == nil {
		return 0, fmt.Errorf("move commits: branch %s already exists", name)
	}
	// Uncommitted work on tracked files would not survive the reset.
	// Untracked files carry over, so they are allowed.
	if dirty, err := s.hasTrackedChanges(ctx); err != nil {
		return 0, err
	} else if dirty {
		return 0, fmt.Errorf("move commits: commit or stash your changes first")
	}

	base, err := s.unpushedBase(ctx, current.Name)
	if err != nil {
		return 0, err
	}
	countOut, err := s.cmd.New([]string{"git", "rev-list", "--count", base + "..HEAD"}).DontLog().RunWithOutput()
	if err != nil {
		return 0, fmt.Errorf("move commits: %w", err)
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(countOut), "%d", &count); err != nil || count == 0 {
		return 0, fmt.Errorf("move commits: %s has no unpushed commits to move", current.Name)
	}

	// Preserve where HEAD is by switching to a new branch at that exact
	// commit first. Never reset the worktree back to base: if HEAD deleted a
	// tracked file found in base's tree, a hard reset could overwrite a new
	// untracked file now occupying that path. Only tracked-dirty changes are
	// rejected above; untracked work deliberately stays on disk untouched.
	head, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return 0, fmt.Errorf("move commits: resolve HEAD: %w", err)
	}
	head = strings.TrimSpace(head)
	if out, err := s.cmd.New([]string{"git", "branch", name, head}).RunWithOutput(); err != nil {
		return 0, fmt.Errorf("move commits: %w%s", err, commandOutputSummary(out))
	}
	if out, err := s.cmd.New([]string{"git", "checkout", name}).RunWithOutput(); err != nil {
		return 0, fmt.Errorf("move commits: %w%s", err, commandOutputSummary(out))
	}
	// The expected-old-object form is atomic: another client cannot race the
	// old branch to a different tip between our fork-point read and this move.
	if out, err := s.cmd.New([]string{"git", "update-ref", "refs/heads/" + current.Name, base, head}).RunWithOutput(); err != nil {
		return 0, fmt.Errorf("move commits: %w%s", err, commandOutputSummary(out))
	}
	return count, nil
}

// unpushedBase finds the commit the current branch diverged from its
// upstream, or from the repository's default branch when it has no upstream.
func (s *gitSession) unpushedBase(ctx context.Context, branch string) (string, error) {
	mergeBase := func(ref string) (string, error) {
		output, err := s.cmd.New([]string{"git", "merge-base", ref, "HEAD"}).DontLog().RunWithOutput()
		if err != nil {
			return "", err
		}
		base := strings.TrimSpace(output)
		if base == "" {
			return "", fmt.Errorf("no common ancestor with %s", ref)
		}
		return base, nil
	}
	if upstream, err := s.cmd.New([]string{"git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch + "@{u}"}).DontLog().RunWithOutput(); err == nil {
		return mergeBase(strings.TrimSpace(upstream))
	}
	for _, candidate := range []string{"origin/HEAD", "main", "master", "trunk"} {
		if err := s.cmd.New([]string{"git", "rev-parse", "--verify", "--quiet", candidate}).DontLog().Run(); err != nil {
			continue
		}
		return mergeBase(candidate)
	}
	return "", fmt.Errorf("cannot determine where %s diverged; set an upstream first", branch)
}

// hasTrackedChanges reports whether tracked files differ from the index or HEAD.
func (s *gitSession) hasTrackedChanges(ctx context.Context) (bool, error) {
	output, err := s.cmd.New([]string{"git", "status", "--porcelain", "--untracked-files=no"}).DontLog().RunWithOutput()
	if err != nil {
		return false, fmt.Errorf("check working tree: %w", err)
	}
	return strings.TrimSpace(output) != "", nil
}
