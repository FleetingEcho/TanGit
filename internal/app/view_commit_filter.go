package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// The commit filter modal narrows the History list and a branch's commit
// list. Filtering runs in Git, so a match older than the snapshot's first
// page is still found, and a branch can be searched without checking it out.

// commitQuery is how one page narrows its commit list: the filter it applies,
// the draft the modal edits, its matches, and the read still in flight. The
// two commit pages own one each, so filtering History leaves the Graph whole
// and the other way round.
type commitQuery struct {
	applied repository.CommitFilter
	draft   repository.CommitFilter
	matches []repository.Commit
	loading bool
	err     string
	token   uint64
	limit   int
	more    bool
	// defaulted says the filter is still the one its page opened with: the
	// history of the branch checked out. The default follows a checkout
	// while this holds; a filter the user shapes or clears is theirs from
	// then on, and the page never takes it back.
	defaulted bool
}

// narrows reports whether the query leaves out any commit.
func (q *commitQuery) narrows() bool {
	return commitFilterNarrows(q.applied)
}

// reset drops the query and the read it had in flight, showing every commit
// again. The default is dropped with it: a list the user empties has said
// what it wants, and is not restored to the current branch behind its back.
func (q *commitQuery) reset() {
	q.applied = repository.CommitFilter{}
	q.draft = repository.CommitFilter{}
	q.matches = nil
	q.loading = false
	q.err = ""
	q.token++
	q.limit = 0
	q.more = false
	q.defaulted = false
}

// commitQueryFor returns the list target narrows: History unless the target
// is the Graph. A branch's commit list is filtered by its own read, so it has
// no query of this kind.
func (a *app) commitQueryFor(target int) *commitQuery {
	if target == sectionGraph {
		return &a.graphFilter
	}
	return &a.historyFilter
}

// visibleCommits is the commits the History or Graph list shows: the page's
// matches while a filter narrows it, the repository's history otherwise.
func (a *app) visibleCommits() []repository.Commit {
	if a.workspaceTab == sectionHistory && a.historyFilter.narrows() {
		return a.historyFilter.matches
	}
	if a.workspaceTab == sectionGraph && a.graphFilter.narrows() {
		return a.graphFilter.matches
	}
	return a.snapshot.Commits
}

// commitFilterNarrows reports whether a filter leaves out any commit.
func commitFilterNarrows(filter repository.CommitFilter) bool {
	return strings.TrimSpace(filter.Query) != "" ||
		strings.TrimSpace(filter.Author) != "" ||
		strings.TrimSpace(filter.Branch) != ""
}

// normalizeCommitFilter trims the fields the user typed, so a field of spaces
// does not narrow the list.
func normalizeCommitFilter(filter repository.CommitFilter) repository.CommitFilter {
	return repository.CommitFilter{
		Query:  strings.TrimSpace(filter.Query),
		Author: strings.TrimSpace(filter.Author),
		Branch: strings.TrimSpace(filter.Branch),
		Limit:  filter.Limit,
	}
}

// commitFiltersEqual reports whether two filters ask for the same commits,
// ignoring their page size.
func commitFiltersEqual(left, right repository.CommitFilter) bool {
	left, right = normalizeCommitFilter(left), normalizeCommitFilter(right)
	return left.Query == right.Query && left.Author == right.Author && left.Branch == right.Branch
}

// currentBranchName names the branch checked out as the filter names it: the
// ref Git would read. HEAD sitting detached, or a repository without a commit
// yet, has no such name, and the commit pages then keep the whole history.
func (a *app) currentBranchName() string {
	for _, branch := range a.snapshot.Branches {
		if branch.Head && !branch.DetachedHead {
			return branch.Name
		}
	}
	return ""
}

// applyDefaultCommitFilters gives both commit pages the filter they open
// with: the history of the branch checked out, rather than the whole
// repository's. Each page keeps its own, so clearing History's still leaves
// the Graph on its branch and the other way round. It reads each list as it
// sets it, so the page shows its commits the first time it is drawn.
func (a *app) applyDefaultCommitFilters(snapshot repository.Snapshot) {
	branch := ""
	for _, candidate := range snapshot.Branches {
		if candidate.Head && !candidate.DetachedHead {
			branch = candidate.Name
			break
		}
	}
	// A symbolic unborn branch can appear before it names a readable commit.
	// Keep the empty-repository view instead of starting a failing log search.
	if branch == "" || len(snapshot.Commits) == 0 {
		return
	}
	for _, target := range [...]int{sectionHistory, sectionGraph} {
		query := a.commitQueryFor(target)
		query.applied = repository.CommitFilter{Branch: branch}
		query.draft = query.applied
		query.defaulted = true
		query.limit = repository.SnapshotCommitLimit
		query.matches = nil
		query.more = false
		a.runCommitFilter(target, "")
	}
}

