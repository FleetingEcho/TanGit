package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// beginCreateBranch creates the branch the modal names from the branch it
// chose, and checks it out. The modal itself is the confirmation: it names
// both the branch and its start point before the button creates it.
func (a *app) beginCreateBranch() {
	name := strings.TrimSpace(a.newBranchName)
	if name == "" || a.actionBusy {
		return
	}
	base := strings.TrimSpace(a.newBranchBase)
	if base == "" {
		base = "HEAD"
	}
	a.beginRepositoryActionMessage(trf("Created branch %s", name), "",
		func(ctx context.Context, session repository.Session) error {
			return requireBranchRemoteSession(session).CreateBranch(ctx, name, base)
		}, func() {
			a.newBranchName = ""
			a.newBranchBase = ""
			a.showBranchForm = false
		})
}

// openBranchForm opens the modal that creates a branch from a local branch,
// starting from base, or from the checked out branch when base is empty.
func (a *app) openBranchForm(base string) {
	if a.actionBusy {
		return
	}
	a.newBranchName = ""
	if base == "" {
		base = a.snapshot.Branch
	}
	if base == "" {
		base = "HEAD"
	}
	a.newBranchBase = base
	a.showBranchForm = true
}

// branchNames lists the local branches the form starts from.
func (a *app) branchNames() []string {
	names := make([]string, 0, len(a.snapshot.Branches))
	for _, branch := range a.snapshot.Branches {
		names = append(names, branch.Name)
	}
	return names
}

// branchFormModal creates a branch from any local branch: the name and the
// branch it starts from are chosen together, and the new branch is checked
// out. Escape and a press outside close it.
func (a *app) branchFormModal(c *ui.Context) {
	if !a.showBranchForm {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(500).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Create Branch")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, branchIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr("Create Branch")).FontSize(17).Bold()
							ui.Text(c, tr("Start a new branch from a local branch.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close branch form", "Escape", closeIcon, false,
							func() { a.showBranchForm = false })
					})
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, tr("Branch name")).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
						input := ui.TextInput(c, &a.newBranchName).
							Label(tr("Branch name")).
							Placeholder(tr("feature/my-branch")).
							AutoFocus()
						if input.Submitted() && strings.TrimSpace(a.newBranchName) != "" {
							a.beginCreateBranch()
						}
					})
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, tr("From branch")).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
						names := a.branchNames()
						if a.newBranchBase != "" && !slices.Contains(names, a.newBranchBase) {
							names = append([]string{a.newBranchBase}, names...)
						}
						ui.Select(c, &a.newBranchBase, names).Grow(1).
							Label(tr("Base branch"))
					})
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Text(c, tr("The new branch starts at this branch and is checked out.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
						if ui.Button(c, tr("Cancel")).Clicked() {
							a.showBranchForm = false
						}
						if ui.PrimaryButton(c, tr("Create branch")).
							Disabled(a.actionBusy || strings.TrimSpace(a.newBranchName) == "").
							Clicked() {
							a.beginCreateBranch()
						}
					})
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showBranchForm = false
			}
		})
	})
}

func (a *app) beginRenameBranch() {
	if a.selectedBranch < 0 || a.selectedBranch >= len(a.snapshot.Branches) {
		return
	}
	oldName := a.snapshot.Branches[a.selectedBranch].Name
	newName := strings.TrimSpace(a.renameBranch)
	a.beginRepositoryActionMessage(trf("Renamed %s to %s", oldName, newName), "",
		func(ctx context.Context, session repository.Session) error {
			return requireBranchRemoteSession(session).RenameBranch(ctx, oldName, newName)
		}, func() {
			a.selectedBranch = selectedBranchIndex(a.snapshot.Branches, newName)
			a.syncBranchRow()
			a.renameBranch = ""
			a.showRenameForm = false
		})
}

func (a *app) confirmMergeSelectedBranch() {
	if a.selectedBranch < 0 || a.selectedBranch >= len(a.snapshot.Branches) {
		return
	}
	a.confirmMergeBranch(a.snapshot.Branches[a.selectedBranch])
}

func (a *app) confirmMergeBranch(branch repository.Branch) {
	if a.actionBusy || a.session == nil {
		return
	}
	currentBranch := a.snapshot.Branch
	a.askConfirm(confirmRequest{
		title:        "Merge Branch",
		message:      trf("Merge %s into %s?", branchDisplay(branch), currentBranch),
		detail:       "Git will create a merge commit when a fast-forward is not possible.",
		confirmLabel: "Merge",
		run: func() {
			a.beginRepositoryActionMessage(trf("Merged %s into %s", branchDisplay(branch), currentBranch), "",
				func(ctx context.Context, session repository.Session) error {
					return requireBranchRemoteSession(session).MergeBranch(ctx, branch.Name)
				}, nil)
		},
	})
}

func (a *app) confirmDeleteSelectedBranch() {
	if a.selectedBranch < 0 || a.selectedBranch >= len(a.snapshot.Branches) {
		return
	}
	a.confirmDeleteBranch(a.snapshot.Branches[a.selectedBranch])
}

func (a *app) confirmDeleteBranch(branch repository.Branch) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Branch",
		message:      trf("Delete branch %s?", branchDisplay(branch)),
		detail:       "The branch must already be merged. This operation cannot be undone.",
		confirmLabel: "Delete",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Deleted branch %s", branchDisplay(branch)), "",
				func(ctx context.Context, session repository.Session) error {
					return requireBranchRemoteSession(session).DeleteBranch(ctx, branch.Name, false)
				}, nil)
		},
	})
}

func (a *app) checkoutBranch(name string) {
	a.confirmAttention("Checkout", name, "This changes the checked-out branch and working-tree files. Uncommitted changes may be carried across or prevent checkout.", func() {
		a.beginRepositoryActionMessage(trf("Checked out %s", name), "",
			func(ctx context.Context, session repository.Session) error {
				return requireBranchRemoteSession(session).CheckoutBranch(ctx, name)
			}, nil)
	})
}

