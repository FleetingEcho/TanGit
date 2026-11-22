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
}

// contributorCommitLimit bounds the history read, so a very large repository
// cannot make the contributor list a long scan.
const contributorCommitLimit = 200000

// Contributors lists everyone who authored commits, most commits first. Several
// names under one email are one contributor, shown under their newest name.
func (s *gitSession) Contributors(ctx context.Context) ([]Contributor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := s.cmd.New([]string{
		"git", "log", "--all", fmt.Sprintf("--max-count=%d", contributorCommitLimit),
		"--format=%aN%x1f%aE%x1f%at",
	}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read contributors: %w", err)
	}
	return parseContributors(output), nil
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
