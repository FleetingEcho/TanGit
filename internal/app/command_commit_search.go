package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// The palette finds commits the way the History filter does: by message, hash,
// author and branch, across every branch. Typing or pasting a hash in the
// default mode finds that commit; the # mode takes the same words plus
// by:name and branch:name. Choosing a commit lists everything that can be done
// with it, as the commit's context menu does.

const (
	commitSearchLimit      = 50
	commitSearchLocalLimit = 300
	commitSearchTimeout    = 20 * time.Second
)

// paletteCommitTarget is the commit whose actions the palette is listing.
type paletteCommitTarget struct {
	app    *app
	commit repository.Commit
}

// paletteCommitSearch is the Git-backed result of the palette's commit search.
type paletteCommitSearch struct {
	app     *app
	key     string
	commits []repository.Commit
	loading bool
	err     string
	token   uint64
	cancel  context.CancelFunc
}

// isCommitHash reports whether text looks like a pasted commit hash: four to
// forty hexadecimal digits, so a short hash works too.
func isCommitHash(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) < 4 || len(text) > 40 {
		return false
	}
	for _, r := range text {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// parseCommitSearch turns the words typed after # into the History filter: by:
// or author: names the author, branch: or on: the branch, and the rest is the
// message or hash. explicit says a field was named, so the free words are not
// also tried as an author.
func parseCommitSearch(text string) (repository.CommitFilter, bool) {
	var filter repository.CommitFilter
	var free []string
	explicit := false
	for _, word := range splitSearchWords(text) {
		lower := strings.ToLower(word)
		switch {
		case strings.HasPrefix(lower, "by:"):
			filter.Author, explicit = word[3:], true
		case strings.HasPrefix(lower, "author:"):
			filter.Author, explicit = word[7:], true
		case strings.HasPrefix(lower, "branch:"):
			filter.Branch, explicit = word[7:], true
		case strings.HasPrefix(lower, "on:"):
			filter.Branch, explicit = word[3:], true
		default:
			free = append(free, word)
		}
	}
	filter.Query = strings.Join(free, " ")
	return filter, explicit
}

// splitSearchWords splits on spaces, keeping a double-quoted run together and
// dropping its quotes, so by:"Jane Doe" names an author with a space.
func splitSearchWords(text string) []string {
	var words []string
	var current strings.Builder
	quoted := false
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for _, r := range text {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return words
}

// typedWord is the field word being typed at the end of the # query, for the
// given prefixes (by: or branch:): the text before it and what has been typed
// of the value. ok is false when the query does not end in one, which includes
// a query that ends in a space (the word is finished).
func typedWord(query string, prefixes ...string) (before, typed string, ok bool) {
	if strings.TrimRight(query, " \t") != query {
		return "", "", false
	}
	for _, prefix := range prefixes {
		index := strings.LastIndex(strings.ToLower(query), prefix)
		if index < 0 || (index > 0 && query[index-1] != ' ') {
			continue
		}
		raw := query[index+len(prefix):]
		value := raw
		if strings.Count(value, `"`)%2 == 1 {
			value = strings.TrimPrefix(value, `"`)
		}
		if !strings.Contains(value, `"`) && (!strings.Contains(value, " ") || strings.HasPrefix(raw, `"`)) {
			return query[:index], value, true
		}
	}
	return "", "", false
}

func authorSuggestion(query string) (before, typed string, ok bool) {
	return typedWord(query, "by:", "author:")
}

func branchSuggestion(query string) (before, typed string, ok bool) {
	return typedWord(query, "branch:", "on:")
}

// paletteCommitText is what the commit search runs on: the query after the #,
// or a pasted hash in the default mode; "" when the palette is not searching
// commits.
func (s *shell) paletteCommitText() string {
	mode, rest := commandQueryMode(s.palette.query)
	switch {
	case s.palette.target != nil:
		return ""
	case mode == '#':
		text := strings.TrimLeft(s.palette.query, " ")[1:]
		if _, _, typing := authorSuggestion(text); typing {
			return ""
		}
		if _, _, typing := branchSuggestion(text); typing {
			return ""
		}
		return strings.TrimSpace(rest)
	case mode == 0 && isCommitHash(rest):
		return strings.TrimSpace(rest)
	}
	return ""
}

// ensureCommitSearch starts the Git search for the palette's commit text when
// it changed, cancelling the one before. It runs while the palette draws, so a
// keystroke needs no extra wiring; the key keeps it from starting twice.
func (s *shell) ensureCommitSearch(a *app) {
	text := s.paletteCommitText()
	search := &s.palette.commitSearch
	if text == "" || a == nil || a.session == nil {
		if search.cancel != nil {
			search.cancel()
		}
		*search = paletteCommitSearch{token: search.token}
		return
	}
	if search.app == a && search.key == text {
		return
	}
	if search.cancel != nil {
		search.cancel()
	}
	filter, explicit := parseCommitSearch(text)
	queries := []repository.CommitFilter{filter}
	if !explicit && filter.Query != "" {
		// Any user name: free words are also tried as an author.
		queries = append(queries, repository.CommitFilter{Author: filter.Query})
	}
	ctx, cancel := context.WithTimeout(a.repositoryWork.Context(), commitSearchTimeout)
	token := search.token + 1
	*search = paletteCommitSearch{app: a, key: text, loading: true, token: token, cancel: cancel}
	session := a.session
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		defer cancel()
		var merged []repository.Commit
		seen := map[string]bool{}
		var firstErr error
		for _, query := range queries {
			if !commitFilterNarrows(query) {
				continue
			}
			query.Limit = commitSearchLimit
			commits, err := requireCommitSearchSession(session).SearchCommits(ctx, query)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			for _, commit := range commits {
				if !seen[commit.Hash] {
					seen[commit.Hash] = true
					merged = append(merged, commit)
				}
			}
		}
		a.update(func() {
			current := &s.palette.commitSearch
			if generation != a.generation || current.token != token {
				return
			}
			current.loading = false
			if len(merged) == 0 && firstErr != nil {
				current.err = safeError(firstErr)
				return
			}
			current.commits = merged
		})
	}()
}

// commitSearchSpecs are the commit rows of the # mode (and of a pasted hash):
// Git's matches once they are in, the loaded history while they are not.
func (s *shell) commitSearchSpecs(a *app, text string, prefix byte) []commandSpec {
	search := s.palette.commitSearch
	var commits []repository.Commit
	remote := search.app == a && search.key == text && !search.loading && search.err == ""
	if remote {
		commits = search.commits
	} else {
		commits = a.visibleCommits()
		if len(commits) > commitSearchLocalLimit {
			commits = commits[:commitSearchLocalLimit]
		}
	}
	specs := make([]commandSpec, 0, len(commits))
	for index, commit := range commits {
		commit := commit
		score := commitSearchLimit*8 - index
		detail := strings.TrimSpace(commit.ShortHash + " · " + commit.AuthorName)
		if commit.UnixTimestamp > 0 {
			detail += " · " + time.Unix(commit.UnixTimestamp, 0).Format("2006-01-02")
		}
		specs = append(specs, commandSpec{
			ID: commandID("history/commit/" + commit.Hash), Label: commit.Subject, Detail: detail,
			Keywords: []string{commit.Hash, commit.ShortHash, commit.AuthorName, commit.AuthorEmail},
			Group:    commandGroupHistory, Prefix: prefix, Glyph: historyIcon, NoFilter: remote, KeepOpen: true,
			Resolve: func(ctx commandContext) commandState {
				state := repositoryStateAllowBusy(ctx)
				state.ContextScore = score
				return state
			},
			// Enter lists everything that can be done with the commit.
			Run: func(ctx commandContext) { ctx.Shell.openCommitActions(ctx.App, commit) },
			Actions: []commandAction{
				{Label: "Show in History", Run: func(ctx commandContext) { ctx.App.revealCommit(commit.Hash) }},
				{Label: "Copy commit hash", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "commit hash", commit.Hash) }},
			},
		})
	}
	return specs
}

