package app

import (
	"strings"

	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"lazygitapp/internal/repository"
)

func (a *app) confirmCommitAction(revert bool) {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.confirmCommitActionFor(commit, revert)
	}
}

// confirmCommitActionFor asks before cherry-picking or reverting commit.
// It backs both the details-pane buttons and the history row context menu.
func (a *app) confirmCommitActionFor(commit repository.Commit, revert bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	title := "Cherry-pick Commit"
	message := trf("Cherry-pick %s onto %s?", commit.ShortHash, a.snapshot.Branch)
	detail := commit.Subject
	button := "Cherry-pick"
	if revert {
		title = "Revert Commit"
		message = trf("Create a commit that reverts %s?", commit.ShortHash)
		button = "Revert"
	}
	a.askConfirm(confirmRequest{
		title:        title,
		message:      message,
		detail:       detail,
		confirmLabel: button,
		run: func() {
			message := trf("Cherry-picked %s", commit.ShortHash)
			if revert {
				message = trf("Reverted %s", commit.ShortHash)
			}
			a.beginRepositoryActionMessage(message, "",
				func(ctx context.Context, session repository.Session) error {
					if revert {
						return session.RevertCommit(ctx, commit)
					}
					return session.CherryPickCommit(ctx, commit)
				}, nil)
		},
	})
}

func (a *app) confirmAbortOperation() {
	if a.actionBusy || a.session == nil || a.snapshot.Operation == "" {
		return
	}
	operation := a.snapshot.Operation
	expected := snapshotOperationFingerprint(a.snapshot)
	a.askConfirm(confirmRequest{
		title:        "Abort Operation",
		message:      trf("Abort the current %s?", operation),
		detail:       "Git will restore the repository to its state before this operation began.",
		confirmLabel: "Abort",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Aborted the %s", operation), "",
				func(ctx context.Context, session repository.Session) error {
					return driveOperation(ctx, session, repository.OperationActionAbort, expected)
				}, nil)
		},
	})
}

// commitMessageDetailState is the lazily read, read-only message of the
// selected commit. Its generation rejects an A-B-A completion even when a
// backend returns a result after its context has been cancelled.
type commitMessageDetailState struct {
	hash       string
	message    repository.CommitMessage
	loading    bool
	error      string
	height     float32
	raw        bool
	markdown   *markdown.Options
	scroll     ui.ScrollState
	cancel     context.CancelFunc
	generation uint64
}

func (s *commitMessageDetailState) reset() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.generation++
	s.hash = ""
	s.message = repository.CommitMessage{}
	s.loading = false
	s.error = ""
	s.scroll = ui.ScrollState{}
}

