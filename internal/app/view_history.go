package app

import (
	"strings"

	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

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
	a.askConfirm(confirmRequest{
		title:        "Abort Operation",
		message:      trf("Abort the current %s?", operation),
		detail:       "Git will restore the repository to its state before this operation began.",
		confirmLabel: "Abort",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Aborted the %s", operation), "",
				func(ctx context.Context, session repository.Session) error {
					return session.AbortOperation(ctx)
				}, nil)
		},
	})
}

// resetCommitDetails drops what the commit detail pane read, so the next
// frame reads the chosen commit again after an action changed it.
func (a *app) resetCommitDetails() {
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
	a.commitFilesHash = commit.Hash
	a.commitFilesLoading = true
	a.commitFilesError = ""
	a.commitFiles = nil
	a.selectedCommitFileRow = -1
	a.selectedCommitFile = -1
	a.commitDiffHash = ""
	a.commitDiffPath = ""
	a.commitDiffPatch = ""
	a.commitDiffLoading = false
	a.commitDiffError = ""
	a.commitDiffState = nil
	a.commitDiffMedia = nil

	go func() {
		defer a.recoverBackground()
		files, err := session.CommitFiles(context.Background(), commit)
		a.update(func() {
			if repositoryGeneration != a.generation {
				// An action rewrote the repository: drop the request so
				// the next frame reads the commit again.
				if a.commitFilesHash == commit.Hash {
					a.commitFilesHash = ""
					a.commitFilesLoading = false
				}
				return
			}
			if a.commitFilesHash != commit.Hash {
				return
			}
			a.commitFilesLoading = false
			if err != nil {
				a.commitFilesError = safeError(err)
				return
			}
			a.commitFiles = files
			if len(files) == 0 {
				a.selectedCommitFileRow = -1
				a.selectedCommitFile = -1
				return
			}
			a.selectedCommitFile = 0
		})
	}()
}

// requestCommitFileDiff reads the diff of the file chosen in commit, unless
// it is already the one the pane shows.
func (a *app) requestCommitFileDiff(commit repository.Commit, file repository.CommitFile) {
	if a.session == nil || (a.commitDiffHash == commit.Hash && a.commitDiffPath == file.Path &&
		a.commitDiffOptions == a.diffOptions) {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	a.stopLineSelection()
	a.commitDiffHash = commit.Hash
	a.commitDiffPath = file.Path
	a.commitDiffOptions = a.diffOptions
	a.commitDiffLoading = true
	a.commitDiffError = ""
	a.commitDiffPatch = ""
	a.commitDiffState = nil
	a.commitDiffMedia = nil

	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
	customPatchSelection := isCommitSection(a.workspaceTab)
	go func() {
		defer a.recoverBackground()
		diff, err := session.CommitFileDiff(context.Background(), commit, file, options)
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
				media, err = loadMediaReview(context.Background(), session, repository.MediaRequest{
					Source: repository.MediaCommit, Commit: commit, CommitFile: file,
				})
			}
		}
		a.update(func() {
			if repositoryGeneration != a.generation {
				if a.commitDiffHash == commit.Hash && a.commitDiffPath == file.Path &&
					a.commitDiffOptions == options {
					a.commitDiffHash = ""
					a.commitDiffPath = ""
					a.commitDiffPatch = ""
					a.commitDiffLoading = false
				}
				return
			}
			if a.commitDiffHash != commit.Hash || a.commitDiffPath != file.Path ||
				a.commitDiffOptions != options {
				return
			}
			a.commitDiffLoading = false
			if err != nil {
				a.commitDiffError = safeError(err)
				return
			}
			a.commitDiffPatch = diff.Patch
			a.commitDiffState = state
			a.commitDiffMedia = media
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
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
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
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
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
				a.keyboardList(c, &a.commitList, len(commits)).Grow(1).Rows(func(row ui.ListRow) {
					commit := commits[row.Index]
					services := row.Context.Services()
					rowElement := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(7, 8)
					rowElement.Children(func() {
						a.commitSelectionCheckbox(row.Context, commit)
						authorAvatar(row.Context, commit.AuthorName, commit.AuthorEmail, 18)
						ui.Column(row.Context).Gap(2).Grow(1).Children(func() {
							ui.Text(row.Context, commit.Subject).SingleLine().Grow(1)
							ui.Row(row.Context).Gap(8).Children(func() {
								ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row)).SingleLine()
								ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row)).SingleLine()
								if rel := relTime(commit.UnixTimestamp); rel != "" {
									ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row)).SingleLine()
								}
							})
						})
					}).PopoverMenu(func(m *ui.Menu) {
						a.selectedCommit = row.Index
						a.commitRowMenu(m, commit, services)
					})
				})
				if a.commitList.AtEnd() {
					a.requestMoreCommits()
				}
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
			a.commitSelectionModeButton(c)
			commitFilterButton(c, "Search and filter the commit list", filtered, func() {
				a.openCommitFilter(sectionHistory)
			})
			if filtered {
				clearFilterButton(c, "Clear every filter and show all commits", func() {
					a.clearCommitFilter(sectionHistory)
				})
			}
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
	query := a.commitQueryFor(target)
	filtered := query.narrows()
	switch {
	case filtered && query.loading:
		ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Searching commits"))
			ui.Text(c, tr("Searching commits…")).TextColor(theme.TextMuted)
		})
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
	// A directory has no diff. Dropping the request identity also rejects a
	// slower file read that was in flight when the directory was chosen.
	a.commitDiffHash = ""
	a.commitDiffPath = ""
	a.commitDiffPatch = ""
	a.commitDiffState = nil
	a.commitDiffMedia = nil
	a.commitDiffLoading = false
	a.commitDiffError = ""
}

