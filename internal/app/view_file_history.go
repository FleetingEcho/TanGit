package app

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// fileHistoryPage is how many commits the drawer reads at a time: it shows
// the first page, and reads another as the list scrolls to the end, so a long
// file history keeps paging in rather than stopping at a fixed count.
const fileHistoryPage = 100

// commitFileMenu adds the actions of a changed file to m: read or restore its
// history, and copy either of its paths.
func (a *app) commitFileMenu(m *ui.Menu, commit *repository.Commit, file repository.CommitFile, services ui.Services) {
	root := a.snapshot.Root
	if m.Item(tr("Open in built-in editor")).Chosen() {
		a.openGitFileInBuiltInEditor(root, file.Path)
	}
	if m.Item(tr("Blame")).Chosen() {
		a.openGitFileBlameInBuiltInEditor(root, file.Path)
	}
	m.Separator()
	if m.Item(tr("View file changes…")).Chosen() {
		if commit == nil {
			a.openFileHistory(file.Path)
		} else {
			a.openFileHistoryFrom(file.Path, *commit)
		}
	}
	if commit != nil && a.customPatchActive() && m.Item(tr("Add file to Custom Patch")).Shortcut(ui.Ctrl, ui.KeyB).
		Disabled(a.actionBusy || a.session == nil).Chosen() {
		a.beginAddCustomPatchFile(*commit, file, nil)
	}
	if commit != nil && m.Item(tr("Restore this historical file…")).Shortcut(ui.Shift, ui.KeyR).
		Disabled(a.actionBusy || a.session == nil).Chosen() {
		a.confirmRestoreCommitFile(*commit, file)
	}
	m.Separator()
	a.copyPathItems(m, services, file.Path)
}

// findFileHistories opens the drawer only when Git has commits for path. A
// working-tree file can be new or unknown to history, in which case the action
// just reports that result instead of leaving an empty drawer open.
func (a *app) findFileHistories(path string) {
	session := a.session
	if session == nil {
		return
	}
	ctx := a.repositoryWork.Context()
	a.fileHistoryToken++
	token := a.fileHistoryToken
	go func() {
		defer a.recoverBackground()
		commits, err := requireHistorySession(session).FileCommits(ctx, path, fileHistoryPage, 0)
		a.updateChanged(func() bool {
			if token != a.fileHistoryToken {
				return false
			}
			if err != nil {
				a.notifyError(err)
				return true
			}
			if len(commits) == 0 {
				a.notify("", "No commits changed this file.", path)
				return true
			}
			a.openFileHistory(path)
			return true
		})
	}()
}

// openFileHistory opens the drawer that lists the commits that changed path,
// read from the repository rather than from the commits on screen.
func (a *app) openFileHistory(path string) {
	a.openFileHistoryFrom(path, repository.Commit{})
}

