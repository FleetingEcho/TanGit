package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (a *app) beginAddSubmodule() {
	name := strings.TrimSpace(a.submoduleName)
	path := strings.TrimSpace(a.submodulePath)
	url := strings.TrimSpace(a.submoduleURL)
	a.beginRepositoryActionMessage(trf("Added submodule %s", name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.AddSubmodule(ctx, name, path, url)
		}, func() {
			a.selectedSubmodule = selectedSubmoduleIndex(a.snapshot.Submodules, path)
			a.syncSubmoduleRow()
			a.submoduleName = ""
			a.submodulePath = ""
			a.submoduleURL = ""
			a.showSubmoduleForm = false
		})
}

func (a *app) confirmDeleteSelectedSubmodule() {
	if a.selectedSubmodule < 0 || a.selectedSubmodule >= len(a.snapshot.Submodules) {
		return
	}
	a.confirmDeleteSubmodule(a.snapshot.Submodules[a.selectedSubmodule])
}

func (a *app) confirmDeleteSubmodule(submodule repository.Submodule) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Submodule",
		message:      trf("Delete submodule %s?", submodule.Name),
		detail:       "The checkout and Git metadata will be removed, and the superproject changes will be staged.",
		confirmLabel: "Delete",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Deleted submodule %s", submodule.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.DeleteSubmodule(ctx, submodule.Path)
				}, nil)
		},
	})
}

func (a *app) visibleSubmodules() []int {
	return filteredSourceRows(len(a.snapshot.Submodules), a.submoduleFilter, func(source int) string {
		submodule := a.snapshot.Submodules[source]
		return strings.Join([]string{submodule.Name, submodule.Path, submodule.URL}, "\x00")
	})
}

func (a *app) selectSubmoduleRow(row int) {
	selectFilteredRow(a.visibleSubmodules(), row, &a.selectedSubmoduleRow, &a.selectedSubmodule)
}

func (a *app) syncSubmoduleRow() {
	syncFilteredRow(a.visibleSubmodules(), &a.selectedSubmoduleRow, &a.selectedSubmodule)
}

// submoduleRowMenu is the right-click menu for a submodule row.
func (a *app) submoduleRowMenu(m *ui.Menu, row ui.ListRow) {
	submodule, ok := sourceRow(a.visibleSubmodules(), a.snapshot.Submodules, row.Index)
	if !ok {
		return
	}
	if m.Item(tr("Initialize / update")).Shortcut(0, ui.KeyU).Disabled(a.actionBusy).Chosen() {
		a.selectSubmoduleRow(row.Index)
		a.updateSelectedSubmodule()
	}
	if m.Item(tr("Update recursively")).Disabled(a.actionBusy).Chosen() {
		a.updateSubmodules([]string{submodule.Path}, true, false)
	}
	if m.Item(tr("Update to remote tracking branch")).Disabled(a.actionBusy).Chosen() {
		a.updateSubmodules([]string{submodule.Path}, false, true)
	}
	if m.Item(tr("Sync URL from .gitmodules")).Disabled(a.actionBusy).Chosen() {
		a.syncSubmodules([]string{submodule.Path})
	}
	m.Separator()
	if m.Item(tr("Update all recursively")).Disabled(a.actionBusy).Chosen() {
		a.updateSubmodules(nil, true, false)
	}
	if m.Item(tr("Sync all submodule URLs")).Disabled(a.actionBusy).Chosen() {
		a.syncSubmodules(nil)
	}
	m.Separator()
	if m.Item(tr("Delete submodule…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
		a.selectSubmoduleRow(row.Index)
		a.confirmDeleteSubmodule(submodule)
	}
}

func (a *app) submoduleFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showSubmoduleForm, 600, "Add Submodule",
		"Vendor another repository inside this one.", submodulesIcon, func() {
			ui.Row(c).Gap(10).Children(func() {
				ui.TextInput(c, &a.submoduleName).Label(tr("Submodule name")).
					Placeholder(tr("library")).Grow(1).AutoFocus()
				ui.TextInput(c, &a.submodulePath).Label(tr("Checkout path")).
					Placeholder(tr("vendor/library")).Grow(1)
			})
			ui.TextInput(c, &a.submoduleURL).Label(tr("Repository URL")).
				Placeholder(tr("git@example.com:team/library.git"))
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
					a.showSubmoduleForm = false
				}
				invalid := strings.TrimSpace(a.submoduleName) == "" ||
					strings.TrimSpace(a.submodulePath) == "" || strings.TrimSpace(a.submoduleURL) == ""
				if ui.PrimaryButton(c, tr("Add submodule")).Disabled(a.actionBusy || invalid).Clicked() {
					a.beginAddSubmodule()
				}
			})
		})
}

