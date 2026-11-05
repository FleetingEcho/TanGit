package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type remoteBranchRow struct {
	source int
	branch repository.RemoteBranch
}

func (a *app) visibleRemotes() []int {
	return filteredSourceRows(len(a.snapshot.Remotes), a.remoteFilter, func(source int) string {
		remote := a.snapshot.Remotes[source]
		return strings.Join(append([]string{remote.Name}, remote.URLs...), "\x00")
	})
}

func (a *app) remoteAt(row int) (repository.Remote, bool) {
	return sourceRow(a.visibleRemotes(), a.snapshot.Remotes, row)
}

func (a *app) selectRemoteRow(row int) {
	selectFilteredRow(a.visibleRemotes(), row, &a.selectedRemoteRow, &a.selectedRemote)
	a.resetRemoteBranchChoice()
	a.showRemoteCheckoutForm = false
}

func (a *app) syncRemoteRow() {
	syncFilteredRow(a.visibleRemotes(), &a.selectedRemoteRow, &a.selectedRemote)
	a.syncRemoteBranchRow()
}

// visibleRemoteBranches returns the selected remote's branches narrowed by
// the quick search. Each row retains its source index because repository
// actions must address the unfiltered branch slice.
func (a *app) visibleRemoteBranches() []remoteBranchRow {
	if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(a.remoteBranchFilter))
	branches := a.snapshot.Remotes[a.selectedRemote].Branches
	rows := make([]remoteBranchRow, 0, len(branches))
	for i, branch := range branches {
		if query == "" || strings.Contains(strings.ToLower(branch.Name), query) {
			rows = append(rows, remoteBranchRow{source: i, branch: branch})
		}
	}
	return rows
}

// remoteBranchAt returns the branch the list row names in the selected
// remote. The list asks for keys and labels before it knows the selection is
// valid, so an out-of-range read reports false instead of panicking.
func (a *app) remoteBranchAt(row int) (repository.RemoteBranch, bool) {
	rows := a.visibleRemoteBranches()
	if row < 0 || row >= len(rows) {
		return repository.RemoteBranch{}, false
	}
	return rows[row].branch, true
}

func (a *app) selectRemoteBranchRow(row int) {
	a.selectedRemoteBranchRow = row
	a.selectedRemoteBranch = -1
	rows := a.visibleRemoteBranches()
	if row >= 0 && row < len(rows) {
		a.selectedRemoteBranch = rows[row].source
	}
}

// syncRemoteBranchRow keeps the filtered list on the source branch chosen by
// snapshot restoration. If that branch is hidden, the first match becomes the
// active checkout target instead of leaving an invisible choice behind.
func (a *app) syncRemoteBranchRow() {
	rows := a.visibleRemoteBranches()
	for row, branch := range rows {
		if branch.source == a.selectedRemoteBranch {
			a.selectedRemoteBranchRow = row
			return
		}
	}
	a.selectRemoteBranchRow(0)
}

func (a *app) beginFetchRemote(remote repository.Remote) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.beginRepositoryActionMessage(trf("Fetched remote %s", remote.Name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.FetchRemote(ctx, remote.Name)
		}, nil)
}

func (a *app) openAddRemote() {
	a.remoteOriginalName = ""
	a.remoteName = ""
	a.remoteURL = ""
	a.showRemoteForm = true
}

func (a *app) openEditRemote(remote repository.Remote) {
	a.remoteOriginalName = remote.Name
	a.remoteName = remote.Name
	a.remoteURL = ""
	if len(remote.URLs) > 0 {
		a.remoteURL = remote.URLs[0]
	}
	a.showRemoteCheckoutForm = false
	a.showRemoteForm = true
}

func (a *app) editSelectedRemote() {
	if a.selectedRemote >= 0 && a.selectedRemote < len(a.snapshot.Remotes) {
		a.openEditRemote(a.snapshot.Remotes[a.selectedRemote])
	}
}

func (a *app) fetchSelectedRemote() {
	if a.selectedRemote >= 0 && a.selectedRemote < len(a.snapshot.Remotes) {
		a.beginFetchRemote(a.snapshot.Remotes[a.selectedRemote])
	}
}