func (a *app) requestCommitMessage(state *commitMessageDetailState, commit repository.Commit, visible func() bool) {
	if a.session == nil || state.hash == commit.Hash {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	state.reset()
	requestGeneration := state.generation
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.hash = commit.Hash
	state.loading = true

	go func() {
		defer a.recoverBackground()
		defer cancel()
		message, err := session.CommitMessage(ctx, commit)
		a.updateChanged(func() bool {
			if requestGeneration != state.generation || state.hash != commit.Hash {
				return false
			}
			state.cancel = nil
			state.loading = false
			if repositoryGeneration != a.generation {
				// An action rewrote the repository. Drop the key so the next
				// frame reads the selected commit again.
				state.hash = ""
				state.message = repository.CommitMessage{}
				return visible()
			}
			if err != nil {
				state.error = safeError(err)
				return visible()
			}
			state.message = message
			return visible()
		})
	}()
}

func (a *app) requestHistoryCommitMessage(commit repository.Commit) {
	a.requestCommitMessage(&a.commitDetailMessage, commit, a.commitDetailsVisible)
}

// resetCommitDetails drops what the commit detail pane read, so the next
// frame reads the chosen commit again after an action changed it.
func (a *app) resetCommitDetails() {
	a.commitDetailMessage.reset()
	a.codeBlameHistoryTarget = nil
	if a.cancelCommitFiles != nil {
		a.cancelCommitFiles()
		a.cancelCommitFiles = nil
	}
	if a.cancelCommitDiff != nil {
		a.cancelCommitDiff()
		a.cancelCommitDiff = nil
	}
	a.commitFilesGeneration++
	a.commitDiffGeneration++
	a.commitFilesHash = ""
	a.commitFiles = nil
	a.commitFilesLoading = false
	a.commitFilesError = ""
	a.selectedCommitFileRow = -1
	a.selectedCommitFile = -1
	a.commitDiffHash = ""
	a.commitDiffPath = ""
	a.commitDiffPatch = ""
	a.commitDiffState = nil
	a.commitDiffMedia = nil
	a.commitDiffLoading = false
	a.commitDiffError = ""
}

// commitDetailsVisible reports whether the shared commit file/diff model is
// on screen. Results still publish while another section is active, but only
// these sections need a frame for that publication.
func (a *app) commitDetailsVisible() bool {
	return isCommitSection(a.workspaceTab) || a.workspaceTab == sectionReflog
}

// requestCommitDetails lists the files commit changed, unless the pane
// already holds them. It is asked for while the pane draws, so a read that an
// action dropped, or a newer choice replaced, starts again on the next frame
// rather than leaving the pane loading forever.
func (a *app) requestCommitDetails(commit repository.Commit) {
	if a.session == nil || a.commitFilesHash == commit.Hash {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	if a.cancelCommitFiles != nil {
		a.cancelCommitFiles()
	}
	if a.cancelCommitDiff != nil {
		a.cancelCommitDiff()
		a.cancelCommitDiff = nil
	}
	a.commitFilesGeneration++
	a.commitDiffGeneration++
	requestGeneration := a.commitFilesGeneration
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelCommitFiles = cancel
	a.commitFilesHash = commit.Hash
	a.commitFilesLoading = true
	a.commitFilesError = ""
	// The current details remain on screen until the next ones are ready.
	// In particular, do not flash a blank file list and diff between two
	// commits whose contents are both already painted.
	a.commitDiffLoading = false
	a.commitDiffError = ""

	go func() {
		defer a.recoverBackground()
		defer cancel()
		files, err := session.CommitFiles(ctx, commit)
		a.updateChanged(func() bool {
			if repositoryGeneration != a.generation {
				// An action rewrote the repository: drop the request so
				// the next frame reads the commit again.
				if requestGeneration == a.commitFilesGeneration && a.commitFilesHash == commit.Hash {
					a.cancelCommitFiles = nil
					a.commitFilesHash = ""
					a.commitFilesLoading = false
					return a.commitDetailsVisible()
				}
				return false
			}
			if requestGeneration != a.commitFilesGeneration || a.commitFilesHash != commit.Hash {
				return false
			}
			a.cancelCommitFiles = nil
			a.commitFilesLoading = false
			if err != nil {
				a.commitFilesError = safeError(err)
				return a.commitDetailsVisible()
			}
			a.commitFiles = files
			if len(files) == 0 {
				a.codeBlameHistoryTarget = nil
				a.selectedCommitFileRow = -1
				a.selectedCommitFile = -1
				return a.commitDetailsVisible()
			}
			a.selectedCommitFile = 0
			if target := a.codeBlameHistoryTarget; target != nil && target.hash == commit.Hash {
				matched := false
				for index, file := range files {
					if file.Path != target.path {
						continue
					}
					a.selectedCommitFile = index
					matched = true
					// Keep the requested file visible when its parent directories
					// were folded by an earlier commit.
					if a.fileTreeCollapsed == nil {
						a.fileTreeCollapsed = make(map[string]bool)
					}
					for _, directory := range fileTreeDirectories([]string{file.Path}) {
						a.fileTreeCollapsed[fileTreeCollapseKey(fileTreeCommitScope, directory)] = false
					}
					break
				}
				if !matched {
					a.codeBlameHistoryTarget = nil
				}
			}
			return a.commitDetailsVisible()
		})
	}()
}

// requestCommitFileDiff reads the diff of the file chosen in commit, unless
// it is already the one the pane shows.
func (a *app) requestCommitFileDiff(commit repository.Commit, file repository.CommitFile) {
	if target := a.codeBlameHistoryTarget; target != nil && (target.hash != commit.Hash || target.path != file.Path) {
		a.codeBlameHistoryTarget = nil
	}
	if a.session == nil || (a.commitDiffHash == commit.Hash && a.commitDiffPath == file.Path &&
		a.commitDiffOptions == a.diffOptions) {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	if a.cancelCommitDiff != nil {
		a.cancelCommitDiff()
	}
	a.commitDiffGeneration++
	requestGeneration := a.commitDiffGeneration
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelCommitDiff = cancel
	a.stopLineSelection()
	a.commitDiffHash = commit.Hash
	a.commitDiffPath = file.Path
	a.commitDiffOptions = a.diffOptions
	a.commitDiffLoading = true
	a.commitDiffError = ""
	// Retain the previous diff until this request can replace it: progress
	// belongs beside content, not in place of the whole viewer.

	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
	customPatchSelection := isCommitSection(a.workspaceTab)
	go func() {
		defer a.recoverBackground()
		defer cancel()
		diff, err := session.CommitFileDiff(ctx, commit, file, options)
		if err == nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		var state *editor.DiffState
		var media *mediaReview
		if err == nil {
			state, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
			if err == nil && state != nil && customPatchSelection {
				state.LineSelection = true
				state.OnLineSelection = func(clicked editor.DiffLineRange) {
					a.extendLineSelection(state, clicked)
				}
			}
			if err == nil && state == nil && isMediaPath(file.Path) {
				media, err = loadMediaReview(ctx, session, repository.MediaRequest{
					Source: repository.MediaCommit, Commit: commit, CommitFile: file,
				})
			}
		}
		a.updateChanged(func() bool {
			if repositoryGeneration != a.generation {
				if requestGeneration == a.commitDiffGeneration && a.commitDiffHash == commit.Hash && a.commitDiffPath == file.Path &&
					a.commitDiffOptions == options {
					a.cancelCommitDiff = nil
					a.commitDiffHash = ""
					a.commitDiffPath = ""
					a.commitDiffPatch = ""
					a.commitDiffLoading = false
					return a.commitDetailsVisible()
				}
				return false
			}
			if requestGeneration != a.commitDiffGeneration || a.commitDiffHash != commit.Hash || a.commitDiffPath != file.Path ||
				a.commitDiffOptions != options {
				return false
			}
			a.cancelCommitDiff = nil
			a.commitDiffLoading = false
			if err != nil {
				a.commitDiffError = safeError(err)
				return a.commitDetailsVisible()
			}
			a.commitDiffPatch = diff.Patch
			a.commitDiffState = state
			a.commitDiffMedia = media
			if target := a.codeBlameHistoryTarget; target != nil && target.hash == commit.Hash && target.path == file.Path && state == nil {
				a.codeBlameHistoryTarget = nil
			}
			return a.commitDetailsVisible()
		})
	}()
}

// requestMoreCommits reads the page of commits after those the list shows.
// History and the Graph call it while they draw, when the list reaches its
// end, so a long history pages in rather than stopping at the snapshot page.
func (a *app) requestMoreCommits() {
	if isCommitSection(a.workspaceTab) && a.commitQueryFor(a.workspaceTab).narrows() {
		a.requestMoreFilteredCommits(a.workspaceTab)
		return
	}
	if a.session == nil || a.commitsLoading || !a.commitsMore {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	a.commitsToken++
	token := a.commitsToken
	a.commitsLoading = true
	a.commitsError = ""
	skip := len(a.snapshot.Commits)
	go func() {
		defer a.recoverBackground()
		commits, err := session.RecentCommits(context.Background(), repository.SnapshotCommitLimit, skip)
		a.updateChanged(func() bool {
			if repositoryGeneration != a.generation || token != a.commitsToken {
				return false
			}
			a.commitsLoading = false
			if err != nil {
				a.commitsError = safeError(err)
				return isCommitSection(a.workspaceTab)
			}
			a.commitsMore = len(commits) == repository.SnapshotCommitLimit
			a.snapshot.Commits = append(a.snapshot.Commits, commits...)
			return isCommitSection(a.workspaceTab)
		})
	}()
}

// commitListFooter says whether older commits remain, and reports the read of
// them, under the commit list History and the Graph share.
func (a *app) commitListFooter(c *ui.Context) {
	theme := c.Theme()
	if isCommitSection(a.workspaceTab) {
		if query := a.commitQueryFor(a.workspaceTab); query.narrows() {
			a.filteredCommitFooter(c, theme, a.workspaceTab, query)
			return
		}
	}
	switch {
	case a.commitsLoading:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c).Label(tr("Loading older commits"))
			ui.Text(c, tr("Loading older commits…")).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 1)
		})
	case a.commitsError != "":
		ui.Column(c).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("Could not load older commits.")).TextColor(theme.Danger).
					FontSize(theme.FontSize - 1).Grow(1)
				if ui.Button(c, tr("Retry")).Clicked() {
					a.commitsError = ""
					a.requestMoreCommits()
				}
			})
			ui.Text(c, a.commitsError).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).SingleLine().Tooltip(a.commitsError)
		})
	case !a.commitsMore:
		ui.Text(c, tr("Beginning of history")).TextColor(theme.TextMuted).
			FontSize(theme.FontSize - 1)
	}
}