// beginPullBranch brings a branch's latest upstream changes in: the checked
// out branch pulls, while another branch takes the commits its upstream has
// gained without leaving the working tree alone.
func (a *app) beginPullBranch(branch repository.Branch) {
	message := trf("Pulled latest %s", branchDisplay(branch))
	if branch.Head {
		message = trf("Pulled latest into %s", branchDisplay(branch))
	}
	a.confirmAttention("Pull", branch.Name, "This integrates upstream changes and may change branch history or working-tree files. Conflicts may require resolution.", func() {
		a.beginRepositoryActionMessage(message, "",
			func(ctx context.Context, session repository.Session) error {
				return requireBranchRemoteSession(session).PullBranch(ctx, branch.Name)
			}, nil)
	})
}

// beginFastForwardBranch moves a branch to its upstream when only new commits
// are in the way, as Lazygit's fast-forward does.
func (a *app) beginFastForwardBranch(branch repository.Branch) {
	a.confirmAttention("Fast-forward", branch.Name, "This integrates upstream changes and may change branch history or working-tree files. Conflicts may require resolution.", func() {
		a.beginRepositoryActionMessage(trf("Fast-forwarded %s", branchDisplay(branch)), "",
			func(ctx context.Context, session repository.Session) error {
				return requireBranchRemoteSession(session).FastForwardBranch(ctx, branch.Name)
			}, nil)
	})
}

func (a *app) beginUnsetUpstream(branch repository.Branch) {
	if a.actionBusy || a.session == nil || branch.UpstreamRemote == "" {
		return
	}
	a.confirmAttention("Unset upstream", branch.Name, "This changes branch tracking configuration and affects future pulls and pushes.", func() {
		a.beginRepositoryActionMessage(trf("%s no longer tracks %s/%s", branch.Name, branch.UpstreamRemote, branch.UpstreamBranch), "",
			func(ctx context.Context, session repository.Session) error {
				return requireBranchRemoteSession(session).UnsetUpstream(ctx, branch.Name)
			}, nil)
	})
}

func (a *app) unsetSelectedUpstream() {
	if a.selectedBranch >= 0 && a.selectedBranch < len(a.snapshot.Branches) {
		a.beginUnsetUpstream(a.snapshot.Branches[a.selectedBranch])
	}
}

// confirmRebaseOntoBranch asks before replaying the current branch onto the
// chosen one, because the rebase rewrites the commits the branch has that the
// chosen one lacks.
func (a *app) confirmRebaseOntoBranch(branch repository.Branch) {
	if a.actionBusy || a.session == nil {
		return
	}
	currentBranch := a.snapshot.Branch
	detail := trf("The commits %s has that %s lacks are replayed on top of it, with new hashes.", currentBranch, branchDisplay(branch))
	if a.markedBaseCommit.Hash != "" {
		detail = trf("Only commits above marked base %s are replayed with git rebase --onto %s %s.", a.markedBaseCommit.Hash, branchDisplay(branch), a.markedBaseCommit.Hash)
	}
	a.askConfirm(confirmRequest{
		title:        "Rebase Branch",
		message:      trf("Rebase %s onto %s?", currentBranch, branchDisplay(branch)),
		detail:       detail,
		confirmLabel: "Rebase",
		run: func() {
			a.beginRepositoryActionMessage(trf("Rebased %s onto %s", currentBranch, branchDisplay(branch)), "",
				func(ctx context.Context, session repository.Session) error {
					return a.rebaseOntoBranch(ctx, session, branch.Name)
				}, a.clearMarkedRebaseBase)
		},
	})
}

// confirmForceCheckoutBranch asks before checking a branch out over work that
// would be thrown away, as Lazygit's force checkout does.
func (a *app) confirmForceCheckoutBranch(branch repository.Branch) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Force Checkout",
		message:      trf("Check out %s and discard what is in the way?", branchDisplay(branch)),
		detail:       "Uncommitted changes to files the two branches differ in are lost.",
		confirmLabel: "Force checkout",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Force checked out %s", branchDisplay(branch)), "",
				func(ctx context.Context, session repository.Session) error {
					return requireBranchRemoteSession(session).ForceCheckoutBranch(ctx, branch.Name)
				}, nil)
		},
	})
}

// branchRowMenu is the right-click menu for a local branch row. Actions
// address the clicked branch even when it is not the selected one.
// branchDisplay names a branch for the user: lazygit only fills DisplayName
// for a detached HEAD ("(HEAD detached at 123asdf)"), so a normal branch falls
// back to its name, as lazygit's own presentation does (gui/presentation).
func branchDisplay(branch repository.Branch) string {
	if branch.DisplayName != "" {
		return branch.DisplayName
	}
	return branch.Name
}

func (a *app) visibleBranches() []int {
	return filteredCached(&a.branchVisibleCache, a.snapshotSeq, len(a.snapshot.Branches), a.branchSearch, func(source int) string {
		branch := a.snapshot.Branches[source]
		return strings.Join([]string{branch.Name, branch.DisplayName, branch.Subject,
			branch.UpstreamRemote, branch.UpstreamBranch}, "\x00")
	})
}

func (a *app) branchAt(row int) (repository.Branch, bool) {
	return sourceRow(a.visibleBranches(), a.snapshot.Branches, row)
}

func (a *app) selectBranchRow(row int) {
	selectFilteredRow(a.visibleBranches(), row, &a.selectedBranchRow, &a.selectedBranch)
}

func (a *app) syncBranchRow() {
	syncFilteredRow(a.visibleBranches(), &a.selectedBranchRow, &a.selectedBranch)
}

