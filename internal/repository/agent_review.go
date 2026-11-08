package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// resolveAgentReviewCommit validates a review endpoint and freezes it to a
// commit hash before it is embedded in a revision range. In particular, an
// invalid or unborn base is an error; it never becomes the empty tree.
func (s *gitSession) resolveAgentReviewCommit(ref, role string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("%s commit is required", role)
	}
	stdout, stderr, err := s.cmd.New([]string{
		"git", "rev-parse", "--verify", "--end-of-options", ref + "^{commit}",
	}).RunWithOutputs()
	if err != nil {
		if message := strings.TrimSpace(stderr); message != "" {
			return "", fmt.Errorf("resolve %s commit %s: %w: %s", role, ref, err, message)
		}
		return "", fmt.Errorf("resolve %s commit %s: %w", role, ref, err)
	}
	hash := strings.TrimSpace(stdout)
	if hash == "" {
		return "", fmt.Errorf("resolve %s commit %s: empty commit hash", role, ref)
	}
	return hash, nil
}

// CommitRange reads base..captured-head directly. Resolving both endpoints
// first keeps option-like revisions out of the log command and prevents a
// branch name from being re-resolved after the review's head was captured.
func (s *gitSession) CommitRange(ctx context.Context, base, head string, limit, skip int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("read review commits: no limit chosen")
	}
	if skip < 0 {
		skip = 0
	}
	baseHash, err := s.resolveAgentReviewCommit(base, "base")
	if err != nil {
		return nil, fmt.Errorf("read review commits: %w", err)
	}
	headHash, err := s.resolveAgentReviewCommit(head, "head")
	if err != nil {
		return nil, fmt.Errorf("read review commits: %w", err)
	}
	args := []string{
		"git", "log",
		fmt.Sprintf("--max-count=%d", limit),
		fmt.Sprintf("--skip=%d", skip),
		commitLogFormat,
		"--end-of-options", baseHash + ".." + headHash,
		"--",
	}
	output, stderr, err := s.cmd.New(args).RunWithOutputs()
	if err != nil {
		if message := strings.TrimSpace(stderr); message != "" {
			return nil, fmt.Errorf("read review commits: %w: %s", err, message)
		}
		return nil, fmt.Errorf("read review commits: %w", err)
	}
	return parseCommitLog(output), nil
}

// agentReviewDiffArgs builds one direct base-to-final tracked diff. With one
// endpoint, git diff compares that commit with the index and tracked worktree;
// adding a separate HEAD-to-worktree diff would count overlapping edits twice.
func (s *gitSession) agentReviewDiffArgs(base, outputMode string, options DiffOptions) ([]string, error) {
	optionArgs, err := diffOptionArgs(options)
	if err != nil {
		return nil, err
	}
	args := []string{
		"git", "diff", "--no-color", "--no-ext-diff",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
	}
	args = append(args, optionArgs...)
	if outputMode != "" {
		args = append(args, outputMode, "-z")
	}
	return append(args, "--end-of-options", base, "--"), nil
}

