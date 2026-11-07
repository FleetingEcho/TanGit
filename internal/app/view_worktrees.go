package app

import (
	"strings"

	"context"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (a *app) beginCreateWorktree() {
	path := strings.TrimSpace(a.worktreePath)
	base := strings.TrimSpace(a.worktreeBase)
	branch := strings.TrimSpace(a.worktreeBranch)
	detach := a.worktreeDetach
	a.beginRepositoryActionMessage(trf("Added worktree at %s", path), "",
		func(ctx context.Context, session repository.Session) error {
			return session.CreateWorktree(ctx, path, base, branch, detach)
		}, func() {
			a.selectedWorktree = selectedWorktreeIndex(a.snapshot.Worktrees, path)
			a.syncWorktreeRow()
			a.worktreePath = ""
			a.worktreeBase = ""
			a.worktreeBranch = ""
			a.worktreeDetach = false
			a.showWorktreeForm = false
		})
}

func (a *app) openSelectedWorktree() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	a.openWorktree(a.snapshot.Worktrees[a.selectedWorktree])
}

func (a *app) openWorktree(worktree repository.Worktree) {
	if worktree.Missing {
		return
	}
	a.path = worktree.Path
	a.beginOpen()
}

func (a *app) confirmDeleteSelectedWorktree() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	a.confirmDeleteWorktree(a.snapshot.Worktrees[a.selectedWorktree])
}

func (a *app) confirmDeleteWorktree(worktree repository.Worktree) {
	if a.actionBusy || a.session == nil {
		return
	}
	if worktree.Current || worktree.Main {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Remove Worktree",
		message:      trf("Remove worktree %s?", worktree.Name),
		detail:       trf("Git will remove the checkout at %s. Uncommitted changes prevent removal.", worktree.Path),
		confirmLabel: "Remove",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Removed worktree %s", worktree.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.DeleteWorktree(ctx, worktree.Path, false)
				}, nil)
		},
	})
}

func (a *app) visibleWorktrees() []int {
	return filteredSourceRows(len(a.snapshot.Worktrees), a.worktreeFilter, func(source int) string {
		worktree := a.snapshot.Worktrees[source]
		return strings.Join([]string{worktree.Name, worktree.Path, worktree.Branch, worktree.Head}, "\x00")
	})
}

func (a *app) selectWorktreeRow(row int) {
	selectFilteredRow(a.visibleWorktrees(), row, &a.selectedWorktreeRow, &a.selectedWorktree)
}

func (a *app) syncWorktreeRow() {
	syncFilteredRow(a.visibleWorktrees(), &a.selectedWorktreeRow, &a.selectedWorktree)
}

// worktreeRowMenu is the right-click menu for a linked-worktree row.
func (a *app) worktreeRowMenu(m *ui.Menu, row ui.ListRow, services ui.Services) {
	worktree, ok := sourceRow(a.visibleWorktrees(), a.snapshot.Worktrees, row.Index)
	if !ok {
		return
	}
	if !worktree.Current && !worktree.Missing {
		if m.Item(tr("Open worktree")).Shortcut(0, ui.KeyO).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.openWorktree(worktree)
		}
	}
	m.Separator()
	if m.Item(tr("Copy path")).Chosen() {
		a.notifyCopied(services, "worktree path", worktree.Path)
	}
	if !worktree.Current && !worktree.Main {
		m.Separator()
		if m.Item(tr("Remove worktree…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.confirmDeleteWorktree(worktree)
		}
	}
}

func (a *app) worktreeFormModal(c *ui.Context) {
	repositoryFormModal(c, &a.showWorktreeForm, 600, "Create Worktree",
		"Check out another branch beside this repository.", worktreesIcon, func() {
			ui.TextInput(c, &a.worktreePath).Label(tr("Worktree path")).
				Placeholder(tr("/path/to/worktree")).AutoFocus()
			ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
				ui.TextInput(c, &a.worktreeBase).Label(tr("Base ref")).
					Placeholder(tr("HEAD")).Grow(1)
				ui.Checkbox(c, &a.worktreeDetach, tr("Detached HEAD"))
			})
			if !a.worktreeDetach {
				ui.TextInput(c, &a.worktreeBranch).Label(tr("New branch (optional)")).
					Placeholder(tr("feature/my-work"))
			}
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
					a.showWorktreeForm = false
				}
				invalid := strings.TrimSpace(a.worktreePath) == "" || strings.TrimSpace(a.worktreeBase) == ""
				if ui.PrimaryButton(c, tr("Create worktree")).Disabled(a.actionBusy || invalid).Clicked() {
					a.beginCreateWorktree()
				}
			})
		})
}