func (a *app) branchRowMenu(m *ui.Menu, row ui.ListRow, services ui.Services) {
	branch, ok := a.branchAt(row.Index)
	if !ok {
		return
	}
	if branch.Head {
		if m.Item(tr("Push")).Disabled(a.actionBusy).Chosen() {
			a.confirmPush()
		}
		if m.Item(tr("Push with tags")).Disabled(a.actionBusy).Chosen() {
			a.confirmPushFollowTags()
		}
		m.Separator()
		if m.Item(tr("Create pull request…")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0).Chosen() {
			a.openCreatePullRequestForm()
		}
		if m.Item(tr("Open pull request in browser")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0).Chosen() {
			a.confirmOpenPullRequest()
		}
		if m.Item(tr("View pull request checks")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0).Chosen() {
			a.openPullRequestChecks()
		}

		if m.Item(tr("Force push with lease…")).Disabled(a.actionBusy).Chosen() {
			a.confirmForcePushWithLease()
		}
		if m.Item(tr("Move unpushed commits to new branch…")).Disabled(a.actionBusy).Chosen() {
			a.openMoveCommitsForm()
		}
	}
	if !branch.Head {
		if m.Item(tr("Checkout")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.checkoutBranch(branch.Name)
		}
		if m.Item(tr("Force checkout…")).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.confirmForceCheckoutBranch(branch)
		}
		if m.Item(tr("Merge into current…")).Shortcut(ui.Shift, ui.KeyM).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.confirmMergeBranch(branch)
		}
		if m.Item(tr("Rebase current onto this…")).Disabled(a.actionBusy || a.snapshot.Branch == "").Chosen() {
			a.selectBranchRow(row.Index)
			a.confirmRebaseOntoBranch(branch)
		}
	}
	if branch.UpstreamRemote != "" {
		m.Separator()
		if m.Item(tr("Pull latest")).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.beginPullBranch(branch)
		}
		if m.Item(tr("Fast-forward")).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.beginFastForwardBranch(branch)
		}
		if m.Item(tr("Unset upstream")).Shortcut(0, ui.KeyU).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.beginUnsetUpstream(branch)
		}
	}
	m.Separator()
	if m.Item(tr("Rename…")).Disabled(a.actionBusy).Chosen() {
		a.selectBranchRow(row.Index)
		a.renameBranch = branch.Name
		a.showRenameForm = true
	}
	if m.Item(tr("New branch here…")).Disabled(a.actionBusy).Chosen() {
		a.selectBranchRow(row.Index)
		a.openBranchForm(branch.Name)
	}
	m.Separator()
	if m.Item(tr("Copy branch name")).Chosen() {
		a.notifyCopied(services, "branch name", branch.Name)
	}
	if !branch.Head {
		m.Separator()
		if m.Item(tr("Delete branch…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.selectBranchRow(row.Index)
			a.confirmDeleteBranch(branch)
		}
	}
}

func (a *app) branchesView(c *ui.Context) {
	if a.branchFullscreen {
		ui.Box(c).Grow(1)
		return
	}
	ui.Column(c).Grow(1).Gap(10).Children(func() {
		// Three columns: the branches, the selected branch's commits, and the
		// commit chosen from them. The branch, which both columns are about,
		// has its header over the two of them.
		a.collapsibleContentPaneSplit(c, &a.branchListWidth, &a.workspacePaneCollapsed[sectionBranches],
			"Expand list pane", func() {
				a.branchListView(c)
			}, func() {
				a.branchPageView(c)
			})
	})
}

func (a *app) branchPageView(c *ui.Context) {
	islandCard(c, ui.Column(c).Grow(1).Padding(16)).Children(func() {
		if a.selectedBranch < 0 || a.selectedBranch >= len(a.snapshot.Branches) {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Select a branch")).TextColor(c.Theme().TextMuted)
			})
			return
		}
		branch := a.snapshot.Branches[a.selectedBranch]
		paneSplitVertical(c, &a.branchHeaderHeight, func() {
			ui.Scroll(c).Fill().Label(tr("Branch summary")).Children(func() {
				ui.Column(c).FillWidth().Gap(10).Padding(0, 12, 8, 0).Children(func() {
					a.branchHeaderView(c, branch)
				})
			})
		}, func() {
			ui.Column(c).Fill().Padding(10, 0, 0, 0).Children(func() {
				collapsiblePaneSplitLane(c, &a.branchCommitsWidth, &a.branchCommitsCollapsed,
					"Expand commit list", func() {
						a.branchCommitsView(c, branch)
					}, func() {
						a.branchCommitDetailView(c, branch)
					})
			})
		}).Fill()
	})
}

func (a *app) branchFullscreenView(c *ui.Context, topInset float32) {
	if !a.branchFullscreen {
		return
	}
	if a.state != stateReady || a.workspaceTab != sectionBranches || a.selectedBranch < 0 || a.selectedBranch >= len(a.snapshot.Branches) {
		a.branchFullscreen = false
		return
	}
	ui.Overlay(c, func() {
		panel := ui.Column(c).Absolute().Left(0).Top(topInset).Right(0).Bottom(0).
			Padding(islandGap / 2).Background(islandBackdrop(c.Theme())).Modal()
		a.registerTerminalModalShortcuts(panel)
		if !a.settings.DisableShortcuts {
			panel.OnShortcut(0, ui.KeyF6, func() { a.cycleKeyboardPane(c, 1) })
			panel.OnShortcut(ui.Shift, ui.KeyF6, func() { a.cycleKeyboardPane(c, -1) })
		}
		panel.Children(func() {
			a.branchPageView(c)
		})
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			a.escapeBranchFullscreen()
		}
	})
}

func (a *app) closeBranchFullscreen() { a.branchFullscreen = false }

func (a *app) escapeBranchFullscreen() {
	if a.showRenameForm {
		a.showRenameForm = false
	} else {
		a.closeBranchFullscreen()
	}
}

