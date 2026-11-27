package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// RepoSyncStatus is the cheap state the Overview shows beside a repository: how
// far the checked-out branch is from its upstream and whether tracked files are
// modified. DirtyKnown is false when the working tree could not be checked in
// time, which is common in very large repositories and is not an error.
type RepoSyncStatus struct {
	HasUpstream bool
	Ahead       int
	Behind      int
	Dirty       bool
	DirtyKnown  bool
}

// OverviewSyncStatus reads ahead/behind with one rev-list count, which never
// walks the working tree, then checks tracked modifications. Untracked files
// are ignored on purpose: finding them is the expensive part of git status.
func (s *gitSession) OverviewSyncStatus(ctx context.Context) (RepoSyncStatus, error) {
	var status RepoSyncStatus
	if err := ctx.Err(); err != nil {
		return status, err
	}
	command, err := s.commandContext(ctx, []string{"git", "rev-list", "--left-right", "--count", "@{u}...HEAD"})
	if err != nil {
		return status, err
	}
	if output, runErr := s.runWithOutputContext(ctx, command); runErr == nil {
		fields := strings.Fields(output)
		if len(fields) == 2 {
			behind, behindErr := strconv.Atoi(fields[0])
			ahead, aheadErr := strconv.Atoi(fields[1])
			if behindErr == nil && aheadErr == nil {
				status.HasUpstream, status.Behind, status.Ahead = true, behind, ahead
			}
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return status, ctxErr
	}
	command, err = s.commandContext(ctx, []string{"git", "status", "--porcelain", "--untracked-files=no"})
	if err != nil {
		return status, nil
	}
	if output, runErr := s.runWithOutputContext(ctx, command); runErr == nil {
		status.Dirty, status.DirtyKnown = strings.TrimSpace(output) != "", true
	}
	return status, nil
}

// CommitPatch returns one commit as a mailbox-format patch, the same text
// git format-patch writes, so it can be sent or applied elsewhere.
func (s *gitSession) CommitPatch(ctx context.Context, commit Commit) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	command, err := s.commandContext(ctx, []string{"git", "format-patch", "-1", "--stdout", commit.Hash})
	if err != nil {
		return "", err
	}
	output, err := s.runWithOutputContext(ctx, command)
	if err != nil {
		return "", fmt.Errorf("format patch %s: %w", commit.ShortHash, err)
	}
	return output, nil
}
