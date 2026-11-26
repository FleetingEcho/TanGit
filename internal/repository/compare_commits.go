package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CompareCommitsLimit bounds each side of a comparison, so two distant
// branches of a very large repository cannot return its whole history.
const CompareCommitsLimit = 500

// CommitComparison is what separates two revisions. Ahead holds the commits
// reachable from `to` but not from `from` (what `to` adds), Behind the commits
// reachable from `from` but not from `to`, newest first. MergeBase is their
// common ancestor, or "" for unrelated histories. A side is flagged More when
// it stopped at the limit.
type CommitComparison struct {
	MergeBase  string
	Ahead      []Commit
	Behind     []Commit
	AheadMore  bool
	BehindMore bool
}

// CompareCommits lists the commits unique to each of two revisions. Either may
// be a branch, tag, commit or any revision Git accepts. Both reads are plain
// `git log from..to` ranges, so merge commits and all parents are honoured as
// Git itself counts them.
func (s *gitSession) CompareCommits(ctx context.Context, from, to string, limit int) (CommitComparison, error) {
	if err := ctx.Err(); err != nil {
		return CommitComparison{}, err
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return CommitComparison{}, fmt.Errorf("choose both refs to compare")
	}
	if strings.HasPrefix(from, "-") || strings.HasPrefix(to, "-") {
		return CommitComparison{}, fmt.Errorf("compare commits: invalid revision")
	}
	if err := s.rejectAmbiguousRef(from); err != nil {
		return CommitComparison{}, err
	}
	if err := s.rejectAmbiguousRef(to); err != nil {
		return CommitComparison{}, err
	}
	if limit <= 0 || limit > CompareCommitsLimit {
		limit = CompareCommitsLimit
	}
	var result CommitComparison
	var err error
	if result.Ahead, result.AheadMore, err = s.compareLog(from, to, limit); err != nil {
		return CommitComparison{}, fmt.Errorf("compare commits: %w", err)
	}
	if result.Behind, result.BehindMore, err = s.compareLog(to, from, limit); err != nil {
		return CommitComparison{}, fmt.Errorf("compare commits: %w", err)
	}
	// Unrelated histories have no merge base; that is a result, not an error.
	if base, baseErr := s.cmd.New([]string{"git", "merge-base", "--end-of-options", from, to}).RunWithOutput(); baseErr == nil {
		result.MergeBase = strings.TrimSpace(base)
	}
	return result, nil
}

// compareLog reads the commits in `exclude..include`, at most limit of them.
func (s *gitSession) compareLog(exclude, include string, limit int) ([]Commit, bool, error) {
	args := []string{
		"git", "log", "--no-color", fmt.Sprintf("--max-count=%d", limit+1),
		"--format=%x1e%H%x1f%h%x1f%an%x1f%ae%x1f%at%x1f%P%x1f%s",
		"--end-of-options", exclude + ".." + include, "--",
	}
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return nil, false, err
	}
	commits := parseCompareLog(output)
	more := len(commits) > limit
	if more {
		commits = commits[:limit]
	}
	return commits, more, nil
}

// parseCompareLog reads records of hash, short hash, author name and email,
// author time, parents and subject, as compareLog formats them.
func parseCompareLog(output string) []Commit {
	var commits []Commit
	for _, record := range strings.Split(output, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 7)
		if len(fields) < 7 {
			continue
		}
		at, _ := strconv.ParseInt(fields[4], 10, 64)
		commit := Commit{
			Hash: fields[0], ShortHash: fields[1],
			AuthorName: fields[2], AuthorEmail: fields[3],
			UnixTimestamp: at, Subject: fields[6],
		}
		if parents := strings.Fields(fields[5]); len(parents) > 0 {
			commit.Parents = parents
		}
		commits = append(commits, commit)
	}
	return commits
}