// branchListView is the first column: the local branches. It takes the
// keyboard as the section opens, so the arrows choose a branch and the other
// columns follow.
func (a *app) branchListView(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Icon(c, branchesIcon).FontSize(14).TextColor(theme.Accent)
			ui.Text(c, tr("Local branches")).Bold().Grow(1)
			if stale := a.staleGoneBranches(); len(stale) > 0 {
				flatIconButton(c, trf("Delete %d stale branch%s", len(stale), pluralS(len(stale))),
					tr("Delete branches whose upstream is gone"), trashIcon, 14, 6,
					a.actionBusy, a.confirmDeleteStaleBranches)
			}
			rowIconButton(c, "New branch…", "Create a branch from a local branch",
				stageIcon, func() { a.openBranchForm("") })
			flatIconButton(c, tr("Sync stack…"), tr("Preview frozen per-branch updates and preview-OID-leased publishes"), fetchIcon, 14, 6,
				a.actionBusy || len(a.snapshot.Branches) == 0, a.openStackSync)
			paneCollapseButton(c, &a.workspacePaneCollapsed[sectionBranches], "Collapse list pane")
		})
		if len(a.snapshot.Branches) == 0 {
			ui.Text(c, tr("No branches")).TextColor(theme.TextMuted)
			return
		}
		visible := a.visibleBranches()
		listSearchField(c, &a.sectionSearch[sectionBranches], &a.branchSearch,
			"Search local branches", "Filter by name, upstream, or commit", "Clear branch search",
			len(visible), len(a.snapshot.Branches), func() {
				a.selectBranchRow(0)
				a.resetBranchCommits()
				a.showRenameForm = false
			}, a.navigateBranchSearch)
		// Changed applies the pending text, so rows captured before the input
		// may belong to its previous query. Re-read them before drawing.
		visible = a.visibleBranches()
		if len(visible) == 0 {
			listSearchEmpty(c, "No local branches match this search")
			return
		}
		list := a.keyboardList(c, &a.branchList, len(visible)).Grow(1).AutoFocus().Rows(func(row ui.ListRow) {
			branch, _ := sourceRow(visible, a.snapshot.Branches, row.Index)
			services := row.Context.Services()
			ui.Row(row.Context).Gap(8).Padding(7, 8).Children(func() {
				if branch.Head {
					// The checked-out branch's dot is green, as in the title bar; on
					// the selected row a light ring keeps it clear of the highlight.
					dot := ui.Box(row.Context).Size(10, 10).Radius(5).Shrink(0).Background(theme.Success)
					if row.Selected() {
						dot.Border(2, theme.AccentText.Alpha(0.85))
					}
					dot.Label(tr("Current branch"))
				}
				ui.Text(row.Context, branchDisplay(branch)).Grow(1).SingleLine()
				ahead, behind, known := aheadBehind(branch)
				if known {
					if n, show := count(ahead); show {
						aheadCountView(row.Context, n, branch, secondaryRowColor(theme, row), theme.FontSize-1)
					}
					if n, show := count(behind); show {
						behindCountView(row.Context, n, branch, secondaryRowColor(theme, row), theme.FontSize-1)
					}
				}
			}).PopoverMenu(func(m *ui.Menu) { a.branchRowMenu(m, row, services) })
		})
		if a.keyboardListChanged(&a.branchList, list) {
			a.selectBranchRow(a.selectedBranchRow)
			a.resetBranchCommits()
			a.showRenameForm = false
		}
		normalizeFilteredRow(visible, &a.selectedBranchRow, &a.selectedBranch)
	})
}

// branchHeaderView is what the branch is and what can be done to it: the
// actions, the rename form, and the upstream state.
func (a *app) branchHeaderView(c *ui.Context, branch repository.Branch) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, branchDisplay(branch)).FontSize(18).Bold().Grow(1).SingleLine().Tooltip(branchDisplay(branch))
		iconButton(c, "Rename…", "Rename…", editIcon, a.actionBusy, func() {
			a.showRenameForm = true
			a.renameBranch = branch.Name
		})
	})
	if !branch.Head {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			if branch.UpstreamRemote != "" {
				iconButton(c, "Pull latest", "Pull latest", pullIcon, a.actionBusy, func() {
					a.beginPullBranch(branch)
				})
			}
			iconButton(c, "Rebase onto this…", "Rebase onto this…", rebaseIcon, a.actionBusy, func() {
				a.confirmRebaseOntoBranch(branch)
			})
			iconButton(c, "Merge into current…", "Merge into current…", mergeIcon, a.actionBusy, func() {
				a.confirmMergeSelectedBranch()
			})
			primaryIconButton(c, "Checkout", "Checkout", branchIcon, a.actionBusy, func() {
				a.checkoutBranch(branch.Name)
			})
			iconButton(c, "Delete…", "Delete…", trashIcon, a.actionBusy, func() {
				a.confirmDeleteSelectedBranch()
			})
		})
	}

	if a.showRenameForm {
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c, &a.renameBranch).Label(tr("New branch name")).Grow(1)
			if ui.Button(c, tr("Cancel rename")).Disabled(a.actionBusy).Clicked() {
				a.showRenameForm = false
			}
			if ui.PrimaryButton(c, tr("Rename")).Disabled(a.actionBusy || strings.TrimSpace(a.renameBranch) == "").Clicked() {
				a.beginRenameBranch()
			}
		})
	}
	if branch.Head {
		ui.Row(c).Children(func() {
			ui.Badge(c, tr("Current"))
		})
	}
	if branch.UpstreamRemote != "" {
		upstream := branch.UpstreamRemote
		if branch.UpstreamBranch != "" {
			upstream += "/" + branch.UpstreamBranch
		}
		ui.Textf(c, tr("Tracks %s"), upstream)
		if ahead, behind, known := aheadBehind(branch); known {
			ui.Textf(c, tr("Ahead %s · Behind %s"), ahead, behind).TextColor(theme.TextMuted)
		} else if note := upstreamUnknown(branch); note != "" {
			ui.Text(c, note).TextColor(theme.TextMuted)
		}
	}
	if branch.Subject != "" {
		ui.Text(c, branch.Subject).TextColor(theme.TextMuted)
	}
}