func (a *app) openFileHistoryFrom(path string, origin repository.Commit) {
	if a.editorOnlyWorkspace() {
		return
	}
	a.fileHistoryOpen = true
	a.fileHistoryPath = path
	a.fileHistoryOrigin = origin
	a.fileHistoryCommits = nil
	a.fileHistorySelected = 0
	a.fileHistoryList.Choose(0)
	a.fileHistoryError = ""
	a.fileHistoryLoading = true
	a.fileHistoryMore = true
	a.fileHistoryMoreError = ""
	a.fileHistoryLoadingMore = false
	a.fileHistoryRead = 0
	a.fileHistoryToken++
	token := a.fileHistoryToken
	a.resetFileHistoryCommit()
	session := a.session
	if session == nil {
		a.fileHistoryLoading = false
		a.fileHistoryMore = false
		a.fileHistoryError = "No repository is open."
		return
	}
	ctx := a.repositoryWork.Context()
	if origin.Hash != "" {
		// Show the entry point's details immediately. The completed Git log
		// replaces this provisional row and selects the same commit by hash.
		a.fileHistoryCommits = []repository.Commit{origin}
		a.fileHistoryList.ScrollTo(0, ui.Start)
		a.loadFileHistoryCommit(origin, true)
	}
	go func() {
		defer a.recoverBackground()
		commits, read, more, err := readFileHistoryForDrawer(ctx, session, path, origin.Hash)
		a.updateChanged(func() bool {
			if token != a.fileHistoryToken || !a.fileHistoryOpen || a.fileHistoryPath != path {
				return false
			}
			a.fileHistoryLoading = false
			a.fileHistoryCommits = nil
			a.storeFileHistoryPage(commits, read, more)
			if origin.Hash != "" {
				index := slices.IndexFunc(a.fileHistoryCommits, func(commit repository.Commit) bool {
					return commit.Hash == origin.Hash
				})
				if index < 0 {
					// The file's own log can leave out the commit it was opened
					// from, as it does a merge whose parents carry the change.
					// Keep that commit as a row, in date order, so the selection
					// and the files and diff already shown stay what they were.
					index = len(a.fileHistoryCommits)
					for i, commit := range a.fileHistoryCommits {
						if commit.UnixTimestamp <= origin.UnixTimestamp {
							index = i
							break
						}
					}
					a.fileHistoryCommits = slices.Insert(a.fileHistoryCommits, index, origin)
				}
				a.fileHistorySelected = index
				a.fileHistoryList.Choose(index)
				a.fileHistoryList.ScrollTo(index, ui.Center)
				if err != nil {
					a.fileHistoryMoreError = safeError(err)
				}
				return true
			}
			if err != nil {
				a.fileHistoryMore = false
				a.fileHistoryError = safeError(err)
				return true
			}
			if len(a.fileHistoryCommits) == 0 {
				return true
			}
			// Select through the list, not only the index: it tracks the
			// chosen row by key, so a stale choice from an earlier open
			// would otherwise leave the first commit unhighlighted.
			a.fileHistorySelected = 0
			a.fileHistoryList.Choose(0)
			a.fileHistoryList.ScrollTo(0, ui.Start)
			a.loadFileHistoryCommit(a.fileHistoryCommits[0], true)
			return true
		})
	}()
}

// readFileHistoryForDrawer reads the newest page, and for a historical entry
// point keeps reading in Git's own order until the chosen commit is visible.
// A single revision union supplies every page, so the result needs no merge
// or timestamp sort and the entry point can be below the first 100 rows.
func readFileHistoryForDrawer(ctx context.Context, session repository.Session, path, originHash string) (commits []repository.Commit, read int, more bool, err error) {
	history := requireHistorySession(session)
	seen := map[string]bool{}
	more = true
	for {
		var page []repository.Commit
		if originHash == "" {
			page, err = history.FileCommits(ctx, path, fileHistoryPage, read)
		} else {
			page, err = history.FileCommitsIncluding(ctx, path, originHash, fileHistoryPage, read)
		}
		if err != nil {
			return commits, read, true, err
		}
		read += len(page)
		more = len(page) == fileHistoryPage
		for _, commit := range page {
			if !seen[commit.Hash] {
				seen[commit.Hash] = true
				commits = append(commits, commit)
			}
		}
		if !more || originHash == "" || seen[originHash] {
			return commits, read, more, nil
		}
	}
}

// loadMoreFileHistory reads the page of commits after those the drawer shows,
// as its list scrolls to the end, so a file changed hundreds of times pages
// in instead of stopping at the first read.
func (a *app) loadMoreFileHistory() {
	if !a.fileHistoryOpen || a.fileHistoryLoading || a.fileHistoryLoadingMore ||
		!a.fileHistoryMore || a.fileHistoryMoreError != "" {
		return
	}
	session := a.session
	if session == nil {
		return
	}
	path := a.fileHistoryPath
	originHash := a.fileHistoryOrigin.Hash
	skip := a.fileHistoryRead
	token := a.fileHistoryToken
	a.fileHistoryLoadingMore = true
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		history := requireHistorySession(session)
		var commits []repository.Commit
		var err error
		if originHash == "" {
			commits, err = history.FileCommits(ctx, path, fileHistoryPage, skip)
		} else {
			commits, err = history.FileCommitsIncluding(ctx, path, originHash, fileHistoryPage, skip)
		}
		a.updateChanged(func() bool {
			if token != a.fileHistoryToken || !a.fileHistoryOpen || a.fileHistoryPath != path {
				return false
			}
			a.fileHistoryLoadingMore = false
			a.storeFileHistoryPage(commits, skip+len(commits), err != nil || len(commits) == fileHistoryPage)
			if err != nil {
				a.fileHistoryMoreError = safeError(err)
				return true
			}
			return true
		})
	}()
}