// keepCommitFilterDefault holds a commit page's filter on the branch checked
// out. The page opens on that branch's history, and a checkout moves the
// default with it; a filter the user shapes or clears is theirs and stops the
// default following. It runs while the page draws, so a checkout made by any
// action is picked up on the next frame rather than by each of them having to
// say so.
func (a *app) keepCommitFilterDefault(target int) {
	query := a.commitQueryFor(target)
	if !query.defaulted {
		return
	}
	branch := a.currentBranchName()
	if branch == "" || query.applied.Branch == branch {
		return
	}
	// A default names a branch and nothing else. Any other field belongs to
	// a filter the user shaped, which a checkout leaves as the user left it.
	if query.applied.Query != "" || query.applied.Author != "" {
		query.defaulted = false
		return
	}
	selectedHash := commitHashAt(a.visibleCommits(), a.selectedCommit)
	query.applied = repository.CommitFilter{Branch: branch}
	query.draft = query.applied
	query.limit = repository.SnapshotCommitLimit
	query.matches = nil
	query.more = false
	a.runCommitFilter(target, selectedHash)
}

// commitHashAt is the hash of the commit at index, "" when the index points
// nowhere.
func commitHashAt(commits []repository.Commit, index int) string {
	if index < 0 || index >= len(commits) {
		return ""
	}
	return commits[index].Hash
}

// filterTargetBranches reports whether the open filter belongs to the
// Branches page's commit list rather than to History.
func (a *app) filterTargetBranches() bool {
	return a.filterTarget == sectionBranches
}

// commitFilterDraft is the filter the open modal edits.
func (a *app) commitFilterDraft() *repository.CommitFilter {
	if a.filterTargetBranches() {
		return &a.branchFilterDraft
	}
	return &a.commitQueryFor(a.filterTarget).draft
}

// openCommitFilter shows the filter for target's commit list, starting from
// the filter it already applies. The author choices are read afresh, so a
// fetch that added authors does not leave them out.
func (a *app) openCommitFilter(target int) {
	a.filterTarget = target
	a.filterOpen = true
	if target == sectionBranches {
		a.branchFilterDraft = a.branchFilter
	} else {
		query := a.commitQueryFor(target)
		query.draft = query.applied
	}
	a.loadCommitAuthors()
}

func (a *app) closeCommitFilter() {
	a.filterOpen = false
}

// applyCommitFilter closes the modal and narrows the list to the filter the
// user asked for. The commit on screen stays chosen when it is still in the
// result.
func (a *app) applyCommitFilter() {
	if a.filterTargetBranches() {
		a.branchFilter = normalizeCommitFilter(a.branchFilterDraft)
		a.branchFilterDraft = a.branchFilter
		a.filterOpen = false
		// The branch's commits are keyed by branch and filter, so clearing
		// the key makes the next frame read them with the new filter.
		a.branchCommitsBranch = ""
		a.selectedBranchCommit = -1
		return
	}
	target := a.filterTarget
	query := a.commitQueryFor(target)
	selectedHash := commitHashAt(a.visibleCommits(), a.selectedCommit)
	normalized := normalizeCommitFilter(query.draft)
	changed := !commitFiltersEqual(query.applied, normalized)
	query.applied = normalized
	query.draft = query.applied
	// A filter the user shapes is theirs. It takes the default back only
	// when it is the default again: the branch checked out and nothing else,
	// which then keeps following the checkout as a default does.
	query.defaulted = normalized.Query == "" &&
		normalized.Author == "" &&
		normalized.Branch == a.currentBranchName()
	a.filterOpen = false
	if !query.narrows() {
		a.dropCommitFilter(target, selectedHash)
		return
	}
	query.limit = repository.SnapshotCommitLimit
	query.more = false
	if changed {
		// The matches on screen belong to the old filter, so the list shows
		// the search it is running instead of them.
		query.matches = nil
	}
	a.runCommitFilter(target, selectedHash)
}

