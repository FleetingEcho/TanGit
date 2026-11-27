package app

import (
	"context"
	"encoding/csv"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// overviewIssuePattern finds a six or seven digit tracker number set off by
// punctuation or spaces, as in "1275450: fix" or "MR: !20450 - 1275450: fix".
var overviewIssuePattern = regexp.MustCompile(`(?:^|[^0-9A-Za-z])(\d{6,7})(?:[^0-9A-Za-z]|$)`)

// overviewIssueID returns the first tracker number in a subject, or "".
func overviewIssueID(subject string) string {
	if match := overviewIssuePattern.FindStringSubmatch(subject); match != nil {
		return match[1]
	}
	return ""
}

func overviewIsAuthorWord(word string) bool {
	lower := strings.ToLower(word)
	return strings.HasPrefix(lower, "by:") || strings.HasPrefix(lower, "author:")
}

// overviewQueryIsMine says whether the search already asks for the user's own
// commits.
func overviewQueryIsMine(query string) bool {
	filter, _ := parseCommitSearch(query)
	return filter.Author == repository.CommitAuthorMe
}

// overviewSetAuthor replaces any author filter in query with author, or drops
// it when author is empty, keeping the other words as typed.
func overviewSetAuthor(query, author string) string {
	var words []string
	for _, word := range splitSearchWords(query) {
		if !overviewIsAuthorWord(word) {
			words = append(words, overviewQuoteWord(word))
		}
	}
	if author != "" {
		words = append(words, "by:"+overviewQuoteWord(author))
	}
	return strings.Join(words, " ")
}

func overviewQuoteWord(word string) string {
	if strings.ContainsAny(word, " \t") {
		return `"` + word + `"`
	}
	return word
}

func overviewToggleMine(query string) string {
	if overviewQueryIsMine(query) {
		return overviewSetAuthor(query, "")
	}
	return overviewSetAuthor(query, repository.CommitAuthorMe)
}

// overviewMRPattern finds a merge request number written as "!20450".
var overviewMRPattern = regexp.MustCompile(`(?:^|[^0-9A-Za-z])!(\d{3,7})(?:[^0-9A-Za-z]|$)`)

func overviewMRID(subject string) string {
	if match := overviewMRPattern.FindStringSubmatch(subject); match != nil {
		return "!" + match[1]
	}
	return ""
}

const overviewUnpushedWord = "is:unpushed"

func overviewQueryIsUnpushed(query string) bool {
	for _, word := range splitSearchWords(query) {
		if strings.EqualFold(word, overviewUnpushedWord) {
			return true
		}
	}
	return false
}

// overviewDropUnpushed removes the keyword so the rest parses as a normal search.
func overviewDropUnpushed(query string) string {
	var words []string
	for _, word := range splitSearchWords(query) {
		if !strings.EqualFold(word, overviewUnpushedWord) {
			words = append(words, overviewQuoteWord(word))
		}
	}
	return strings.Join(words, " ")
}

func overviewToggleUnpushed(query string) string {
	if overviewQueryIsUnpushed(query) {
		return overviewDropUnpushed(query)
	}
	return strings.TrimSpace(query + " " + overviewUnpushedWord)
}

// overviewSyncLabel is the compact state after a repository's branch name:
// commits ahead and behind its upstream, and a star for modified tracked files.
func overviewSyncLabel(status *repository.RepoSyncStatus) string {
	if status == nil {
		return ""
	}
	label := ""
	if status.Ahead > 0 {
		label += fmt.Sprintf(" ↑%d", status.Ahead)
	}
	if status.Behind > 0 {
		label += fmt.Sprintf(" ↓%d", status.Behind)
	}
	if status.Dirty {
		label += " *"
	}
	return label
}

// overviewFolderCount is one directory and how many changed files it holds.
type overviewFolderCount struct {
	Dir   string
	Files int
}

const overviewFolderChips = 5

// overviewFolderSummary groups changed files by the shallowest directory depth
// that separates them, so a commit that touches one big tree is summarised by
// its sub-folders instead of one chip. It is one pass over the files and runs
// once, when the commit's file list arrives.
func overviewFolderSummary(files []repository.CommitFile) (top []overviewFolderCount, others int) {
	if len(files) == 0 {
		return nil, 0
	}
	const maxDepth = 6
	group := func(depth int) map[string]int {
		counts := make(map[string]int)
		for _, file := range files {
			segments := strings.Split(file.Path, "/")
			n := min(depth, len(segments)-1)
			if n < 1 {
				counts["."]++
				continue
			}
			counts[strings.Join(segments[:n], "/")]++
		}
		return counts
	}
	counts := group(1)
	for depth := 1; depth <= maxDepth && len(counts) < 2; depth++ {
		next := group(depth + 1)
		if len(next) < 1 {
			break
		}
		counts = next
		if len(counts) >= 2 {
			break
		}
	}
	for dir, n := range counts {
		top = append(top, overviewFolderCount{Dir: dir, Files: n})
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Files != top[j].Files {
			return top[i].Files > top[j].Files
		}
		return top[i].Dir < top[j].Dir
	})
	if len(top) > overviewFolderChips {
		for _, folder := range top[overviewFolderChips:] {
			others += folder.Files
		}
		top = top[:overviewFolderChips]
	}
	return top, others
}