func (a *app) worktreesView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Children(func() {
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionWorktrees],
			"Expand list pane", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					paneTitleAction(c, worktreesIcon, "Linked worktrees", "New worktree…",
						"Check out another branch beside this one", a.actionBusy, func() {
							a.showWorktreeForm = true
							a.worktreeBase = "HEAD"
						}, func() {
							paneCollapseButton(c, &a.workspacePaneCollapsed[sectionWorktrees], "Collapse list pane")
						})
					if len(a.snapshot.Worktrees) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("No linked worktrees")).TextColor(theme.TextMuted)
						})
						return
					}
					visible := a.visibleWorktrees()
					listSearchField(c, &a.sectionSearch[sectionWorktrees], &a.worktreeFilter,
						"Search worktrees", "Filter by name, path, branch, or commit", "Clear worktree search",
						len(visible), len(a.snapshot.Worktrees), func() { a.selectWorktreeRow(0) }, a.navigateWorktreeSearch)
					visible = a.visibleWorktrees()
					if len(visible) == 0 {
						listSearchEmpty(c, "No worktrees match this search")
						return
					}
					list := ui.List(c, &a.worktreeList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
						worktree, _ := sourceRow(visible, a.snapshot.Worktrees, row.Index)
						services := row.Context.Services()
						ui.Row(row.Context).Gap(8).Padding(7, 8).Children(func() {
							if worktree.Current {
								ui.Text(row.Context, tr("●")).TextColor(theme.Success)
							}
							ui.Text(row.Context, worktree.Name).Grow(1)
							if worktree.Branch != "" {
								ui.Badge(row.Context, worktree.Branch)
							}
						}).PopoverMenu(func(m *ui.Menu) { a.worktreeRowMenu(m, row, services) })
					})
					if list.Changed() {
						a.selectWorktreeRow(a.selectedWorktreeRow)
					}
					normalizeFilteredRow(visible, &a.selectedWorktreeRow, &a.selectedWorktree)
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
					if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Select a worktree")).TextColor(theme.TextMuted)
						})
						return
					}
					worktree := a.snapshot.Worktrees[a.selectedWorktree]
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, worktree.Name).FontSize(18).Bold().Grow(1)
						if !worktree.Current && !worktree.Missing {
							if ui.PrimaryButton(c, tr("Open worktree")).Disabled(a.actionBusy).Clicked() {
								a.openSelectedWorktree()
							}
						}
						if !worktree.Current && !worktree.Main {
							if ui.Button(c, tr("Remove…")).Disabled(a.actionBusy).Clicked() {
								a.confirmDeleteSelectedWorktree()
							}
						}
					})
					ui.Text(c, worktree.Path).TextColor(theme.TextMuted).Selectable()
					if worktree.Current || worktree.Main {
						ui.Row(c).Gap(6).Children(func() {
							if worktree.Current {
								ui.Badge(c, tr("Current"))
							}
							if worktree.Main {
								ui.Badge(c, tr("Main worktree"))
							}
						})
					}
					if worktree.Missing {
						ui.Text(c, tr("The worktree path is missing.")).TextColor(theme.Danger)
					}
					if worktree.Branch != "" {
						ui.Textf(c, tr("Branch: %s"), worktree.Branch)
					} else {
						ui.Textf(c, tr("Detached at %s"), worktree.Head)
					}
					if worktree.OperationLive {
						ui.Text(c, tr("A rebase or bisect is active in this worktree.")).TextColor(theme.Warning)
					}
				})
			})
	})
}