// filteredCommitFooter says whether more matches remain below a narrowed
// commit list, and reports the read of them. The list is History's or the
// Graph's, whichever the user is on.
func (a *app) filteredCommitFooter(c *ui.Context, theme *ui.Theme, target int, query *commitQuery) {
	switch {
	case query.loading:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c).Label(tr("Loading older matches"))
			ui.Text(c, tr("Loading older matches…")).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 1)
		})
	case query.err != "":
		ui.Column(c).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("Could not load older matches.")).TextColor(theme.Danger).
					FontSize(theme.FontSize - 1).Grow(1)
				if ui.Button(c, tr("Retry")).Clicked() {
					query.err = ""
					a.requestMoreFilteredCommits(target)
				}
			})
			ui.Text(c, query.err).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).SingleLine().Tooltip(query.err)
		})
	case !query.more:
		ui.Text(c, tr("Every matching commit is shown")).TextColor(theme.TextMuted).
			FontSize(theme.FontSize - 1)
	}
}

func (a *app) historyView(c *ui.Context) {
	if a.rebaseWorkspaceVisible() {
		a.rebaseHistoryView(c)
		return
	}
	theme := c.Theme()
	a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionHistory],
		"Expand list pane", func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
				a.historyListHeader(c)
				a.commitSelectionBar(c)
				commits := a.visibleCommits()
				if len(commits) == 0 {
					a.commitEmptyView(c, theme, sectionHistory)
					return
				}
				if a.historyFilter.err != "" {
					ui.Text(c, a.historyFilter.err).TextColor(theme.Danger).SingleLine().
						Tooltip(a.historyFilter.err)
				}
				a.commitListElement(c, commits).Grow(1).Rows(func(row ui.ListRow) {
					commit := commits[row.Index]
					services := row.Context.Services()
					rowElement := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(7, 8)
					if a.markedBaseCommit.Hash != "" && a.markedBaseCommit.Hash == commit.Hash {
						rowElement.Background(theme.Warning.Alpha(0.12)).Border(1, theme.Warning.Alpha(0.55)).Radius(theme.Radius)
					}
					rowElement.Children(func() {
						a.commitSelectionCheckbox(row.Context, commit)
						a.authorAvatar(row.Context, commit, 18)
						ui.Column(row.Context).Gap(2).Grow(1).Children(func() {
							ui.Text(row.Context, commit.Subject).SingleLine().Grow(1)
							ui.Row(row.Context).Gap(8).Children(func() {
								ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row)).SingleLine()
								if a.markedBaseCommit.Hash != "" && a.markedBaseCommit.Hash == commit.Hash {
									ui.Badge(row.Context, tr("Rebase base"))
								}
								ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row)).SingleLine()
								if rel := relTime(commit.UnixTimestamp); rel != "" {
									ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row)).SingleLine()
								}
							})
						})
						commitDiffStatBlocks(row.Context, commit.ShortHash, commit.LinesAdded, commit.LinesDeleted)
					})
					if a.commitSelectionMode && rowElement.RowClicked() {
						a.toggleCommitForBatch(commit)
					}
					rowElement.PopoverMenu(func(m *ui.Menu) {
						a.selectedCommit = row.Index
						a.commitRowMenu(m, commit, services)
					})
				})
				a.commitListFooter(c)
			})
		}, func() {
			a.commitDetailPane(c)
		})
}