// overviewExport renders rows as Markdown, or CSV when path ends in .csv.
func overviewExport(path string, rows []overviewCommit, names map[string]string) []byte {
	var out strings.Builder
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		writer := csv.NewWriter(&out)
		_ = writer.Write([]string{"date", "repository", "hash", "author", "subject"})
		for _, row := range rows {
			_ = writer.Write([]string{
				time.Unix(row.Commit.UnixTimestamp, 0).Format("2006-01-02 15:04"), names[row.Repo],
				row.Commit.Hash, authorLabel(row.Commit.AuthorName, row.Commit.AuthorEmail), row.Commit.Subject,
			})
		}
		writer.Flush()
		return []byte(out.String())
	}
	out.WriteString("# Overview\n")
	day := ""
	for _, row := range rows {
		made := time.Unix(row.Commit.UnixTimestamp, 0)
		if key := made.Format(time.DateOnly); key != day {
			day = key
			out.WriteString("\n## " + key + "\n\n")
		}
		subject := strings.ReplaceAll(row.Commit.Subject, "\n", " ")
		fmt.Fprintf(&out, "- %s **%s** `%s` %s — %s\n", made.Format("15:04"), names[row.Repo],
			row.Commit.ShortHash, subject, authorLabel(row.Commit.AuthorName, row.Commit.AuthorEmail))
	}
	return []byte(out.String())
}

type overviewSyncSession interface {
	OverviewSyncStatus(context.Context) (repository.RepoSyncStatus, error)
}

// readOverviewSync reads one repository's state within a few seconds, so one
// huge working tree cannot hold the rest of the sidebar back.
func readOverviewSync(ctx context.Context, opener repositoryOpener, path string) (repository.RepoSyncStatus, bool) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	session, err := opener.Open(ctx, path)
	if err != nil {
		return repository.RepoSyncStatus{}, false
	}
	defer closeRepositorySession(session)
	reader, ok := session.(overviewSyncSession)
	if !ok {
		return repository.RepoSyncStatus{}, false
	}
	status, err := reader.OverviewSyncStatus(ctx)
	return status, err == nil
}

// markSeen remembers when the Overview on show was opened and keeps the time it
// was opened before, so commits that arrived in between can be marked as new.
// The first time there is nothing to compare with, so nothing is marked.
func (o *overviewState) markSeen(dataDir string) {
	def := o.cur()
	if def == nil {
		o.seenBefore = 0
		return
	}
	now := time.Now().Unix()
	o.seenBefore = def.LastSeen
	if o.seenBefore == 0 {
		o.seenBefore = now
	}
	def.LastSeen = now
	if saved, err := saveOverviewDef(dataDir, *def, false); err == nil {
		o.config = saved
	}
}