// dropCommitFilter returns target's page to every commit, choosing the one
// that was on screen when it is still there.
func (a *app) dropCommitFilter(target int, selectedHash string) {
	query := a.commitQueryFor(target)
	query.reset()
	a.selectedCommit = selectedCommitIndex(a.visibleCommits(), selectedHash)
}

// clearCommitFilter drops every field of target's filter and shows the whole
// list again. target names the list, so the empty-state button of one page
// never clears the other page's filter.
func (a *app) clearCommitFilter(target int) {
	a.filterOpen = false
	if target == sectionBranches {
		a.branchFilter = repository.CommitFilter{}
		a.branchFilterDraft = repository.CommitFilter{}
		a.branchCommitsBranch = ""
		a.selectedBranchCommit = -1
		return
	}
	selectedHash := commitHashAt(a.visibleCommits(), a.selectedCommit)
	a.commitQueryFor(target).reset()
	a.selectedCommit = selectedCommitIndex(a.visibleCommits(), selectedHash)
}

// refreshCommitFilter reads the authors again and re-runs the filter that is
// applied, so commits added since it was set show up. The modal stays open.
func (a *app) refreshCommitFilter() {
	a.loadCommitAuthors()
	if a.filterTargetBranches() {
		a.branchCommitsBranch = ""
		return
	}
	query := a.commitQueryFor(a.filterTarget)
	if !query.narrows() {
		return
	}
	if query.limit < repository.SnapshotCommitLimit {
		query.limit = repository.SnapshotCommitLimit
	}
	a.runCommitFilter(a.filterTarget, commitHashAt(a.visibleCommits(), a.selectedCommit))
}

// runCommitFilter asks Git for target's filter's matches. selectedHash is the
// commit to choose when it is among them.
func (a *app) runCommitFilter(target int, selectedHash string) {
	if a.session == nil {
		return
	}
	query := a.commitQueryFor(target)
	filter := query.applied
	if filter.Limit <= 0 {
		filter.Limit = query.limit
	}
	if filter.Limit <= 0 {
		filter.Limit = repository.SnapshotCommitLimit
	}
	session := a.session
	repositoryGeneration := a.generation
	query.token++
	token := query.token
	query.loading = true
	query.err = ""
	go func() {
		defer a.recoverBackground()
		commits, err := session.SearchCommits(context.Background(), filter)
		a.update(func() {
			if repositoryGeneration != a.generation || token != query.token {
				return
			}
			query.loading = false
			if err != nil {
				query.err = safeError(err)
				return
			}
			query.matches = commits
			query.more = len(commits) >= filter.Limit
			// A read for a page the user has left must not move the
			// selection the page on screen is showing.
			if a.workspaceTab == target {
				a.selectedCommit = selectedCommitIndex(commits, selectedHash)
			}
		})
	}()
}

// requestMoreFilteredCommits pages target's filtered history in: Git's log
// has no offset, so the next page is the same search asked for a larger limit,
// whose first commits are the ones already shown.
func (a *app) requestMoreFilteredCommits(target int) {
	query := a.commitQueryFor(target)
	if a.session == nil || query.loading || !query.more {
		return
	}
	query.limit += repository.SnapshotCommitLimit
	a.runCommitFilter(target, commitHashAt(a.visibleCommits(), a.selectedCommit))
}

