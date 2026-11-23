package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// The Activity Center's second view lists the people behind the repository's
// history: who contributed, how much, and when they last did. Choosing one
// shows what they committed most recently; a commit opens its action list in
// the command palette, and the whole history can be narrowed to them.

const (
	activityViewFeed = iota
	activityViewContributors

	contributorRecentLimit = 20

	// contributorPageSize is how many contributors are drawn at first and how
	// many each "show more" adds.
	contributorPageSize = 50
)

// How the contributor list is ordered.
const (
	contributorSortCommits = 0
	contributorSortRecent  = 1
	contributorSortName    = 2
)

// contributorSession is the optional capability behind the contributor list;
// sessions without it show a notice instead of failing.
type contributorSession interface {
	Contributors(context.Context) ([]repository.Contributor, error)
}

type contributorsState struct {
	app *app
	// branch is the branch the list was read for, "" for every branch.
	branch      string
	loading     bool
	err         string
	unavailable bool
	list        []repository.Contributor
	token       uint64

	selected int
	// sortBy orders the list; limit is how many rows are drawn (0 is one page),
	// so a repository with hundreds of contributors stays quick to draw.
	sortBy int
	limit  int
	// The filtered, sorted list is kept until the search, order or data change.
	cache      []repository.Contributor
	cacheQuery string
	cacheSort  int
	cacheToken uint64
	cacheValid bool
	// expanded is the key of the contributor whose recent commits are shown.
	expanded       string
	commits        []repository.Commit
	commitsLoading bool
	commitsErr     string
	commitsToken   uint64
}

func contributorKey(c repository.Contributor) string {
	if email := strings.ToLower(strings.TrimSpace(c.Email)); email != "" {
		return email
	}
	return "name:" + strings.ToLower(strings.TrimSpace(c.Name))
}

// contributorAuthorFilter is what Git's author filter takes for a contributor:
// the email when there is one, which is exact, else the name.
func contributorAuthorFilter(c repository.Contributor) string {
	if strings.TrimSpace(c.Email) != "" {
		return c.Email
	}
	return c.Name
}

// ensureContributors reads the active repository's contributors when the view
// is first shown or the active tab changed.
func (s *shell) ensureContributors() {
	state := &s.activity.contributors
	a := s.activeApp()
	if a == nil || a.session == nil {
		*state = contributorsState{token: state.token, unavailable: true}
		return
	}
	// The list follows the branch chosen on the Overview's card.
	branch := a.cockpit.contributors.branch
	if state.app == a && state.branch == branch && (state.loading || state.list != nil || state.err != "" || state.unavailable) {
		return
	}
	token := state.token + 1
	*state = contributorsState{app: a, token: token, branch: branch}
	var read func(context.Context) ([]repository.Contributor, error)
	switch session := a.session.(type) {
	case contributorSummarySession:
		read = func(ctx context.Context) ([]repository.Contributor, error) {
			summary, err := session.ContributorSummary(ctx, branch)
			return summary.Contributors, err
		}
	case contributorSession:
		if branch == "" {
			read = session.Contributors
		}
	}
	if read == nil {
		state.unavailable = true
		return
	}
	state.loading = true
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		list, err := read(ctx)
		a.update(func() {
			current := &s.activity.contributors
			if generation != a.generation || current.token != token {
				return
			}
			current.loading = false
			if err != nil {
				current.err = safeError(err)
				return
			}
			if list == nil {
				list = []repository.Contributor{}
			}
			current.list = list
			current.cacheValid = false
		})
	}()
}

// visibleContributors is the whole list matching the search, in the chosen
// order. It is computed once and reused until the search, order or data change.
func (s *shell) visibleContributors() []repository.Contributor {
	state := &s.activity.contributors
	needle := strings.ToLower(strings.TrimSpace(s.activity.query))
	if state.cacheValid && state.cacheQuery == needle && state.cacheSort == state.sortBy && state.cacheToken == state.token {
		return state.cache
	}
	matched := make([]repository.Contributor, 0, len(state.list))
	for _, contributor := range state.list {
		if needle == "" || strings.Contains(strings.ToLower(contributor.Name), needle) ||
			strings.Contains(strings.ToLower(contributor.Email), needle) {
			matched = append(matched, contributor)
		}
	}
	switch state.sortBy {
	case contributorSortRecent:
		sort.SliceStable(matched, func(i, j int) bool { return matched[i].LastCommit > matched[j].LastCommit })
	case contributorSortName:
		sort.SliceStable(matched, func(i, j int) bool {
			return strings.ToLower(authorLabel(matched[i].Name, matched[i].Email)) < strings.ToLower(authorLabel(matched[j].Name, matched[j].Email))
		})
	}
	state.cache, state.cacheQuery, state.cacheSort, state.cacheToken, state.cacheValid = matched, needle, state.sortBy, state.token, true
	return matched
}

