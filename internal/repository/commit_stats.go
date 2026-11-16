package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// withCommitLineStats reads a whole page in one command, including over SSH.
// The first-parent comparison matches CommitFiles; roots compare to an empty
// tree. No path filter is applied, even when loading a file's history.
func (s *gitSession) withCommitLineStats(ctx context.Context, commits []Commit) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return commits, nil
	}
	var input strings.Builder
	for _, commit := range commits {
		fmt.Fprintln(&input, commit.Hash)
	}
	output, err := s.cmd.New([]string{
		"git", "diff-tree", "--stdin", "--root", "-r", "--numstat", "-z",
		"--no-ext-diff", "--no-textconv", "--diff-merges=first-parent",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
	}).SetStdin(input.String()).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("load commit line statistics: %w", err)
	}
	stats := parseCommitTotals(output)
	for i := range commits {
		stat := stats[commits[i].Hash]
		commits[i].LinesAdded, commits[i].LinesDeleted = stat.added, stat.deleted
	}
	return commits, ctx.Err()
}

func parseCommitTotals(output string) map[string]commitLineStats {
	stats := make(map[string]commitLineStats)
	fields := strings.Split(output, "\x00")
	var hash string
	for i := 0; i < len(fields); i++ {
		columns := strings.SplitN(fields[i], "\t", 3)
		if len(columns) != 3 {
			hash = fields[i]
			continue
		}
		added, _ := strconv.Atoi(columns[0])
		deleted, _ := strconv.Atoi(columns[1])
		stat := stats[hash]
		stat.added += added
		stat.deleted += deleted
		stats[hash] = stat
		if columns[2] == "" {
			// Renames carry two further NUL-delimited path fields. Neither
			// can be interpreted as a commit header, regardless of its name.
			i += 2
		}
	}
	return stats
}