// storeFileHistoryPage appends in Git's order. read includes duplicate rows
// from a history that shifted between reads, so the next Git offset is exact.
func (a *app) storeFileHistoryPage(commits []repository.Commit, read int, more bool) {
	a.fileHistoryRead = read
	a.fileHistoryMore = more
	selected := commitHashAt(a.fileHistoryCommits, a.fileHistorySelected)
	seen := make(map[string]bool, len(a.fileHistoryCommits)+len(commits))
	for _, commit := range a.fileHistoryCommits {
		seen[commit.Hash] = true
	}
	for _, commit := range commits {
		if !seen[commit.Hash] {
			seen[commit.Hash] = true
			a.fileHistoryCommits = append(a.fileHistoryCommits, commit)
		}
	}
	if selected != "" {
		a.fileHistorySelected = selectedCommitIndex(a.fileHistoryCommits, selected)
		a.fileHistoryList.Choose(a.fileHistorySelected)
	}
}

// closeFileHistory hides the drawer and drops every read it had in flight.
func (a *app) closeFileHistory() {
	a.fileHistoryOpen = false
	a.fileHistoryToken++
	a.fileHistoryPath = ""
	a.fileHistoryOrigin = repository.Commit{}
	a.fileHistoryCommits = nil
	a.fileHistoryLoading = false
	a.fileHistoryError = ""
	a.fileHistorySelected = 0
	a.fileHistoryMore = false
	a.fileHistoryMoreError = ""
	a.fileHistoryLoadingMore = false
	a.fileHistoryRead = 0
	a.resetFileHistoryCommit()
}

func (a *app) openFileHistoryCommitInHistory(hash string) {
	if !a.sectionVisible(sectionHistory) {
		return
	}
	a.closeFileHistory()
	a.showCommitInHistory(hash)
}

// showCommitInHistory opens History on a commit, searching for it by hash when
// the loaded page or the active filter does not contain it.
func (a *app) showCommitInHistory(hash string) {
	a.showCommitInSection(sectionHistory, hash)
}

// showCommitInSection is showCommitInHistory for either commit list, History
// or Graph.
func (a *app) showCommitInSection(section int, hash string) {
	if !a.sectionVisible(section) {
		return
	}
	a.setWorkspaceTab(section)
	for index, commit := range a.visibleCommits() {
		if commit.Hash == hash {
			a.commitList.Choose(index)
			a.commitList.ScrollTo(index, ui.Center)
			return
		}
	}

	// The file's commits can be older than the loaded History page or outside its filter.
	query := a.commitQueryFor(section)
	query.reset()
	query.applied = repository.CommitFilter{Query: hash}
	query.draft = query.applied
	query.limit = repository.SnapshotCommitLimit
	a.commitList.Choose(-1)
	a.resetCommitDetails()
	a.runCommitFilter(section, "")
}

// resetFileHistoryCommit drops the second and third columns, and the reads
// they had in flight.
func (a *app) resetFileHistoryCommit() {
	a.fileHistoryFiles = nil
	a.fileHistoryFilesLoading = false
	a.fileHistoryFilesError = ""
	a.fileHistoryFilesHash = ""
	a.fileHistoryFilesToken++
	a.fileHistorySelectedFile = -1
	a.resetFileHistoryDiff()
}

// resetFileHistoryDiff drops the diff column and the read it had in flight.
func (a *app) resetFileHistoryDiff() {
	a.fileHistoryDiffState = nil
	a.fileHistoryDiffMedia = nil
	a.fileHistoryDiffPatch = ""
	a.fileHistoryDiffLoading = false
	a.fileHistoryDiffError = ""
	a.fileHistoryDiffHash = ""
	a.fileHistoryDiffPath = ""
	a.fileHistoryDiffToken++
}