func (a *app) beginUpdateRemote() {
	oldName := a.remoteOriginalName
	newName := strings.TrimSpace(a.remoteName)
	url := strings.TrimSpace(a.remoteURL)
	if oldName == "" {
		return
	}
	a.confirmAttention("Edit remote", oldName+" → "+newName+"\n"+url, "This changes the remote destination used by future fetches, pulls, and pushes.", func() {
		a.beginRepositoryActionMessage(trf("Updated remote %s", newName), "",
			func(ctx context.Context, session repository.Session) error {
				return session.UpdateRemote(ctx, oldName, newName, url)
			}, func() {
				a.selectedRemote = selectedRemoteIndex(a.snapshot.Remotes, newName)
				a.syncRemoteRow()
				a.remoteOriginalName = ""
				a.remoteName = ""
				a.remoteURL = ""
				a.showRemoteForm = false
			})
	})
}

func (a *app) beginAddRemote() {
	name := strings.TrimSpace(a.remoteName)
	url := strings.TrimSpace(a.remoteURL)
	a.beginRepositoryActionMessage(trf("Added remote %s", name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.AddRemote(ctx, name, url)
		}, func() {
			a.selectedRemote = selectedRemoteIndex(a.snapshot.Remotes, name)
			a.syncRemoteRow()
			a.remoteOriginalName = ""
			a.remoteName = ""
			a.remoteURL = ""
			a.showRemoteForm = false
		})
}

// resetRemoteBranchChoice starts the selected remote's branch list at its
// first branch, or at no branch when the selection names none. A list reports
// a change before its selection is known to be valid, so the index is checked
// here rather than trusted.
func (a *app) resetRemoteBranchChoice() {
	a.remoteBranchFilter = ""
	a.selectedRemoteBranch, a.selectedRemoteBranchRow = -1, -1
	if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
		return
	}
	if len(a.snapshot.Remotes[a.selectedRemote].Branches) > 0 {
		a.selectedRemoteBranch, a.selectedRemoteBranchRow = 0, 0
	}
}

func (a *app) beginCheckoutRemoteBranch() {
	if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
		return
	}
	remote := a.snapshot.Remotes[a.selectedRemote]
	if a.selectedRemoteBranch < 0 || a.selectedRemoteBranch >= len(remote.Branches) {
		return
	}
	branch := remote.Branches[a.selectedRemoteBranch]
	localName := strings.TrimSpace(a.remoteLocalBranch)
	a.confirmAttention("Checkout", remote.Name+"/"+branch.Name+" → "+localName, "This changes the checked-out branch and working-tree files. Uncommitted changes may be carried across or prevent checkout.", func() {
		a.beginRepositoryActionMessage(trf("Checked out %s/%s", remote.Name, branch.Name), "",
			func(ctx context.Context, session repository.Session) error {
				return session.CheckoutRemoteBranch(ctx, remote.Name, branch.Name, localName)
			}, func() {
				a.remoteLocalBranch = ""
				a.showRemoteCheckoutForm = false
			})
	})
}

func (a *app) currentRemoteBranchSelection() (repository.Remote, repository.RemoteBranch, bool) {
	if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
		return repository.Remote{}, repository.RemoteBranch{}, false
	}
	remote := a.snapshot.Remotes[a.selectedRemote]
	if a.selectedRemoteBranch < 0 || a.selectedRemoteBranch >= len(remote.Branches) {
		return repository.Remote{}, repository.RemoteBranch{}, false
	}
	return remote, remote.Branches[a.selectedRemoteBranch], true
}

func (a *app) openRemoteBranchCheckout(remote repository.Remote, branch repository.RemoteBranch) {
	a.remoteLocalBranch = branch.Name
	a.showRemoteCheckoutForm = true
}

func (a *app) beginSetUpstream(remote repository.Remote, branch repository.RemoteBranch) {
	localBranch := a.currentBranchName()
	if a.actionBusy || a.session == nil || localBranch == "" {
		return
	}
	a.confirmAttention("Set upstream", localBranch+" → "+remote.Name+"/"+branch.Name, "This changes branch tracking configuration and affects future pulls and pushes.", func() {
		a.beginRepositoryActionMessage(trf("%s now tracks %s/%s", localBranch, remote.Name, branch.Name), "",
			func(ctx context.Context, session repository.Session) error {
				return session.SetUpstream(ctx, remote.Name, branch.Name, localBranch)
			}, nil)
	})
}

func (a *app) setSelectedRemoteBranchUpstream() {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.beginSetUpstream(remote, branch)
	}
}

func (a *app) checkoutSelectedRemoteBranch() {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.openRemoteBranchCheckout(remote, branch)
	}
}