// resetBranchCommits drops what the branch browse read, so the next frame
// reads the branch then selected again. An action changed the commits, and a
// read still in flight must not land on the new state.
func (a *app) resetBranchCommits() {
	a.branchCommitsKeep = ""
	a.branchCommitDetailMessage.reset()
	a.branchCommitsToken++
	a.branchCommitToken++
	a.branchCommits = nil
	a.branchCommitsBranch = ""
	a.branchCommitsLoading = false
	a.branchCommitsLoadingMore = false
	a.branchCommitsError = ""
	a.branchCommitsMoreError = ""
	a.branchCommitsMore = false
	a.branchCommitsLimit = 0
	a.selectedBranchCommit = -1
	a.branchCommitFiles = nil
	a.branchCommitFilesHash = ""
	a.selectedBranchCommitFileRow = -1
	a.selectedBranchCommitFile = -1
	a.branchCommitFilesLoading = false
	a.branchCommitFilesError = ""
	a.branchCommitDiffState = nil
	a.branchCommitDiffMedia = nil
	a.branchCommitDiffHash = ""
	a.branchCommitDiffPath = ""
	a.branchCommitDiffPatch = ""
	a.branchCommitDiffLoading = false
	a.branchCommitDiffError = ""
}

// branchCommitsKey names the read a branch browse holds open: the branch,
// and the filter narrowing it when one does. A different key means the
// column must read the branch again.
func (a *app) branchCommitsKey(branch string) string {
	if !commitFilterNarrows(a.branchFilter) {
		return branch
	}
	return branch + "\x00" + a.branchFilter.Query + "\x00" + a.branchFilter.Author
}

// requestBranchCommits reads the commits of branch when another branch's are
// on screen. It runs while the section draws, so it must never start a second
// read of the branch it already holds or is reading.
func (a *app) requestBranchCommits(branch string) {
	key := a.branchCommitsKey(branch)
	if branch == "" || a.session == nil || a.branchCommitsBranch == key {
		return
	}
	filter := a.branchFilter
	filter.Branch = branch
	filter.Limit = repository.SnapshotCommitLimit
	filtered := commitFilterNarrows(a.branchFilter)
	session := a.session
	repositoryGeneration := a.generation
	ctx := a.repositoryWork.Context()
	// A quiet refresh names the commit it keeps chosen; a user asking for
	// another branch leaves both empty, and the list starts at its top.
	keep := a.branchCommitsKeep
	if keep != "" {
		// Preserve the whole range already paged through on a quiet refresh,
		// rather than replacing it with the first page.
		filter.Limit = max(filter.Limit, a.branchCommitsLimit, len(a.branchCommits))
	}
	a.branchCommitsKeep = ""
	a.branchCommitsBranch = key
	a.branchCommitsLoading = true
	a.branchCommitsError = ""
	a.branchCommitsMoreError = ""
	a.branchCommitsMore = false
	a.branchCommitsLimit = filter.Limit
	a.branchCommitsLoadingMore = false
	a.branchCommitsToken++
	token := a.branchCommitsToken
	a.branchCommitDetailMessage.reset()
	if keep == "" {
		a.branchCommits = nil
		a.selectedBranchCommit = -1
	}
	a.branchCommitToken++
	a.branchCommitFiles = nil
	a.branchCommitFilesHash = ""
	a.selectedBranchCommitFileRow = -1
	a.selectedBranchCommitFile = -1
	a.branchCommitFilesLoading = false
	a.branchCommitFilesError = ""
	a.branchCommitDiffState = nil
	a.branchCommitDiffMedia = nil
	a.branchCommitDiffHash = ""
	a.branchCommitDiffPath = ""
	a.branchCommitDiffPatch = ""
	a.branchCommitDiffLoading = false
	a.branchCommitDiffError = ""

	go func() {
		defer a.recoverBackground()
		var commits []repository.Commit
		var err error
		if filtered {
			commits, err = requireCommitSearchSession(session).SearchCommits(ctx, filter)
		} else {
			commits, err = requireBranchCommitsSession(session).BranchCommits(ctx, branch, filter.Limit)
		}
		a.updateChanged(func() bool {
			if repositoryGeneration != a.generation || token != a.branchCommitsToken {
				return false
			}
			a.branchCommitsLoading = false
			if err != nil {
				a.branchCommitsError = safeError(err)
				a.notify("error", "Could not load branch commits", safeError(err))
				return a.workspaceTab == sectionBranches
			}
			a.branchCommits = commits
			a.branchCommitsMore = len(commits) >= filter.Limit
			// A quiet refresh keeps the commit the user was reading, by
			// hash: the list moved because the branch did, not because they
			// chose another commit.
			if keep != "" {
				a.selectedBranchCommit = selectedCommitIndex(commits, keep)
			}
			if a.selectedBranchCommit < 0 && len(commits) > 0 {
				a.selectedBranchCommit = 0
			}
			if a.workspaceTab == sectionBranches && keep == "" {
				// Only a newly chosen branch or filter set starts at its top.
				a.branchCommitList.ScrollTo(0, ui.Start)
			}
			return a.workspaceTab == sectionBranches
		})
	}()
}