// historyListHeader keeps the title, match count and controls on the first
// line. The applied filter, including a long branch name, gets the second.
func (a *app) historyListHeader(c *ui.Context) {
	// History opens on the branch checked out, and follows a checkout while
	// it holds that default. Keeping it here reads it as the pane draws.
	a.keepCommitFilterDefault(sectionHistory)
	filtered := a.historyFilter.narrows()
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			paneTitle(c, historyIcon, "Commits")
			if filtered {
				if a.historyFilter.loading {
					ui.Spinner(c).Label(tr("Searching commits"))
				} else if a.historyFilter.err == "" {
					ui.Badge(c, fmt.Sprintf("%d", len(a.historyFilter.matches)))
				}
			}
			ui.Spacer(c).Grow(1)
			commitHeaderButton(c, "Open Rebase TODO workspace", "Start or inspect a real Git rebase plan", rebaseIcon, a.openRebaseWorkspace)
			a.commitSelectionModeButton(c)
			commitFilterButton(c, "Search and filter the commit list", filtered, func() {
				a.openCommitFilter(sectionHistory)
			})
			if filtered {
				clearFilterButton(c, "Clear every filter and show all commits", func() {
					a.clearCommitFilter(sectionHistory)
				})
			}
			a.commitRefreshButton(c)
			paneCollapseButton(c, &a.workspacePaneCollapsed[sectionHistory], "Collapse list pane")
		})
		if filtered {
			ui.Row(c).Children(func() {
				ui.Badge(c, commitFilterSummary(a.historyFilter.applied))
			})
		}
	})
}