// shownContributors is the part of the list that is drawn: one page, and as
// many more as "show more" has added.
func (s *shell) shownContributors() []repository.Contributor {
	list := s.visibleContributors()
	limit := s.activity.contributors.limit
	if limit <= 0 {
		limit = contributorPageSize
	}
	if len(list) > limit {
		return list[:limit]
	}
	return list
}

// toggleContributor shows or hides a contributor's recent commits, reading
// them from Git when they are shown.
func (s *shell) toggleContributor(contributor repository.Contributor) {
	state := &s.activity.contributors
	key := contributorKey(contributor)
	if state.expanded == key {
		state.expanded = ""
		return
	}
	a := state.app
	state.expanded, state.commits, state.commitsErr = key, nil, ""
	state.commitsToken++
	token := state.commitsToken
	if a == nil || a.session == nil {
		return
	}
	state.commitsLoading = true
	session := a.session
	generation := a.generation
	ctx := a.repositoryWork.Context()
	filter := repository.CommitFilter{Author: contributorAuthorFilter(contributor), Limit: contributorRecentLimit}
	go func() {
		defer a.recoverBackground()
		commits, err := requireCommitSearchSession(session).SearchCommits(ctx, filter)
		a.update(func() {
			current := &s.activity.contributors
			if generation != a.generation || current.commitsToken != token {
				return
			}
			current.commitsLoading = false
			if err != nil {
				current.commitsErr = safeError(err)
				return
			}
			current.commits = commits
		})
	}()
}

func (s *shell) moveContributor(delta int) {
	list := s.shownContributors()
	if len(list) == 0 {
		return
	}
	state := &s.activity.contributors
	state.selected = (state.selected + delta + len(list)) % len(list)
	s.activity.reveal = true
}

func (s *shell) toggleSelectedContributor() {
	list := s.shownContributors()
	state := &s.activity.contributors
	if state.selected >= 0 && state.selected < len(list) {
		s.toggleContributor(list[state.selected])
	}
}

// filterHistoryByAuthor narrows History to one author and shows it.
func (a *app) filterHistoryByAuthor(author string) {
	a.setWorkspaceTab(sectionHistory)
	query := a.commitQueryFor(sectionHistory)
	selected := commitHashAt(a.visibleCommits(), a.selectedCommit)
	query.applied = normalizeCommitFilter(repository.CommitFilter{Author: author})
	query.draft = query.applied
	query.defaulted = false
	query.limit = repository.SnapshotCommitLimit
	query.more = false
	query.matches = nil
	a.runCommitFilter(sectionHistory, selected)
}

// openContributorCommit closes the center and lists the commit's actions in the
// palette.
func (s *shell) openContributorCommit(commit repository.Commit) {
	a := s.activity.contributors.app
	s.closeActivityCenter(false)
	if a == nil {
		return
	}
	s.openCommandPalette(nil)
	if s.palette.open {
		s.openCommitActions(a, commit)
	}
}

func contributorDate(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Local().Format("2006-01-02")
}

func contributorCountLabel(count int) string {
	if count == 1 {
		return tr("1 commit")
	}
	return trf("%d commits", count)
}

// contributorsList draws the contributor view inside the center's scroll area.
func (s *shell) contributorsList(c *ui.Context, theme *ui.Theme) {
	state := &s.activity.contributors
	all := s.visibleContributors()
	list := s.shownContributors()
	if state.selected >= len(list) {
		state.selected = max(len(list)-1, 0)
	}
	message := func(title, detail string) {
		ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
			ui.Icon(c, personIcon).FontSize(25).TextColor(theme.TextMuted)
			ui.Text(c, title).Bold()
			if detail != "" {
				ui.Text(c, detail).TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(460)
			}
		})
	}
	switch {
	case state.unavailable:
		message(tr("Contributors are not available here"), tr("Open a Git repository in the active tab to see who contributed."))
		return
	case state.loading:
		message(tr("Reading the history…"), "")
		return
	case state.err != "":
		message(tr("Could not read the contributors"), state.err)
		return
	case len(list) == 0:
		message(tr("No contributors match"), tr("Clear the search to see everyone."))
		return
	}
	top := 1
	for _, contributor := range state.list {
		top = max(top, contributor.Commits)
	}
	ui.Column(c).FillWidth().Gap(4).Role(ui.RoleListBox).Label(tr("Contributors")).Children(func() {
		for index, contributor := range list {
			index, contributor := index, contributor
			key := contributorKey(contributor)
			expanded := state.expanded == key
			c.Key("contributor:" + key)
			s.contributorRow(c, theme, contributor, top, index == state.selected, expanded, func() {
				state.selected = index
				s.toggleContributor(contributor)
			})
			if expanded {
				s.contributorCommits(c, theme, contributor)
			}
		}
		if len(all) > len(list) {
			if ui.Button(c, trf("Show %d more", min(contributorPageSize, len(all)-len(list)))).
				Tooltip(trf("%d of %d contributors are shown", len(list), len(all))).Clicked() {
				state.limit = len(list) + contributorPageSize
			}
		}
		s.activity.reveal = false
	})
}

