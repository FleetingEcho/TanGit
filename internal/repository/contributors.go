package repository

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
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
}

// ContributorSummary is the contributor list with statistics of the same
// history read: totals, how many people were recently active, and the commits
// per week for the last contributorWeeks weeks, oldest first.
type ContributorSummary struct {
	Contributors  []Contributor
	TotalCommits  int
	ActiveRecent  int
	WeeklyCommits []int
	FirstCommit   int64
	LastCommit    int64
}

const (
	// ContributorWeeks is how many weeks the weekly commit series covers.
	ContributorWeeks = 12
	// contributorRecentDays is the window that counts a contributor as active.
	contributorRecentDays = 30
)

// contributorCommitLimit bounds the history read, so a very large repository
// cannot make the contributor list a long scan.
const contributorCommitLimit = 200000

// Contributors lists everyone who authored commits, most commits first. Several
// names under one email are one contributor, shown under their newest name.
func (s *gitSession) Contributors(ctx context.Context) ([]Contributor, error) {
	summary, err := s.ContributorSummary(ctx)
	return summary.Contributors, err
}

// ContributorSummary reads the contributors and their statistics in one pass
// over the history.
func (s *gitSession) ContributorSummary(ctx context.Context) (ContributorSummary, error) {
	if err := ctx.Err(); err != nil {
		return ContributorSummary{}, err
	}
	output, err := s.cmd.New([]string{
		"git", "log", "--all", fmt.Sprintf("--max-count=%d", contributorCommitLimit),
		"--format=%aN%x1f%aE%x1f%at",
	}).RunWithOutput()
	if err != nil {
		return ContributorSummary{}, fmt.Errorf("read contributors: %w", err)
	}
	return summarizeContributors(output, time.Now()), nil
}

// summarizeContributors aggregates `git log` output into the summary, counting
// weeks back from now.
func summarizeContributors(output string, now time.Time) ContributorSummary {
	summary := ContributorSummary{
		Contributors:  parseContributors(output),
		WeeklyCommits: make([]int, ContributorWeeks),
	}
	recent := map[string]bool{}
	weekSeconds := int64(7 * 24 * 3600)
	nowUnix := now.Unix()
	cutoff := nowUnix - contributorRecentDays*24*3600
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\x1f")
		if len(fields) != 3 {
			continue
		}
		name, email := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if name == "" && email == "" {
			continue
		}
		at, _ := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		summary.TotalCommits++
		if at > summary.LastCommit {
			summary.LastCommit = at
		}
		if at != 0 && (summary.FirstCommit == 0 || at < summary.FirstCommit) {
			summary.FirstCommit = at
		}
		if at >= cutoff {
			key := strings.ToLower(email)
			if key == "" {
				key = "name:" + strings.ToLower(name)
			}
			recent[key] = true
		}
		if age := nowUnix - at; at > 0 && age >= 0 {
			if week := int(age / weekSeconds); week < ContributorWeeks {
				summary.WeeklyCommits[ContributorWeeks-1-week]++
			}
		}
	}
	summary.ActiveRecent = len(recent)
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
		contributors = append(contributors, byKey[key].Contributor)
	}
	sort.SliceStable(contributors, func(i, j int) bool {
		if contributors[i].Commits != contributors[j].Commits {
			return contributors[i].Commits > contributors[j].Commits
		}
		return strings.ToLower(contributors[i].Name) < strings.ToLower(contributors[j].Name)
	})
	return contributors
}
