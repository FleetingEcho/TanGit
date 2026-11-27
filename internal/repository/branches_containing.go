package repository

import (
	"context"
	"fmt"
	"strings"
)

// BranchesContaining names the local branches that hold commit: first the ones
// whose tip it is, then the others that reach it through their history.
func (s *gitSession) BranchesContaining(ctx context.Context, commit Commit) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	read := func(args ...string) ([]string, error) {
		command, err := s.commandContext(ctx, append([]string{"git", "for-each-ref", "--format=%(refname:short)"}, args...))
		if err != nil {
			return nil, err
		}
		output, err := s.runWithOutputContext(ctx, command)
		if err != nil {
			return nil, fmt.Errorf("find branches at %s: %w", commit.ShortHash, err)
		}
		return strings.Fields(output), nil
	}
	tips, err := read("--points-at", commit.Hash, "refs/heads")
	if err != nil {
		return nil, err
	}
	contained, err := read("--contains", commit.Hash, "refs/heads")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(tips))
	names := make([]string, 0, len(contained))
	for _, name := range append(tips, contained...) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, nil
}