func (s *shell) contributorRow(c *ui.Context, theme *ui.Theme, contributor repository.Contributor, top int, selected, expanded bool, toggle func()) {
	label := authorLabel(contributor.Name, contributor.Email) + " — " + contributorCountLabel(contributor.Commits)
	row := ui.ButtonBase(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).
		Padding(8, 10).Radius(theme.Radius).Role(ui.RoleOption).Label(label).Selected(selected)
	if selected {
		row.Background(theme.Accent.Alpha(0.12)).Border(1, theme.Accent.Alpha(0.55))
		if s.activity.reveal {
			row.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollNearest})
		}
	} else if row.Hovered() {
		row.Background(theme.SurfaceHover)
	}
	row.Children(func() {
		drawAuthorAvatar(c, contributor.Name, contributor.Email, 28, nil)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, authorLabel(contributor.Name, contributor.Email)).Bold().SingleLine().TextColor(theme.Text)
			if contributor.Email != "" && contributor.Name != "" {
				ui.Text(c, contributor.Email).FontSize(theme.FontSize - 1).SingleLine().MinWidth(0).TextColor(theme.TextMuted)
			}
			// A bar against the top contributor, so the spread reads at a glance.
			ui.Box(c).Height(3).Width(float32(max(4, 200*contributor.Commits/top))).Radius(2).
				Background(theme.Accent.Alpha(0.55))
		})
		ui.Column(c).Gap(2).Shrink(0).AlignItems(ui.End).Children(func() {
			ui.Text(c, contributorCountLabel(contributor.Commits)).FontSize(theme.FontSize - 1).SingleLine().TextColor(theme.Text)
			if last := contributorDate(contributor.LastCommit); last != "" {
				ui.Text(c, trf("Last commit %s", last)).FontSize(theme.FontSize - 2).SingleLine().TextColor(theme.TextMuted)
			}
		})
		icon := chevronRightIcon
		if expanded {
			icon = chevronDownIcon
		}
		ui.Icon(c, icon).FontSize(12).TextColor(theme.TextMuted).Shrink(0)
	})
	if row.Clicked() {
		toggle()
	}
}

// contributorCommits is the expanded part of a contributor's row: their
// recent commits, and the ways to go on from them.
func (s *shell) contributorCommits(c *ui.Context, theme *ui.Theme, contributor repository.Contributor) {
	state := &s.activity.contributors
	ui.Column(c).FillWidth().Gap(2).Padding(0, 0, 6, 38).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(2, 0).Children(func() {
			ui.Text(c, trf("Recent commits by %s", authorLabel(contributor.Name, contributor.Email))).
				FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Grow(1).SingleLine().MinWidth(0)
			author := contributorAuthorFilter(contributor)
			if ui.Button(c, tr("Show in History")).Label(tr("Show this author's commits in History")).
				Tooltip(tr("Narrow History to this author")).Clicked() {
				a := state.app
				s.closeActivityCenter(false)
				if a != nil {
					a.filterHistoryByAuthor(author)
				}
			}
			if ui.Button(c, tr("Search in palette")).Label(tr("Search this author's commits in the palette")).
				Tooltip(tr("Search every commit by this author in the command palette")).Clicked() {
				name := authorLabel(contributor.Name, contributor.Email)
				s.closeActivityCenter(false)
				s.openCommandPalette(nil)
				if s.palette.open {
					s.palette.query = "#by:" + quoteSearchWord(name) + " "
				}
			}
		})
		switch {
		case state.commitsLoading:
			ui.Text(c, tr("Reading their commits…")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		case state.commitsErr != "":
			ui.Text(c, state.commitsErr).FontSize(theme.FontSize - 1).TextColor(theme.Danger)
		case len(state.commits) == 0:
			ui.Text(c, tr("No commits found")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		}
		for _, commit := range state.commits {
			commit := commit
			c.Key("contributor-commit:" + commit.Hash)
			item := ui.ButtonBase(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(8).
				Padding(5, 8).Radius(theme.Radius).Role(ui.RoleOption).
				Label(commit.ShortHash + " " + commit.Subject)
			if item.Hovered() {
				item.Background(theme.SurfaceHover)
			}
			item.Children(func() {
				ui.Text(c, commit.ShortHash).FontSize(theme.FontSize - 1).TextColor(theme.Accent).SingleLine().Shrink(0)
				ui.Text(c, commit.Subject).SingleLine().Grow(1).MinWidth(0).TextColor(theme.Text)
				if commit.UnixTimestamp > 0 {
					ui.Text(c, contributorDate(commit.UnixTimestamp)).FontSize(theme.FontSize - 2).SingleLine().TextColor(theme.TextMuted).Shrink(0)
				}
			})
			if item.Clicked() {
				s.openContributorCommit(commit)
			}
		}
	})
}