// requestMoreBranchCommits increases the branch's query limit while keeping
// the selected commit and scroll position. Filtering has no Git offset, so the
// larger response replaces the prefix with one coherent ordered list.
func (a *app) requestMoreBranchCommits(branch string) {
	if a.workspaceTab != sectionBranches || a.session == nil || branch == "" ||
		a.branchCommitsLoading || a.branchCommitsLoadingMore || !a.branchCommitsMore {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	ctx := a.repositoryWork.Context()
	token := a.branchCommitsToken
	key := a.branchCommitsKey(branch)
	firstVisible, _ := a.branchCommitList.Visible()
	selectedHash := commitHashAt(a.branchCommits, a.selectedBranchCommit)
	limit := a.branchCommitsLimit + repository.SnapshotCommitLimit
	filter := a.branchFilter
	filter.Branch = branch
	filter.Limit = limit
	filtered := commitFilterNarrows(a.branchFilter)
	a.branchCommitsLoadingMore = true
	a.branchCommitsMoreError = ""

	go func() {
		defer a.recoverBackground()
		var commits []repository.Commit
		var err error
		if filtered {
			commits, err = requireCommitSearchSession(session).SearchCommits(ctx, filter)
		} else {
			commits, err = requireBranchCommitsSession(session).BranchCommits(ctx, branch, limit)
		}
		a.updateChanged(func() bool {
			if repositoryGeneration != a.generation || token != a.branchCommitsToken || key != a.branchCommitsKey(branch) {
				return false
			}
			a.branchCommitsLoadingMore = false
			if err != nil {
				a.branchCommitsMoreError = safeError(err)
				return a.workspaceTab == sectionBranches
			}
			a.branchCommits = commits
			a.branchCommitsLimit = limit
			a.branchCommitsMore = len(commits) >= limit
			if a.workspaceTab == sectionBranches && firstVisible >= 0 {
				a.branchCommitList.ScrollTo(firstVisible, ui.Start)
			}
			if selectedHash != "" {
				a.selectedBranchCommit = selectedCommitIndex(commits, selectedHash)
			}
			return a.workspaceTab == sectionBranches
		})
	}()
}

func (a *app) requestBranchCommitMessage(commit repository.Commit) {
	a.requestCommitMessage(&a.branchCommitDetailMessage, commit, func() bool {
		return a.workspaceTab == sectionBranches
	})
}

// requestBranchCommitFiles lists the files the chosen commit changed, unless
// the column already holds them. It is asked for while the column draws, so a
// read an action dropped starts again on the next frame instead of leaving the
// column loading forever. The files and the diff read the commit's own hashes,
// so neither touches the working tree.
func (a *app) requestBranchCommitFiles(commit repository.Commit) {
	if a.session == nil || a.branchCommitFilesHash == commit.Hash {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	ctx := a.repositoryWork.Context()
	// A quiet refresh keeps the file the user was reading within the commit.
	keepPath := a.branchCommitFilesKeepPath
	a.branchCommitFilesKeepPath = ""
	a.branchCommitToken++
	token := a.branchCommitToken
	a.branchCommitFilesHash = commit.Hash
	a.branchCommitFilesLoading = true
	a.branchCommitFilesError = ""
	// Keep the last painted files and diff while this branch's next commit
	// loads. The shared detail renderer shows progress beside that content.
	a.branchCommitDiffLoading = false
	a.branchCommitDiffError = ""

	go func() {
		defer a.recoverBackground()
		files, err := requireHistorySession(session).CommitFiles(ctx, commit)
		a.updateChanged(func() bool {
			if token != a.branchCommitToken {
				return false
			}
			if repositoryGeneration != a.generation {
				// An action rewrote the repository: drop the request so
				// the next frame reads the commit again.
				a.branchCommitFilesHash = ""
				a.branchCommitFilesLoading = false
				return a.workspaceTab == sectionBranches
			}
			if a.branchCommitFilesHash != commit.Hash {
				return false
			}
			a.branchCommitFilesLoading = false
			if err != nil {
				a.branchCommitFilesError = safeError(err)
				return a.workspaceTab == sectionBranches
			}
			a.branchCommitFiles = files
			if keepPath != "" {
				for i, file := range files {
					if file.Path == keepPath {
						a.selectedBranchCommitFile = i
						return a.workspaceTab == sectionBranches
					}
				}
			}
			if len(files) > 0 {
				a.selectedBranchCommitFile = 0
			}
			return a.workspaceTab == sectionBranches
		})
	}()
}

// requestBranchCommitFileDiff reads the diff of the file chosen in the chosen
// commit, unless it is already the one on screen.
func (a *app) requestBranchCommitFileDiff(commit repository.Commit, file repository.CommitFile) {
	if a.session == nil || (a.branchCommitDiffHash == commit.Hash && a.branchCommitDiffPath == file.Path) {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	ctx := a.repositoryWork.Context()
	a.branchCommitToken++
	token := a.branchCommitToken
	a.branchCommitDiffHash = commit.Hash
	a.branchCommitDiffPath = file.Path
	a.branchCommitDiffLoading = true
	a.branchCommitDiffError = ""

	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
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
			if token != a.branchCommitToken {
				return false
			}
			if repositoryGeneration != a.generation {
				a.branchCommitDiffHash = ""
				a.branchCommitDiffPath = ""
				a.branchCommitDiffPatch = ""
				a.branchCommitDiffLoading = false
				return a.workspaceTab == sectionBranches
			}
			if a.branchCommitDiffHash != commit.Hash || a.branchCommitDiffPath != file.Path {
				return false
			}
			a.branchCommitDiffLoading = false
			if err != nil {
				a.branchCommitDiffError = safeError(err)
				return a.workspaceTab == sectionBranches
			}
			a.branchCommitDiffPatch = diff.Patch
			a.branchCommitDiffState = state
			a.branchCommitDiffMedia = media
			return a.workspaceTab == sectionBranches
		})
	}()
}