func (a *app) commitDetailCard(c *ui.Context, commit *repository.Commit, emptyLabel string,
	actionIcons func(*ui.Context, repository.Commit)) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
		if commit == nil {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr(emptyLabel)).TextColor(theme.TextMuted)
			})
			return
		}
		ui.Text(c, commit.Subject).FontSize(18).Bold().SingleLine().Tooltip(commit.Subject)
		if actionIcons != nil {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Wrap().Children(func() { actionIcons(c, *commit) })
		}
		a.diffViewOptions(c)
		a.customPatchBar(c)
		copyLabel := "Copy commit hash"
		if a.workspaceTab == sectionReflog {
			copyLabel = "Copy reflog commit hash"
		}
		a.commitHashRow(c, *commit, copyLabel)
		if commit.AuthorName != "" || commit.AuthorEmail != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				authorAvatar(c, commit.AuthorName, commit.AuthorEmail, 24)
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
		a.requestCommitDetails(*commit)
		if a.commitFilesLoading {
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading commit details"))
				ui.Text(c, tr("Loading commit details…")).TextColor(theme.TextMuted)
			})
			return
		}
		if a.commitFilesError != "" {
			ui.Text(c, a.commitFilesError).TextColor(theme.Danger)
		}
		if a.commitFiles == nil {
			ui.Column(c).Grow(1).Center().Children(func() {
				if ui.Button(c, tr("Load changed files")).Clicked() {
					// Drop the failed read's key so this click reads it
					// again.
					a.commitFilesHash = ""
					a.requestCommitDetails(*commit)
				}
			})
			return
		}
		paths := commitFilePaths(a.commitFiles)
		a.commitFileRows = a.syncFileTreeRows(fileTreeCommitScope, paths, a.commitFileRows,
			&a.selectedCommitFileRow, &a.selectedCommitFile)
		// The files and the diff share the rest of the pane, and the line
		// between them is dragged to give one more room than the other.
		paneSplitVertical(c, &a.commitFilesHeight, func() {
			ui.Column(c).Fill().Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Icon(c, folderIcon).FontSize(13).TextColor(theme.Accent)
					ui.Text(c, tr("Commit files")).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d", len(a.commitFiles)))
					a.fileTreeButtons(c, fileTreeCommitScope, paths)
				})
				list := a.keyboardList(c, &a.commitFileList, len(a.commitFileRows)).Grow(1).Rows(func(row ui.ListRow) {
					item := a.commitFileRows[row.Index]
					if item.isDirectory() {
						a.fileTreeDirectoryRow(row.Context, fileTreeCommitScope, item, row.Selected(), func() {
							a.selectCommitFileRow(row.Index)
						})
						return
					}
					file := a.commitFiles[item.source]
					services := row.Context.Services()
					ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8, 6, float32(8+16*item.depth)).Children(func() {
						fileTypeMark(row.Context, file.Path, 13, row.Selected())
						ui.Text(row.Context, item.name).Grow(1).SingleLine().Tooltip(file.Path)
						fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
						statusSlot(row.Context, statusLetter(file.Status),
							rowStatusColor(theme, row, theme.Warning))
					}).PopoverMenu(func(m *ui.Menu) {
						a.commitFileMenu(m, commit, file, services)
					})
				})
				if a.keyboardListChanged(&a.commitFileList, list) {
					a.selectCommitFileRow(a.selectedCommitFileRow)
				}
				normalizeFileTreeRow(a.commitFileRows, &a.selectedCommitFileRow, &a.selectedCommitFile)
			})
		}, func() {
			ui.Column(c).Fill().Children(func() {
				if a.selectedCommitFile >= 0 && a.selectedCommitFile < len(a.commitFiles) {
					a.requestCommitFileDiff(*commit, a.commitFiles[a.selectedCommitFile])
				}
				switch {
				case a.commitDiffLoading:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Label(tr("Loading diff"))
						ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
					})
				case a.commitDiffError != "":
					ui.Text(c, a.commitDiffError).TextColor(theme.Danger)
				case a.commitDiffState != nil:
					a.customPatchSelectionControls(c)
					a.keyboardDiffViewer(c, a.commitDiffState)
				default:
					noTextualDiffOrMedia(c, a.commitDiffMedia, a.commitDiffPath)
				}
			})
		}).Grow(1)
	})
}
