package repository

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CommitVolume is how much one commit changed, from Git's --shortstat.
type CommitVolume struct {
	At      int64
	Added   int
	Deleted int
	Files   int
}

// CodeVolume is the lines added and deleted by each commit of a period, so the
// caller can chart any bucket size itself.
type CodeVolume struct {
	Commits []CommitVolume
	// Limited is true when the read stopped at its commit limit, so older
	// commits of the period are not counted.
	Limited bool
}

// CodeVolume reads per-commit line changes for branch, or every branch when it
// is empty, for commits since the Unix time since (0 for no start) and at most
// maxCommits of them. Merge commits are left out: their changes are counted in
// the commits they merged. The branch is read without being checked out.
func (s *gitSession) CodeVolume(ctx context.Context, branch string, since int64, maxCommits int) (CodeVolume, error) {
	if err := ctx.Err(); err != nil {
		return CodeVolume{}, err
	}
	branch = strings.TrimSpace(branch)
	if strings.HasPrefix(branch, "-") {
		return CodeVolume{}, fmt.Errorf("read code changes: invalid branch %q", branch)
	}
	if maxCommits <= 0 {
		maxCommits = 20000
	}
	args := []string{"git", "log", "--no-merges", fmt.Sprintf("--max-count=%d", maxCommits+1), "--format=%x1e%at", "--shortstat"}
	if since > 0 {
		args = append(args, fmt.Sprintf("--since=@%d", since))
	}
	if branch == "" {
		args = append(args, "--all")
	} else {
		args = append(args, s.qualifyCommitFilterBranch(branch))
	}
	args = append(args, "--")
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return CodeVolume{}, fmt.Errorf("read code changes: %w", err)
	}
	volume := parseCodeVolume(output)
	if len(volume.Commits) > maxCommits {
		volume.Commits = volume.Commits[:maxCommits]
		volume.Limited = true
	}
	return volume, nil
}

// parseCodeVolume reads `git log --format=%x1e%at --shortstat`: each commit is
// its timestamp, then (when it changed anything) a line such as
// " 3 files changed, 10 insertions(+), 2 deletions(-)".
func parseCodeVolume(output string) CodeVolume {
	var volume CodeVolume
	for _, record := range strings.Split(output, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		lines := strings.SplitN(record, "\n", 2)
		at, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
		if err != nil {
			continue
		}
		commit := CommitVolume{At: at}
		if len(lines) == 2 {
			for _, part := range strings.Split(lines[1], ",") {
				fields := strings.Fields(part)
				if len(fields) < 2 {
					continue
				}
				count, err := strconv.Atoi(fields[0])
				if err != nil {
					continue
				}
				switch {
				case strings.HasPrefix(fields[1], "file"):
					commit.Files = count
				case strings.HasPrefix(fields[1], "insertion"):
					commit.Added = count
				case strings.HasPrefix(fields[1], "deletion"):
					commit.Deleted = count
				}
			}
		}
		volume.Commits = append(volume.Commits, commit)
	}
	return volume
}

// BranchHealthEntry is one branch with what decides whether it needs care.
type BranchHealthEntry struct {
	// Name is the short name: "feature", or "origin/feature" for a remote one.
	Name    string
	Remote  bool
	Current bool
	// LastCommit is the Unix time of the branch's newest commit.
	LastCommit int64
	// Merged says the branch's tip is already in the default branch.
	Merged bool
	// Ahead and Behind count commits against the default branch; -1 when this
	// Git cannot tell.
	Ahead  int
	Behind int
}

// BranchHealth lists every branch against the repository's default branch.
type BranchHealth struct {
	// Default is the branch the others are compared with.
	Default  string
	Branches []BranchHealthEntry
}

// BranchHealth reads every local and remote branch with its newest commit time,
// whether it is merged into the default branch, and how far it is from it, in
// a few Git calls however many branches there are.
func (s *gitSession) BranchHealth(ctx context.Context) (BranchHealth, error) {
	if err := ctx.Err(); err != nil {
		return BranchHealth{}, err
	}
	health := BranchHealth{Default: s.defaultBranchName()}
	format := "%(refname)%1f%(refname:short)%1f%(committerdate:unix)%1f%(HEAD)"
	withDistance := format
	if health.Default != "" {
		withDistance += "%1f%(ahead-behind:" + health.Default + ")"
	}
	read := func(format string) (string, error) {
		return s.cmd.New([]string{"git", "for-each-ref", "--format=" + format, "refs/heads", "refs/remotes"}).RunWithOutput()
	}
	output, err := read(withDistance)
	distance := health.Default != ""
	if err != nil && distance {
		// An older Git has no ahead-behind: read the rest without it.
		distance = false
		output, err = read(format)
	}
	if err != nil {
		return BranchHealth{}, fmt.Errorf("read branch health: %w", err)
	}
	merged := map[string]bool{}
	if health.Default != "" {
		if out, err := s.cmd.New([]string{"git", "for-each-ref", "--merged=" + health.Default, "--format=%(refname)", "refs/heads", "refs/remotes"}).RunWithOutput(); err == nil {
			for _, line := range strings.Split(out, "\n") {
				merged[strings.TrimSpace(line)] = true
			}
		}
	}
	health.Branches = parseBranchHealth(output, merged, distance)
	return health, nil
}

// defaultBranchName is the branch work is compared with: the remote's HEAD
// when it is known, else main, master, trunk or develop, else the current
// branch. "" when there is none.
func (s *gitSession) defaultBranchName() string {
	if out, err := s.cmd.New([]string{"git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"}).RunWithOutput(); err == nil {
		if name := strings.TrimSpace(out); name != "" {
			return name
		}
	}
	for _, name := range []string{"main", "master", "trunk", "develop"} {
		if err := s.cmd.New([]string{"git", "show-ref", "--verify", "--quiet", "refs/heads/" + name}).Run(); err == nil {
			return name
		}
	}
	if out, err := s.cmd.New([]string{"git", "symbolic-ref", "--short", "HEAD"}).RunWithOutput(); err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

// parseBranchHealth reads the for-each-ref lines of refname, short name, commit
// time, HEAD marker and (when distance) "ahead behind".
func parseBranchHealth(output string, merged map[string]bool, distance bool) []BranchHealthEntry {
	var entries []BranchHealthEntry
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\x1f")
		if len(fields) < 4 {
			continue
		}
		ref, name := fields[0], fields[1]
		remote := strings.HasPrefix(ref, "refs/remotes/")
		// A remote's HEAD is a pointer to its default branch, not a branch.
		if remote && (strings.HasSuffix(ref, "/HEAD") || !strings.Contains(name, "/")) {
			continue
		}
		at, _ := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		entry := BranchHealthEntry{
			Name: name, Remote: remote, Current: strings.TrimSpace(fields[3]) == "*",
			LastCommit: at, Merged: merged[ref], Ahead: -1, Behind: -1,
		}
		if distance && len(fields) >= 5 {
			if parts := strings.Fields(fields[4]); len(parts) == 2 {
				if ahead, err := strconv.Atoi(parts[0]); err == nil {
					entry.Ahead = ahead
				}
				if behind, err := strconv.Atoi(parts[1]); err == nil {
					entry.Behind = behind
				}
			}
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].LastCommit > entries[j].LastCommit })
	return entries
}