// loadCommitAuthors reads the repository's author names for the filter's
// choices. A read still in flight is replaced by the newer one.
func (a *app) loadCommitAuthors() {
	if a.session == nil {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	a.authorsToken++
	token := a.authorsToken
	a.authorsLoading = true
	a.authorsError = ""
	go func() {
		defer a.recoverBackground()
		authors, err := session.Authors(context.Background())
		a.update(func() {
			if repositoryGeneration != a.generation || token != a.authorsToken {
				return
			}
			a.authorsLoading = false
			if err != nil {
				a.authorsError = safeError(err)
				return
			}
			a.authors = authors
		})
	}()
}

// filterAuthorChoices are the authors the popup offers: all of them when it
// first opens, those containing what was typed while filtering.
func (a *app) filterAuthorChoices(typed string, filtering bool) []string {
	authors := a.authors
	if filtering {
		needle := strings.ToLower(strings.TrimSpace(typed))
		if needle != "" {
			matched := make([]string, 0, len(authors))
			for _, author := range authors {
				if strings.Contains(strings.ToLower(author), needle) {
					matched = append(matched, author)
				}
			}
			authors = matched
		}
	}
	if len(authors) > 200 {
		authors = authors[:200]
	}
	return authors
}

// filterPopupList wraps the options of a filter popup in a vertical scroll,
// so a long author or branch list scrolls instead of running off the window.
// The popup panel keeps room for about a dozen options, and moving the
// highlight with the arrows scrolls the option into view.
func filterPopupList(c *ui.Context, theme *ui.Theme, build func()) {
	ui.Scroll(c).MaxHeight(theme.Space(70)).Children(build)
}

// commitFilterModal is the filter dialog, over the page it filters.
func (a *app) commitFilterModal(c *ui.Context) {
	if !a.filterOpen {
		return
	}
	theme := c.Theme()
	draft := a.commitFilterDraft()
	title := "Filter commits"
	subtitle := "Narrow the History list by message, author and branch."
	switch a.filterTarget {
	case sectionBranches:
		subtitle = "Narrow this branch's commits by message and author."
	case sectionGraph:
		subtitle = "Narrow the graph by message, author and branch."
	}
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(560).MaxWidthPercent(92).MaxHeightPercent(94).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr(title)).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, filterIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr(title)).FontSize(19).Bold()
							ui.Text(c, tr(subtitle)).TextColor(theme.TextMuted).
								FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close filter", "Escape", closeIcon, false, a.closeCommitFilter)
					})
					ui.Divider(c)
					a.commitFilterBody(c, draft)
					ui.Divider(c)
					a.commitFilterFooter(c)
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCommitFilter()
			}
		})
	})
}

// commitFilterBody is the fields the filter is made of.
func (a *app) commitFilterBody(c *ui.Context, draft *repository.CommitFilter) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(16).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, tr("Message")).Bold().FontSize(theme.FontSize - 1)
			field := ui.SearchField(c, &draft.Query).
				Placeholder(tr("Search commit messages")).
				Label(tr("Search commit messages"))
			if field.Submitted() {
				a.applyCommitFilter()
			}
		})
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Icon(c, personIcon).FontSize(13).TextColor(theme.TextMuted)
				ui.Text(c, tr("Author")).Bold().FontSize(theme.FontSize - 1)
			})
			combo := ui.ComboboxBase(c, &draft.Author)
			combo.Input.Grow(1).Padding(7, 10).
				Background(theme.Surface).Border(1, theme.Border).
				Radius(theme.Radius).
				Label(tr("Filter by author")).
				Placeholder(tr("Any author"))
			combo.Popup(func(panel ui.Element) {
				panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).
					Radius(theme.Radius)
				choices := a.filterAuthorChoices(draft.Author, combo.Filtering())
				filterPopupList(c, theme, func() {
					if len(choices) == 0 {
						item := ui.Row(c).Padding(7, 10)
						item.Children(func() {
							ui.Text(c, tr("No authors match")).TextColor(theme.TextMuted)
						})
						return
					}
					for _, author := range choices {
						item := combo.Item(author).Padding(7, 10)
						if item.Highlighted() {
							item.Background(theme.SurfaceHover)
						}
						item.Children(func() {
							ui.Text(c, author).SingleLine().Tooltip(author)
						})
					}
				})
			})
			if author, ok := combo.Chosen(); ok {
				draft.Author = author
			}
		})
		// A branch's commit list is already one branch, so only History
		// chooses which branch to read.
		if !a.filterTargetBranches() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Icon(c, branchIcon).FontSize(13).TextColor(theme.TextMuted)
					ui.Text(c, tr("Branch")).Bold().FontSize(theme.FontSize - 1)
				})
				selects := ui.SelectBase(c, &draft.Branch)
				selects.Trigger.Grow(1).Padding(7, 10).Gap(8).
					Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
					Label(tr("Filter by branch")).
					Children(func() {
						ui.Text(c, branchFilterLabel(a.snapshot.Branches, draft.Branch)).Grow(1)
					})
				selects.Popup(func(panel ui.Element) {
					panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).
						Radius(theme.Radius)
					filterPopupList(c, theme, func() {
						a.branchFilterOptions(c, selects, theme)
					})
				})
			})
		}
		a.commitFilterStatus(c, draft)
	})
}

