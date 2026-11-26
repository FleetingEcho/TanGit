package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const fileTreeCompareCommitScope = "compare-commit"

// commitComparisonSession is optional: a session that cannot list the commits
// between two revisions still compares files.
type commitComparisonSession interface {
	CompareCommits(ctx context.Context, from, to string, limit int) (repository.CommitComparison, error)
}

// compareMode is what "from → to" means; it is the index of the mode tab.
// Branch changes show what `to` adds on top of the point where the two
// diverged (Git's from...to), the answer to "what is on this branch that main
// does not have". Exact compares the two ends as they are now (Git's from..to),
// including whatever `from` gained since.
type compareMode = int

const (
	compareModeBranch compareMode = iota
	compareModeExact
)

// compareTab is the index of the results tab: the commits only the second ref
// has, the commits only the first has, or the changed files.
type compareTab = int

const (
	compareTabAhead compareTab = iota
	compareTabBehind
	compareTabFiles
)

// compareCommitsState holds the commit view of the applied comparison and the
// commit chosen in it. The fields after selected mirror what History and
// Branches keep for their chosen commit, so the shared commit detail renderer
// draws this one too (commitDetailBody).
type compareCommitsState struct {
	loaded     bool
	comparison repository.CommitComparison
	selected   int
	list       ui.ListState

	message         commitMessageDetailState
	filesHash       string
	filesLoading    bool
	filesError      string
	files           []repository.CommitFile
	fileRows        []fileTreeRow
	fileList        ui.ListState
	selectedFileRow int
	selectedFile    int
	filesHeight     float32
	filesWidth      float32
	filesCollapsed  bool

	diffHash    string
	diffPath    string
	diffPatch   string
	diffError   string
	diffLoading bool
	diffState   *editor.DiffState
	diffMedia   *mediaReview
	token       uint64
}

// compareCommitsShown is the commit list of the chosen tab.
func (a *app) compareCommitsShown() []repository.Commit {
	if a.compare.tab == compareTabBehind {
		return a.compare.commits.comparison.Behind
	}
	return a.compare.commits.comparison.Ahead
}

func (a *app) compareCommitsMore() bool {
	if a.compare.tab == compareTabBehind {
		return a.compare.commits.comparison.BehindMore
	}
	return a.compare.commits.comparison.AheadMore
}

// compareCommitsSelected is the commit chosen in the shown list.
func (a *app) compareCommitsSelected() (repository.Commit, bool) {
	shown := a.compareCommitsShown()
	index := a.compare.commits.selected
	if a.compareEffectiveTab() == compareTabFiles || index < 0 || index >= len(shown) {
		return repository.Commit{}, false
	}
	return shown[index], true
}

// compareCommitsAvailable reports whether the commit view has anything to
// show; otherwise the page falls back to the file view alone.
func (a *app) compareCommitsAvailable() bool {
	state := &a.compare.commits
	return state.loaded && len(state.comparison.Ahead)+len(state.comparison.Behind) > 0
}

func (a *app) compareEffectiveTab() compareTab {
	if a.compare.tab != compareTabFiles && !a.compareCommitsAvailable() {
		return compareTabFiles
	}
	return a.compare.tab
}

// compareFilesFrom is the revision the file view starts from: the merge base
// in branch mode, else the typed ref.
func (a *app) compareFilesFrom() string {
	if a.compare.appliedBase != "" {
		return a.compare.appliedBase
	}
	return a.compare.appliedFrom
}