func (a *app) confirmCheckoutRemoteBranch(remote repository.Remote, branch repository.RemoteBranch) {
	qualifiedName := remote.Name + "/" + branch.Name
	a.confirmCheckoutCommit(repository.Commit{
		Hash:      qualifiedName,
		ShortHash: qualifiedName,
		Subject:   qualifiedName,
	})
}

func (a *app) confirmCheckoutSelectedRemoteBranch() {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.confirmCheckoutRemoteBranch(remote, branch)
	}
}

func remoteLocalBranch(remote repository.Remote, branch repository.RemoteBranch) repository.Branch {
	return repository.Branch{Name: remote.Name + "/" + branch.Name, DisplayName: remote.Name + "/" + branch.Name}
}

func (a *app) confirmMergeSelectedRemoteBranch() {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.confirmMergeBranch(remoteLocalBranch(remote, branch))
	}
}

func (a *app) confirmRebaseSelectedRemoteBranch() {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.confirmRebaseOntoBranch(remoteLocalBranch(remote, branch))
	}
}

func (a *app) confirmDeleteRemoteBranch(remote repository.Remote, branch repository.RemoteBranch) {
	if a.actionBusy || a.session == nil {
		return
	}
	qualifiedName := remote.Name + "/" + branch.Name
	a.askConfirm(confirmRequest{
		title:        "Delete Remote Branch",
		message:      trf("Delete %s on %s?", qualifiedName, remote.Name),
		detail:       trf("This deletes the branch on the remote. Local branch %s is kept, but anyone tracking the remote branch will lose it.", branch.Name),
		confirmLabel: "Delete remote branch",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Deleted remote branch %s", qualifiedName), "",
				func(ctx context.Context, session repository.Session) error {
					return session.DeleteRemoteBranch(ctx, remote.Name, branch.Name)
				}, nil)
		},
	})
}

func (a *app) confirmDeleteSelectedRemoteBranch() {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.confirmDeleteRemoteBranch(remote, branch)
	}
}

func (a *app) copySelectedRemoteBranch(services ui.Services) {
	if remote, branch, ok := a.currentRemoteBranchSelection(); ok {
		a.notifyCopied(services, "remote branch", remote.Name+"/"+branch.Name)
	}
}

// remoteBranchRowMenu keeps every action on the branch named by its filtered
// row. Selecting that row first also makes its detail controls agree.
func (a *app) remoteBranchRowMenu(m *ui.Menu, row ui.ListRow, services ui.Services, remote repository.Remote, branch repository.RemoteBranch) {
	if m.Item(tr("Checkout as local branch…")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
		a.selectRemoteBranchRow(row.Index)
		a.openRemoteBranchCheckout(remote, branch)
	}
	if m.Item(tr("Checkout detached…")).Shortcut(ui.Shift, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
		a.selectRemoteBranchRow(row.Index)
		a.confirmCheckoutRemoteBranch(remote, branch)
	}
	if a.snapshot.Branch != "" {
		qualified := remoteLocalBranch(remote, branch)
		m.Separator()
		if m.Item(tr("Merge into current…")).Shortcut(ui.Shift, ui.KeyM).Disabled(a.actionBusy).Chosen() {
			a.selectRemoteBranchRow(row.Index)
			a.confirmMergeBranch(qualified)
		}
		if m.Item(tr("Rebase current onto this…")).Shortcut(0, ui.KeyR).Disabled(a.actionBusy).Chosen() {
			a.selectRemoteBranchRow(row.Index)
			a.confirmRebaseOntoBranch(qualified)
		}
		if localBranch := a.currentBranchName(); localBranch != "" {
			if m.Item(trf("Set as upstream of %s", localBranch)).Shortcut(0, ui.KeyU).Disabled(a.actionBusy).Chosen() {
				a.selectRemoteBranchRow(row.Index)
				a.beginSetUpstream(remote, branch)
			}
		}
	}
	m.Separator()
	if m.Item(tr("Copy full branch name")).Shortcut(0, ui.KeyC).Chosen() {
		a.notifyCopied(services, "remote branch", remote.Name+"/"+branch.Name)
	}
	m.Separator()
	if m.Item(tr("Delete remote branch…")).Shortcut(ui.Shift, ui.KeyD).Disabled(a.actionBusy).Chosen() {
		a.selectRemoteBranchRow(row.Index)
		a.confirmDeleteRemoteBranch(remote, branch)
	}
}