// branchCommitsView lists the commits of the selected branch: reading another
// branch's history never checks it out.
func (a *app) branchCommitsView(c *ui.Context, branch repository.Branch) {
	theme := c.Theme()
	a.requestBranchCommits(branch.Name)
	filtered := a.branchFilterActive()
	ui.Column(c).Fill().Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 6, 0, 0).Children(func() {
			ui.Icon(c, branchIcon).FontSize(13).TextColor(theme.Accent)
			ui.Text(c, tr("Commits")).Bold().Grow(1)
			if a.branchCommitsLoading && len(a.branchCommits) > 0 {
				ui.Spinner(c).Label(tr("Loading commits"))
			} else if len(a.branchCommits) > 0 {
				ui.Badge(c, fmt.Sprintf("%d", len(a.branchCommits)))
			}
			if filtered {
				ui.Badge(c, commitFilterSummary(a.branchFilter))
			}
			commitFilterButton(c, "Search this branch's commits", filtered, func() {
				a.openCommitFilter(sectionBranches)
			})
			if filtered {
				clearFilterButton(c, "Clear every filter and show all commits", func() {
					a.clearCommitFilter(sectionBranches)
				})
			}
			a.commitRefreshButton(c)
			paneCollapseButton(c, &a.branchCommitsCollapsed, "Collapse commit list")
		})
		if a.branchCommitsError != "" {
			if ui.Button(c, tr("Load again")).Clicked() {
				// The next pass reads the branch afresh.
				a.branchCommitsBranch = ""
			}
			return
		}
		if a.branchCommitsLoading && len(a.branchCommits) == 0 {
			message := "Reading the branch's history…"
			if filtered {
				message = "Searching commits…"
			}
			loadingPane(c, "Loading commits", message)
			return
		}
		if len(a.branchCommits) == 0 {
			if filtered {
				ui.Column(c).Grow(1).Center().Gap(8).Padding(20).Children(func() {
					ui.Icon(c, filterIcon).FontSize(22).TextColor(theme.TextMuted)
					ui.Text(c, tr("No commits match the filter.")).Bold().TextAlign(ui.Center)
					if ui.Button(c, tr("Clear filters")).Clicked() {
						a.clearCommitFilter(sectionBranches)
					}
				})
				return
			}
			ui.Text(c, tr("No commits on this branch yet.")).TextColor(theme.TextMuted)
			return
		}
		a.keyboardList(c, &a.branchCommitList, len(a.branchCommits)).Grow(1).Rows(func(row ui.ListRow) {
			commit := a.branchCommits[row.Index]
			services := row.Context.Services()
			ui.Row(row.Context).Gap(8).AlignItems(ui.Center).
				Padding(6, 8+scrollbarGutter(theme), 6, 8).Children(func() {
				a.authorAvatar(row.Context, commit, 18)
				ui.Column(row.Context).Gap(2).Grow(1).Children(func() {
					ui.Text(row.Context, commit.Subject).SingleLine()
					ui.Row(row.Context).Gap(8).MinWidth(0).Children(func() {
						ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row)).
							SingleLine().MinWidth(0)
						ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row)).
							SingleLine().MinWidth(0).Tooltip(commit.AuthorName)
						if rel := relTime(commit.UnixTimestamp); rel != "" {
							ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row)).
								SingleLine().MinWidth(0).Tooltip(rel)
						}
					})
				})
				commitDiffStatBlocks(row.Context, commit.ShortHash, commit.LinesAdded, commit.LinesDeleted)
			}).PopoverMenu(func(m *ui.Menu) {
				// History edits move the checked-out branch, so offer those only
				// while this list shows it. Every other action uses commit directly,
				// including when browsing another branch without checking it out.
				a.commitRowMenuWithHistory(m, commit, services, branch.Head)
			})
		})
		a.branchCommitListFooter(c)
	})
}

// branchCommitListFooter makes older-history progress and failure visible
// below the same list whose EndReached callback requested it.
func (a *app) branchCommitListFooter(c *ui.Context) {
	theme := c.Theme()
	switch {
	case a.branchCommitsLoadingMore:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			ui.Spinner(c).Label(tr("Loading older commits"))
			ui.Text(c, tr("Loading older commits…")).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 1)
		})
	case a.branchCommitsMoreError != "":
		ui.Column(c).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("Could not load older commits.")).TextColor(theme.Danger).
					FontSize(theme.FontSize - 1).Grow(1)
				if ui.Button(c, tr("Retry")).Clicked() {
					a.requestMoreBranchCommits(a.snapshot.Branches[a.selectedBranch].Name)
				}
			})
			ui.Text(c, a.branchCommitsMoreError).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).SingleLine().Tooltip(a.branchCommitsMoreError)
		})
	case !a.branchCommitsMore:
		ui.Text(c, tr("Beginning of history")).TextColor(theme.TextMuted).
			FontSize(theme.FontSize - 1)
	}
}

func (a *app) selectBranchCommitFileRow(row int) {
	selectFileTreeRow(a.branchCommitFileRows, row, &a.selectedBranchCommitFileRow, &a.selectedBranchCommitFile)
	if a.selectedBranchCommitFile >= 0 {
		return
	}
	a.branchCommitToken++
	a.branchCommitDiffHash = ""
	a.branchCommitDiffPath = ""
	a.branchCommitDiffPatch = ""
	a.branchCommitDiffState = nil
	a.branchCommitDiffMedia = nil
	a.branchCommitDiffLoading = false
	a.branchCommitDiffError = ""
}

func (a *app) branchCommitDetailViewState() commitDetailViewState {
	return commitDetailViewState{
		scope:          fileTreeBranchScope,
		message:        &a.branchCommitDetailMessage,
		requestMessage: a.requestBranchCommitMessage,
		filesHash:      &a.branchCommitFilesHash, filesLoading: &a.branchCommitFilesLoading, filesError: &a.branchCommitFilesError,
		files: &a.branchCommitFiles, fileRows: &a.branchCommitFileRows, fileList: &a.branchCommitFileList,
		selectedFileRow: &a.selectedBranchCommitFileRow, selectedFile: &a.selectedBranchCommitFile,
		filesHeight: &a.branchCommitFilesHeight, filesWidth: &a.branchCommitFilesWidth, filesCollapsed: &a.branchCommitFilesCollapsed,
		diffLoading: &a.branchCommitDiffLoading, diffError: &a.branchCommitDiffError, diffPath: &a.branchCommitDiffPath,
		diffState: &a.branchCommitDiffState, diffMedia: &a.branchCommitDiffMedia,
		requestDetails: a.requestBranchCommitFiles, requestDiff: a.requestBranchCommitFileDiff,
		selectFileRow: a.selectBranchCommitFileRow,
	}
}