// loadFileHistoryCommit reads the files the chosen commit changed, then the
// diff of the file the drawer is about, or of the first file when that commit
// did not touch it directly.
func (a *app) loadFileHistoryCommit(commit repository.Commit, force bool) {
	if !a.fileHistoryOpen || a.session == nil {
		return
	}
	if !force && a.fileHistoryFilesHash == commit.Hash && (a.fileHistoryFilesLoading || a.fileHistoryFilesError == "") {
		return
	}
	a.fileHistoryFiles = nil
	a.fileHistorySelectedFile = -1
	a.fileHistoryFilesLoading = true
	a.fileHistoryFilesError = ""
	a.fileHistoryFilesHash = commit.Hash
	a.resetFileHistoryDiff()
	a.fileHistoryFilesToken++
	token := a.fileHistoryFilesToken
	path := a.fileHistoryPath
	session := a.session
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		files, err := requireHistorySession(session).CommitFilesWithoutStats(ctx, commit)
		a.updateChanged(func() bool {
			if token != a.fileHistoryFilesToken || !a.fileHistoryOpen || a.fileHistoryPath != path {
				return false
			}
			a.fileHistoryFilesLoading = false
			if err != nil {
				a.fileHistoryFilesError = safeError(err)
				return true
			}
			a.fileHistoryFiles = files
			if len(files) == 0 {
				return true
			}
			choice := 0
			for i, file := range files {
				if file.Path == path || file.PreviousPath == path {
					choice = i
					break
				}
			}
			a.fileHistorySelectedFile = choice
			a.fileHistoryFileList.Choose(choice)
			a.fileHistoryFileList.ScrollTo(choice, ui.Center)
			a.loadFileHistoryDiff(commit, files[choice], true)
			return true
		})
	}()
}

// loadFileHistoryDiff reads the diff of one file in one commit into the third
// column.
func (a *app) loadFileHistoryDiff(commit repository.Commit, file repository.CommitFile, force bool) {
	if !a.fileHistoryOpen || a.session == nil {
		return
	}
	if !force && a.fileHistoryDiffHash == commit.Hash && a.fileHistoryDiffPath == file.Path &&
		(a.fileHistoryDiffLoading || a.fileHistoryDiffError == "") {
		return
	}
	a.fileHistoryDiffState = nil
	a.fileHistoryDiffMedia = nil
	a.fileHistoryDiffPatch = ""
	a.fileHistoryDiffLoading = true
	a.fileHistoryDiffError = ""
	a.fileHistoryDiffHash = commit.Hash
	a.fileHistoryDiffPath = file.Path
	a.fileHistoryDiffToken++
	token := a.fileHistoryDiffToken
	session := a.session
	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		diff, err := requireHistorySession(session).CommitFileDiff(ctx, commit, file, options)
		var state *editor.DiffState
		var media *mediaReview
		if err == nil {
			state, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
			if err == nil && state == nil && isMediaPath(file.Path) {
				media, err = loadMediaReview(ctx, session, repository.MediaRequest{
					Source: repository.MediaCommit, Commit: commit, CommitFile: file,
				})
			}
		}
		a.updateChanged(func() bool {
			if token != a.fileHistoryDiffToken || !a.fileHistoryOpen {
				return false
			}
			a.fileHistoryDiffLoading = false
			if err != nil {
				a.fileHistoryDiffError = safeError(err)
				return true
			}
			a.fileHistoryDiffPatch = diff.Patch
			// Bring the first change into view, as the Changes diff does,
			// so a hunk far down the file doesn't need a manual scroll.
			if state != nil && len(state.Hunks()) > 0 {
				state.GoToHunk(0)
			}
			a.fileHistoryDiffState = state
			a.fileHistoryDiffMedia = media
			return true
		})
	}()
}

// fileHistoryDrawer is the drawer listing the commits that changed a file,
// the files of the chosen commit, and the diff of the chosen file. Its outward
// edge and both inner dividers drag.
func (a *app) fileHistoryDrawer(c *ui.Context, overlayTopInset float32) {
	if !a.fileHistoryOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(overlayTopInset).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.3)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			// The drawer's share of the width, and the dimmed page it
			// leaves on the left for the divider to move.
			if a.fileHistoryWidth < 30 {
				a.fileHistoryWidth = 80
			}
			if a.fileHistoryWidth > 96 {
				a.fileHistoryWidth = 96
			}
			// Click-away area, drawer, and an invisible handle on the drawer's
			// left edge: no line or grip is drawn, only the resize cursor.
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).WidthPercent(100 - a.fileHistoryWidth).
				Tooltip(tr("Close the file history")).OnClick(a.closeFileHistory)
			ui.Column(c).Absolute().Right(0).Top(0).Bottom(0).WidthPercent(a.fileHistoryWidth).
				Padding(islandGap / 2).Gap(0).Background(islandBackdrop(theme)).
				Role(ui.RoleDialog).Label(tr("File history")).
				Children(func() {
					a.fileHistoryPanel(c)
				})
			handle := ui.Box(c).Absolute().Top(0).Bottom(0).LeftPercent(100-a.fileHistoryWidth).Width(10).
				Translate(-5, 0).Cursor(ui.CursorResizeEW).Label(tr("Drag to resize"))
			if dx, _, held := handle.Dragged(); held && dx != 0 {
				if total := back.Bounds().W; total > 0 {
					a.fileHistoryWidth = min(max(a.fileHistoryWidth-dx*100/total, 30), 96)
				}
			}
			if back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeFileHistory()
			}
		})
	})
}