func (a *app) confirmRemoveSelectedRemote() {
	if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
		return
	}
	a.confirmRemoveRemote(a.snapshot.Remotes[a.selectedRemote])
}

func (a *app) confirmRemoveRemote(remote repository.Remote) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Remove Remote",
		message:      trf("Remove remote %s?", remote.Name),
		detail:       "Local commits and branches are kept, but remote-tracking refs will be removed.",
		confirmLabel: "Remove",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Removed remote %s", remote.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.RemoveRemote(ctx, remote.Name)
				}, nil)
		},
	})
}

// remoteRowMenu is the right-click menu for a remote row.
func (a *app) remoteRowMenu(m *ui.Menu, row ui.ListRow, services ui.Services) {
	remote, ok := a.remoteAt(row.Index)
	if !ok {
		return
	}
	if m.Item(tr("Fetch remote")).Shortcut(0, ui.KeyF).Disabled(a.actionBusy).Chosen() {
		a.selectRemoteRow(row.Index)
		a.beginFetchRemote(remote)
	}
	if m.Item(tr("Edit remote…")).Shortcut(0, ui.KeyE).Disabled(a.actionBusy).Chosen() {
		a.selectRemoteRow(row.Index)
		a.openEditRemote(remote)
	}
	m.Separator()
	if len(remote.URLs) > 0 {
		if m.Item(tr("Copy URL")).Chosen() {
			a.notifyCopied(services, "remote URL", remote.URLs[0])
		}
		m.Separator()
	}
	if m.Item(tr("Remove remote…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
		a.selectRemoteRow(row.Index)
		a.confirmRemoveRemote(remote)
	}
}

func (a *app) remoteFormModal(c *ui.Context) {
	editing := a.remoteOriginalName != ""
	title, subtitle, submit := "Add Remote", "Connect another repository with a short name.", "Add remote"
	if editing {
		title, subtitle, submit = "Edit Remote", "Change this remote's name or fetch URL.", "Save changes"
	}
	a.repositoryFormModal(c, &a.showRemoteForm, 560, title, subtitle, remotesIcon, func() {
		ui.Column(c).Gap(10).Children(func() {
			ui.TextInput(c, &a.remoteName).Label(tr("Remote name")).
				Placeholder(tr("origin")).AutoFocus()
			ui.TextInput(c, &a.remoteURL).Label(tr("Remote URL")).
				Placeholder(tr("git@example.com:team/project.git"))
		})
		ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
			if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
				a.showRemoteForm = false
			}
			if ui.PrimaryButton(c, tr(submit)).
				Disabled(a.actionBusy || strings.TrimSpace(a.remoteName) == "" || strings.TrimSpace(a.remoteURL) == "").
				Clicked() {
				if editing {
					a.beginUpdateRemote()
				} else {
					a.beginAddRemote()
				}
			}
		})
	})
}