// commitEmptyView explains an empty commit list: a filter with no matches, a
// read still running, a failed read, or a repository without commits. target
// names the page, History or the Graph, so the clear and retry controls act on
// the filter that emptied it.
func (a *app) commitEmptyView(c *ui.Context, theme *ui.Theme, target int) {
	if a.repositoryDetailsLoading {
		// A progressive core has no commits until its deferred history lands.
		// That is a loading state, not an empty repository.
		loadingPane(c, "Loading commits", "Reading the branch's history…")
		return
	}
	query := a.commitQueryFor(target)
	filtered := query.narrows()
	switch {
	case filtered && query.loading:
		loadingPane(c, "Searching commits", "Searching commits…")
	case filtered && query.err != "":
		ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
			ui.Icon(c, filterIcon).FontSize(24).TextColor(theme.Danger)
			ui.Text(c, tr("The filter could not read the commits.")).Bold().TextAlign(ui.Center)
			ui.Text(c, query.err).TextColor(theme.TextMuted).TextAlign(ui.Center)
			if ui.PrimaryButton(c, tr("Try again")).Clicked() {
				query.err = ""
				a.runCommitFilter(target, "")
			}
		})
	case filtered:
		ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
			ui.Icon(c, filterIcon).FontSize(24).TextColor(theme.TextMuted)
			ui.Text(c, tr("No commits match the filter.")).Bold().TextAlign(ui.Center)
			ui.Text(c, tr("Clear the filter to see every commit again.")).
				TextColor(theme.TextMuted).TextAlign(ui.Center)
			if ui.Button(c, tr("Clear filters")).Clicked() {
				a.clearCommitFilter(target)
			}
		})
	default:
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No commits yet")).TextColor(theme.TextMuted)
		})
	}
}

// commitDetailPane is the chosen History or Graph commit. The card itself is
// also used by Reflog, whose entries name commits but offer different actions.
func (a *app) commitDetailPane(c *ui.Context) {
	commits := a.visibleCommits()
	if a.selectedCommit < 0 || a.selectedCommit >= len(commits) {
		a.commitDetailCard(c, nil, "Select a commit", nil)
		return
	}
	commit := commits[a.selectedCommit]
	a.commitDetailCard(c, &commit, "Select a commit", func(c *ui.Context, commit repository.Commit) {
		a.commitActionIcons(c, commit)
	})
}

// commitDetailCard renders one commit's metadata, changed files, and selected
// file diff. Keeping the data source outside the card lets History, Graph and
// Reflog share the useful detail view without making their list selections
// alias one another.
func (a *app) selectCommitFileRow(row int) {
	selectFileTreeRow(a.commitFileRows, row, &a.selectedCommitFileRow, &a.selectedCommitFile)
	if a.selectedCommitFile >= 0 {
		return
	}
	// A directory has no diff. Cancel and version the old request so even a
	// non-cooperative reader cannot repaint or populate this row later.
	if a.cancelCommitDiff != nil {
		a.cancelCommitDiff()
		a.cancelCommitDiff = nil
	}
	a.commitDiffGeneration++
	a.commitDiffHash = ""
	a.commitDiffPath = ""
	a.commitDiffPatch = ""
	a.commitDiffState = nil
	a.commitDiffMedia = nil
	a.commitDiffLoading = false
	a.commitDiffError = ""
}

const (
	commitDetailsLeftRight = iota
	commitDetailsTopBottom
	commitDetailsPaneGap float32 = 12
)

// commitDetailsLayoutControl switches the changed files and their diff between
// side-by-side and stacked panes. The two icons stay visible together so the
// current choice is clear without opening a menu.
func (a *app) commitDetailsLayoutControl(c *ui.Context) {
	theme := c.Theme()
	parts := ui.SegmentedBase(c, &a.commitDetailsLayout, 2)
	track := parts.Track.AlignItems(ui.Center).Padding(2).Gap(2).
		Radius(theme.Radius).Background(theme.Surface).Border(1, theme.Border).
		Label(tr("Commit details layout"))
	track.Children(func() {
		options := []struct {
			label string
			icon  *ui.SVG
		}{
			{label: "Switch layout (Left/Right)", icon: splitIcon},
			{label: "Switch layout (Top/Bottom)", icon: diffUnifiedIcon},
		}
		for i, option := range options {
			chosen := i == a.commitDetailsLayout
			segment := parts.Segment(i).Size(26, 22).Padding(0).Center().
				Radius(max(theme.Radius-2, 0)).Border(1, theme.Border.Alpha(0)).
				Label(tr(option.label)).Tooltip(tr(option.label))
			switch {
			case chosen:
				segment.Background(theme.Accent.Alpha(0.14)).TextColor(theme.Accent).
					Border(1, theme.Accent.Alpha(0.55))
			case segment.Hovered():
				segment.Background(theme.SurfaceHover)
			}
			segment.Children(func() {
				ui.Icon(c, option.icon).FontSize(14)
			})
		}
	})
	if track.Changed() {
		a.settings.CommitDetailsLayout = a.commitDetailsLayout
		a.saveSettings()
	}
}

