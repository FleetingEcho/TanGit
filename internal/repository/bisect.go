package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// BisectState is a snapshot of an in-flight (or finished) git bisect session.
// The zero value means no bisect is running. It is read from the Git
// metadata files rather than from Git porcelain so it works the same for
// local and SSH workspaces.
type BisectState struct {
	// Active reports that a bisect session is in progress.
	Active bool
	// Current is the commit Git has checked out for testing (BISECT_EXPECTED_REV).
	Current string
	// Start is the ref the bisect was started from (BISECT_START).
	Start string
	// NewTerm is the "bad" side's term, OldTerm the "good" side's.
	NewTerm string
	OldTerm string
	// Bad lists the refs marked new (usually just the start bad revision),
	// Good lists the refs marked old, and Skipped the refs marked skip.
	Bad     []string
	Good    []string
	Skipped []string
	// Done, when non-empty, is the hash of the first bad commit, reported by
	// the last mark command. Git keeps the bisect state on disk until
	// `bisect reset`, so callers detect completion from command output, not
	// from the files disappearing.
	Done string
}

var firstBadCommitPattern = regexp.MustCompile(`([0-9a-f]{4,40}) is the first bad commit`)

// BisectState reads the bisect metadata in the repository. A missing
// BISECT_START file means no bisect is running; that is not an error.
func (s *gitSession) BisectState(ctx context.Context) (*BisectState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gitDir := s.git.RepoPaths.WorktreeGitDirPath()
	files := s.fileService()
	start, err := files.ReadFile(s.joinPath(gitDir, "BISECT_START"))
	if err != nil {
		// No bisect in progress (or the file is unreadable): either way the
		// app must not claim one is.
		return &BisectState{}, nil
	}
	state := &BisectState{
		Active:  true,
		Start:   strings.TrimSpace(string(start)),
		NewTerm: "bad",
		OldTerm: "good",
	}
	if terms, err := files.ReadFile(s.joinPath(gitDir, "BISECT_TERMS")); err == nil {
		lines := strings.Split(strings.TrimSpace(string(terms)), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
			state.NewTerm = strings.TrimSpace(lines[0])
		}
		if len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
			state.OldTerm = strings.TrimSpace(lines[1])
		}
	}
	if current, err := files.ReadFile(s.joinPath(gitDir, "BISECT_EXPECTED_REV")); err == nil {
		state.Current = strings.TrimSpace(string(current))
	}
	refsDir := s.joinPath(gitDir, "refs", "bisect")
	if entries, err := files.ReadDir(refsDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			content, readErr := files.ReadFile(s.joinPath(refsDir, name))
			if readErr != nil {
				continue
			}
			hash := strings.TrimSpace(string(content))
			switch {
			case name == state.NewTerm:
				state.Bad = append(state.Bad, hash)
			case strings.HasPrefix(name, state.OldTerm+"-"):
				state.Good = append(state.Good, hash)
			case strings.HasPrefix(name, "skipped-"):
				state.Skipped = append(state.Skipped, hash)
			}
		}
	}
	return state, nil
}

// BisectStart begins a bisect session marking start as the known-bad commit.
// If goodCommit is non-empty it is recorded as the first good commit in the
// same command. Without it the session waits for a good mark.
func (s *gitSession) BisectStart(ctx context.Context, badRef string, goodCommit string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	badRef = strings.TrimSpace(badRef)
	goodCommit = strings.TrimSpace(goodCommit)
	if badRef == "" {
		return fmt.Errorf("bisect: a bad revision is required")
	}
	if existing, err := s.BisectState(ctx); err == nil && existing.Active {
		return fmt.Errorf("bisect: a bisect session is already in progress; reset it first")
	}
	args := []string{"git", "bisect", "start", badRef}
	if goodCommit != "" {
		args = append(args, goodCommit)
	}
	if out, err := s.cmd.New(args).RunWithOutput(); err != nil {
		return fmt.Errorf("bisect start: %w%s", err, commandOutputSummary(out))
	}
	return nil
}

// BisectMark marks ref (or the commit under test when empty) as good, bad, or
// skipped. The returned string is non-empty when the bisect completed with
// this mark, naming the first bad commit.
func (s *gitSession) BisectMark(ctx context.Context, term string, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	term = strings.TrimSpace(term)
	if term != "bad" && term != "good" && term != "skip" {
		return "", fmt.Errorf("bisect: unsupported term %q", term)
	}
	state, err := s.BisectState(ctx)
	if err != nil {
		return "", err
	}
	if !state.Active {
		return "", fmt.Errorf("bisect: no bisect in progress")
	}
	args := []string{"git", "bisect", term}
	if ref = strings.TrimSpace(ref); ref != "" {
		args = append(args, ref)
	}
	out, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("bisect %s: %w%s", term, err, commandOutputSummary(out))
	}
	if match := firstBadCommitPattern.FindStringSubmatch(out); match != nil {
		return match[1], nil
	}
	return "", nil
}

// BisectReset ends the bisect session and returns to the pre-bisect commit.
func (s *gitSession) BisectReset(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if out, err := s.cmd.New([]string{"git", "bisect", "reset"}).RunWithOutput(); err != nil {
		return fmt.Errorf("bisect reset: %w%s", err, commandOutputSummary(out))
	}
	return nil
}

// commandOutputSummary returns a short suffix with Git's own explanation of a
// failed command, or "" when there is nothing useful to add.
func commandOutputSummary(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	output = strings.Join(strings.Fields(output), " ")
	if len(output) > 200 {
		output = output[:200] + "…"
	}
	return ": " + output
}