// fileHistoryPanel is the drawer's contents: its header, then the three
// columns.
func (a *app) fileHistoryPanel(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Gap(5).Padding(10, 12)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			iconButton(c, "Close file history", "Escape", chevronLeftIcon, false, a.closeFileHistory)
			ui.Icon(c, historyIcon).FontSize(15).TextColor(theme.Accent)
			ui.Text(c, tr("File history")).FontSize(16).Bold().Grow(1)
			if n := len(a.fileHistoryCommits); n > 0 {
				label := trf("%d commits", n)
				if a.fileHistoryMore {
					label = trf("%d+ commits", n)
				}
				ui.Badge(c, label)
			}
			iconButton(c, "Close file history", "Escape", closeIcon, false, a.closeFileHistory)
		})
		services := c.Services()
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			fileTypeMark(c, a.fileHistoryPath, 13, false)
			ui.Text(c, a.fileHistoryPath).TextColor(theme.TextMuted).SingleLine().
				Tooltip(a.fileHistoryPath).Selectable()
			path := a.fileHistoryPath
			flatIconButton(c, "Copy file path", "Copy the file path", copyIcon, 14, 3, false,
				func() { a.notifyCopied(services, "relative path", path) })
		})
	})
	collapsiblePaneSplitPercent(c, &a.fileHistoryCommitPercent, &a.fileHistoryCommitsCollapsed,
		"Expand file history commits", func() {
			a.fileHistoryCommitsView(c)
		}, func() {
			collapsiblePaneSplitPercent(c, &a.fileHistoryFilePercent, &a.fileHistoryFilesCollapsed,
				"Expand changed files", func() {
					a.fileHistoryFilesView(c)
				}, func() {
					a.fileHistoryDiffView(c)
				})
		})
}

// fileHistoryCommitsView is the first column: the commits that changed the
// file, newest first.
func (a *app) fileHistoryCommitsView(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, commitIcon).FontSize(13).TextColor(theme.TextMuted)
			ui.Text(c, tr("Commits")).Bold().Grow(1)
			a.fileHistoryFinderButton(c)
			paneCollapseButton(c, &a.fileHistoryCommitsCollapsed, "Collapse file history commits")
		})
		visible := a.fileHistoryVisibleCommits()
		a.fileHistoryRows = visible
		a.syncFileHistoryRow()
		a.fileHistoryFinderRow(c, len(visible), len(a.fileHistoryCommits))
		switch {
		case a.fileHistoryLoading && len(a.fileHistoryCommits) == 0:
			loadingPane(c, "Loading file history", "Reading the file's history…")
		case a.fileHistoryError != "":
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, a.fileHistoryError).TextColor(theme.Danger)
				if ui.Button(c, tr("Try again")).Clicked() {
					a.openFileHistoryFrom(a.fileHistoryPath, a.fileHistoryOrigin)
				}
			})
		case len(a.fileHistoryCommits) == 0:
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("No commits changed this file.")).TextColor(theme.TextMuted)
			})
		case len(visible) == 0:
			listSearchEmpty(c, "No commits match this filter")
		default:
			list := a.keyboardList(c, &a.fileHistoryList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
				if row.Index >= len(visible) {
					return
				}
				commit := a.fileHistoryCommits[visible[row.Index]]
				services := row.Context.Services()
				element := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).
					Padding(6, 8+scrollbarGutter(theme), 6, 8)
				reveal := element.Hovered() && a.sectionVisible(sectionHistory)
				element.Children(func() {
					a.authorAvatar(row.Context, commit, 18)
					ui.Column(row.Context).Gap(2).Grow(1).Children(func() {
						ui.Text(row.Context, commit.Subject).SingleLine()
						ui.Row(row.Context).Gap(8).Children(func() {
							ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row))
							ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row)).SingleLine()
							if rel := relTime(commit.UnixTimestamp); rel != "" {
								ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row)).SingleLine()
							}
						})
					})
					if reveal {
						row.Context.Key("file-history-open-in-history")
						ui.ButtonBase(row.Context).Padding(3).Shrink(0).
							Label(tr("Open in History")).Tooltip(tr("Open this commit in History")).
							OnClick(func() { a.openFileHistoryCommitInHistory(commit.Hash) }).
							Children(func() {
								ui.Icon(row.Context, historyIcon).FontSize(rowActionIconSize(theme)).
									TextColor(secondaryRowColor(theme, row))
							})
					}
				}).OnClick(func() {
					a.fileHistorySelected = visible[row.Index]
					a.fileHistoryRow = row.Index
					a.loadFileHistoryCommit(commit, false)
				}).PopoverMenu(func(m *ui.Menu) {
					a.commitRowMenu(m, commit, services)
				})
			})
			if a.keyboardListChanged(&a.fileHistoryList, list) && a.fileHistoryRow >= 0 && a.fileHistoryRow < len(visible) {
				a.fileHistorySelected = visible[a.fileHistoryRow]
				a.loadFileHistoryCommit(a.fileHistoryCommits[a.fileHistorySelected], false)
			}
			// Scrolling to the last commit reads the page below it, so the
			// history pages in as the list is scrolled rather than ending
			// at the first read.
			if a.fileHistoryList.AtEnd() {
				a.loadMoreFileHistory()
			}
			switch {
			case a.fileHistoryLoading || a.fileHistoryLoadingMore:
				ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Center().PassThrough().Children(func() {
					ui.Row(c).MaxWidthPercent(94).Padding(5, 8).Gap(8).AlignItems(ui.Center).
						Background(theme.Background).Radius(theme.Radius).PassThrough().Children(func() {
						ui.Spinner(c).Label(tr("Loading older commits"))
						ui.Text(c, tr("Loading older commits…")).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
					})
				})
			case a.fileHistoryMoreError != "":
				ui.Column(c).Gap(2).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, tr("Could not load older commits.")).TextColor(theme.Danger).
							FontSize(theme.FontSize - 1).Grow(1)
						if ui.Button(c, tr("Retry")).Clicked() {
							a.fileHistoryMoreError = ""
							a.loadMoreFileHistory()
						}
					})
					ui.Text(c, a.fileHistoryMoreError).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 2).SingleLine().Tooltip(a.fileHistoryMoreError)
				})
			case !a.fileHistoryMore:
				ui.Text(c, tr("Beginning of file history")).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 1)
			}
		}
	})
}

