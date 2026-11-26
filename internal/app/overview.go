package app

import (
	"context"
	"errors"
	"hash/fnv"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
)

// The Overview page follows recent commits across several repositories. It is
// Git access is read-only. Each refresh captures the current checkout of each
// followed repository and the timeline pages from those revisions.

const (
	overviewPerRepoLimit = 50
	// overviewConcurrency bounds how many repositories are read at once.
	overviewConcurrency = 4
	overviewPageRows    = 100

	overviewSidebarWidth float32 = 232
)

var overviewIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><circle cx="3.5" cy="4" r="1.1"/><circle cx="3.5" cy="8" r="1.1"/><circle cx="3.5" cy="12" r="1.1"/><path d="M7 4h6.5M7 8h6.5M7 12h4"/></svg>`))

// overviewCommit is one row of the timeline: a commit and the repository it
// was read from.
type overviewCommit struct {
	Repo   string
	Commit repository.Commit
}

// overviewRepoState is how far reading one repository got.
type overviewRepoState struct {
	loading  bool
	err      string
	count    int
	more     bool
	nextTime int64
	branch   string
	revision string
	order    []string
}

type overviewHistorySession interface {
	CommitHistoryHead(context.Context) (branch, revision string, err error)
	CommitHistoryOrder(context.Context, string, time.Time, time.Time, repository.CommitHistorySearch) ([]string, error)
	CommitHistoryPage(context.Context, []string) ([]repository.Commit, error)
}

type overviewRepoPage struct {
	branch, revision string
	order            []string
	commits          []repository.Commit
}

type overviewCommitDetail struct {
	row     overviewCommit
	cancel  context.CancelFunc
	loading bool
	body    string
	files   []repository.CommitFile
	err     string
}

// overviewSetup is the "choose repositories" dialog. Its draft is applied only
// by the confirm button, so cancelling leaves the followed set alone.
type overviewSetup struct {
	open bool
	// isNew says the dialog creates an Overview rather than editing the one on
	// show; name is the draft of its name.
	isNew     bool
	name      string
	selected  map[string]bool
	custom    []string
	names     map[string]string
	resolving bool
	checking  bool
	err       string
}

// overviewState is the Overview tab's page state.
type overviewState struct {
	config         overviewConfig
	rangeIdx       int
	period         *overviewPeriod
	periodDraft    *overviewPeriod
	token          uint64
	ctx            context.Context
	cancel         context.CancelFunc
	readPeriod     overviewPeriod
	repos          map[string]*overviewRepoState
	commits        []overviewCommit
	query          string
	requestedQuery string
	searchTimer    *time.Timer
	filter         string
	shown          int
	scroll         ui.ScrollState
	loaded         time.Time
	started        bool
	setup          overviewSetup
	setupVersion   uint64
	rename         overviewRepo
	renameErr      string
	detail         *overviewCommitDetail
}

// cur is the Overview on show, nil when the user has none yet.
func (o *overviewState) cur() *overviewDef { return o.config.current() }

// followed is the set the Overview on show follows.
func (o *overviewState) followed() []overviewRepo {
	if def := o.cur(); def != nil {
		return def.Repos
	}
	return nil
}

// syncRange points the time-range control at the Overview on show.
func (o *overviewState) syncRange() {
	o.rangeIdx = overviewDefaultRange
	o.period, o.periodDraft = nil, nil
	if def := o.cur(); def != nil {
		if def.Period != nil && def.Period.valid() {
			o.period = def.Period
		}
		for i, days := range overviewRanges {
			if days == def.Days {
				o.rangeIdx = i
			}
		}
	}
}

// saveRange remembers the chosen window on the Overview on show.
func (a *app) saveOverviewRange() {
	o := a.overview
	if def := o.cur(); def != nil {
		def.Days = overviewRanges[o.rangeIdx]
		def.Period = o.period
		_ = saveOverviewConfig(a.dataDir(), o.config)
	}
}

func newOverviewState() *overviewState {
	return &overviewState{
		repos:    map[string]*overviewRepoState{},
		shown:    overviewPageRows,
		rangeIdx: overviewDefaultRange,
	}
}

// overviewIntent says what the Overview page does on arrival.
type overviewIntent int

const (
	// overviewShow reads and shows the chosen Overview.
	overviewShow overviewIntent = iota
	// overviewCreate opens the dialog for a new Overview.
	overviewCreate
	// overviewEdit opens the dialog for the chosen Overview.
	overviewEdit
)

// applyOverviewRequest points the page at the Overview the home page chose and
// does what was asked. The saved set is read again each time: the home page's
// list may have renamed, removed or added Overviews since the tab last looked.
func (a *app) applyOverviewRequest(id string, intent overviewIntent) {
	o := a.overview
	if o == nil {
		return
	}
	o.started = true
	o.config = loadOverviewConfig(a.dataDir())
	if o.config.byID(id) != nil {
		o.config.Active = id
	}
	o.filter, o.query = "", ""
	o.rename = overviewRepo{}
	o.syncRange()
	a.closeOverviewSetup()
	switch {
	case intent == overviewCreate:
		a.stopOverview()
		o.commits, o.repos = nil, map[string]*overviewRepoState{}
		a.newOverview()
	case o.cur() == nil || len(o.followed()) == 0:
		a.stopOverview()
		o.commits, o.repos = nil, map[string]*overviewRepoState{}
		a.openOverviewSetup()
	case intent == overviewEdit:
		a.reloadOverview()
		a.openOverviewSetup()
	default:
		a.reloadOverview()
	}
}

// stopOverview ends reads still in flight, for a tab that is going away.
func (a *app) stopOverview() {
	if o := a.overview; o != nil {
		o.token++
		if o.searchTimer != nil {
			o.searchTimer.Stop()
			o.searchTimer = nil
		}
		if o.cancel != nil {
			o.cancel()
			o.cancel = nil
		}
		o.ctx = nil
		o.detail = nil
	}
}

// reloadOverview reads every followed repository again. Each one reports as it
// finishes, so the timeline fills in rather than waiting for the slowest.
func (a *app) reloadOverview() {
	o := a.overview
	if o == nil {
		return
	}
	a.stopOverview()
	o.requestedQuery = strings.TrimSpace(o.query)
	o.commits = nil
	o.repos = map[string]*overviewRepoState{}
	o.resetTimeline()
	o.loaded = time.Time{}
	repos := o.followed()
	if len(repos) == 0 || a.opener == nil {
		return
	}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	o.readPeriod = overviewPeriod{}
	if !o.searching() {
		o.readPeriod = o.overviewReadPeriod(time.Now())
	}
	for _, repo := range repos {
		o.repos[repo.Path] = &overviewRepoState{}
	}
	a.readOverviewPages(o, true)
}

func (o *overviewState) resetTimeline() {
	o.shown = overviewPageRows
	o.scroll.Y = 0
}

func (o *overviewState) canLoadMore() bool {
	filter := o.activeRepoFilter()
	for path, state := range o.repos {
		if state.more && (filter == "" || path == filter) {
			return true
		}
	}
	return false
}

func (a *app) readOverviewPages(o *overviewState, initial bool) {
	if o.ctx == nil || o.anyLoading() {
		return
	}
	ctx, token, period, opener := o.ctx, o.token, o.readPeriod, a.opener
	slots := make(chan struct{}, overviewConcurrency)
	filter := o.activeRepoFilter()
	for _, repo := range o.followed() {
		path := repo.Path
		state := o.repos[path]
		if state == nil || (!initial && (!state.more || (filter != "" && path != filter))) {
			continue
		}
		state.loading = true
		search := overviewSearchForRepo(o.query, repo)
		offset := state.count
		page := overviewRepoPage{branch: state.branch, revision: state.revision, order: state.order}
		go func() {
			defer a.recoverBackground()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			result, err := readOverviewCommits(ctx, opener, path, period, search, offset, page, initial)
			a.update(func() { a.finishOverviewRepo(token, path, result, err) })
		}()
	}
}

func readOverviewCommits(ctx context.Context, opener repositoryOpener, path string, period overviewPeriod, search repository.CommitHistorySearch,
	offset int, page overviewRepoPage, initial bool) (overviewRepoPage, error) {
	session, err := opener.Open(ctx, path)
	if err != nil {
		return page, err
	}
	defer closeRepositorySession(session)
	searcher, ok := session.(overviewHistorySession)
	if !ok {
		return page, errors.New("this repository cannot list commits")
	}
	if initial {
		page.branch, page.revision, err = searcher.CommitHistoryHead(ctx)
		if err != nil {
			return page, err
		}
		page.order, err = searcher.CommitHistoryOrder(ctx, page.revision, period.From, period.To, search)
		if err != nil {
			return page, err
		}
	}
	h := page.order[offset:min(offset+overviewPerRepoLimit+1, len(page.order))]
	page.commits, err = searcher.CommitHistoryPage(ctx, h)
	return page, err
}

func (a *app) finishOverviewRepo(token uint64, path string, page overviewRepoPage, err error) {
	o := a.overview
	if o == nil || token != o.token {
		return
	}
	state := o.repos[path]
	if state == nil {
		return
	}
	state.loading = false
	state.branch, state.revision, state.order = page.branch, page.revision, page.order
	commits := page.commits
	state.more = false
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			state.err = tr("Folder no longer exists")
		case errors.Is(err, repository.ErrNotRepository):
			state.err = tr("Not a Git repository")
		default:
			state.err = safeError(err)
		}
	}
	if err == nil && len(commits) > overviewPerRepoLimit {
		state.more = true
		state.nextTime = commits[overviewPerRepoLimit].UnixTimestamp
		commits = commits[:overviewPerRepoLimit]
	}
	state.count += len(commits)
	for _, commit := range commits {
		o.commits = append(o.commits, overviewCommit{Repo: path, Commit: commit})
	}
	sort.SliceStable(o.commits, func(i, j int) bool {
		left, right := o.commits[i], o.commits[j]
		if left.Commit.UnixTimestamp != right.Commit.UnixTimestamp {
			return left.Commit.UnixTimestamp > right.Commit.UnixTimestamp
		}
		return left.Repo < right.Repo
	})
	if !o.anyLoading() {
		o.loaded = time.Now()
	}
}

// timelineCommits keeps the oldest buffered rows off screen until the other
// repositories have paged far enough to merge their commits below the viewport.
func (o *overviewState) timelineCommits() []overviewCommit {
	var nextTime int64
	var nextRepo string
	filter := o.activeRepoFilter()
	for path, state := range o.repos {
		if !state.more || (filter != "" && path != filter) {
			continue
		}
		if nextRepo == "" || state.nextTime > nextTime || (state.nextTime == nextTime && path < nextRepo) {
			nextTime, nextRepo = state.nextTime, path
		}
	}
	if nextRepo == "" {
		return o.commits
	}
	end := sort.Search(len(o.commits), func(i int) bool {
		row := o.commits[i]
		return row.Commit.UnixTimestamp < nextTime || (row.Commit.UnixTimestamp == nextTime && row.Repo > nextRepo)
	})
	return o.commits[:end]
}

func (o *overviewState) anyLoading() bool {
	for _, state := range o.repos {
		if state.loading {
			return true
		}
	}
	return false
}

func (o *overviewState) failures() int {
	count := 0
	for _, state := range o.repos {
		if state.err != "" {
			count++
		}
	}
	return count
}

// visibleCommits applies the sidebar filter to the ordered timeline.
func (o *overviewState) visibleCommits() []overviewCommit {
	commits := o.timelineCommits()
	filter := o.activeRepoFilter()
	if filter == "" {
		return commits
	}
	var out []overviewCommit
	for _, row := range commits {
		if row.Repo == filter {
			out = append(out, row)
		}
	}
	return out
}

// overviewRepoColor gives a repository the same color every time, so its pill
// is recognisable down a long timeline.
func overviewRepoColor(path string) ui.Color {
	return overviewHashColor(path)
}

func overviewHashColor(value string) ui.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(value)))
	names := accentNames
	return accentChoices[names[int(h.Sum32())%len(names)]]
}

func overviewInitial(name, email string) string {
	for _, source := range []string{name, email} {
		for _, r := range source {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return strings.ToUpper(string(r))
			}
		}
	}
	return "?"
}

// overviewDayLabel names the day a group of rows belongs to.
func overviewDayLabel(day, now time.Time) string {
	y1, m1, d1 := day.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return tr("Today")
	}
	yesterday := now.AddDate(0, 0, -1)
	y3, m3, d3 := yesterday.Date()
	if y1 == y3 && m1 == m3 && d1 == d3 {
		return tr("Yesterday")
	}
	if y1 == y2 {
		return day.Format("Mon, Jan 2")
	}
	return day.Format("Mon, Jan 2, 2006")
}

// ---- Setup dialog state ----------------------------------------------------

// followableRecents lists the recent repositories the Overview can follow:
// Git repositories on this machine. Editor-only folders are not repositories,
// and an SSH workspace would open a connection for every refresh.
func (a *app) followableRecents() (local []recentRepository, remote []recentRepository) {
	for _, repo := range a.recentRepositories {
		if repo.EditorOnly {
			continue
		}
		if location, err := workspace.Parse(repo.Path); err == nil && location.IsRemote() {
			remote = append(remote, repo)
			continue
		}
		local = append(local, repo)
	}
	return local, remote
}

// openOverviewSetup edits the Overview on show, or creates the first one when
// there is none.
func (a *app) openOverviewSetup() {
	a.beginOverviewSetup(a.overview != nil && a.overview.cur() == nil)
}

// newOverview opens the dialog for an additional Overview.
func (a *app) newOverview() { a.beginOverviewSetup(true) }

func (a *app) beginOverviewSetup(isNew bool) {
	o := a.overview
	if o == nil {
		return
	}
	local, _ := a.followableRecents()
	setup := &o.setup
	o.setupVersion++
	*setup = overviewSetup{open: true, isNew: isNew, selected: map[string]bool{}, names: map[string]string{}}
	if isNew {
		name := tr("New overview")
		if len(o.config.Overviews) == 0 {
			name = tr("My repositories")
		}
		setup.name = uniqueOverviewName(o.config.Overviews, name, "")
		if len(o.config.Overviews) == 0 {
			// The first time: start with the most recent few ticked, so the
			// common case is one click on the confirm button.
			for i, repo := range local {
				if i >= 5 {
					break
				}
				setup.selected[repo.Path] = true
			}
		}
		return
	}
	def := o.cur()
	setup.name = def.Name
	known := make(map[string]string, len(local))
	for _, repo := range local {
		known[cleanRepoPath(repo.Path)] = repo.Path
	}
	var unmatched []string
	for _, repo := range def.Repos {
		path := repo.Path
		if recent := known[cleanRepoPath(path)]; recent != "" {
			path = recent
			setup.selected[path] = true
		} else {
			setup.custom = append(setup.custom, path)
			unmatched = append(unmatched, path)
		}
		if repo.Name != "" && setup.names[path] == "" {
			setup.names[path] = repo.Name
		}
	}
	if len(unmatched) > 0 && len(known) > 0 && a.opener != nil {
		a.matchOverviewRecentRoots(o, known, unmatched)
	}
}

func (a *app) matchOverviewRecentRoots(o *overviewState, known map[string]string, unmatched []string) {
	o.setup.resolving = true
	version, opener := o.setupVersion, a.opener
	go func() {
		defer a.recoverBackground()
		matches := make(map[string]string)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, path := range unmatched {
			if root, err := openOverviewWorkspaceRoot(ctx, opener, path); err == nil {
				matches[path] = known[root]
			}
		}
		a.update(func() {
			if a.overview != o || o.setupVersion != version {
				return
			}
			setup := &o.setup
			setup.resolving = false
			kept := setup.custom[:0]
			for _, path := range setup.custom {
				recent := matches[path]
				if recent == "" {
					kept = append(kept, path)
					continue
				}
				setup.selected[recent] = true
				if setup.names[recent] == "" {
					setup.names[recent] = setup.names[path]
				}
			}
			setup.custom = kept
		})
	}()
}

func (a *app) closeOverviewSetup() {
	if o := a.overview; o != nil {
		o.setupVersion++
		o.setup = overviewSetup{}
	}
}

// applyOverviewSetup stores the draft: a new Overview, or the changes to the
// one on show, and reads it.
func (a *app) applyOverviewSetup() {
	o := a.overview
	if o == nil || !o.setup.open || o.setup.resolving {
		return
	}
	local, _ := a.followableRecents()
	var repos []overviewRepo
	for _, repo := range local {
		if o.setup.selected[repo.Path] {
			repos = append(repos, overviewRepo{Path: repo.Path, Name: o.setup.names[repo.Path]})
		}
	}
	for _, path := range o.setup.custom {
		repos = append(repos, overviewRepo{Path: path, Name: o.setup.names[path], Custom: true})
	}
	repos = normalizeOverviewRepos(repos)
	name := cleanOverviewName(o.setup.name)
	if name == "" {
		name = tr("Overview")
	}
	if o.setup.isNew {
		name = uniqueOverviewName(o.config.Overviews, name, "")
		id := newOverviewID(o.config.Overviews)
		o.config.Overviews = append(o.config.Overviews, overviewDef{
			ID: id, Name: name, Repos: repos, Days: overviewRanges[overviewDefaultRange],
		})
		o.config.Active = id
		o.syncRange()
	} else if def := o.cur(); def != nil {
		def.Name = uniqueOverviewName(o.config.Overviews, name, def.ID)
		def.Repos = repos
	}
	o.filter, o.query = "", ""
	_ = saveOverviewConfig(a.dataDir(), o.config)
	a.closeOverviewSetup()
	a.reloadOverview()
}

func (o *overviewState) selectedCount() int {
	count := len(o.setup.custom)
	for _, on := range o.setup.selected {
		if on {
			count++
		}
	}
	return count
}

// addOverviewFolder asks for a folder, checks it is a Git repository, and adds
// it to the draft. Opening it is the check: it is the same test the picker
// applies, so a folder accepted here is one a tab can open later.
func (a *app) addOverviewFolder() {
	o := a.overview
	if o == nil || o.setup.resolving || o.setup.checking || a.opener == nil {
		return
	}
	o.setup.checking = true
	o.setup.err = ""
	opener := a.opener
	options := mygo.OpenDialogOptions{
		Parent:      a.window,
		Title:       tr("Add a repository folder"),
		ButtonLabel: tr("Add"),
		Directory:   true,
	}
	version := o.setupVersion
	go func() {
		defer a.recoverBackground()
		paths, err := chooseRepositoryFolder(options)
		path := ""
		if err == nil && len(paths) > 0 {
			path = cleanRepoPath(paths[0])
			root, openErr := openOverviewWorkspaceRoot(context.Background(), opener, path)
			if openErr == nil {
				path = root
			}
			err = openErr
		}
		a.update(func() {
			if a.overview != o || version != o.setupVersion || !o.setup.open {
				return
			}
			o.setup.checking = false
			switch {
			case path == "" && err == nil:
				// The chooser was cancelled.
			case errors.Is(err, repository.ErrNotRepository):
				o.setup.err = trf("%s is not a Git repository", repoName(path))
			case err != nil:
				o.setup.err = safeError(err)
			default:
				a.addOverviewDraftPath(path)
			}
		})
	}()
}

func openOverviewWorkspaceRoot(ctx context.Context, opener repositoryOpener, path string) (string, error) {
	session, err := opener.Open(ctx, path)
	if err != nil {
		return "", err
	}
	defer closeRepositorySession(session)
	if location := openedLocation(session, repository.Snapshot{Root: path}, path); location.Valid() {
		return cleanRepoPath(location.String()), nil
	}
	return cleanRepoPath(path), nil
}

// addOverviewDraftPath adds a folder to the draft: ticking it when it is one of
// the recent repositories, listing it under added folders otherwise.
func (a *app) addOverviewDraftPath(path string) {
	o := a.overview
	local, _ := a.followableRecents()
	for _, repo := range local {
		if cleanRepoPath(repo.Path) == path {
			o.setup.selected[repo.Path] = true
			return
		}
	}
	for _, existing := range o.setup.custom {
		if existing == path {
			return
		}
	}
	o.setup.custom = append(o.setup.custom, path)
}

// ---- Page ------------------------------------------------------------------

func (a *app) overviewView(c *ui.Context) {
	o := a.overview
	theme := c.Theme()
	// The same floating islands the repository page uses: a header bar, then
	// the content card, on the window's backdrop.
	ui.Box(c).Fill().Background(islandBackdrop(theme)).Children(func() {
		ui.Column(c).Fill().Padding(islandGap / 2).Children(func() {
			a.overviewHeader(c, o)
			islandCard(c, ui.Row(c).Grow(1).FillWidth().MinHeight(0).Clip()).Children(func() {
				a.overviewSidebar(c, o)
				ui.Box(c).Width(1).FillHeight().Background(theme.Border)
				a.overviewMain(c, o)
			})
		})
	})
	a.overviewSetupModal(c, o)
	a.overviewRenameModal(c, o)
	a.overviewPeriodModal(c, o)
}

// overviewHeader is the repository page's bar, adapted: the Overview's own name
// (a dropdown over all of them) where the repository's name and branch sit,
// and only the controls that mean something here on the right — refresh, the
// repository set, the theme and settings. Fetch, pull, push and the editor
// belong to one repository, so they are left out.
func (a *app) overviewHeader(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	header := islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(6, 14)).Label(tr("Overview header"))
	header.Children(func() {
		a.overviewTitle(c, o)
		if def := o.cur(); def != nil {
			ui.Text(c, a.overviewSubtitle(o)).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).
				SingleLine().MinWidth(0)
		}
		ui.Row(c).Grow(1).Justify(ui.End).AlignItems(ui.Center).Gap(6).Children(func() {
			if o.cur() != nil {
				a.overviewRangeControl(c, o)
				toolbarSeparator(c)
				iconButton(c, "Refresh overview", "Read the repositories again (does not fetch)", refreshIcon,
					len(o.followed()) == 0, a.reloadOverview)
				iconButton(c, "Edit overview", "Rename this Overview or change its repositories", folderIcon,
					false, a.openOverviewSetup)
			}
			themeSettings := a.anyHeaderActionVisible("theme", "settings")
			if themeSettings && o.cur() != nil {
				toolbarSeparator(c)
			}
			if a.headerActionVisible("theme") {
				a.themeButton(c)
			}
			if a.headerActionVisible("settings") {
				iconButton(c, "Settings", "Settings (Ctrl+,)", settingsIcon, false, a.openSettings)
			}
		})
	})
}

// overviewTitle is the Overview's name in the bar. Switching, creating and
// deleting Overviews happen from the home page's Overview entry.
func (a *app) overviewTitle(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	name := tr("Overview")
	if def := o.cur(); def != nil {
		name = def.Name
	}
	ui.Icon(c, overviewIcon).FontSize(16).TextColor(theme.Accent)
	ui.Text(c, name).FontSize(16).Bold().SingleLine().MaxWidth(280).Tooltip(name)
}

func (a *app) overviewSubtitle(o *overviewState) string {
	n := len(o.followed())
	if n == 0 {
		return ""
	}
	text := trf("%d repositories", n)
	if n == 1 {
		text = tr("1 repository")
	}
	if o.anyLoading() {
		return text + " · " + tr("reading…")
	}
	if !o.loaded.IsZero() {
		text += " · " + trf("updated %s", o.loaded.Format("15:04"))
	}
	return text
}

// overviewSidebar lists the followed repositories with how each read went. A
// click narrows the timeline to one of them.
func (a *app) overviewSidebar(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	ui.Column(c).Width(overviewSidebarWidth).FillHeight().Padding(12, 10).Gap(4).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(0, 6, 0, 8).Gap(6).Children(func() {
			ui.Text(c, tr("Repositories")).Bold().TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
				Grow(1).MinWidth(0).SingleLine()
			flatIconButton(c, "Choose repositories", "Choose which repositories the Overview follows",
				plusIcon, 14, 5, false, a.openOverviewSetup)
		})
		ui.Scroll(c).Grow(1).FillWidth().Label(tr("Followed repositories")).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				total := len(o.commits)
				filter := o.activeRepoFilter()
				a.overviewSidebarRow(c, "", tr("All repositories"), "", theme.Accent, filter == "",
					strconvCount(total), "", func() { o.filter = ""; o.resetTimeline() })
				for _, repo := range o.followed() {
					repo := repo
					state := o.repos[repo.Path]
					badge, problem, branch := "", "", ""
					if state != nil {
						branch = state.branch
						if branch == "" && state.revision != "" {
							branch = tr("Detached HEAD")
						}
						switch {
						case state.loading || o.searchTimer != nil:
							badge = "…"
						case state.err != "":
							problem = state.err
							badge = "!"
						default:
							badge = strconvCount(state.count)
							if state.more {
								badge += "+"
							}
						}
					}
					a.overviewSidebarRow(c, repo.Path, repo.displayName(), branch, overviewRepoColor(repo.Path),
						filter == repo.Path, badge, problem, func() {
							if o.filter == repo.Path {
								o.filter = ""
							} else {
								o.filter = repo.Path
							}
							o.resetTimeline()
						})
				}
			})
		})
	})
}

func strconvCount(n int) string {
	return trf("%d", n)
}

func (a *app) overviewSidebarRow(c *ui.Context, key, name, branch string, color ui.Color, selected bool, badge, problem string, run func()) {
	theme := c.Theme()
	item := ui.Row(c).Key("overview-repo:" + key).FillWidth().AlignItems(ui.Center).Radius(theme.Radius)
	if key != "" {
		item.Padding(0, 6, 0, 0)
	}
	switch {
	case selected:
		item.Background(theme.Accent.Alpha(0.12))
	case item.Hovered():
		item.Background(theme.SurfaceHover)
	}
	item.Children(func() {
		tip := overviewRowTooltip(key, name, problem)
		if branch != "" {
			tip += "\n" + branch
		}
		row := ui.ButtonBase(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Justify(ui.Start).
			Gap(8).Padding(7, 8).Radius(theme.Radius).Label(name).Tooltip(tip).
			Disabled(a.overview.searching()).OnClick(run)
		row.Children(func() {
			ui.Box(c).Size(8, 8).Shrink(0).Radius(4).Background(color)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				text := ui.Text(c, name).SingleLine()
				if selected {
					text.Bold()
				}
				if branch != "" {
					ui.Text(c, branch).SingleLine().FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
				}
			})
			if badge != "" {
				tone := theme.TextMuted
				if problem != "" {
					tone = theme.Danger
				}
				ui.Text(c, badge).FontSize(theme.FontSize - 1).TextColor(tone)
			}
		})
		if key != "" {
			flatIconButton(c, "Rename repository", name, editIcon, 14, 4, false, func() {
				a.overview.rename = overviewRepo{Path: key, Name: name}
				a.overview.renameErr = ""
			})
			flatIconButton(c, trf("Remove %s", name), "Remove repository", trashIcon, 14, 4, a.confirm != nil,
				func() { a.confirmRemoveOverviewRepo(key, name) })
		}
	})
}

func (a *app) confirmRemoveOverviewRepo(path, name string) {
	o := a.overview
	if o == nil || o.cur() == nil {
		return
	}
	id := o.cur().ID
	a.askConfirm(confirmRequest{
		title:        "Remove repository",
		message:      trf("Remove %s from this Overview?", name),
		detail:       "The repository and its files will remain on disk.",
		confirmLabel: "Remove",
		destructive:  true,
		run: func() {
			if a.overview == o && o.cur() != nil && o.cur().ID == id {
				a.removeOverviewRepo(o, id, path)
			}
		},
	})
}

func (a *app) removeOverviewRepo(o *overviewState, id, path string) {
	config := o.config
	config.Overviews = slices.Clone(config.Overviews)
	def := config.byID(id)
	def.Repos = slices.DeleteFunc(slices.Clone(def.Repos), func(repo overviewRepo) bool { return repo.Path == path })
	if err := saveOverviewConfig(a.dataDir(), config); err != nil {
		a.notifyError(err)
		return
	}
	o.config = config
	delete(o.repos, path)
	o.commits = slices.DeleteFunc(o.commits, func(row overviewCommit) bool { return row.Repo == path })
	if o.detail != nil && o.detail.row.Repo == path {
		if o.detail.cancel != nil {
			o.detail.cancel()
		}
		o.detail = nil
	}
	if o.filter == path {
		o.filter = ""
		o.resetTimeline()
	}
	if len(def.Repos) == 0 {
		a.stopOverview()
	}
}

func overviewRowTooltip(path, name, problem string) string {
	if path == "" {
		return name
	}
	if problem != "" {
		return path + "\n" + problem
	}
	return path
}

func (a *app) overviewMain(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
		if o.cur() == nil || len(o.followed()) == 0 {
			a.overviewEmpty(c, o, "folder")
			return
		}
		a.overviewToolbar(c, o)
		rows := o.visibleCommits()
		switch {
		case o.searchTimer != nil || (len(rows) == 0 && (o.anyLoading() || o.canLoadMore())):
			label := "Reading repositories…"
			if o.searching() {
				label = "Searching commits…"
			}
			ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Spinner(c).Label(tr(label))
				ui.Text(c, tr(label)).TextColor(theme.TextMuted)
			})
			a.readOverviewPages(o, false)
		case len(rows) == 0:
			a.overviewEmpty(c, o, "quiet")
		default:
			a.overviewTimeline(c, o, rows)
		}
	})
}

func (a *app) overviewToolbar(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(8).Padding(12, 20, 4).Children(func() {
		bar := ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Gap(8).
			Padding(3, 10).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius + 2)
		if bar.Hovered() {
			bar.Border(1, theme.Accent.Alpha(0.5))
		}
		bar.Children(func() {
			ui.Icon(c, searchIcon).FontSize(15).TextColor(theme.TextMuted)
			ui.TextInputBase(c, &o.query).Label(tr("Search commits")).
				Placeholder(tr("Search message, hash or repository — by:name filters the author")).
				Height(32).Grow(1).OnChange(func() { a.overviewSearchChanged(o) })
			if strings.TrimSpace(o.query) != "" {
				iconButton(c, "Clear search", "Clear search", closeIcon, false, func() {
					o.query = ""
					a.overviewSearchChanged(o)
				})
			}
		})
		if failed := o.failures(); failed > 0 {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(6, 10).
				Background(theme.Danger.Alpha(0.1)).Border(1, theme.Danger.Alpha(0.35)).Radius(theme.Radius).
				Children(func() {
					ui.Icon(c, conflictIcon).FontSize(14).TextColor(theme.Danger)
					ui.Text(c, trf("%d repositories could not be read — hover them in the list for the reason", failed)).
						FontSize(theme.FontSize - 1).Grow(1).SingleLine()
				})
		}
	})
}

func (a *app) overviewEmpty(c *ui.Context, o *overviewState, kind string) {
	theme := c.Theme()
	ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(10).Padding(24).Children(func() {
		switch kind {
		case "folder":
			ui.Box(c).Size(56, 56).Radius(16).Center().Background(theme.Accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, overviewIcon).FontSize(28).TextColor(theme.Accent)
			})
			ui.Text(c, tr("Follow your repositories in one place")).FontSize(18).Bold()
			iconTextButton(c, "Choose repositories", "Choose which repositories the Overview follows", folderIcon, true,
				a.openOverviewSetup)
		default:
			ui.Icon(c, historyIcon).FontSize(30).TextColor(theme.TextMuted)
			if strings.TrimSpace(o.query) != "" || o.filter != "" {
				ui.Text(c, tr("No commits match")).FontSize(16).Bold()
				ui.Text(c, tr("Try a different search, or show all repositories.")).TextColor(theme.TextMuted)
				iconTextButton(c, "Clear filters", "Show every repository and clear the search", closeIcon, false, func() {
					o.query, o.filter = "", ""
					o.resetTimeline()
					a.overviewSearchChanged(o)
				})
				return
			}
			ui.Text(c, tr("No commits in the selected period")).FontSize(16).Bold()
			ui.Text(c, tr("Nothing was committed in these repositories in that time.")).TextColor(theme.TextMuted)
			if o.period == nil && o.rangeIdx < len(overviewRanges)-1 {
				iconTextButton(c, "Look further back", "Show a longer time range", historyIcon, false, func() {
					o.rangeIdx++
					a.saveOverviewRange()
					a.reloadOverview()
				})
			}
		}
	})
}

// overviewTimeline draws the rows newest first, under a heading per day.
func (a *app) overviewTimeline(c *ui.Context, o *overviewState, rows []overviewCommit) {
	theme := c.Theme()
	now := time.Now()
	limit := min(len(rows), o.shown)
	names := make(map[string]string, len(o.followed()))
	for _, repo := range o.followed() {
		names[repo.Path] = repo.displayName()
	}
	counts := make(map[string]int)
	for _, row := range rows {
		counts[time.Unix(row.Commit.UnixTimestamp, 0).Format(time.DateOnly)]++
	}
	ui.Scroll(c).Grow(1).FillWidth().TrackScroll(&o.scroll).Label(tr("Recent commits")).Children(func() {
		ui.Column(c).FillWidth().Padding(4, 20, 24).Gap(2).Children(func() {
			var day string
			for i := 0; i < limit; i++ {
				row := rows[i]
				made := time.Unix(row.Commit.UnixTimestamp, 0).Local()
				key := made.Format(time.DateOnly)
				if key != day {
					day = key
					c.Key("overview-day:" + key)
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(14, 4, 6).Children(func() {
						ui.Text(c, overviewDayLabel(made, now)).Bold().FontSize(theme.FontSize - 1)
						ui.Text(c, contributorCountLabel(counts[key])).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
					})
				}
				a.overviewRow(c, o, row, names[row.Repo], made)
			}
			if limit == len(rows) && o.anyLoading() {
				ui.Row(c).FillWidth().Justify(ui.Center).Padding(12).Children(func() {
					ui.Spinner(c).Label(tr("Reading repositories…"))
				})
			}
		})
	})
	if bounds := o.scroll.Bounds(c); bounds.H > 0 && o.scroll.Y+96 >= o.scroll.MaxY {
		switch {
		case limit < len(rows):
			o.shown += overviewPageRows
			c.Invalidate()
		case o.canLoadMore() && !o.anyLoading():
			a.readOverviewPages(o, false)
			c.Invalidate()
		}
	}
}

func (a *app) overviewRow(c *ui.Context, o *overviewState, row overviewCommit, name string, made time.Time) {
	detail := o.detail
	expanded := detail != nil && detail.row.Repo == row.Repo && detail.row.Commit.Hash == row.Commit.Hash
	ui.Column(c).Key("overview-commit:" + row.Repo + ":" + row.Commit.Hash).FillWidth().Children(func() {
		a.overviewCommitHeading(c, row, name, made, expanded)
		if expanded {
			a.overviewCommitPreview(c, detail, made)
		}
	})
}

func (a *app) overviewCommitHeading(c *ui.Context, row overviewCommit, name string, made time.Time, expanded bool) {
	theme := c.Theme()
	commit := row.Commit
	color := overviewRepoColor(row.Repo)
	line := ui.ButtonBase(c).FillWidth().AlignItems(ui.Center).Justify(ui.Start).
		Gap(12).Padding(8, 10).Radius(theme.Radius)
	line.Label(name + " " + commit.ShortHash + " " + commit.Subject).
		Expanded(expanded).Tooltip(commit.Subject).OnClick(func() { a.toggleOverviewCommit(row) })
	hovered := line.Hovered()
	if hovered || expanded {
		line.Background(theme.SurfaceHover)
	}
	line.Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(color)
		ui.Text(c, made.Format("15:04")).Width(40).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
		ui.Row(c).Width(132).AlignItems(ui.Center).Children(func() {
			ui.Row(c).MaxWidth(132).Padding(2, 8).Radius(theme.Radius).
				Background(color.Alpha(0.16)).Children(func() {
				ui.Text(c, name).FontSize(theme.FontSize - 1).SingleLine().TextColor(theme.Text)
			})
		})
		ui.Text(c, commit.ShortHash).Width(64).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
		ui.Text(c, commit.Subject).Grow(1).MinWidth(0).SingleLine()
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Box(c).Size(20, 20).Radius(10).Center().
				Background(overviewHashColor(commit.AuthorEmail + commit.AuthorName).Alpha(0.22)).Children(func() {
				ui.Text(c, overviewInitial(commit.AuthorName, commit.AuthorEmail)).FontSize(theme.FontSize - 3).Bold()
			})
			ui.Text(c, authorLabel(commit.AuthorName, commit.AuthorEmail)).MaxWidth(120).
				FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
		})
		glyph := chevronRightIcon
		if expanded {
			glyph = chevronDownIcon
		}
		icon := ui.Icon(c, glyph).FontSize(13)
		if hovered || expanded {
			icon.TextColor(theme.Text)
		} else {
			icon.TextColor(theme.TextMuted.Alpha(0.4))
		}
	})
}

func (a *app) saveOverviewRepoName(o *overviewState) {
	def := o.cur()
	if def == nil || o.rename.Path == "" {
		return
	}
	// Copy the draft before persisting so a failed save leaves the visible name intact.
	config := o.config
	config.Overviews = append([]overviewDef(nil), config.Overviews...)
	target := config.byID(def.ID)
	target.Repos = append([]overviewRepo(nil), target.Repos...)
	for i := range target.Repos {
		if target.Repos[i].Path != o.rename.Path {
			continue
		}
		target.Repos[i].Name = recentNameForStorage(o.rename.Path, o.rename.Name)
		if err := saveOverviewConfig(a.dataDir(), config); err != nil {
			o.renameErr = safeError(err)
			return
		}
		o.config = config
		o.rename = overviewRepo{}
		if o.searching() {
			a.reloadOverview()
		}
		return
	}
}

func (a *app) overviewRenameModal(c *ui.Context, o *overviewState) {
	if o.rename.Path == "" {
		return
	}
	theme := c.Theme()
	save := func() { a.saveOverviewRepoName(o) }
	closeModal := func() { o.rename = overviewRepo{} }
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
			Background(ui.RGBA(0, 0, 0, 0.45)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(420).MaxWidthPercent(94).Padding(18).Gap(12).
				Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Rename repository"))
			panel.Children(func() {
				ui.Text(c, tr("Rename repository")).FontSize(16).Bold()
				ui.TextInput(c, &o.rename.Name).Label(tr("Repository name")).AutoFocus().
					Placeholder(repoName(o.rename.Path)).OnSubmit(save)
				if o.renameErr != "" {
					ui.Text(c, o.renameErr).TextColor(theme.Danger)
				}
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					ui.Button(c, tr("Cancel")).OnClick(closeModal)
					ui.PrimaryButton(c, tr("Save")).OnClick(save)
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				closeModal()
			}
		})
	})
}

// ---- Setup dialog ----------------------------------------------------------

func (a *app) overviewSetupModal(c *ui.Context, o *overviewState) {
	if o == nil || !o.setup.open {
		return
	}
	theme := c.Theme()
	setup := &o.setup
	local, remote := a.followableRecents()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(640).Height(580).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius+10).
				Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, 0.3)).Clip().
				Role(ui.RoleDialog).Label(tr("Choose repositories")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Box(c).Size(38, 38).Radius(theme.Radius + 4).Center().
							Background(theme.Accent.Alpha(0.12)).Children(func() {
							ui.Icon(c, overviewIcon).FontSize(19).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
							title := tr("Edit overview")
							if setup.isNew {
								title = tr("New overview")
							}
							ui.Text(c, title).FontSize(21).Bold()
							ui.Text(c, tr("The Overview reads these for recent commits. Nothing is changed or fetched.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close", "Close (Esc)", closeIcon, false, a.closeOverviewSetup)
					})
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Text(c, tr("Name")).Bold().TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						field := ui.Row(c).FillWidth().Height(38).AlignItems(ui.Center).Padding(3, 12).
							Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius + 2)
						if field.Hovered() {
							field.Border(1, theme.Accent.Alpha(0.5))
						}
						field.Children(func() {
							name := ui.TextInputBase(c, &setup.name).Label(tr("Overview name")).
								Placeholder(tr("For example: Backend services")).Height(30).Grow(1)
							if setup.isNew {
								name.AutoFocus()
							}
							if name.Submitted() && o.selectedCount() > 0 {
								a.applyOverviewSetup()
							}
						})
					})
					ui.Scroll(c).Grow(1).FillWidth().Label(tr("Repositories to follow")).Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							if setup.resolving {
								ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
									ui.Spinner(c)
									ui.Text(c, tr("Reading repositories…")).TextColor(theme.TextMuted)
								})
								return
							}
							a.overviewSetupRecents(c, o, local, remote)
							a.overviewSetupCustom(c, o)
						})
					})
					if setup.err != "" {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(8, 10).
							Background(theme.Danger.Alpha(0.1)).Border(1, theme.Danger.Alpha(0.4)).Radius(theme.Radius).
							Children(func() {
								ui.Icon(c, conflictIcon).FontSize(14).TextColor(theme.Danger)
								ui.Text(c, setup.err).Grow(1).FontSize(theme.FontSize - 1)
							})
					}
					ui.Divider(c)
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						iconTextButtonState(c, "Add folder…", "Add a repository that is not in the recent list", initRepoIcon,
							false, setup.checking || setup.resolving, a.addOverviewFolder)
						if setup.checking {
							ui.Spinner(c).Label(tr("Checking folder…"))
						}
						ui.Text(c, trf("%d selected", o.selectedCount())).TextColor(theme.TextMuted).Grow(1).
							Padding(0, 0, 0, 6)
						if ui.Button(c, tr("Cancel")).Label(tr("Cancel")).Clicked() {
							a.closeOverviewSetup()
						}
						confirmLabel := tr("Save")
						if setup.isNew {
							confirmLabel = tr("Create overview")
						}
						confirm := ui.PrimaryButton(c, confirmLabel).Label(confirmLabel).
							Tooltip(tr("Read the selected repositories")).Disabled(setup.resolving || o.selectedCount() == 0)
						if confirm.Clicked() {
							a.applyOverviewSetup()
						}
					})
				})
			if panel.PressedOutside() || a.onOverlayShortcut(back, 0, ui.KeyEscape,
				shortcut("overview/setup-close", "Escape", tr("Close")), nil) {
				a.closeOverviewSetup()
			}
		})
	})
}

func (a *app) overviewSetupRecents(c *ui.Context, o *overviewState, local, remote []recentRepository) {
	theme := c.Theme()
	setup := &o.setup
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, tr("Recent repositories")).Bold().TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if len(local) > 0 {
			if ui.Button(c, tr("Select all")).Label(tr("Select all recent repositories")).Clicked() {
				for _, repo := range local {
					setup.selected[repo.Path] = true
				}
			}
			if ui.Button(c, tr("Clear")).Label(tr("Deselect all recent repositories")).Clicked() {
				setup.selected = map[string]bool{}
			}
		}
	})
	if len(local) == 0 {
		ui.Text(c, tr("No recent repositories yet. Use Add folder… to pick one.")).TextColor(theme.TextMuted)
	}
	for _, repo := range local {
		repo := repo
		c.Key("overview-setup:" + repo.Path)
		checked := setup.selected[repo.Path]
		name := setup.names[repo.Path]
		if name == "" {
			name = repo.displayName()
		}
		row := ui.ButtonBase(c).FillWidth().AlignItems(ui.Center).Justify(ui.Start).Gap(10).
			Padding(8, 10).Radius(theme.Radius)
		row.Label(name).Tooltip(repo.Path)
		switch {
		case checked:
			row.Background(theme.Accent.Alpha(0.08))
		case row.Hovered():
			row.Background(theme.SurfaceHover)
		}
		clicked := row.Clicked()
		row.Children(func() {
			value := checked
			ui.Checkbox(c, &value, "").Label(tr("Follow") + " " + name)
			ui.Box(c).Size(8, 8).Radius(4).Background(overviewRepoColor(repo.Path))
			ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, name).Bold().SingleLine()
				ui.Text(c, repo.Path).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine()
			})
			if repo.Branch != "" {
				ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
					ui.Icon(c, branchIcon).FontSize(12).TextColor(theme.TextMuted)
					ui.Text(c, repo.Branch).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).
						MaxWidth(120).SingleLine()
				})
			}
			if value != checked {
				setup.selected[repo.Path] = value
			}
		})
		if clicked {
			setup.selected[repo.Path] = !checked
		}
	}
	if len(remote) > 0 {
		ui.Text(c, trf("%d SSH workspaces are not supported by the Overview yet", len(remote))).
			FontSize(theme.FontSize-2).TextColor(theme.TextMuted).Padding(4, 2)
	}
}

func (a *app) overviewSetupCustom(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	setup := &o.setup
	if len(setup.custom) == 0 {
		return
	}
	ui.Text(c, tr("Added folders")).Bold().TextColor(theme.TextMuted).FontSize(theme.FontSize-1).
		Padding(10, 0, 0)
	remove := ""
	for _, path := range setup.custom {
		c.Key("overview-custom:" + path)
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(8, 10).
			Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(overviewRepoColor(path))
			ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, repoName(path)).Bold().SingleLine()
				ui.Text(c, path).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine()
			})
			iconButton(c, "Remove "+repoName(path), "Stop following this folder", closeIcon, false,
				func() { remove = path })
		})
	}
	if remove != "" {
		kept := setup.custom[:0:0]
		for _, path := range setup.custom {
			if path != remove {
				kept = append(kept, path)
			}
		}
		setup.custom = kept
	}
}