func (a *app) remotesView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Gap(10).Children(func() {
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionRemotes],
			"Expand list pane", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					paneTitleAction(c, remotesIcon, "Remotes", "Add remote…", "Connect another remote (N)",
						a.actionBusy, a.openAddRemote, func() {
							paneCollapseButton(c, &a.workspacePaneCollapsed[sectionRemotes], "Collapse list pane")
						})
					if len(a.snapshot.Remotes) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("No remotes configured")).TextColor(theme.TextMuted)
						})
						return
					}
					visible := a.visibleRemotes()
					listSearchField(c, &a.sectionSearch[sectionRemotes], &a.remoteFilter,
						"Search remotes", "Filter by name or URL", "Clear remote search",
						len(visible), len(a.snapshot.Remotes), func() { a.selectRemoteRow(0) }, a.navigateRemoteSearch)
					visible = a.visibleRemotes()
					if len(visible) == 0 {
						listSearchEmpty(c, "No remotes match this search")
						return
					}
					list := a.keyboardList(c, &a.remoteList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
						remote, _ := sourceRow(visible, a.snapshot.Remotes, row.Index)
						services := row.Context.Services()
						ui.Row(row.Context).Padding(7, 8).Children(func() {
							ui.Text(row.Context, remote.Name).Grow(1).SingleLine()
							ui.Badge(row.Context, fmt.Sprintf("%d", len(remote.Branches)))
						}).PopoverMenu(func(m *ui.Menu) { a.remoteRowMenu(m, row, services) })
					})
					if a.keyboardListChanged(&a.remoteList, list) {
						a.selectRemoteRow(a.selectedRemoteRow)
					}
					normalizeFilteredRow(visible, &a.selectedRemoteRow, &a.selectedRemote)
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
					if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Select a remote")).TextColor(theme.TextMuted)
						})
						return
					}
					remote := a.snapshot.Remotes[a.selectedRemote]
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, remote.Name).FontSize(18).Bold().Grow(1)
						flatIconButton(c, "Fetch remote", "Fetch branches from this remote (F)", fetchIcon, 14, 3, a.actionBusy, func() {
							a.beginFetchRemote(remote)
						})
						flatIconButton(c, "Edit remote…", "Edit this remote's name or fetch URL (E)", editIcon, 14, 3, a.actionBusy, func() {
							a.openEditRemote(remote)
						})
						flatIconButton(c, "Remove remote…", "Remove this remote and its remote-tracking refs (D)", trashIcon, 14, 3, a.actionBusy,
							a.confirmRemoveSelectedRemote)
					})
					for _, url := range remote.URLs {
						ui.Text(c, url).TextColor(theme.TextMuted).Selectable()
					}
					ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
						ui.Icon(c, branchIcon).FontSize(13).TextColor(theme.Accent)
						ui.Text(c, tr("Remote branches")).Bold().Grow(1)
						if strings.TrimSpace(a.remoteBranchFilter) == "" {
							ui.Badge(c, fmt.Sprintf("%d", len(remote.Branches)))
						} else {
							ui.Badge(c, fmt.Sprintf("%d / %d", len(a.visibleRemoteBranches()), len(remote.Branches)))
						}
					})
					if len(remote.Branches) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Fetch to discover remote branches")).TextColor(theme.TextMuted)
						})
						return
					}
					visible := a.visibleRemoteBranches()
					listSearchField(c, &a.remoteBranchSearch, &a.remoteBranchFilter,
						"Search remote branches", "Filter by branch name", "Clear remote branch search",
						len(visible), len(remote.Branches), func() {
							a.selectRemoteBranchRow(0)
							a.showRemoteCheckoutForm = false
						}, a.navigateRemoteBranchSearch)
					visible = a.visibleRemoteBranches()
					if len(visible) == 0 {
						listSearchEmpty(c, "No remote branches match this search")
						return
					}
					branches := a.keyboardList(c, &a.remoteBranches, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
						branch := visible[row.Index]
						ui.Row(row.Context).AlignItems(ui.Center).Gap(8).Padding(7, 8).Children(func() {
							ui.Icon(row.Context, branchIcon).FontSize(12).TextColor(secondaryRowColor(theme, row))
							ui.Text(row.Context, branch.branch.Name).Grow(1).SingleLine()
						}).
							PopoverMenu(func(m *ui.Menu) {
								a.remoteBranchRowMenu(m, row, row.Context.Services(), remote, branch.branch)
							})
					})
					if a.keyboardListChanged(&a.remoteBranches, branches) {
						a.selectRemoteBranchRow(a.selectedRemoteBranchRow)
						a.showRemoteCheckoutForm = false
					}
					if len(visible) > 0 && a.selectedRemoteBranchRow < 0 {
						a.selectRemoteBranchRow(0)
					}
					if a.selectedRemoteBranch >= 0 && a.selectedRemoteBranch < len(remote.Branches) {
						branch := remote.Branches[a.selectedRemoteBranch]
						ui.Row(c).AlignItems(ui.Center).Children(func() {
							ui.Spacer(c)
							if ui.PrimaryButton(c, tr("Checkout as local…")).Disabled(a.actionBusy).Clicked() {
								a.showRemoteCheckoutForm = true
								a.remoteLocalBranch = branch.Name
							}
						})
						if a.showRemoteCheckoutForm {
							ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
								ui.TextInput(c, &a.remoteLocalBranch).Label(tr("Local branch name")).Grow(1)
								if ui.Button(c, tr("Cancel checkout")).Disabled(a.actionBusy).Clicked() {
									a.showRemoteCheckoutForm = false
								}
								if ui.PrimaryButton(c, tr("Checkout remote")).Disabled(a.actionBusy || strings.TrimSpace(a.remoteLocalBranch) == "").Clicked() {
									a.beginCheckoutRemoteBranch()
								}
							})
						}
					}
				})
			})
	})
}