// commitDetailsSplit keeps independent divider positions for the two layouts,
// so trying one never loses the size the user chose in the other. Each pane
// leaves half the visual gap beside the divider, keeping the draggable line
// centered in open space instead of pressed against either pane's content.
func (a *app) commitDetailsSplit(c *ui.Context, first, second func()) ui.Element {
	return a.commitDetailsSplitWith(c, &a.commitFilesWidth, &a.commitFilesHeight,
		&a.commitFilesCollapsed, first, second)
}

// commitDetailsSplitWith shares the layout while keeping each data source's
// divider and collapse state independent.
func (a *app) commitDetailsSplitWith(c *ui.Context, width, height *float32, collapsed *bool,
	first, second func()) ui.Element {
	vertical := a.commitDetailsLayout == commitDetailsTopBottom
	withDividerSpace := func(before bool, content func()) func() {
		return func() {
			pane := ui.Box(c).Fill()
			inset := commitDetailsPaneGap / 2
			switch {
			case vertical && before:
				pane.Padding(0, 0, inset, 0)
			case vertical:
				pane.Padding(inset, 0, 0, 0)
			case before:
				pane.PaddingEnd(inset)
			default:
				pane.PaddingStart(inset)
			}
			pane.Children(content)
		}
	}
	first = withDividerSpace(true, first)
	second = withDividerSpace(false, second)
	host := ui.Box(c).Grow(1)
	host.Children(func() {
		if vertical {
			collapsiblePaneSplitVertical(c, height, collapsed,
				"Expand changed files", first, second)
			return
		}
		collapsiblePaneSplit(c, width, collapsed,
			"Expand changed files", first, second)
	})
	return host
}

// commitDetailViewState binds the shared commit-detail renderer to one data
// source. Keeping these as pointers preserves History and Branches' separate
// selections, async reads, keyboard identities, and pane sizes.
type commitDetailViewState struct {
	scope           string
	message         *commitMessageDetailState
	requestMessage  func(repository.Commit)
	filesHash       *string
	filesLoading    *bool
	filesError      *string
	files           *[]repository.CommitFile
	fileRows        *[]fileTreeRow
	fileList        *ui.ListState
	selectedFileRow *int
	selectedFile    *int
	filesHeight     *float32
	filesWidth      *float32
	filesCollapsed  *bool
	diffLoading     *bool
	diffError       *string
	diffPath        *string
	diffState       **editor.DiffState
	diffMedia       **mediaReview
	requestDetails  func(repository.Commit)
	requestDiff     func(repository.Commit, repository.CommitFile)
	selectFileRow   func(int)
}

func (a *app) historyCommitDetailViewState() commitDetailViewState {
	return commitDetailViewState{
		scope:          fileTreeCommitScope,
		message:        &a.commitDetailMessage,
		requestMessage: a.requestHistoryCommitMessage,
		filesHash:      &a.commitFilesHash, filesLoading: &a.commitFilesLoading, filesError: &a.commitFilesError,
		files: &a.commitFiles, fileRows: &a.commitFileRows, fileList: &a.commitFileList,
		selectedFileRow: &a.selectedCommitFileRow, selectedFile: &a.selectedCommitFile,
		filesHeight: &a.commitFilesHeight, filesWidth: &a.commitFilesWidth, filesCollapsed: &a.commitFilesCollapsed,
		diffLoading: &a.commitDiffLoading, diffError: &a.commitDiffError, diffPath: &a.commitDiffPath,
		diffState: &a.commitDiffState, diffMedia: &a.commitDiffMedia,
		requestDetails: a.requestCommitDetails, requestDiff: a.requestCommitFileDiff,
		selectFileRow: a.selectCommitFileRow,
	}
}

func (a *app) commitDetailCard(c *ui.Context, commit *repository.Commit, emptyLabel string,
	actionIcons func(*ui.Context, repository.Commit)) {
	islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
		a.commitDetailBody(c, commit, emptyLabel, actionIcons, a.historyCommitDetailViewState())
	})
}

const commitMessageDefaultHeight = float32(120)

// commitMessageBody shows the message read-only. When a body is present,
// commitDetailBody places this view above a draggable middle divider.
func (a *app) commitMessageBody(c *ui.Context, commit repository.Commit, state commitDetailViewState) {
	a.commitMessageBodyWithScrollState(c, commit, state, &state.message.scroll)
}

