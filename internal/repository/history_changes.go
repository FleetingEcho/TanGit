package repository

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	historyChangeWindow = 500
	historyChangeLimit  = 50
	historyChangeBatch  = 25
)

type HistoryChangeQuery struct {
	Text        string
	Occurrences bool
	Regex       bool
	Path        string
	Ref         string
	AllBranches bool
	Since       time.Time
	Until       time.Time
}

// HistoryChangeCursor pins the refs and the position in their candidate walk.
type HistoryChangeCursor struct {
	revisions string
	offset    int
}

type HistoryChangeHit struct {
	Commit Commit
	Path   string
}

type HistoryChangePage struct {
	Hits    []HistoryChangeHit
	Scanned int
	More    bool
	Start   HistoryChangeCursor
	Next    HistoryChangeCursor
}

type HistoryChangeSession interface {
	SearchHistoryChanges(context.Context, HistoryChangeQuery, HistoryChangeCursor, func(HistoryChangePage)) (HistoryChangePage, error)
}

// SearchHistoryChanges bounds the candidate walk separately from the number of
// hits. Only the selected candidate IDs are passed to Git's diff-based search.
func (s *gitSession) SearchHistoryChanges(ctx context.Context, query HistoryChangeQuery, cursor HistoryChangeCursor, publish func(HistoryChangePage)) (HistoryChangePage, error) {
	page := HistoryChangePage{Start: cursor, Next: cursor, More: true}
	if query.Text == "" || strings.ContainsRune(query.Text, 0) {
		return page, fmt.Errorf("enter valid text to search")
	}
	if !query.Occurrences && strings.Contains(query.Text, "\n") {
		return page, fmt.Errorf("use occurrence count to search for multiple lines")
	}
	if !query.Since.IsZero() && !query.Until.IsZero() && query.Until.Before(query.Since) {
		return page, fmt.Errorf("the end time is before the start time")
	}
	scope := path.Clean(query.Path)
	if path.IsAbs(scope) || scope == ".." || strings.HasPrefix(scope, "../") || strings.ContainsRune(scope, 0) {
		return page, fmt.Errorf("enter a repository-relative path")
	}
	if cursor.revisions == "" {
		revisions, err := s.historyChangeRevisions(ctx, query)
		if err != nil || revisions == "" {
			page.More = err != nil
			return page, err
		}
		cursor.revisions = revisions
		page.Start, page.Next = cursor, cursor
	}
	args := []string{"rev-list", "--stdin", "--no-merges", "--max-count=" + strconv.Itoa(historyChangeWindow+1), "--skip=" + strconv.Itoa(cursor.offset)}
	if !query.Since.IsZero() {
		args = append(args, "--since="+query.Since.Format(time.RFC3339))
	}
	if !query.Until.IsZero() {
		args = append(args, "--until="+query.Until.Format(time.RFC3339))
	}
	output, err := s.historyChangeGit(ctx, cursor.revisions, args...)
	if err != nil {
		return page, err
	}
	candidates := strings.Fields(output)
	filter := "-S" + query.Text
	if !query.Occurrences {
		pattern := query.Text
		if !query.Regex {
			pattern = regexp.QuoteMeta(pattern)
		}
		filter = "-G" + pattern
	}
	args = []string{"--literal-pathspecs", "log", "--stdin", "--no-walk=unsorted", "--no-show-signature",
		"--no-ext-diff", "--no-textconv", "--root", "--name-only", "-z",
		"--find-renames=" + strconv.Itoa(s.renameSimilarityThreshold) + "%",
		strings.Replace(commitLogFormat, "--format=", "--format=tformat:%x00", 1), filter, "--"}
	if query.Occurrences && query.Regex {
		args = append(args[:len(args)-1], "--pickaxe-regex", "--")
	}
	if scope != "." {
		args = append(args, scope)
	}
	for page.Scanned < min(historyChangeWindow, len(candidates)) {
		end := min(page.Scanned+historyChangeBatch, historyChangeWindow, len(candidates))
		batch := candidates[page.Scanned:end]
		output, err := s.historyChangeGit(ctx, strings.Join(batch, "\n")+"\n", args...)
		if err != nil {
			return page, err
		}
		hits := historyChangeHits(output)
		for _, hash := range batch {
			page.Scanned++
			if hit, ok := hits[hash]; ok {
				page.Hits = append(page.Hits, hit)
				if len(page.Hits) == historyChangeLimit {
					break
				}
			}
		}
		page.Next.offset = cursor.offset + page.Scanned
		page.More = page.Scanned < len(candidates)
		if publish != nil {
			// Later batches only append. Restrict capacity for the consumer.
			progress := page
			progress.Hits = progress.Hits[:len(progress.Hits):len(progress.Hits)]
			publish(progress)
		}
		if len(page.Hits) == historyChangeLimit {
			break
		}
	}
	page.More = page.Scanned < len(candidates)
	return page, nil
}