// fileHistoryFilesView is the second column: the files the chosen commit
// changed.
func (a *app) fileHistoryFilesView(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, folderIcon).FontSize(13).TextColor(theme.TextMuted)
			ui.Text(c, tr("Changed files")).Bold().Grow(1).SingleLine()
			if a.fileHistorySelected >= 0 && a.fileHistorySelected < len(a.fileHistoryCommits) {
				ui.Badge(c, a.fileHistoryCommits[a.fileHistorySelected].ShortHash)
			}
			a.fileHistoryFilesFinderButton(c)
			paneCollapseButton(c, &a.fileHistoryFilesCollapsed, "Collapse changed files")
		})
		visibleFiles := a.fileHistoryVisibleFiles()
		a.fileHistoryFileRows = visibleFiles
		a.syncFileHistoryFileRow()
		a.fileHistoryFilesFinderRow(c, len(visibleFiles), len(a.fileHistoryFiles))
		switch {
		case a.fileHistoryFilesLoading:
			loadingPane(c, "Loading commit files", "Reading the commit's files…")
		case a.fileHistoryFilesError != "":
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, a.fileHistoryFilesError).TextColor(theme.Danger)
				if ui.Button(c, tr("Try again")).Clicked() && a.fileHistorySelected >= 0 && a.fileHistorySelected < len(a.fileHistoryCommits) {
					a.loadFileHistoryCommit(a.fileHistoryCommits[a.fileHistorySelected], true)
				}
			})
		case len(a.fileHistoryFiles) == 0:
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Choose a commit to see its files.")).TextColor(theme.TextMuted)
			})
		default:
			if a.fileHistorySelected < 0 || a.fileHistorySelected >= len(a.fileHistoryCommits) {
				ui.Column(c).Grow(1).Center().Children(func() {
					ui.Text(c, tr("Choose a commit to see its files.")).TextColor(theme.TextMuted)
				})
				return
			}
			commit := a.fileHistoryCommits[a.fileHistorySelected]
			if len(visibleFiles) == 0 {
				listSearchEmpty(c, "No files match this filter")
				return
			}
			list := a.keyboardList(c, &a.fileHistoryFileList, len(visibleFiles)).Grow(1).Rows(func(row ui.ListRow) {
				// A menu action on an earlier row (View file changes…) reloads the
				// drawer and empties the list while its rows are still drawing.
				if row.Index >= len(visibleFiles) || visibleFiles[row.Index] >= len(a.fileHistoryFiles) {
					return
				}
				source := visibleFiles[row.Index]
				file := a.fileHistoryFiles[source]
				style := diffFileStyle(theme, file.Status)
				ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8).Children(func() {
					fileTypeMark(row.Context, file.Path, 13, row.Selected())
					ui.Text(row.Context, file.Path).Grow(1).SingleLine().Tooltip(file.Path)
					if file.PreviousPath != "" {
						ui.Text(row.Context, "← "+file.PreviousPath).
							TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1).SingleLine()
					}
					statusSlot(row.Context, statusLetter(file.Status), rowStatusColor(theme, row, style))
				}).OnClick(func() {
					a.fileHistorySelectedFile = source
					a.fileHistoryFileRow = row.Index
					a.loadFileHistoryDiff(commit, file, false)
				}).PopoverMenu(func(m *ui.Menu) {
					a.fileHistorySelectedFile = source
					a.fileHistoryFileRow = row.Index
					a.commitFileMenu(m, &commit, file, row.Context.Services())
				})
			})
			if a.keyboardListChanged(&a.fileHistoryFileList, list) && a.fileHistoryFileRow >= 0 && a.fileHistoryFileRow < len(visibleFiles) {
				a.fileHistorySelectedFile = visibleFiles[a.fileHistoryFileRow]
				a.loadFileHistoryDiff(commit, a.fileHistoryFiles[a.fileHistorySelectedFile], false)
			}
		}
	})
}