// openCommitActions makes the palette list every action for one commit.
func (s *shell) openCommitActions(a *app, commit repository.Commit) {
	s.palette.target = &paletteCommitTarget{app: a, commit: commit}
	s.palette.query = ""
	s.palette.selected = 0
	s.palette.actionIndex = 0
	s.palette.notice = ""
	s.palette.revealSelection = true
	s.palette.scroll.Y = 0
	s.palette.search.CancelFocus()
}

func (s *shell) closeCommitActions() {
	s.palette.target = nil
	s.palette.query = ""
	s.palette.selected = 0
	s.palette.notice = ""
	s.palette.revealSelection = true
	s.palette.search.CancelFocus()
}

// commitHistoryEditable says the commit is in the loaded history of the
// checked-out branch, so actions that rewrite that branch make sense. Commits
// found elsewhere keep only the actions that act on the commit itself, as the
// context menu does for another branch's commits.
func (a *app) commitHistoryEditable(commit repository.Commit) bool {
	for _, loaded := range a.snapshot.Commits {
		if loaded.Hash == commit.Hash {
			return true
		}
	}
	return false
}

// commitTargetSpecs lists the actions of the chosen commit, the same set the
// commit's context menu offers, each revalidated when it runs.
func commitTargetSpecs(target *paletteCommitTarget) []commandSpec {
	a, commit := target.app, target.commit
	if a == nil {
		return nil
	}
	editable := a.commitHistoryEditable(commit)
	var specs []commandSpec
	type item struct {
		label    string
		detail   string
		keywords []string
		chord    string
		run      func(*app)
		disabled func(*app) string
		history  bool
	}
	busy := func(a *app) string {
		if a.actionBusy {
			return "Wait for the current repository action to finish"
		}
		return ""
	}
	idle := func(a *app) string {
		if a.actionBusy {
			return "Wait for the current repository action to finish"
		}
		if a.snapshot.Operation != "" {
			return "Finish the current merge, rebase or other operation first"
		}
		return ""
	}
	hasStaged := func(a *app) string {
		if reason := busy(a); reason != "" {
			return reason
		}
		if !hasStagedFiles(a.snapshot.Files) {
			return "No staged changes"
		}
		return ""
	}
	markLabel := "Mark as base commit for rebase"
	if a.markedBaseCommit.Hash == commit.Hash {
		markLabel = "Cancel marked base commit"
	}
	items := []item{
		{label: "Show in History", detail: "Select this commit in the History list", keywords: []string{"reveal", "select", "go to"},
			run: func(a *app) { a.revealCommit(commit.Hash) }},
		{label: "Copy commit hash", keywords: []string{"copy", "sha"},
			run: func(a *app) { a.notifyCopied(a.services, "commit hash", commit.Hash) }},
		{label: "Copy commit subject", keywords: []string{"copy", "message"},
			run: func(a *app) { a.notifyCopied(a.services, "commit subject", commit.Subject) }},
		{label: markLabel, keywords: []string{"rebase", "base"}, history: true, disabled: idle,
			run: func(a *app) { a.markRebaseBase(commit) }},
		{label: "Edit commit (pause rebase)…", chord: "E", keywords: []string{"rebase", "edit"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmEditCommit(commit) }},
		{label: "Reword commit…", keywords: []string{"rebase", "message", "rename"}, history: true, disabled: idle,
			run: func(a *app) { a.openCommitRewordForm(commit) }},
		{label: "Amend with staged changes…", chord: "Shift+A", keywords: []string{"amend"}, history: true, disabled: hasStaged,
			run: func(a *app) { a.confirmAmendCommit(commit) }},
		{label: "Create fixup commit from staged changes…", keywords: []string{"fixup"}, history: true, disabled: hasStaged,
			run: func(a *app) { a.confirmCreateFixupCommit(commit) }},
		{label: "Apply fixups from this commit through HEAD…", keywords: []string{"fixup", "autosquash"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmApplyFixupCommits(commit) }},
		{label: "Squash into older commit…", chord: "S", keywords: []string{"squash", "combine"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmCombineCommit(commit, repository.CombineSquash) }},
		{label: "Fixup into older commit…", chord: "F", keywords: []string{"fixup", "combine"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmCombineCommit(commit, repository.CombineFixup) }},
		{label: "Move commit toward HEAD", chord: "Ctrl+K", keywords: []string{"move", "reorder", "newer"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmMoveCommit(commit, repository.MoveCommitNewer) }},
		{label: "Move commit toward older history", chord: "Ctrl+J", keywords: []string{"move", "reorder", "older"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmMoveCommit(commit, repository.MoveCommitOlder) }},
		{label: "Drop commit…", chord: "D", keywords: []string{"delete", "remove", "drop"}, history: true, disabled: idle,
			run: func(a *app) { a.confirmDropCommit(commit) }},
		{label: "Checkout to this commit (detached)…", keywords: []string{"checkout", "switch", "detached"}, disabled: busy,
			run: func(a *app) { a.confirmCheckoutCommit(commit) }},
	}
	if editable && a.canUndoLatestCommit(commit) {
		items = append(items,
			item{label: "Undo latest commit and keep changes staged…", keywords: []string{"undo", "reset", "soft"}, history: true, disabled: busy,
				run: func(a *app) { a.confirmUndoLatestCommit(commit, repository.ResetSoft) }},
			item{label: "Undo latest commit and keep changes unstaged…", keywords: []string{"undo", "reset", "mixed"}, history: true, disabled: busy,
				run: func(a *app) { a.confirmUndoLatestCommit(commit, repository.ResetMixed) }},
		)
	}
	items = append(items,
		item{label: "Soft reset to this commit…", keywords: []string{"reset"}, history: true, disabled: busy,
			run: func(a *app) { a.confirmResetCommit(commit, repository.ResetSoft) }},
		item{label: "Mixed reset to this commit…", keywords: []string{"reset"}, history: true, disabled: busy,
			run: func(a *app) { a.confirmResetCommit(commit, repository.ResetMixed) }},
		item{label: "Hard reset to this commit…", keywords: []string{"reset", "discard"}, history: true, disabled: busy,
			run: func(a *app) { a.confirmResetCommit(commit, repository.ResetHard) }},
		item{label: "Export as mailbox patch…", keywords: []string{"patch", "export", "mbox", "format-patch"}, disabled: busy,
			run: func(a *app) { a.openExportPatchForm([]repository.Commit{commit}) }},
		item{label: "Create a tag here…", keywords: []string{"tag"}, disabled: busy,
			run: func(a *app) { a.openCommitTagForm(commit) }},
		item{label: "Create a branch here…", keywords: []string{"branch"}, disabled: busy,
			run: func(a *app) { a.openCommitBranchForm(commit) }},
	)
	if a.snapshot.Bisect != nil && a.snapshot.Bisect.Active {
		items = append(items,
			item{label: "Mark commit good (bisect)", keywords: []string{"bisect", "good"}, disabled: busy,
				run: func(a *app) { a.confirmBisectMark("good", commit.Hash) }},
			item{label: "Mark commit bad (bisect)", chord: "B", keywords: []string{"bisect", "bad"}, disabled: busy,
				run: func(a *app) { a.confirmBisectMark("bad", commit.Hash) }},
			item{label: "Skip commit (bisect)", chord: "K", keywords: []string{"bisect", "skip"}, disabled: busy,
				run: func(a *app) { a.confirmBisectMark("skip", commit.Hash) }},
		)
	} else {
		items = append(items, item{label: "Start bisect from this commit…", chord: "B", keywords: []string{"bisect"}, disabled: busy,
			run: func(a *app) { a.openBisectStartForm(commit) }})
	}
	items = append(items,
		item{label: "Cherry-pick onto current branch…", keywords: []string{"cherry-pick", "cherry"}, disabled: busy,
			run: func(a *app) { a.confirmCommitActionFor(commit, false) }},
		item{label: "Revert commit…", keywords: []string{"revert", "undo"}, disabled: busy,
			run: func(a *app) { a.confirmCommitActionFor(commit, true) }},
		item{label: "Add all files to Custom Patch", keywords: []string{"custom patch", "patch"}, disabled: busy,
			run: func(a *app) { a.beginAddCustomPatchCommit(commit) }},
		item{label: "Compare commit with HEAD", chord: "H", keywords: []string{"compare", "diff"},
			run: func(a *app) { a.compareCommit(commit, false) }},
		item{label: "Compare first parent with commit", chord: "Shift+H", keywords: []string{"compare", "diff", "parent"},
			disabled: func(*app) string {
				if len(commit.Parents) == 0 {
					return "This commit has no parent"
				}
				return ""
			}, run: func(a *app) { a.compareCommit(commit, true) }},
	)
	for index, entry := range items {
		entry, index := entry, index
		if entry.history && !editable {
			continue
		}
		specs = append(specs, commandSpec{
			ID:       commandID(fmt.Sprintf("commit-action/%s/%d", commit.Hash, index)),
			Label:    entry.label,
			Keywords: entry.keywords, Group: commandGroupHistory, Chord: entry.chord, Prefix: 'a', Glyph: commitActionGlyph(entry.label),
			Resolve: func(ctx commandContext) commandState {
				state := repositoryStateAllowBusy(ctx)
				state.ContextScore = len(items) - index
				if state.Enabled && entry.disabled != nil {
					if reason := entry.disabled(ctx.App); reason != "" {
						state.Enabled = false
						state.DisabledReason = reason
					}
				}
				return state
			},
			Run: func(ctx commandContext) { entry.run(ctx.App) },
		})
	}
	return specs
}