func (a *app) submodulesView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Gap(10).Children(func() {
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionSubmodules],
			"Expand list pane", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					paneTitleAction(c, submodulesIcon, "Configured submodules", "Add submodule…",
						"Vendor another repository", a.actionBusy, func() { a.showSubmoduleForm = true }, func() {
							paneCollapseButton(c, &a.workspacePaneCollapsed[sectionSubmodules], "Collapse list pane")
						})
					if len(a.snapshot.Submodules) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("No submodules")).TextColor(theme.TextMuted)
						})
						return
					}
					visible := a.visibleSubmodules()
					listSearchField(c, &a.sectionSearch[sectionSubmodules], &a.submoduleFilter,
						"Search submodules", "Filter by name, path, or URL", "Clear submodule search",
						len(visible), len(a.snapshot.Submodules), func() { a.selectSubmoduleRow(0) }, a.navigateSubmoduleSearch)
					visible = a.visibleSubmodules()
					if len(visible) == 0 {
						listSearchEmpty(c, "No submodules match this search")
						return
					}
					list := a.keyboardList(c, &a.submoduleList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
						submodule, _ := sourceRow(visible, a.snapshot.Submodules, row.Index)
						ui.Column(row.Context).Gap(2).Padding(7, 8).Children(func() {
							ui.Text(row.Context, submodule.Name)
							ui.Text(row.Context, submodule.Path).TextColor(secondaryRowColor(theme, row))
						}).PopoverMenu(func(m *ui.Menu) { a.submoduleRowMenu(m, row) })
					})
					if a.keyboardListChanged(&a.submoduleList, list) {
						a.selectSubmoduleRow(a.selectedSubmoduleRow)
					}
					normalizeFilteredRow(visible, &a.selectedSubmoduleRow, &a.selectedSubmodule)
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
					if a.selectedSubmodule < 0 || a.selectedSubmodule >= len(a.snapshot.Submodules) {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Select a submodule")).TextColor(theme.TextMuted)
						})
						return
					}
					submodule := a.snapshot.Submodules[a.selectedSubmodule]
					ui.Text(c, submodule.Name).FontSize(18).Bold()
					ui.Text(c, submodule.Path).TextColor(theme.TextMuted).Selectable()
					ui.Text(c, submodule.URL).TextColor(theme.TextMuted).Selectable()
					ui.Row(c).Gap(8).Children(func() {
						if ui.PrimaryButton(c, tr("Initialize / update")).Disabled(a.actionBusy).Clicked() {
							a.updateSelectedSubmodule()
						}
						if ui.Button(c, tr("Update to remote")).Disabled(a.actionBusy).Clicked() {
							a.updateSubmodules([]string{submodule.Path}, false, true)
						}
						if ui.Button(c, tr("Delete…")).Disabled(a.actionBusy).Clicked() {
							a.confirmDeleteSelectedSubmodule()
						}
					})
					ui.Row(c).Gap(8).Children(func() {
						if ui.Button(c, tr("Update all recursively")).Disabled(a.actionBusy).Clicked() {
							a.updateSubmodules(nil, true, false)
						}
						if ui.Button(c, tr("Sync all URLs")).Disabled(a.actionBusy).Clicked() {
							a.syncSubmodules(nil)
						}
					})
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("Submodule changes are committed in the superproject.")).TextColor(theme.TextMuted)
					})
				})
			})
	})
}

func (a *app) updateSubmodules(paths []string, recursive, remote bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	message := tr("Updated submodules")
	if remote {
		message = tr("Updated submodules to their remote tracking branches")
	}
	a.beginRepositoryActionMessage(message, "",
		func(ctx context.Context, session repository.Session) error {
			return session.UpdateSubmodules(ctx, paths, recursive, remote)
		}, nil)
}

func (a *app) syncSubmodules(paths []string) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.beginRepositoryActionMessage(tr("Synchronized submodule URLs"), "",
		func(ctx context.Context, session repository.Session) error {
			return session.SyncSubmodules(ctx, paths)
		}, nil)
}
