package repository

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Contributor is one person who authored commits in the repository's history,
// identified by email (or by name when a commit has none).
type Contributor struct {
	Name  string
	Email string
	// Commits counts the commits they authored across every branch.
	Commits int
	// FirstCommit and LastCommit are Unix timestamps of their oldest and newest
	// authored commit.
	FirstCommit int64
	LastCommit  int64
	// CommitTimes are the Unix timestamps of their commits, oldest first, so
	// any time range can be counted without reading Git again.
	CommitTimes []int64
}

// ContributorSummary is the contributor list with the totals of the history it
// was read from. Each contributor carries their commit times, so the caller can
// count any period itself.
type ContributorSummary struct {
	Contributors []Contributor
	TotalCommits int
	FirstCommit  int64
	LastCommit   int64
}

// contributorCommitLimit bounds the history read, so a very large repository
// cannot make the contributor list a long scan.
const contributorCommitLimit = 200000

// Contributors lists everyone who authored commits on any branch, most commits
// first. Several names under one email are one contributor, shown under their
// newest name.
func (s *gitSession) Contributors(ctx context.Context) ([]Contributor, error) {
	summary, err := s.ContributorSummary(ctx, "")
	return summary.Contributors, err
}

// ContributorSummary reads the contributors and their commit times in one pass
// over the history of branch, or of every branch when branch is empty. A
// branch's history can be read without checking it out.
func (s *gitSession) ContributorSummary(ctx context.Context, branch string) (ContributorSummary, error) {
	return s.ContributorSummarySince(ctx, branch, 0)
}

// ContributorSummarySince is ContributorSummary limited to commits made at or
// after the Unix time since (0 for the whole history), so a short period of a
// huge history is a short read.
func (s *gitSession) ContributorSummarySince(ctx context.Context, branch string, since int64) (ContributorSummary, error) {
	if err := ctx.Err(); err != nil {
		return ContributorSummary{}, err
	}
	branch = strings.TrimSpace(branch)
	if strings.HasPrefix(branch, "-") {
		return ContributorSummary{}, fmt.Errorf("read contributors: invalid branch %q", branch)
	}
	args := []string{"git", "log", fmt.Sprintf("--max-count=%d", contributorCommitLimit), "--format=%aN%x1f%aE%x1f%at"}
	if since > 0 {
		args = append(args, fmt.Sprintf("--since=%d", since))
	}
	if branch == "" {
		args = append(args, "--all")
	} else {
		args = append(args, s.qualifyCommitFilterBranch(branch))
	}
	args = append(args, "--")
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return ContributorSummary{}, fmt.Errorf("read contributors: %w", err)
	}
	return summarizeContributors(output), nil
}

// summarizeContributors aggregates `git log` output into the summary.
func summarizeContributors(output string) ContributorSummary {
	summary := ContributorSummary{Contributors: parseContributors(output)}
	for _, contributor := range summary.Contributors {
		summary.TotalCommits += contributor.Commits
		if contributor.LastCommit > summary.LastCommit {
			summary.LastCommit = contributor.LastCommit
		}
		if contributor.FirstCommit != 0 && (summary.FirstCommit == 0 || contributor.FirstCommit < summary.FirstCommit) {
			summary.FirstCommit = contributor.FirstCommit
		}
	}
	return summary
}

// parseContributors aggregates `git log` lines of name, email and timestamp,
// newest first, into contributors.
func parseContributors(output string) []Contributor {
	type entry struct {
		Contributor
		nameAt int64
	}
	byKey := map[string]*entry{}
	var order []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\x1f")
		if len(fields) != 3 {
			continue
		}
		name, email := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		at, _ := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		key := strings.ToLower(email)
		if key == "" {
			key = "name:" + strings.ToLower(name)
		}
		if name == "" && email == "" {
			continue
		}
		current, ok := byKey[key]
		if !ok {
			current = &entry{Contributor: Contributor{Name: name, Email: email, FirstCommit: at, LastCommit: at}, nameAt: at}
			byKey[key] = current
			order = append(order, key)
		}
		current.Commits++
		current.CommitTimes = append(current.CommitTimes, at)
		if at > current.LastCommit {
			current.LastCommit = at
		}
		if at != 0 && (current.FirstCommit == 0 || at < current.FirstCommit) {
			current.FirstCommit = at
		}
		if name != "" && at >= current.nameAt {
			current.Name, current.nameAt = name, at
		}
	}
	contributors := make([]Contributor, 0, len(order))
	for _, key := range order {
		contributor := byKey[key].Contributor
		sort.Slice(contributor.CommitTimes, func(i, j int) bool { return contributor.CommitTimes[i] < contributor.CommitTimes[j] })
		contributors = append(contributors, contributor)
	}
	sort.SliceStable(contributors, func(i, j int) bool {
		if contributors[i].Commits != contributors[j].Commits {
			return contributors[i].Commits > contributors[j].Commits
		}
		return strings.ToLower(contributors[i].Name) < strings.ToLower(contributors[j].Name)
	})
	return contributors
}
