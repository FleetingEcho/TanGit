package app

import (
	"context"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// fileHistoryPage is how many commits the drawer reads at a time: it shows
// the first page, and reads another as the list scrolls to the end, so a long
// file history keeps paging in rather than stopping at a fixed count.
const fileHistoryPage = 100

// commitFileMenu adds the actions of a changed file to m: read or restore its
// history, and copy either of its paths.
func (a *app) commitFileMenu(m *ui.Menu, commit *repository.Commit, file repository.CommitFile, services ui.Services) {
	if m.Item(tr("View file changes…")).Chosen() {
		a.openFileHistory(file.Path)
	}
	if commit != nil && m.Item(tr("Restore this historical file…")).Shortcut(ui.Shift, ui.KeyR).
		Disabled(a.actionBusy || a.session == nil).Chosen() {
		a.confirmRestoreCommitFile(*commit, file)
	}
	m.Separator()
	a.copyPathItems(m, services, file.Path)
}

// openFileHistory opens the drawer that lists the commits that changed path,
// read from the repository rather than from the commits on screen.
func (a *app) openFileHistory(path string) {
	a.fileHistoryOpen = true
	a.fileHistoryPath = path
	a.fileHistoryCommits = nil
	a.fileHistorySelected = 0
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
	go func() {
		defer a.recoverBackground()
		commits, err := session.FileCommits(context.Background(), path, fileHistoryPage, 0)
		a.update(func() {
			if token != a.fileHistoryToken || !a.fileHistoryOpen || a.fileHistoryPath != path {
				return
			}
			a.fileHistoryLoading = false
			if err != nil {
				a.fileHistoryMore = false
				a.fileHistoryError = safeError(err)
				return
			}
			a.fileHistoryCommits = commits
			a.fileHistoryRead = len(commits)
			a.fileHistoryMore = len(commits) == fileHistoryPage
			if len(commits) == 0 {
				return
			}
			a.fileHistorySelected = 0
			a.loadFileHistoryCommit(commits[0], true)
		})
	}()
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
	skip := a.fileHistoryRead
	token := a.fileHistoryToken
	a.fileHistoryLoadingMore = true
	go func() {
		defer a.recoverBackground()
		commits, err := session.FileCommits(context.Background(), path, fileHistoryPage, skip)
		a.update(func() {
			if token != a.fileHistoryToken || !a.fileHistoryOpen || a.fileHistoryPath != path {
				return
			}
			a.fileHistoryLoadingMore = false
			if err != nil {
				a.fileHistoryMoreError = safeError(err)
				return
			}
			a.fileHistoryRead += len(commits)
			// A commit can repeat across pages when the history shifted
			// between reads; the list keys rows by hash, so keep each
			// commit once.
			seen := make(map[string]bool, len(a.fileHistoryCommits)+len(commits))
			for _, commit := range a.fileHistoryCommits {
				seen[commit.Hash] = true
			}
			for _, commit := range commits {
				if seen[commit.Hash] {
					continue
				}
				seen[commit.Hash] = true
				a.fileHistoryCommits = append(a.fileHistoryCommits, commit)
			}
			a.fileHistoryMore = len(commits) == fileHistoryPage
		})
	}()
}

// closeFileHistory hides the drawer and drops every read it had in flight.
func (a *app) closeFileHistory() {
	a.fileHistoryOpen = false
	a.fileHistoryToken++
	a.fileHistoryPath = ""
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
	go func() {
		defer a.recoverBackground()
		files, err := session.CommitFiles(context.Background(), commit)
		a.update(func() {
			if token != a.fileHistoryFilesToken || !a.fileHistoryOpen || a.fileHistoryPath != path {
				return
			}
			a.fileHistoryFilesLoading = false
			if err != nil {
				a.fileHistoryFilesError = safeError(err)
				return
			}
			a.fileHistoryFiles = files
			if len(files) == 0 {
				return
			}
			choice := 0
			for i, file := range files {
				if file.Path == path || file.PreviousPath == path {
					choice = i
					break
				}
			}
			a.fileHistorySelectedFile = choice
			a.loadFileHistoryDiff(commit, files[choice], true)
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
	go func() {
		defer a.recoverBackground()
		diff, err := session.CommitFileDiff(context.Background(), commit, file, options)
		var state *editor.DiffState
		var media *mediaReview
		if err == nil {
			state, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
			if err == nil && state == nil && isMediaPath(file.Path) {
				media, err = loadMediaReview(context.Background(), session, repository.MediaRequest{
					Source: repository.MediaCommit, Commit: commit, CommitFile: file,
				})
			}
		}
		a.update(func() {
			if token != a.fileHistoryDiffToken || !a.fileHistoryOpen {
				return
			}
			a.fileHistoryDiffLoading = false
			if err != nil {
				a.fileHistoryDiffError = safeError(err)
				return
			}
			a.fileHistoryDiffPatch = diff.Patch
			a.fileHistoryDiffState = state
			a.fileHistoryDiffMedia = media
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
			a.fileHistoryShown = 100 - a.fileHistoryWidth
			paneSplitPercent(c, &a.fileHistoryShown, func() {
				ui.Box(c).Fill().Tooltip(tr("Close the file history")).
					OnClick(a.closeFileHistory)
			}, func() {
				panel := ui.Column(c).FillHeight().Padding(islandGap / 2).Gap(islandGap).
					Background(islandBackdrop(theme)).
					Role(ui.RoleDialog).Label(tr("File history")).
					Children(func() {
						a.fileHistoryPanel(c)
					})
				_ = panel
			}).Fill()
			if a.fileHistoryShown != 100-a.fileHistoryWidth {
				a.fileHistoryWidth = 100 - a.fileHistoryShown
				if a.fileHistoryWidth < 30 {
					a.fileHistoryWidth = 30
				}
				if a.fileHistoryWidth > 96 {
					a.fileHistoryWidth = 96
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
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			fileTypeMark(c, a.fileHistoryPath, 13, false)
			ui.Text(c, a.fileHistoryPath).TextColor(theme.TextMuted).SingleLine().
				Tooltip(a.fileHistoryPath).Selectable()
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
			paneCollapseButton(c, &a.fileHistoryCommitsCollapsed, "Collapse file history commits")
		})
		switch {
		case a.fileHistoryLoading:
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading file history"))
				ui.Text(c, tr("Reading the file's history…")).TextColor(theme.TextMuted)
			})
		case a.fileHistoryError != "":
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, a.fileHistoryError).TextColor(theme.Danger)
				if ui.Button(c, tr("Try again")).Clicked() {
					a.openFileHistory(a.fileHistoryPath)
				}
			})
		case len(a.fileHistoryCommits) == 0:
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("No commits changed this file.")).TextColor(theme.TextMuted)
			})
		default:
			list := a.keyboardList(c, &a.fileHistoryList, len(a.fileHistoryCommits)).Grow(1).Rows(func(row ui.ListRow) {
				commit := a.fileHistoryCommits[row.Index]
				services := row.Context.Services()
				ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8).Children(func() {
					authorAvatar(row.Context, commit.AuthorName, commit.AuthorEmail, 18)
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
				}).OnClick(func() {
					a.fileHistorySelected = row.Index
					a.loadFileHistoryCommit(commit, false)
				}).PopoverMenu(func(m *ui.Menu) {
					a.commitRowMenu(m, commit, services)
				})
			})
			if a.keyboardListChanged(&a.fileHistoryList, list) && a.fileHistorySelected >= 0 && a.fileHistorySelected < len(a.fileHistoryCommits) {
				a.loadFileHistoryCommit(a.fileHistoryCommits[a.fileHistorySelected], false)
			}
			// Scrolling to the last commit reads the page below it, so the
			// history pages in as the list is scrolled rather than ending
			// at the first read.
			if a.fileHistoryList.AtEnd() {
				a.loadMoreFileHistory()
			}
			switch {
			case a.fileHistoryLoadingMore:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label(tr("Loading older commits"))
					ui.Text(c, tr("Loading older commits…")).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 1)
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
			paneCollapseButton(c, &a.fileHistoryFilesCollapsed, "Collapse changed files")
		})
		switch {
		case a.fileHistoryFilesLoading:
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading commit files"))
				ui.Text(c, tr("Reading the commit's files…")).TextColor(theme.TextMuted)
			})
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
			list := a.keyboardList(c, &a.fileHistoryFileList, len(a.fileHistoryFiles)).Grow(1).Rows(func(row ui.ListRow) {
				file := a.fileHistoryFiles[row.Index]
				style := diffFileStyle(theme, file.Status)
				ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8).Children(func() {
					fileTypeMark(row.Context, file.Path, 13, row.Selected())
					ui.Text(row.Context, file.Path).Grow(1).SingleLine().Tooltip(file.Path)
					if file.PreviousPath != "" {
						ui.Text(row.Context, "← "+file.PreviousPath).
							TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1).SingleLine()
					}
					fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
					statusSlot(row.Context, statusLetter(file.Status), rowStatusColor(theme, row, style))
				}).OnClick(func() {
					a.fileHistorySelectedFile = row.Index
					a.loadFileHistoryDiff(commit, file, false)
				}).PopoverMenu(func(m *ui.Menu) {
					a.fileHistorySelectedFile = row.Index
					a.commitFileMenu(m, &commit, file, row.Context.Services())
				})
			})
			if a.keyboardListChanged(&a.fileHistoryFileList, list) && a.fileHistorySelectedFile >= 0 && a.fileHistorySelectedFile < len(a.fileHistoryFiles) {
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
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading file diff"))
				ui.Text(c, tr("Reading the file's diff…")).TextColor(theme.TextMuted)
			})
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
