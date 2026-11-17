package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// withCommitLineStats reads a whole page in one command, including over SSH.
// The first-parent comparison matches CommitFiles; roots compare to an empty
// tree. Commit objects and their totals are immutable, so a session caches
// them instead of repeating expensive diff-tree work on refresh or another view.
func (s *gitSession) withCommitLineStats(ctx context.Context, commits []Commit) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return commits, nil
	}

	// Holding the lock over the read avoids both map races and duplicate
	// in-flight batches when History and Graph request the same commits.
	s.commitTotalsMu.Lock()
	defer s.commitTotalsMu.Unlock()
	if s.commitTotals == nil {
		s.commitTotals = make(map[string]commitLineStats)
	}
	var input strings.Builder
	uncached := make(map[string]bool, len(commits))
	for _, commit := range commits {
		if uncached[commit.Hash] {
			continue
		}
		if _, ok := s.commitTotals[commit.Hash]; !ok {
			uncached[commit.Hash] = true
			fmt.Fprintln(&input, commit.Hash)
		}
	}
	if input.Len() > 0 {
		output, err := s.cmd.New([]string{
			"git", "diff-tree", "--stdin", "--root", "-r", "--numstat", "-z",
			"--no-ext-diff", "--no-textconv", "--diff-merges=first-parent",
			fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
		}).SetStdin(input.String()).RunWithOutput()
		if err != nil {
			// Counts are presentation-only metadata. Preserve immutable commits
			// and already cached totals rather than vetoing all repository details.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return commits, nil
		}
		stats := parseCommitTotals(output)
		for hash := range uncached {
			// Only newly fetched keys may be replaced. A cache hit was omitted
			// from this command's input and must never be overwritten by the
			// zero value of a missing parsed entry.
			s.commitTotals[hash] = stats[hash]
		}
	}
	for i := range commits {
		stat := s.commitTotals[commits[i].Hash]
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