// branchFilterLabel names the branch the filter reads, or every branch.
func branchFilterLabel(branches []repository.Branch, name string) string {
	if name == "" {
		return tr("All branches")
	}
	for _, branch := range branches {
		if branch.Name == name {
			return branchDisplay(branch)
		}
	}
	return name
}

// branchFilterOptions builds the branch choices of the filter: every branch,
// then the local branches. It is generic in the branch list type so the
// option element it returns matches the select's own value type.
func (a *app) branchFilterOptions(c *ui.Context, selects ui.SelectParts[string], theme *ui.Theme) {
	item := selects.Item("").Padding(7, 10)
	if item.Highlighted() {
		item.Background(theme.SurfaceHover)
	}
	item.Children(func() {
		ui.Text(c, tr("All branches")).SingleLine()
	})
	for _, branch := range a.snapshot.Branches {
		item := selects.Item(branch.Name).Padding(7, 10)
		if item.Highlighted() {
			item.Background(theme.SurfaceHover)
		}
		item.Children(func() {
			ui.Text(c, branchDisplay(branch)).SingleLine().Tooltip(branch.Name)
		})
	}
}

// commitFilterStatus says what the filter is doing: reading authors or
// commits, why it failed, or how many commits match once the fields equal the
// filter that is applied.
func (a *app) commitFilterStatus(c *ui.Context, draft *repository.CommitFilter) {
	theme := c.Theme()
	text := tr("Matches the commit message, author and branch.")
	if a.filterTargetBranches() {
		text = tr("Matches the commit message and author.")
	}
	color := theme.TextMuted
	query := a.commitQueryFor(a.filterTarget)
	switch {
	case a.authorsLoading:
		text = tr("Reading authors…")
	case a.authorsError != "":
		text = trf("Authors could not be read: %s", a.authorsError)
		color = theme.Danger
	case !a.filterTargetBranches() && query.loading:
		text = tr("Searching commits…")
	case !a.filterTargetBranches() && query.err != "":
		text = query.err
		color = theme.Danger
	case !a.filterTargetBranches() && query.narrows() &&
		commitFiltersEqual(query.applied, *draft):
		text = trf("%d commits match", len(query.matches))
	case a.filterTargetBranches() && a.branchFilterActive() &&
		commitFiltersEqual(a.branchFilter, *draft):
		text = trf("%d commits match", len(a.branchCommits))
	}
	ui.Text(c, text).TextColor(color).FontSize(theme.FontSize - 1)
}

// commitFilterFooter is the modal's actions: clear, refresh, and search.
func (a *app) commitFilterFooter(c *ui.Context) {
	theme := c.Theme()
	searching := a.commitQueryFor(a.filterTarget).loading
	if a.filterTargetBranches() {
		searching = a.branchCommitsLoading
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, tr("Escape cancels")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		iconButton(c, "Clear filters", "Show every commit again", closeIcon, false, func() {
			a.clearCommitFilter(a.filterTarget)
		})
		iconButton(c, "Refresh filters", "Read the authors and matching commits again", refreshIcon, searching, a.refreshCommitFilter)
		primaryIconButton(c, "Search commits", "Apply the filter", searchIcon, searching, a.applyCommitFilter)
	})
}

// branchFilterActive reports whether the Branches page's commit list is
// narrowed.
func (a *app) branchFilterActive() bool {
	return commitFilterNarrows(a.branchFilter)
}

// commitFilterSummary says what a filter narrows to, in the user's words,
// for the chip beside the list's title.
func commitFilterSummary(filter repository.CommitFilter) string {
	var parts []string
	if filter.Query != "" {
		parts = append(parts, fmt.Sprintf("%q", filter.Query))
	}
	if filter.Author != "" {
		parts = append(parts, filter.Author)
	}
	if filter.Branch != "" {
		parts = append(parts, filter.Branch)
	}
	summary := strings.Join(parts, " · ")
	runes := []rune(summary)
	if len(runes) > 40 {
		return string(runes[:39]) + "…"
	}
	return summary
}
