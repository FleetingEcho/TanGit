package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// AgentTargetStatus is an immutable-by-value comparison of one session's HEAD
// with a resolved base. Ahead counts commits in the target HEAD that are not in
// BaseSHA; Behind counts commits in BaseSHA that are not in the target HEAD.
// It describes commit divergence only, not whether an agent task is ready.
type AgentTargetStatus struct {
	HeadSHA string
	BaseSHA string
	Ahead   int
	Behind  int
}

// AgentTargetStatus reads only the exact session on which it is called. This
// matters for linked worktrees and remote repositories: no global repository
// state or currently selected tab is consulted. Both endpoints are frozen to
// full commit IDs before the symmetric rev-list range is evaluated.
func (s *gitSession) AgentTargetStatus(ctx context.Context, base string) (AgentTargetStatus, error) {
	if err := ctx.Err(); err != nil {
		return AgentTargetStatus{}, err
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return AgentTargetStatus{}, fmt.Errorf("agent target base is required")
	}

	headSHA, err := s.resolveAgentTargetCommit(ctx, "HEAD", "target HEAD")
	if err != nil {
		return AgentTargetStatus{}, err
	}
	baseSHA, err := s.resolveAgentTargetCommit(ctx, base, "base")
	if err != nil {
		return AgentTargetStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return AgentTargetStatus{}, err
	}

	stdout, stderr, err := s.cmd.New([]string{
		"git", "rev-list", "--left-right", "--count", "--end-of-options",
		headSHA + "..." + baseSHA, "--",
	}).RunWithOutputs()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AgentTargetStatus{}, ctxErr
		}
		if message := strings.TrimSpace(stderr); message != "" {
			return AgentTargetStatus{}, fmt.Errorf("count agent target divergence: %w: %s", err, message)
		}
		return AgentTargetStatus{}, fmt.Errorf("count agent target divergence: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return AgentTargetStatus{}, err
	}
	fields := strings.Fields(stdout)
	if len(fields) != 2 {
		return AgentTargetStatus{}, fmt.Errorf("count agent target divergence: unexpected output %q", strings.TrimSpace(stdout))
	}
	ahead, aheadErr := strconv.Atoi(fields[0])
	behind, behindErr := strconv.Atoi(fields[1])
	if aheadErr != nil || behindErr != nil || ahead < 0 || behind < 0 {
		return AgentTargetStatus{}, fmt.Errorf("count agent target divergence: unexpected counts %q", strings.TrimSpace(stdout))
	}
	return AgentTargetStatus{HeadSHA: headSHA, BaseSHA: baseSHA, Ahead: ahead, Behind: behind}, nil
}

func (s *gitSession) resolveAgentTargetCommit(ctx context.Context, ref, role string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stdout, stderr, err := s.cmd.New([]string{
		"git", "rev-parse", "--verify", "--end-of-options", ref + "^{commit}",
	}).RunWithOutputs()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if message := strings.TrimSpace(stderr); message != "" {
			return "", fmt.Errorf("resolve agent %s commit %s: %w: %s", role, ref, err, message)
		}
		return "", fmt.Errorf("resolve agent %s commit %s: %w", role, ref, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	hash := strings.TrimSpace(stdout)
	if hash == "" {
		return "", fmt.Errorf("resolve agent %s commit %s: empty commit ID", role, ref)
	}
	return hash, nil
}