// commitActionGlyph picks the drawing of an action from what it does.
func commitActionGlyph(label string) *ui.SVG {
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "copy"):
		return copyIcon
	case strings.Contains(lower, "reset"), strings.Contains(lower, "undo"):
		return revertIcon
	case strings.Contains(lower, "drop"):
		return trashIcon
	case strings.Contains(lower, "cherry"):
		return cherryPickIcon
	case strings.Contains(lower, "revert"):
		return revertIcon
	case strings.Contains(lower, "branch"), strings.Contains(lower, "checkout"):
		return branchIcon
	case strings.Contains(lower, "tag"):
		return tagsIcon
	case strings.Contains(lower, "compare"):
		return compareIcon
	case strings.Contains(lower, "squash"), strings.Contains(lower, "fixup"):
		return mergeIcon
	case strings.Contains(lower, "rebase"), strings.Contains(lower, "move"), strings.Contains(lower, "bisect"):
		return rebaseIcon
	case strings.Contains(lower, "patch"), strings.Contains(lower, "export"):
		return externalLinkIcon
	case strings.Contains(lower, "reword"), strings.Contains(lower, "edit"), strings.Contains(lower, "amend"):
		return editIcon
	}
	return historyIcon
}

const authorSuggestionLimit = 30

// authorSuggestionSpecs list the repository's authors while a by: word is being
// typed. Choosing one completes the word and goes on to the commits.
func (s *shell) authorSuggestionSpecs(a *app, before, typed string) []commandSpec {
	if len(a.authors) == 0 && !a.authorsLoading && a.session != nil {
		a.loadCommitAuthors()
	}
	needle := strings.ToLower(strings.TrimSpace(typed))
	var specs []commandSpec
	for index, author := range a.authors {
		author := author
		if needle != "" && !strings.Contains(strings.ToLower(author), needle) {
			continue
		}
		score := authorSuggestionLimit*10 - index
		completed := strings.TrimLeft(before, " ") + "by:" + quoteSearchWord(author) + " "
		specs = append(specs, commandSpec{
			ID: commandID("authors/" + author), Label: author, Detail: "Search this author's commits",
			Group: commandGroupHistory, Prefix: '#', Glyph: personIcon, NoFilter: true, KeepOpen: true,
			Resolve: func(commandContext) commandState {
				return commandState{Visible: true, Enabled: true, ContextScore: score}
			},
			Run: func(ctx commandContext) {
				ctx.Shell.palette.query = "#" + completed
				ctx.Shell.palette.selected = 0
				ctx.Shell.palette.actionIndex = 0
			},
		})
		if len(specs) == authorSuggestionLimit {
			break
		}
	}
	return specs
}