func historyChangeHits(output string) map[string]HistoryChangeHit {
	hits := make(map[string]HistoryChangeHit)
	// Git terminates paths with NUL. A second NUL starts the next commit's
	// format, so filenames containing newlines or whitespace stay intact.
	for _, record := range strings.Split(strings.TrimPrefix(output, "\x00"), "\x00\x00") {
		header, paths, ok := strings.Cut(record, "\x00\n")
		if !ok {
			continue
		}
		commits := parseCommitLog(header)
		file, _, _ := strings.Cut(paths, "\x00")
		if len(commits) == 1 && file != "" {
			hits[commits[0].Hash] = HistoryChangeHit{Commit: commits[0], Path: file}
		}
	}
	return hits
}

func (s *gitSession) historyChangeRevisions(ctx context.Context, query HistoryChangeQuery) (string, error) {
	if query.AllBranches {
		out, err := s.historyChangeGit(ctx, "", "for-each-ref", "--format=%(objectname)", "refs/heads", "refs/remotes")
		return out, err
	}
	ref := strings.TrimSpace(query.Ref)
	if ref == "" {
		ref = "HEAD"
	}
	from, to, ranged := strings.Cut(ref, "..")
	if !ranged {
		to = from
	}
	resolve := func(ref string) (string, error) {
		if ref == "" || strings.HasPrefix(ref, "-") || strings.HasPrefix(ref, ".") {
			return "", fmt.Errorf("invalid revision %q", ref)
		}
		output, err := s.historyChangeGit(ctx, "", "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		return strings.TrimSpace(output), err
	}
	end, err := resolve(to)
	if err != nil {
		if ctx.Err() == nil && ref == "HEAD" && s.isUnbornBranch("HEAD") {
			return "", nil
		}
		return "", err
	}
	if !ranged {
		return end + "\n", nil
	}
	start, err := resolve(from)
	if err != nil {
		return "", err
	}
	return end + "\n^" + start + "\n", nil
}

func (s *gitSession) historyChangeGit(ctx context.Context, stdin string, args ...string) (string, error) {
	command := s.cmd.New(append([]string{"git", "--no-pager"}, args...)).DontLog()
	command.AddEnvVars("GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")
	command.SetStdin(stdin)
	built := command.GetCmd()
	var stdout, stderr bytes.Buffer
	var err error
	if s.remote != nil {
		err = s.remote.execute(ctx, command, built.Stdin, &stdout, &stderr)
	} else {
		cmd := exec.CommandContext(ctx, built.Path, built.Args[1:]...)
		cmd.Dir, cmd.Env, cmd.Stdin = built.Dir, built.Env, built.Stdin
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		configureBackgroundCommand(cmd)
		err = cmd.Run()
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("search code changes: %w%s", err, commandOutputSummary(stderr.String()))
	}
	return stdout.String(), nil
}