// shortCompareRef drops the namespace of a fully spelled ref for display.
func shortCompareRef(ref string) string {
	for _, prefix := range []string{"refs/heads/", "refs/remotes/", "refs/tags/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

// shortCompareRevision is shortCompareRef that also abbreviates a full hash.
func shortCompareRevision(ref string) string {
	ref = shortCompareRef(ref)
	if len(ref) == 40 && strings.Trim(ref, "0123456789abcdef") == "" {
		return ref[:8]
	}
	return ref
}

// setCompareCommits installs a finished commit read and opens the tab that has
// something to show: the commits the second ref adds, else the first's, else
// the files.
func (a *app) setCompareCommits(comparison repository.CommitComparison) {
	a.resetCompareCommits()
	a.compare.commits.loaded = true
	a.compare.commits.comparison = comparison
	switch {
	case len(comparison.Ahead) > 0:
		a.compare.tab = compareTabAhead
	case len(comparison.Behind) > 0:
		a.compare.tab = compareTabBehind
	default:
		a.compare.tab = compareTabFiles
	}
}

// resetCompareCommits drops the commit view of the previous comparison. The
// room the files and diff share is the user's and survives it; a first
// comparison starts from the sizes History and Branches start from.
func (a *app) resetCompareCommits() {
	old := a.compare.commits
	state := compareCommitsState{
		selectedFileRow: -1, selectedFile: -1,
		filesHeight: old.filesHeight, filesWidth: old.filesWidth, filesCollapsed: old.filesCollapsed,
	}
	if state.filesHeight <= 0 {
		state.filesHeight = 150
	}
	if state.filesWidth <= 0 {
		state.filesWidth = 300
	}
	a.compare.commits = state
}

// chooseCompareTab changes the results tab. The commit list is a different one
// on each side, so the choice starts at its first commit.
func (a *app) chooseCompareTab() {
	a.compare.commits.selected = 0
	a.clearCompareDiff()
}

func (a *app) requestCompareCommitMessage(commit repository.Commit) {
	a.requestCommitMessage(&a.compare.commits.message, commit, func() bool {
		return a.workspaceTab == sectionCompare
	})
}

// requestCompareCommitFiles lists the files the chosen commit changed, unless
// the pane already holds them. It is asked for while the pane draws, so a read
// an action dropped starts again on the next frame. The files and the diff
// read the commit's own hashes, so neither touches the working tree.
func (a *app) requestCompareCommitFiles(commit repository.Commit) {
	state := &a.compare.commits
	if a.session == nil || state.filesHash == commit.Hash {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	ctx := a.repositoryWork.Context()
	state.token++
	token := state.token
	state.filesHash = commit.Hash
	state.filesLoading = true
	state.filesError = ""
	// Keep the last painted files and diff while this commit loads; the shared
	// renderer shows progress beside that content.
	state.diffLoading = false
	state.diffError = ""
	go func() {
		defer a.recoverBackground()
		files, err := requireHistorySession(session).CommitFiles(ctx, commit)
		a.updateChanged(func() bool {
			current := &a.compare.commits
			if token != current.token {
				return false
			}
			if repositoryGeneration != a.generation {
				current.filesHash = ""
				current.filesLoading = false
				return a.workspaceTab == sectionCompare
			}
			if current.filesHash != commit.Hash {
				return false
			}
			current.filesLoading = false
			if err != nil {
				current.filesError = safeError(err)
				return a.workspaceTab == sectionCompare
			}
			current.files = files
			if len(files) > 0 {
				current.selectedFile = 0
			}
			return a.workspaceTab == sectionCompare
		})
	}()
}

// requestCompareCommitFileDiff reads the diff of the file chosen in the chosen
// commit, unless it is already the one on screen.
func (a *app) requestCompareCommitFileDiff(commit repository.Commit, file repository.CommitFile) {
	state := &a.compare.commits
	if a.session == nil || (state.diffHash == commit.Hash && state.diffPath == file.Path) {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	ctx := a.repositoryWork.Context()
	state.token++
	token := state.token
	state.diffHash, state.diffPath = commit.Hash, file.Path
	state.diffLoading = true
	state.diffError = ""
	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
	go func() {
		defer a.recoverBackground()
		diff, err := requireHistorySession(session).CommitFileDiff(ctx, commit, file, options)
		var diffState *editor.DiffState
		var media *mediaReview
		if err == nil {
			diffState, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
			if err == nil && diffState == nil && isMediaPath(file.Path) {
				media, err = loadMediaReview(ctx, session, repository.MediaRequest{
					Source: repository.MediaCommit, Commit: commit, CommitFile: file,
				})
			}
		}
		a.updateChanged(func() bool {
			current := &a.compare.commits
			if token != current.token {
				return false
			}
			if repositoryGeneration != a.generation {
				current.diffHash, current.diffPath, current.diffPatch = "", "", ""
				current.diffLoading = false
				return a.workspaceTab == sectionCompare
			}
			if current.diffHash != commit.Hash || current.diffPath != file.Path {
				return false
			}
			current.diffLoading = false
			if err != nil {
				current.diffError = safeError(err)
				return a.workspaceTab == sectionCompare
			}
			current.diffPatch = diff.Patch
			current.diffState = diffState
			current.diffMedia = media
			return a.workspaceTab == sectionCompare
		})
	}()
}

func (a *app) selectCompareCommitFileRow(row int) {
	state := &a.compare.commits
	selectFileTreeRow(state.fileRows, row, &state.selectedFileRow, &state.selectedFile)
	if state.selectedFile >= 0 {
		return
	}
	a.resetCompareCommitDiff()
}

// resetCompareCommitDiff drops the commit diff, so it is read again, with the
// diff options now in force.
func (a *app) resetCompareCommitDiff() {
	state := &a.compare.commits
	state.token++
	state.diffHash, state.diffPath, state.diffPatch = "", "", ""
	state.diffState, state.diffMedia = nil, nil
	state.diffLoading = false
	state.diffError = ""
}

// compareCommitDetailViewState binds the Compare page's commit to the detail
// renderer History, Graph, Reflog and Branches share.
func (a *app) compareCommitDetailViewState() commitDetailViewState {
	state := &a.compare.commits
	// The shared renderer draws a list whose chosen row is this state's; History
	// and Branches bind theirs once at start, this one is replaced with each
	// comparison, so it is bound each time.
	state.fileList.Selected = &state.selectedFileRow
	state.fileList.Key = func(row int) any { return fileTreeKey(state.fileRows, row) }
	state.fileList.Label = func(row int) string { return fileTreeLabel(state.fileRows, row) }
	return commitDetailViewState{
		scope:          fileTreeCompareCommitScope,
		message:        &state.message,
		requestMessage: a.requestCompareCommitMessage,
		filesHash:      &state.filesHash, filesLoading: &state.filesLoading, filesError: &state.filesError,
		files: &state.files, fileRows: &state.fileRows, fileList: &state.fileList,
		selectedFileRow: &state.selectedFileRow, selectedFile: &state.selectedFile,
		filesHeight: &state.filesHeight, filesWidth: &state.filesWidth, filesCollapsed: &state.filesCollapsed,
		diffLoading: &state.diffLoading, diffError: &state.diffError, diffPath: &state.diffPath,
		diffState: &state.diffState, diffMedia: &state.diffMedia,
		requestDetails: a.requestCompareCommitFiles, requestDiff: a.requestCompareCommitFileDiff,
		selectFileRow: a.selectCompareCommitFileRow,
	}
}

// compareModeTabs chooses what the refs mean. Changing it re-reads an applied
// comparison at once.
func (a *app) compareModeTabs(c *ui.Context) {
	theme := c.Theme()
	modes := [...]struct{ label, tooltip string }{
		compareModeBranch: {"Branch changes", "Compare the common ancestor with the To ref to see what changed on that side since the refs diverged."},
		compareModeExact:  {"Exact difference", "Compare the current contents of the From and To refs, including changes made on either side."},
	}
	tabs := ui.TabsBase(c, &a.compare.mode, len(modes))
	tabs.List.Gap(theme.Space(1)).Label(tr("Compare as")).Children(func() {
		for index, mode := range modes {
			tab := tabs.Tab(index).Padding(theme.Space(2), theme.Space(3)).
				Radius(theme.Radius).Tooltip(tr(mode.tooltip)).TextColor(theme.TextMuted)
			if index == a.compare.mode {
				tab.TextColor(theme.Text).FontWeight(600).DrawOver(func(p *ui.Painter, r ui.Rect) {
					inset, height := theme.Space(1.5), theme.Space(0.5)
					line := ui.Rect{X: r.X + inset, Y: r.Y + r.H - height, W: r.W - 2*inset, H: height}
					p.Fill(line, theme.Accent, height/2)
				})
			} else if tab.Hovered() {
				tab.TextColor(theme.Text)
			}
			tab.Children(func() { ui.Text(c, tr(mode.label)).SingleLine() })
		}
	})
	if tabs.List.Changed() {
		a.refreshAppliedCompare()
	}
}

// compareSummary is the one-line account of the comparison: where the two
// diverged, and what the files changed in total.
func (a *app) compareSummary(c *ui.Context) {
	theme := c.Theme()
	from, to := shortCompareRef(a.compare.appliedFrom), shortCompareRef(a.compare.appliedTo)
	added, deleted := 0, 0
	for _, file := range a.compare.files {
		added += file.LinesAdded
		deleted += file.LinesDeleted
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
		if state := &a.compare.commits; state.loaded {
			if base := state.comparison.MergeBase; base != "" {
				short := base
				if len(short) > 8 {
					short = short[:8]
				}
				cockpitChipTip(c, trf("merge base %s", short),
					trf("The latest commit %s and %s have in common", from, to)+"\n"+base, theme.TextMuted, nil)
			} else {
				cockpitChipTip(c, tr("no common history"),
					tr("These refs share no commits; Branch changes compares them directly."), theme.TextMuted, nil)
			}
		}
		cockpitChip(c, trf("%d files changed", len(a.compare.files)), theme.TextMuted, nil)
		if added+deleted > 0 {
			ui.Text(c, "+"+groupedNumber(added)).TextColor(theme.Success).FontSize(theme.FontSize - 1)
			ui.Text(c, "−"+groupedNumber(deleted)).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
		}
	})
}

// compareResultTabs is one row of tabs for the whole result: the commits only
// the second ref has, those only the first has, and the changed files.
func (a *app) compareResultTabs(c *ui.Context) {
	if !a.compareCommitsAvailable() {
		return
	}
	state := &a.compare.commits
	from, to := shortCompareRef(a.compare.appliedFrom), shortCompareRef(a.compare.appliedTo)
	count := func(n int, more bool) string {
		if more {
			return fmt.Sprintf("%d+", n)
		}
		return fmt.Sprintf("%d", n)
	}
	tabs := ui.Tabs(c, &a.compare.tab,
		trf("Only in %s", to)+" · "+count(len(state.comparison.Ahead), state.comparison.AheadMore),
		trf("Only in %s", from)+" · "+count(len(state.comparison.Behind), state.comparison.BehindMore),
		tr("Files")+" · "+count(len(a.compare.files), false)).
		Label(tr("Comparison results"))
	if tabs.Changed() {
		a.chooseCompareTab()
	}
}

// compareCommitsView is the commit tabs: the commits on one side of the
// comparison, and the shared detail of the chosen one.
func (a *app) compareCommitsView(c *ui.Context, theme *ui.Theme) {
	a.collapsibleContentPaneSplit(c, &a.compare.listWidth, &a.workspacePaneCollapsed[sectionCompare],
		"Expand list pane", func() {
			ui.Column(c).Grow(1).FillHeight().Padding(0, comparePaneDividerInset, 0, 0).Children(func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					a.compareCommitList(c, theme)
				})
			})
		}, func() {
			var commit *repository.Commit
			if selected, ok := a.compareCommitsSelected(); ok {
				commit = &selected
			}
			ui.Column(c).Grow(1).FillHeight().Padding(0, 0, 0, comparePaneDividerInset).Children(func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
					a.commitDetailBody(c, commit, "Select a commit to see its files and diff.",
						func(c *ui.Context, commit repository.Commit) {
							a.commitActionIconsWithHistory(c, commit, false)
						}, a.compareCommitDetailViewState())
				})
			})
		})
}