// quoteSearchWord quotes a value with a space so it stays one word.
func quoteSearchWord(value string) string {
	if strings.ContainsAny(value, " \t") {
		return `"` + value + `"`
	}
	return value
}

// branchSuggestionSpecs list the branches, local and remote, while a branch:
// word is being typed. Choosing one completes the word and goes on to that
// branch's commits.
func (s *shell) branchSuggestionSpecs(a *app, before, typed string) []commandSpec {
	needle := strings.ToLower(strings.TrimSpace(typed))
	var names []string
	for _, branch := range a.snapshot.Branches {
		names = append(names, branch.Name)
	}
	for _, remote := range a.snapshot.Remotes {
		for _, branch := range remote.Branches {
			names = append(names, remote.Name+"/"+branch.Name)
		}
	}
	var specs []commandSpec
	for index, name := range names {
		name := name
		if needle != "" && !strings.Contains(strings.ToLower(name), needle) {
			continue
		}
		score := authorSuggestionLimit*10 - index
		completed := strings.TrimLeft(before, " ") + "branch:" + quoteSearchWord(name) + " "
		specs = append(specs, commandSpec{
			ID: commandID("branch-suggestions/" + name), Label: name, Detail: "Search this branch's commits",
			Group: commandGroupBranches, Prefix: '#', Glyph: branchIcon, NoFilter: true, KeepOpen: true,
			Resolve: func(commandContext) commandState {
				return commandState{Visible: true, Enabled: true, ContextScore: score}
			},
			Run: func(ctx commandContext) {
				ctx.Shell.palette.query = "#" + completed
				ctx.Shell.palette.selected = 0
				ctx.Shell.palette.actionIndex = 0
			},
		})
		if len(specs) == authorSuggestionLimit {
			break
		}
	}
	return specs
}

// commitSearchGuideSpecs are the rows shown after a bare #: what the search
// understands. Choosing one starts that kind of search in the box.
func (s *shell) commitSearchGuideSpecs() []commandSpec {
	guide := []struct{ id, label, detail, query string }{
		{"guide/message", "Search commit messages", "Type words after #, for example #fix login", "#"},
		{"guide/by", "by:name", "Only commits by one author, for example #by:alice", "#by:"},
		{"guide/branch", "branch:name", "Only commits on one branch, for example #branch:main", "#branch:"},
		{"guide/hash", "Paste a commit hash", "Four or more hex digits jump to that commit, for example #3f9a2c1", "#"},
	}
	specs := make([]commandSpec, 0, len(guide))
	for _, item := range guide {
		item := item
		specs = append(specs, commandSpec{
			ID: commandID("commits/" + item.id), Label: item.label, Detail: item.detail,
			Group: commandGroupApplication, Prefix: '#', Glyph: historyIcon, KeepOpen: true, NoFilter: true,
			Resolve: func(commandContext) commandState { return commandState{Visible: true, Enabled: true} },
			Run: func(ctx commandContext) {
				ctx.Shell.palette.query = item.query
				ctx.Shell.palette.selected = 0
			},
		})
	}
	return specs
}