// branchCommitDetailView is the third column. Its independent branch data is
// bound into the same detail body History, Graph, and Reflog render.
func (a *app) branchCommitDetailView(c *ui.Context, branch repository.Branch) {
	var commit *repository.Commit
	if a.selectedBranchCommit >= 0 && a.selectedBranchCommit < len(a.branchCommits) {
		selected := a.branchCommits[a.selectedBranchCommit]
		commit = &selected
	}
	ui.Column(c).Fill().Gap(10).PaddingStart(16).Children(func() {
		a.commitDetailBody(c, commit, "Select a commit to see its files and diff.",
			func(c *ui.Context, commit repository.Commit) {
				a.commitActionIconsWithHistory(c, commit, branch.Head)
			}, a.branchCommitDetailViewState())
	})
}

// unpushedCommitCount reports how many commits the checked-out branch is
// ahead of its upstream, when Git can tell.
func (a *app) unpushedCommitCount() (int, bool) {
	for _, branch := range a.snapshot.Branches {
		if !branch.Head {
			continue
		}
		ahead, _, known := aheadBehind(branch)
		if !known {
			return 0, false
		}
		count, err := strconv.Atoi(ahead)
		if err != nil {
			return 0, false
		}
		return count, true
	}
	return 0, false
}

// openMoveCommitsForm opens the dialog for moving the branch's unpushed
// commits onto a new branch.
func (a *app) openMoveCommitsForm() {
	if a.actionBusy || a.session == nil {
		return
	}
	a.moveCommitsName = ""
	a.showMoveCommitsForm = true
}

func (a *app) moveCommitsModal(c *ui.Context) {
	if !a.showMoveCommitsForm {
		return
	}
	theme := c.Theme()
	count, known := a.unpushedCommitCount()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(500).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Move Commits")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, branchIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr("Move Unpushed Commits")).FontSize(17).Bold()
							if known {
								ui.Text(c, trf("%d commit(s) move; %s returns to its fork point.", count, a.snapshot.Branch)).
									TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
							} else {
								ui.Text(c, tr("The current branch returns to where it forked from its upstream.")).
									TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
							}
						})
						iconButton(c, "Close branch form", "Escape", closeIcon, false,
							func() { a.showMoveCommitsForm = false })
					})
					ui.Column(c).Gap(4).Children(func() {
						ui.Text(c, tr("New branch name")).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
						input := ui.TextInput(c, &a.moveCommitsName).
							Label(tr("New branch name")).
							Placeholder(tr("feature/my-branch")).
							AutoFocus()
						if input.Submitted() && strings.TrimSpace(a.moveCommitsName) != "" {
							a.beginMoveCommitsToNewBranch()
						}
					})
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Text(c, tr("Tracked edits must be committed or stashed first. Untracked files stay untouched.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
						if ui.Button(c, tr("Cancel")).Clicked() {
							a.showMoveCommitsForm = false
						}
						if ui.PrimaryButton(c, tr("Move commits")).
							Disabled(a.actionBusy || strings.TrimSpace(a.moveCommitsName) == "").
							Clicked() {
							a.beginMoveCommitsToNewBranch()
						}
					})
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showMoveCommitsForm = false
			}
		})
	})
}

func (a *app) beginMoveCommitsToNewBranch() {
	name := strings.TrimSpace(a.moveCommitsName)
	if name == "" || a.actionBusy {
		return
	}
	a.showMoveCommitsForm = false
	a.askConfirm(confirmRequest{
		title:        "Move Unpushed Commits",
		message:      trf("Move the unpushed commits from %s to %s?", a.snapshot.Branch, name),
		detail:       tr("The current branch is reset to where it forked from its upstream; the unpushed commits land on the new branch, which is checked out. Uncommitted changes stay in the working tree."),
		confirmLabel: "Move commits",
		run: func() {
			a.beginRepositoryActionMessage(trf("Moved unpushed commits to %s", name), "",
				func(ctx context.Context, session repository.Session) error {
					_, err := requireHistorySession(session).MoveUnpushedCommits(ctx, name)
					return err
				}, func() {
					a.moveCommitsName = ""
				})
		},
	})
}

// staleGoneBranches lists local branches whose upstream is gone on the
// remote and that carry no unpushed work of their own. They are the residue
// left behind when the merge happens on the server.
func (a *app) staleGoneBranches() []repository.Branch {
	var stale []repository.Branch
	for _, branch := range a.snapshot.Branches {
		if branch.Head || !branch.UpstreamGone {
			continue
		}
		if ahead, _, known := aheadBehind(branch); known {
			if n, err := strconv.Atoi(ahead); err == nil && n > 0 {
				continue
			}
		}
		stale = append(stale, branch)
	}
	return stale
}

func (a *app) confirmDeleteStaleBranches() {
	stale := a.staleGoneBranches()
	if len(stale) == 0 || a.actionBusy || a.session == nil {
		return
	}
	names := make([]string, 0, len(stale))
	for _, branch := range stale {
		names = append(names, branch.Name)
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Stale Branches",
		message:      trf("Delete %d local branch%s whose upstream is gone?", len(stale), pluralS(len(stale))),
		detail:       strings.Join(names, ", ") + "\n\n" + tr("Branches that are not fully merged are skipped; deletions can be undone from the Safety Journal."),
		confirmLabel: "Delete",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Deleted stale branches"), "", func(ctx context.Context, session repository.Session) error {
				var failed []string
				for _, name := range names {
					if err := requireBranchRemoteSession(session).DeleteBranch(ctx, name, false); err != nil {
						failed = append(failed, name)
					}
				}
				if len(failed) > 0 {
					return fmt.Errorf("could not delete: %s (not fully merged or checked out elsewhere)", strings.Join(failed, ", "))
				}
				return nil
			}, nil)
		},
	})
}

// moveCommitsModal preserves existing files while committing only the branch
// movement. Tracked edits are rejected before Git runs; untracked work must
// survive even when the fork point bears a file of the same name.