// fileHistoryDiffView is the third column: the diff of the chosen file in the
// chosen commit.
func (a *app) fileHistoryDiffView(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, diffUnifiedIcon).FontSize(13).TextColor(theme.TextMuted)
			ui.Text(c, tr("File diff")).Bold()
			if a.fileHistoryDiffPath != "" {
				ui.Text(c, a.fileHistoryDiffPath).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 1).SingleLine().Grow(1).Tooltip(a.fileHistoryDiffPath)
			}
		})
		ui.Row(c).Justify(ui.End).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			a.diffViewOptions(c)
		})
		switch {
		case a.fileHistoryDiffLoading:
			loadingPane(c, "Loading file diff", "Reading the file's diff…")
		case a.fileHistoryDiffError != "":
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, a.fileHistoryDiffError).TextColor(theme.Danger)
				if ui.Button(c, tr("Try again")).Clicked() &&
					a.fileHistorySelected >= 0 && a.fileHistorySelected < len(a.fileHistoryCommits) &&
					a.fileHistorySelectedFile >= 0 && a.fileHistorySelectedFile < len(a.fileHistoryFiles) {
					a.loadFileHistoryDiff(
						a.fileHistoryCommits[a.fileHistorySelected],
						a.fileHistoryFiles[a.fileHistorySelectedFile], true)
				}
			})
		case a.fileHistoryDiffState == nil && a.fileHistoryDiffMedia != nil:
			mediaReviewView(c, a.fileHistoryDiffMedia, a.fileHistoryDiffPath)
		case a.fileHistoryDiffState == nil:
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Choose a file to see its diff.")).TextColor(theme.TextMuted)
			})
		default:
			a.keyboardDiffViewer(c, a.fileHistoryDiffState)
		}
	})
}

// fileHistorySubtitle names the commit whose file the drawer is showing, for
// the full-window diff's header.
func fileHistorySubtitle(commits []repository.Commit, hash string) string {
	for _, commit := range commits {
		if commit.Hash != hash {
			continue
		}
		subject := commit.ShortHash
		if commit.Subject != "" {
			subject += " · " + commit.Subject
		}
		return subject
	}
	return ""
}

// diffFileStyle colors a changed file's status letter, as the commit detail
// panes do.
func diffFileStyle(theme *ui.Theme, status string) ui.Color {
	switch {
	case status == "":
		return theme.TextMuted
	case status[0] == 'A':
		return theme.Success
	case status[0] == 'D':
		return theme.Danger
	case status[0] == 'R' || status[0] == 'C':
		return theme.Accent
	default:
		return theme.Warning
	}
}