// commitMessageBodyWithScrollState accepts a distinct widget state when the
// normal details and fullscreen details are mounted together. A nil state
// keeps scrolling in the element's own identity instead of sharing a Handle.
func (a *app) commitMessageBodyWithScrollState(c *ui.Context, commit repository.Commit, state commitDetailViewState, scrollState *ui.ScrollState) {
	state.requestMessage(commit)
	message := state.message
	switch {
	case message.loading:
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading the full commit message…"))
			ui.Text(c, tr("Loading the full commit message…")).TextColor(c.Theme().TextMuted)
		})
	case message.error != "":
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, message.error).TextColor(c.Theme().Danger).SingleLine().Tooltip(message.error).Grow(1).MinWidth(0)
			if ui.Button(c, tr("Retry")).Clicked() {
				message.hash = ""
				state.requestMessage(commit)
			}
		})
	case message.message.Description != "":
		ui.Column(c).Fill().Gap(4).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, tr("Description")).Bold().TextColor(c.Theme().TextMuted).Grow(1)
				label, icon := "Show raw commit message", fileCodeIcon
				if message.raw {
					label, icon = "Show rendered commit message", fileDocIcon
				}
				flatIconButton(c, label, label, icon, 14, 3, false, func() {
					message.raw = !message.raw
				})
			})
			body := ui.Scroll(c).Grow(1).FillWidth().Label(tr("Commit message"))
			if scrollState != nil {
				body.TrackScroll(scrollState)
			}
			body.Children(func() {
				if message.raw {
					ui.Text(c, message.message.Description).Selectable().FillWidth()
					return
				}
				if message.markdown == nil {
					message.markdown = &markdown.Options{WrapCode: true}
				}
				markdown.View(c, message.message.Description, message.markdown)
			})
		})
	}
}

// commitDetailBody is the one presentation used by History, Graph, Reflog,
// and Branches. Data loading and commit actions remain specific to the caller.
func (a *app) commitDetailBody(c *ui.Context, commit *repository.Commit, emptyLabel string,
	actionIcons func(*ui.Context, repository.Commit), state commitDetailViewState) {
	a.commitDetailBodyWithOptions(c, commit, emptyLabel, actionIcons, state, false, nil)
}

// commitDetailMetadataBody reuses the commit metadata and Markdown body
// without duplicating the files/diff that the fullscreen diff already owns.
func (a *app) commitDetailMetadataBody(c *ui.Context, commit *repository.Commit, state commitDetailViewState, scrollState *ui.ScrollState) {
	a.commitDetailBodyWithOptions(c, commit, "Select a commit", nil, state, true, scrollState)
}