// FinalFiles lists the review's actual final state: every commit since base,
// staged and unstaged tracked changes, plus untracked files. The two tracked
// reads use the same direct base comparison so statuses, renames and line
// statistics cannot disagree about an intermediate HEAD.
func (s *gitSession) FinalFiles(ctx context.Context, base string, options DiffOptions) ([]CommitFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	baseHash, err := s.resolveAgentReviewCommit(base, "base")
	if err != nil {
		return nil, fmt.Errorf("load final files: %w", err)
	}
	nameArgs, err := s.agentReviewDiffArgs(baseHash, "--name-status", options)
	if err != nil {
		return nil, fmt.Errorf("load final files: %w", err)
	}
	output, stderr, err := s.cmd.New(nameArgs).RunWithOutputs()
	if err != nil {
		if message := strings.TrimSpace(stderr); message != "" {
			return nil, fmt.Errorf("load final files: %w: %s", err, message)
		}
		return nil, fmt.Errorf("load final files: %w", err)
	}
	files, err := parseNameStatus(output)
	if err != nil {
		return nil, fmt.Errorf("load final files: %w", err)
	}

	statArgs, err := s.agentReviewDiffArgs(baseHash, "--numstat", options)
	if err != nil {
		return nil, fmt.Errorf("load final line statistics: %w", err)
	}
	output, stderr, err = s.cmd.New(statArgs).RunWithOutputs()
	if err != nil {
		if message := strings.TrimSpace(stderr); message != "" {
			return nil, fmt.Errorf("load final line statistics: %w: %s", err, message)
		}
		return nil, fmt.Errorf("load final line statistics: %w", err)
	}
	stats := parseCommitLineStats(output)
	seen := make(map[string]struct{}, len(files))
	for i := range files {
		stat := stats[files[i].Path]
		files[i].LinesAdded = stat.added
		files[i].LinesDeleted = stat.deleted
		seen[files[i].Path] = struct{}{}
	}

	listing, stderr, err := s.cmd.New([]string{
		"git", "ls-files", "--others", "--exclude-standard", "-z", "--",
	}).RunWithOutputs()
	if err != nil {
		if message := strings.TrimSpace(stderr); message != "" {
			return nil, fmt.Errorf("list final untracked files: %w: %s", err, message)
		}
		return nil, fmt.Errorf("list final untracked files: %w", err)
	}
	for path := range strings.SplitSeq(listing, "\x00") {
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		stat, err := s.untrackedLineStats(path)
		if err != nil {
			return nil, fmt.Errorf("load final line statistics for %s: %w", path, err)
		}
		files = append(files, CommitFile{
			Path: path, Status: "??", LinesAdded: stat.added, LinesDeleted: stat.deleted,
		})
		seen[path] = struct{}{}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func literalReviewPath(path string) string {
	return ":(literal)" + path
}

// FinalFileDiff reads the same direct base-to-final patch as FinalFiles. An
// untracked file has no index side, so /dev/null supplies its empty old side.
func (s *gitSession) FinalFileDiff(ctx context.Context, base string, file CommitFile, options DiffOptions) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
	baseHash, err := s.resolveAgentReviewCommit(base, "base")
	if err != nil {
		return Diff{}, fmt.Errorf("load final file diff: %w", err)
	}
	optionArgs, err := diffOptionArgs(options)
	if err != nil {
		return Diff{}, fmt.Errorf("load final file diff: %w", err)
	}
	if file.Path == "" {
		return Diff{}, fmt.Errorf("load final file diff: file path is required")
	}

	if strings.HasPrefix(file.Status, "?") {
		args := append([]string{"git", "diff", "--no-index", "--no-color", "--no-ext-diff"}, optionArgs...)
		args = append(args, "--", "/dev/null", file.Path)
		patch, stderr, commandErr := s.cmd.New(args).RunWithOutputs()
		if commandErr != nil {
			if exitCode, ok := commandExitCode(commandErr); !ok || exitCode != 1 {
				if message := strings.TrimSpace(stderr); message != "" {
					return Diff{}, fmt.Errorf("load final untracked file diff: %w: %s", commandErr, message)
				}
				return Diff{}, fmt.Errorf("load final untracked file diff: %w", commandErr)
			}
		}
		if strings.TrimSpace(patch) != "" {
			return Diff{Patch: patch, Kind: "Final"}, nil
		}
		content, err := s.fileService().ReadFile(s.joinPath(s.git.RepoPaths.WorktreePath(), file.Path))
		if err != nil {
			return Diff{}, fmt.Errorf("read final untracked file: %w", err)
		}
		return Diff{NewText: string(content), Kind: "Final"}, nil
	}

	args, err := s.agentReviewDiffArgs(baseHash, "", options)
	if err != nil {
		return Diff{}, fmt.Errorf("load final file diff: %w", err)
	}
	args = append(args, literalReviewPath(file.Path))
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		args = append(args, literalReviewPath(file.PreviousPath))
	}
	patch, stderr, err := s.cmd.New(args).RunWithOutputs()
	if err != nil {
		if message := strings.TrimSpace(stderr); message != "" {
			return Diff{}, fmt.Errorf("load final file diff: %w: %s", err, message)
		}
		return Diff{}, fmt.Errorf("load final file diff: %w", err)
	}
	return Diff{Patch: patch, Kind: "Final"}, nil
}
