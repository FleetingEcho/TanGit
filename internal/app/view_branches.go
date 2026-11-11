package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
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
			return session.CreateBranch(ctx, name, base)
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
			return session.RenameBranch(ctx, oldName, newName)
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
					return session.MergeBranch(ctx, branch.Name)
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
					return session.DeleteBranch(ctx, branch.Name, false)
				}, nil)
		},
	})
}

func (a *app) checkoutBranch(name string) {
	a.confirmAttention("Checkout", name, "This changes the checked-out branch and working-tree files. Uncommitted changes may be carried across or prevent checkout.", func() {
		a.beginRepositoryActionMessage(trf("Checked out %s", name), "",
			func(ctx context.Context, session repository.Session) error {
				return session.CheckoutBranch(ctx, name)
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
				return session.PullBranch(ctx, branch.Name)
			}, nil)
	})
}

// beginFastForwardBranch moves a branch to its upstream when only new commits
// are in the way, as Lazygit's fast-forward does.
func (a *app) beginFastForwardBranch(branch repository.Branch) {
	a.confirmAttention("Fast-forward", branch.Name, "This integrates upstream changes and may change branch history or working-tree files. Conflicts may require resolution.", func() {
		a.beginRepositoryActionMessage(trf("Fast-forwarded %s", branchDisplay(branch)), "",
			func(ctx context.Context, session repository.Session) error {
				return session.FastForwardBranch(ctx, branch.Name)
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
				return session.UnsetUpstream(ctx, branch.Name)
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
	a.askConfirm(confirmRequest{
		title:        "Rebase Branch",
		message:      trf("Rebase %s onto %s?", currentBranch, branchDisplay(branch)),
		detail:       trf("The commits %s has that %s lacks are replayed on top of it, with new hashes.", currentBranch, branchDisplay(branch)),
		confirmLabel: "Rebase",
		run: func() {
			a.beginRepositoryActionMessage(trf("Rebased %s onto %s", currentBranch, branchDisplay(branch)), "",
				func(ctx context.Context, session repository.Session) error {
					return session.RebaseOntoBranch(ctx, branch.Name)
				}, nil)
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
					return session.ForceCheckoutBranch(ctx, branch.Name)
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
	return filteredSourceRows(len(a.snapshot.Branches), a.branchSearch, func(source int) string {
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

		if m.Item(tr("Force push with lease…")).Disabled(a.actionBusy).Chosen() {
			a.confirmForcePushWithLease()
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
			rowIconButton(c, "New branch…", "Create a branch from a local branch",
				stageIcon, func() { a.openBranchForm("") })
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
					ui.Text(row.Context, tr("●")).TextColor(secondaryRowColor(theme, row))
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
		if ui.Button(c, tr("Rename…")).Disabled(a.actionBusy).Clicked() {
			a.showRenameForm = true
			a.renameBranch = branch.Name
		}

		label, icon := "Expand branch page", expandIcon
		if a.branchFullscreen {
			label, icon = "Restore branch page", shrinkIcon
		}
		button := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(label))
		button.Children(func() { ui.Icon(c, icon).FontSize(14) })
		button.OnClick(func() { a.branchFullscreen = !a.branchFullscreen })
	})
	if !branch.Head {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			if branch.UpstreamRemote != "" {
				if ui.Button(c, tr("Pull latest")).Disabled(a.actionBusy).Clicked() {
					a.beginPullBranch(branch)
				}
			}
			if ui.Button(c, tr("Rebase onto this…")).Disabled(a.actionBusy).Clicked() {
				a.confirmRebaseOntoBranch(branch)
			}
			if ui.Button(c, tr("Merge into current…")).Disabled(a.actionBusy).Clicked() {
				a.confirmMergeSelectedBranch()
			}
			if ui.PrimaryButton(c, tr("Checkout")).Disabled(a.actionBusy).Clicked() {
				a.checkoutBranch(branch.Name)
			}
			if ui.Button(c, tr("Delete…")).Disabled(a.actionBusy).Clicked() {
				a.confirmDeleteSelectedBranch()
			}
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

// branchCommitsLimit bounds the commits one branch browse reads.
const branchCommitsLimit = 200

// resetBranchCommits drops what the branch browse read, so the next frame
// reads the branch then selected again. An action changed the commits, and a
// read still in flight must not land on the new state.
func (a *app) resetBranchCommits() {
	a.branchCommitsToken++
	a.branchCommitToken++
	a.branchCommits = nil
	a.branchCommitsBranch = ""
	a.branchCommitsLoading = false
	a.branchCommitsError = ""
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
	filter.Limit = branchCommitsLimit
	filtered := commitFilterNarrows(filter)
	session := a.session
	repositoryGeneration := a.generation
	// A quiet refresh names the commit it keeps chosen; a user asking for
	// another branch leaves both empty, and the list starts at its top.
	keep := a.branchCommitsKeep
	a.branchCommitsKeep = ""
	a.branchCommitsBranch = key
	a.branchCommitsLoading = true
	a.branchCommitsError = ""
	a.branchCommitsToken++
	token := a.branchCommitsToken
	a.branchCommits = nil
	a.selectedBranchCommit = -1
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
			commits, err = session.SearchCommits(context.Background(), filter)
		} else {
			commits, err = session.BranchCommits(context.Background(), branch, branchCommitsLimit)
		}
		a.update(func() {
			if repositoryGeneration != a.generation || token != a.branchCommitsToken {
				return
			}
			a.branchCommitsLoading = false
			if err != nil {
				a.branchCommitsError = safeError(err)
				return
			}
			a.branchCommits = commits
			// A quiet refresh keeps the commit the user was reading, by
			// hash: the list moved because the branch did, not because they
			// chose another commit.
			if keep != "" {
				for i, commit := range commits {
					if commit.Hash == keep {
						a.selectedBranchCommit = i
						return
					}
				}
			}
			if len(commits) > 0 {
				a.selectedBranchCommit = 0
			}
		})
	}()
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
	// A quiet refresh keeps the file the user was reading within the commit.
	keepPath := a.branchCommitFilesKeepPath
	a.branchCommitFilesKeepPath = ""
	a.branchCommitToken++
	token := a.branchCommitToken
	a.branchCommitFilesHash = commit.Hash
	a.branchCommitFiles = nil
	a.selectedBranchCommitFileRow = -1
	a.selectedBranchCommitFile = -1
	a.branchCommitFilesLoading = true
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
		files, err := session.CommitFiles(context.Background(), commit)
		a.update(func() {
			if token != a.branchCommitToken {
				return
			}
			if repositoryGeneration != a.generation {
				// An action rewrote the repository: drop the request so
				// the next frame reads the commit again.
				a.branchCommitFilesHash = ""
				a.branchCommitFilesLoading = false
				return
			}
			if a.branchCommitFilesHash != commit.Hash {
				return
			}
			a.branchCommitFilesLoading = false
			if err != nil {
				a.branchCommitFilesError = safeError(err)
				return
			}
			a.branchCommitFiles = files
			if keepPath != "" {
				for i, file := range files {
					if file.Path == keepPath {
						a.selectedBranchCommitFile = i
						return
					}
				}
			}
			if len(files) > 0 {
				a.selectedBranchCommitFile = 0
			}
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
	a.branchCommitToken++
	token := a.branchCommitToken
	a.branchCommitDiffHash = commit.Hash
	a.branchCommitDiffPath = file.Path
	a.branchCommitDiffState = nil
	a.branchCommitDiffMedia = nil
	a.branchCommitDiffPatch = ""
	a.branchCommitDiffLoading = true
	a.branchCommitDiffError = ""

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
			if token != a.branchCommitToken {
				return
			}
			if repositoryGeneration != a.generation {
				a.branchCommitDiffHash = ""
				a.branchCommitDiffPath = ""
				a.branchCommitDiffPatch = ""
				a.branchCommitDiffLoading = false
				return
			}
			if a.branchCommitDiffHash != commit.Hash || a.branchCommitDiffPath != file.Path {
				return
			}
			a.branchCommitDiffLoading = false
			if err != nil {
				a.branchCommitDiffError = safeError(err)
				return
			}
			a.branchCommitDiffPatch = diff.Patch
			a.branchCommitDiffState = state
			a.branchCommitDiffMedia = media
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
			if a.branchCommitsLoading {
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
			paneCollapseButton(c, &a.branchCommitsCollapsed, "Collapse commit list")
		})
		if a.branchCommitsError != "" {
			ui.Text(c, a.branchCommitsError).TextColor(theme.Danger).SingleLine()
			if ui.Button(c, tr("Load again")).Clicked() {
				// The next pass reads the branch afresh.
				a.branchCommitsBranch = ""
			}
			return
		}
		if a.branchCommitsLoading && len(a.branchCommits) == 0 {
			if filtered {
				ui.Text(c, tr("Searching commits…")).TextColor(theme.TextMuted)
			} else {
				ui.Text(c, tr("Reading the branch's history…")).TextColor(theme.TextMuted)
			}
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
			ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8).Children(func() {
				authorAvatar(row.Context, commit.AuthorName, commit.AuthorEmail, 18)
				ui.Column(row.Context).Gap(2).Grow(1).Children(func() {
					ui.Text(row.Context, commit.Subject).SingleLine()
					ui.Row(row.Context).Gap(8).Children(func() {
						ui.Text(row.Context, commit.ShortHash).TextColor(secondaryRowColor(theme, row))
						ui.Text(row.Context, commit.AuthorName).TextColor(secondaryRowColor(theme, row))
						if rel := relTime(commit.UnixTimestamp); rel != "" {
							ui.Text(row.Context, rel).TextColor(secondaryRowColor(theme, row))
						}
					})
				})
			}).PopoverMenu(func(m *ui.Menu) {
				// History edits move the checked-out branch, so offer those only
				// while this list shows it. Every other action uses commit directly,
				// including when browsing another branch without checking it out.
				a.commitRowMenuWithHistory(m, commit, services, branch.Head)
			})
		})
	})
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

// branchCommitDetailView is the third column: the chosen commit, the files it
// changed, and the diff of the file chosen from them.
func (a *app) branchCommitDetailView(c *ui.Context, branch repository.Branch) {
	theme := c.Theme()
	ui.Column(c).Fill().Gap(8).PaddingStart(16).Children(func() {
		if a.selectedBranchCommit < 0 || a.selectedBranchCommit >= len(a.branchCommits) {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Select a commit to see its files and diff.")).TextColor(theme.TextMuted).TextAlign(ui.Center)
			})
			return
		}
		commit := a.branchCommits[a.selectedBranchCommit]
		// The column is narrow, so the subject, the metadata and the actions
		// each take their own row.
		ui.Text(c, commit.Subject).FontSize(16).Bold().SingleLine().Tooltip(commit.Subject)
		a.commitHashRow(c, commit, "Copy commit hash")
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			authorAvatar(c, commit.AuthorName, commit.AuthorEmail, 20)
			ui.Textf(c, "%s <%s>", commit.AuthorName, commit.AuthorEmail).TextColor(theme.TextMuted).
				SingleLine().Grow(1).MinWidth(0).Tooltip(fmt.Sprintf("%s <%s>", commit.AuthorName, commit.AuthorEmail))
			if rel := relTime(commit.UnixTimestamp); rel != "" {
				ui.Text(c, rel).TextColor(theme.TextMuted).SingleLine().Shrink(0)
			}
		})
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Wrap().Children(func() {
			a.commitActionIconsWithHistory(c, commit, branch.Head)
		})
		a.diffViewOptions(c)
		a.requestBranchCommitFiles(commit)
		if a.branchCommitFilesLoading {
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading commit files"))
				ui.Text(c, tr("Loading changed files…")).TextColor(theme.TextMuted)
			})
			return
		}
		if a.branchCommitFilesError != "" {
			ui.Text(c, a.branchCommitFilesError).TextColor(theme.Danger)
			return
		}
		if len(a.branchCommitFiles) == 0 {
			ui.Text(c, tr("This commit changed no files.")).TextColor(theme.TextMuted)
			return
		}
		paths := commitFilePaths(a.branchCommitFiles)
		a.branchCommitFileRows = a.syncFileTreeRows(fileTreeBranchScope, paths, a.branchCommitFileRows,
			&a.selectedBranchCommitFileRow, &a.selectedBranchCommitFile)
		// The files and the diff share the rest of the column, and the line
		// between them is dragged to give one more room than the other.
		paneSplitVertical(c, &a.branchCommitFilesHeight, func() {
			ui.Column(c).Fill().Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Text(c, tr("Changed files")).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d", len(a.branchCommitFiles)))
					a.fileTreeButtons(c, fileTreeBranchScope, paths)
				})
				list := a.keyboardList(c, &a.branchCommitFileList, len(a.branchCommitFileRows)).Grow(1).Rows(func(row ui.ListRow) {
					item := a.branchCommitFileRows[row.Index]
					if item.isDirectory() {
						a.fileTreeDirectoryRow(row.Context, fileTreeBranchScope, item, row.Selected(), func() {
							a.selectBranchCommitFileRow(row.Index)
						})
						return
					}
					file := a.branchCommitFiles[item.source]
					services := row.Context.Services()
					ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(5, 8, 5, float32(8+16*item.depth)).Children(func() {
						ui.Text(row.Context, file.Status).Width(24).TextColor(theme.Warning)
						ui.Text(row.Context, item.name).Grow(1).SingleLine().Tooltip(file.Path)
						fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
					}).PopoverMenu(func(m *ui.Menu) {
						a.commitFileMenu(m, &commit, file, services)
					})
				})
				if a.keyboardListChanged(&a.branchCommitFileList, list) {
					a.selectBranchCommitFileRow(a.selectedBranchCommitFileRow)
				}
				normalizeFileTreeRow(a.branchCommitFileRows, &a.selectedBranchCommitFileRow, &a.selectedBranchCommitFile)
			})
		}, func() {
			ui.Column(c).Fill().Children(func() {
				if a.selectedBranchCommitFile >= 0 && a.selectedBranchCommitFile < len(a.branchCommitFiles) {
					a.requestBranchCommitFileDiff(commit, a.branchCommitFiles[a.selectedBranchCommitFile])
				}
				switch {
				case a.branchCommitDiffLoading:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Spinner(c).Label(tr("Loading diff"))
						ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
					})
				case a.branchCommitDiffError != "":
					ui.Text(c, a.branchCommitDiffError).TextColor(theme.Danger)
				case a.branchCommitDiffState != nil:
					a.keyboardDiffViewer(c, a.branchCommitDiffState)
				default:
					noTextualDiffOrMedia(c, a.branchCommitDiffMedia, a.branchCommitDiffPath)
				}
			})
		}).Grow(1)
	})
}