// commitDetailBodyWithOptions optionally restricts the shared renderer to
// metadata and message content for the expandable full-window detail pane.
func (a *app) commitDetailBodyWithOptions(c *ui.Context, commit *repository.Commit, emptyLabel string,
	actionIcons func(*ui.Context, repository.Commit), state commitDetailViewState, metadataOnly bool, scrollState *ui.ScrollState) {
	theme := c.Theme()
	if commit == nil {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr(emptyLabel)).TextColor(theme.TextMuted)
		})
		return
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, commit.Subject).FontSize(18).Bold().SingleLine().Tooltip(commit.Subject).
			Grow(1).MinWidth(0)
		if !metadataOnly {
			a.expandDiffButton(c)
		}
	})
	if actionIcons != nil {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Wrap().Children(func() { actionIcons(c, *commit) })
	}
	if !metadataOnly {
		a.diffViewOptionsWith(c, a.commitDetailsLayoutControl, false)
		a.customPatchBar(c)
	}
	copyLabel := "Copy commit hash"
	if a.workspaceTab == sectionReflog {
		copyLabel = "Copy reflog commit hash"
	}
	a.commitHashRow(c, *commit, copyLabel)
	if commit.AuthorName != "" || commit.AuthorEmail != "" {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			a.authorAvatar(c, *commit, 24)
			switch {
			case commit.AuthorName == "":
				ui.Text(c, commit.AuthorEmail).Grow(1).SingleLine().Tooltip(commit.AuthorEmail)
			case commit.AuthorEmail == "":
				ui.Text(c, commit.AuthorName).Grow(1).SingleLine().Tooltip(commit.AuthorName)
			default:
				ui.Textf(c, "%s <%s>", commit.AuthorName, commit.AuthorEmail).Grow(1).SingleLine().
					Tooltip(fmt.Sprintf("%s <%s>", commit.AuthorName, commit.AuthorEmail))
			}
		})
	}
	if rel := relTime(commit.UnixTimestamp); rel != "" {
		ui.Text(c, rel).TextColor(theme.TextMuted)
	}
	if len(commit.Tags) > 0 {
		ui.Textf(c, tr("Tags: %s"), strings.Join(commit.Tags, ", ")).TextColor(theme.TextMuted)
	}
	state.requestMessage(*commit)
	if metadataOnly {
		a.commitMessageBodyWithScrollState(c, *commit, state, scrollState)
		return
	}
	state.requestDetails(*commit)
	filesAndDiff := func() {
		if *state.filesLoading && *state.files == nil {
			loadingPane(c, "Loading commit details", "Loading commit details…")
			return
		}
		if *state.filesError != "" {
			ui.Text(c, *state.filesError).TextColor(theme.Danger)
		}
		if *state.files == nil {
			ui.Column(c).Grow(1).Center().Children(func() {
				if ui.Button(c, tr("Load changed files")).Clicked() {
					// Drop the failed read's key so this click reads it again.
					*state.filesHash = ""
					state.requestDetails(*commit)
				}
			})
			return
		}
		files := *state.files
		paths := commitFilePaths(files)
		previousSelectedRow := *state.selectedFileRow
		*state.fileRows = a.syncFileTreeRows(state.scope, paths, *state.fileRows,
			state.selectedFileRow, state.selectedFile)
		revealSelectedRow := *state.selectedFileRow >= 0 && *state.selectedFileRow != previousSelectedRow
		// The files and the diff share the rest of the pane, and the line
		// between them is dragged to give one more room than the other.
		a.commitDetailsSplitWith(c, state.filesWidth, state.filesHeight, state.filesCollapsed, func() {
			ui.Column(c).Fill().Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Icon(c, folderIcon).FontSize(13).TextColor(theme.Accent)
					ui.Text(c, tr("Commit files")).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d", len(files)))
					a.fileTreeModeButton(c)
					if *state.filesLoading {
						ui.Spinner(c).Label(tr("Loading commit details"))
					}
					paneCollapseButton(c, state.filesCollapsed, "Collapse changed files").MarginEnd(8)
				})
				guides := treeGuideGroups(*state.fileRows)
				list := a.keyboardList(c, state.fileList, len(*state.fileRows)).Grow(1).Rows(func(row ui.ListRow) {
					item := (*state.fileRows)[row.Index]
					if item.isDirectory() {
						directory := a.fileTreeDirectoryRow(row.Context, state.scope, item, row.Selected(), func() {
							state.selectFileRow(row.Index)
						})
						guides.decorate(directory, item, row)
						return
					}
					file := files[item.source]
					services := row.Context.Services()
					fileRow := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8, 6, float32(8+16*item.depth))
					guides.decorate(fileRow, item, row).Children(func() {
						fileTypeMark(row.Context, file.Path, 13, row.Selected())
						ui.Text(row.Context, item.name).Grow(1).SingleLine().Tooltip(file.Path)
						fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
						statusSlot(row.Context, statusLetter(file.Status),
							rowStatusColor(theme, row, theme.Warning))
					}).PopoverMenu(func(m *ui.Menu) {
						a.commitFileMenu(m, commit, file, services)
					})
				})
				if a.keyboardListChanged(state.fileList, list) {
					state.selectFileRow(*state.selectedFileRow)
				}
				if revealSelectedRow {
					// A programmatic choice has no list input for MyGo to reveal.
					// Scroll after its rows exist, not when the async read chooses it.
					state.fileList.ScrollIntoView(*state.selectedFileRow)
				}
				normalizeFileTreeRow(*state.fileRows, state.selectedFileRow, state.selectedFile)
			})
		}, func() {
			ui.Column(c).Fill().Children(func() {
				if *state.selectedFile >= 0 && *state.selectedFile < len(files) && !*state.filesLoading && *state.filesError == "" {
					state.requestDiff(*commit, files[*state.selectedFile])
				}
				if *state.diffLoading && (*state.diffState != nil || *state.diffMedia != nil) {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Label(tr("Loading diff"))
						ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
				}
				switch {
				case *state.diffLoading && *state.diffState == nil && *state.diffMedia == nil:
					loadingPane(c, "Loading diff", "Loading diff…")
				case *state.diffError != "":
					ui.Text(c, *state.diffError).TextColor(theme.Danger)
				case *state.diffState != nil:
					a.keyboardDiffViewer(c, *state.diffState)
				default:
					noTextualDiffOrMedia(c, *state.diffMedia, *state.diffPath)
				}
			})
		}).Grow(1)
	}
	if state.message.message.Description != "" && !state.message.loading && state.message.error == "" {
		paneSplitVerticalReserve(c, &state.message.height, diffRegionMinHeight,
			func() { a.commitMessageBody(c, *commit, state) }, filesAndDiff).Grow(1)
		return
	}
	a.commitMessageBody(c, *commit, state)
	filesAndDiff()
}