func (a *app) compareCommitList(c *ui.Context, theme *ui.Theme) {
	state := &a.compare.commits
	shown := a.compareCommitsShown()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Icon(c, commitIcon).FontSize(13).TextColor(theme.Accent)
		ui.Text(c, tr("Commits")).Bold().Grow(1)
		commitCountBadge(c, len(shown), a.compareCommitsMore())
		paneCollapseButton(c, &a.workspacePaneCollapsed[sectionCompare], "Collapse list pane")
	})
	if len(shown) == 0 {
		ref := shortCompareRef(a.compare.appliedTo)
		if a.compare.tab == compareTabBehind {
			ref = shortCompareRef(a.compare.appliedFrom)
		}
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, trf("No commits only in %s.", ref)).TextColor(theme.TextMuted)
		})
		return
	}
	if state.selected < 0 || state.selected >= len(shown) {
		state.selected = 0
	}
	state.list.Selected = &state.selected
	state.list.Key = func(index int) any { return shown[index].Hash }
	state.list.Label = func(index int) string { return shown[index].Subject }
	a.keyboardList(c, &state.list, len(shown)).Grow(1).Rows(func(row ui.ListRow) {
		commit := shown[row.Index]
		services := row.Context.Services()
		element := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).
			Padding(6, 8+scrollbarGutter(theme), 6, 8)
		reveal := element.Hovered() && a.sectionVisible(sectionHistory)
		element.Children(func() {
			a.authorAvatar(row.Context, commit, 18)
			ui.Column(row.Context).Gap(2).Grow(1).MinWidth(0).Children(func() {
				ui.Text(row.Context, commit.Subject).SingleLine().Tooltip(commit.Subject)
				ui.Row(row.Context).Gap(8).MinWidth(0).Children(func() {
					ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row)).SingleLine().MinWidth(0)
					ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row)).
						SingleLine().MinWidth(0).Tooltip(commit.AuthorName)
					if rel := relTime(commit.UnixTimestamp); rel != "" {
						ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row)).SingleLine().MinWidth(0)
					}
				})
			})
			if reveal {
				row.Context.Key("compare-open-in-history")
				ui.ButtonBase(row.Context).Padding(3).Shrink(0).
					Label(tr("Open in History")).Tooltip(tr("Open this commit in History")).
					OnClick(func() { a.showCommitInHistory(commit.Hash) }).
					Children(func() {
						ui.Icon(row.Context, historyIcon).FontSize(rowActionIconSize(theme)).
							TextColor(secondaryRowColor(theme, row))
					})
			}
		}).PopoverMenu(func(m *ui.Menu) {
			a.commitRowMenuWithHistory(m, commit, services, false)
		})
	})
	if a.compareCommitsMore() {
		ui.Text(c, trf("Showing the newest %d commits.", repository.CompareCommitsLimit)).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
}

// compareShownDiff is the diff on screen in the Compare section, for the
// full-window view and the save action.
func (a *app) compareShownDiff() (shownDiff, bool) {
	if a.compareEffectiveTab() != compareTabFiles {
		state := &a.compare.commits
		commit, ok := a.compareCommitsSelected()
		if !ok || state.selectedFile < 0 || state.selectedFile >= len(state.files) {
			return shownDiff{}, false
		}
		file := state.files[state.selectedFile]
		return shownDiff{
			title:    file.Path,
			subtitle: commit.ShortHash + " " + commit.Subject,
			path:     file.Path,
			patch:    state.diffPatch,
			state:    state.diffState,
			media:    state.diffMedia,
		}, true
	}
	if a.compare.selected < 0 || a.compare.selected >= len(a.compare.files) {
		return shownDiff{}, false
	}
	file := a.compare.files[a.compare.selected]
	return shownDiff{
		title:    file.Path,
		subtitle: shortCompareRevision(a.compareFilesFrom()) + " → " + shortCompareRevision(a.compare.appliedTo),
		path:     file.Path,
		patch:    a.compare.diffPatch,
		state:    a.compare.diffState,
	}, true
}
