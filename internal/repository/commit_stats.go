package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CommitLineStats returns copies of commits with their added and deleted line
// counts filled in and StatsPending cleared. It is how the progressive open
// completes the commits it published without counts.
func (s *gitSession) CommitLineStats(ctx context.Context, commits []Commit) ([]Commit, error) {
	out := make([]Commit, len(commits))
	copy(out, commits)
	return s.readCommitLineStats(ctx, out)
}

// SetDeferCommitStats chooses whether commit reads wait for line counts. On a
// large repository that read is by far the slowest part of listing commits, so
// the app turns it off and asks for the counts of the commits it shows through
// CommitLineStats. Counts already cached still arrive with the commits.
func (s *gitSession) SetDeferCommitStats(deferred bool) {
	s.deferCommitStats.Store(deferred)
}

func markStatsPending(commits []Commit) {
	for i := range commits {
		commits[i].StatsPending = true
	}
}

func clearStatsPending(commits []Commit) {
	for i := range commits {
		commits[i].StatsPending = false
	}
}

// withCommitLineStats reads a whole page in one command, including over SSH.
// The first-parent comparison matches CommitFiles; roots compare to an empty
// tree. Commit objects and their totals are immutable, so a session caches
// them instead of repeating expensive diff-tree work on refresh or another view.
func (s *gitSession) withCommitLineStats(ctx context.Context, commits []Commit) ([]Commit, error) {
	if !s.deferCommitStats.Load() {
		return s.readCommitLineStats(ctx, commits)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.commitTotalsMu.Lock()
	defer s.commitTotalsMu.Unlock()
	for i := range commits {
		if stat, ok := s.commitTotals[commits[i].Hash]; ok {
			commits[i].LinesAdded, commits[i].LinesDeleted = stat.added, stat.deleted
			commits[i].StatsPending = false
		} else {
			commits[i].StatsPending = true
		}
	}
	return commits, nil
}

func (s *gitSession) readCommitLineStats(ctx context.Context, commits []Commit) ([]Commit, error) {
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
	// A caller canceled while waiting must not start another Git read after
	// the previous batch releases the lock.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
			clearStatsPending(commits)
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
		commits[i].StatsPending = false
	}
	return commits, ctx.Err()
}

// nextNULField returns the next -z record without allocating a slice of
// every record and path. The input string remains alive through its map keys,
// exactly as it did with strings.Split, while a 12k-file read avoids thousands
// of temporary field headers.
func nextNULField(input string, at *int) (string, bool) {
	if *at >= len(input) {
		return "", false
	}
	start := *at
	if end := strings.IndexByte(input[start:], 0); end >= 0 {
		*at = start + end + 1
		return input[start : start+end], true
	}
	*at = len(input)
	return input[start:], true
}

// splitNumstatField recognizes one added/deleted/path record without creating
// a three-string slice. A false result is a commit header in diff-tree output.
func splitNumstatField(field string) (commitLineStats, string, bool) {
	first := strings.IndexByte(field, '\t')
	if first < 0 {
		return commitLineStats{}, "", false
	}
	second := strings.IndexByte(field[first+1:], '\t')
	if second < 0 {
		return commitLineStats{}, "", false
	}
	second += first + 1
	added, _ := strconv.Atoi(field[:first])
	deleted, _ := strconv.Atoi(field[first+1 : second])
	return commitLineStats{added: added, deleted: deleted}, field[second+1:], true
}

func parseCommitTotals(output string) map[string]commitLineStats {
	stats := make(map[string]commitLineStats)
	var hash string
	at := 0
	for at < len(output) {
		field, _ := nextNULField(output, &at)
		item, path, ok := splitNumstatField(field)
		if !ok {
			hash = field
			continue
		}
		stat := stats[hash]
		stat.added += item.added
		stat.deleted += item.deleted
		stats[hash] = stat
		if path == "" {
			// Renames carry two further NUL-delimited path fields. Neither
			// can be interpreted as a commit header, regardless of its name.
			nextNULField(output, &at)
			nextNULField(output, &at)
		}
	}
	return stats
}
