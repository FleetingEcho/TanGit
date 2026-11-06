package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// commitHashRow keeps the copy action beside the value it copies. The hash
// yields the remaining width in a narrow pane rather than pushing the icon
// out of reach.
func (a *app) commitHashRow(c *ui.Context, commit repository.Commit, copyLabel string) {
	muted := c.Theme().TextMuted
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
		ui.Text(c, commit.Hash).TextColor(muted).SingleLine().
			Tooltip(commit.Hash).Selectable().Shrink(1).MinWidth(0)
		control := ui.ButtonBase(c).Padding(3).Shrink(0).Label(tr(copyLabel)).
			Tooltip(tr("Copy the full commit hash")).Disabled(a.actionBusy)
		control.Children(func() { ui.Icon(c, copyIcon).FontSize(14).TextColor(muted) })
		if control.Clicked() {
			a.notifyCopied(c.Services(), "commit hash", commit.Hash)
		}
	})
}

// commitActionIcons is the action bar of a commit: reset the branch to it,
// tag it, branch from it, or cherry-pick it. Every button has a tooltip, so
// the glyphs are never a guess.
func (a *app) commitActionIcons(c *ui.Context, commit repository.Commit) {
	a.commitActionIconsWithHistory(c, commit, true)
}

// commitActionIconsWithHistory hides operations that move or rewrite the
// checked-out branch while the detail pane browses a different branch.
func (a *app) commitActionIconsWithHistory(c *ui.Context, commit repository.Commit, historyEditable bool) {
	button := func(label, tooltip string, icon *ui.SVG, disabled bool, run func()) {
		b := ui.ButtonBase(c).Padding(3)
		clicked := b.Label(tr(label)).Tooltip(tr(tooltip)).Disabled(disabled || a.actionBusy).Clicked()
		b.Children(func() {
			ui.Icon(c, icon).FontSize(14)
		})
		if clicked {
			run()
		}
	}
	if historyEditable {
		button("Reword this commit", "Change this commit's message without adding staged changes",
			editIcon, a.session == nil, func() {
				a.openCommitRewordForm(commit)
			})
	}
	button("Checkout this commit", "Check out this commit in detached HEAD mode",
		historyIcon, a.session == nil, func() {
			a.confirmCheckoutCommit(commit)
		})
	if historyEditable {
		button("Soft reset to this commit", "Soft reset: move the branch here, keep the changes staged",
			softResetIcon, a.session == nil, func() {
				a.confirmResetCommit(commit, repository.ResetSoft)
			})
		button("Mixed reset to this commit", "Mixed reset: move the branch here, keep the changes unstaged",
			softResetIcon, a.session == nil, func() {
				a.confirmResetCommit(commit, repository.ResetMixed)
			})
		button("Hard reset to this commit", "Hard reset: move the branch here and discard every change",
			hardResetIcon, a.session == nil, func() {
				a.confirmResetCommit(commit, repository.ResetHard)
			})
	}
	button("Create a tag here", "Create a tag at this commit", tagsIcon, a.session == nil, func() {
		a.openCommitTagForm(commit)
	})
	button("Create a branch here", "Create a branch at this commit and check it out",
		branchIcon, a.session == nil, func() {
			a.openCommitBranchForm(commit)
		})
	button("Cherry-pick this commit", "Cherry-pick this commit onto the current branch",
		cherryPickIcon, a.session == nil, func() {
			a.confirmCommitActionFor(commit, false)
		})
	button("Compare commit with HEAD", "H · Compare this commit with HEAD",
		compareIcon, a.session == nil, func() { a.compareCommit(commit, false) })
	button("Compare parent with commit", "Shift+H · Compare this commit's first parent with the commit",
		compareIcon, a.session == nil || len(commit.Parents) == 0, func() { a.compareCommit(commit, true) })
}

// commitRowMenu adds the commit actions to a row's context menu, so they are
// reachable without the hover bar.
func (a *app) commitRowMenu(m *ui.Menu, commit repository.Commit, services ui.Services) {
	a.commitRowMenuWithHistory(m, commit, services, true)
}

// commitRowMenuWithHistory includes actions that rewrite the checked-out
// branch only in a view of that branch's history. A different branch can be
// browsed without checkout, where its commits still support the commit-local
// actions but not a rebase or reset of the branch that happens to be current.
func (a *app) commitRowMenuWithHistory(m *ui.Menu, commit repository.Commit, services ui.Services, historyEditable bool) {
	if a.workspaceTab == sectionHistory || a.workspaceTab == sectionGraph {
		if m.Item(tr("Add all files to Custom Patch")).Shortcut(ui.Ctrl, ui.KeyB).Disabled(a.actionBusy).Chosen() {
			a.beginAddCustomPatchCommit(commit)
		}
		if len(a.customPatch.entries) > 0 && m.Item(tr("Review Custom Patch")).Shortcut(ui.Shift, ui.KeyB).Disabled(a.actionBusy).Chosen() {
			a.openCustomPatchBuilder()
		}
		m.Separator()
		if m.Item(tr("Toggle batch selection")).Shortcut(ui.Ctrl, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
			a.toggleCommitForBatch(commit)
		}
		if a.commitSelection[commit.Hash] {
			a.commitBatchMenu(m)
		}
		m.Separator()
	}

	if m.Item(tr("Copy commit hash")).Chosen() {
		a.notifyCopied(services, "commit hash", commit.Hash)
	}
	m.Separator()
	if historyEditable {
		if m.Item(tr("Reword commit…")).Disabled(a.actionBusy).Chosen() {
			a.openCommitRewordForm(commit)
		}
		if m.Item(tr("Amend with staged changes…")).Shortcut(ui.Shift, ui.KeyA).Disabled(a.actionBusy).Chosen() {
			a.confirmAmendCommit(commit)
		}
		if m.Item(tr("Create fixup commit from staged changes…")).Disabled(a.actionBusy).Chosen() {
			a.confirmCreateFixupCommit(commit)
		}
		if m.Item(tr("Apply fixups from this commit through HEAD…")).Disabled(a.actionBusy).Chosen() {
			a.confirmApplyFixupCommits(commit)
		}
		m.Separator()
		if m.Item(tr("Squash into older commit…")).Shortcut(0, ui.KeyS).Disabled(a.actionBusy).Chosen() {
			a.confirmCombineCommit(commit, repository.CombineSquash)
		}
		if m.Item(tr("Fixup into older commit…")).Shortcut(0, ui.KeyF).Disabled(a.actionBusy).Chosen() {
			a.confirmCombineCommit(commit, repository.CombineFixup)
		}
		if m.Item(tr("Move commit toward HEAD")).Shortcut(ui.Ctrl, ui.KeyK).Disabled(a.actionBusy).Chosen() {
			a.confirmMoveCommit(commit, repository.MoveCommitNewer)
		}
		if m.Item(tr("Move commit toward older history")).Shortcut(ui.Ctrl, ui.KeyJ).Disabled(a.actionBusy).Chosen() {
			a.confirmMoveCommit(commit, repository.MoveCommitOlder)
		}
		if m.Item(tr("Drop commit…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.confirmDropCommit(commit)
		}
		m.Separator()
	}
	if m.Item(tr("Checkout this commit (detached)…")).Disabled(a.actionBusy).Chosen() {
		a.confirmCheckoutCommit(commit)
	}
	if historyEditable {
		m.Separator()
		if m.Item(tr("Soft reset to this commit…")).Disabled(a.actionBusy).Chosen() {
			a.confirmResetCommit(commit, repository.ResetSoft)
		}
		if m.Item(tr("Mixed reset to this commit…")).Disabled(a.actionBusy).Chosen() {
			a.confirmResetCommit(commit, repository.ResetMixed)
		}
		if m.Item(tr("Hard reset to this commit…")).Disabled(a.actionBusy).Chosen() {
			a.confirmResetCommit(commit, repository.ResetHard)
		}
	}
	m.Separator()
	if m.Item(tr("Create a tag here…")).Disabled(a.actionBusy).Chosen() {
		a.openCommitTagForm(commit)
	}
	if m.Item(tr("Create a branch here…")).Disabled(a.actionBusy).Chosen() {
		a.openCommitBranchForm(commit)
	}
	m.Separator()
	if m.Item(tr("Cherry-pick onto current branch…")).Disabled(a.actionBusy).Chosen() {
		a.confirmCommitActionFor(commit, false)
	}
	if m.Item(tr("Revert commit…")).Disabled(a.actionBusy).Chosen() {
		a.confirmCommitActionFor(commit, true)
	}
	m.Separator()
	if m.Item(tr("Compare commit with HEAD")).Shortcut(0, ui.KeyH).Disabled(a.session == nil).Chosen() {
		a.compareCommit(commit, false)
	}
	if m.Item(tr("Compare first parent with commit")).Shortcut(ui.Shift, ui.KeyH).
		Disabled(a.session == nil || len(commit.Parents) == 0).Chosen() {
		a.compareCommit(commit, true)
	}
}

// confirmResetCommit asks before moving the branch, spelling out what the
// selected mode keeps in the index and working tree.
func (a *app) confirmResetCommit(commit repository.Commit, mode repository.ResetMode) {
	if a.actionBusy || a.session == nil {
		return
	}
	title := "Soft Reset"
	message := trf("Move %s to %s?", a.snapshot.Branch, commit.ShortHash)
	detail := "The commits after it are dropped, and their changes stay staged."
	button := "Soft reset"
	actionMessage := trf("Soft reset to %s", commit.ShortHash)
	destructive := false
	switch mode {
	case repository.ResetMixed:
		title = "Mixed Reset"
		detail = "The commits after it are dropped, and their changes stay in the working tree unstaged."
		button = "Mixed reset"
		actionMessage = trf("Mixed reset to %s", commit.ShortHash)
	case repository.ResetHard:
		title = "Hard Reset"
		message = trf("Move %s to %s and discard every change?", a.snapshot.Branch, commit.ShortHash)
		detail = "The commits after it, the staged changes, and the working tree changes are lost."
		button = "Hard reset"
		actionMessage = trf("Hard reset to %s", commit.ShortHash)
		destructive = true
	}
	a.askConfirm(confirmRequest{
		title:        title,
		message:      message,
		detail:       commit.Subject + "\n\n" + detail,
		confirmLabel: button,
		destructive:  destructive,
		run: func() {
			a.beginRepositoryActionMessage(actionMessage, "",
				func(ctx context.Context, session repository.Session) error {
					return session.ResetToCommit(ctx, commit, mode)
				}, nil)
		},
	})
}

// confirmCheckoutCommit makes the detached-HEAD consequence explicit before
// checking out a historical or reflog commit.
func (a *app) confirmCheckoutCommit(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Checkout Commit",
		message:      trf("Check out %s in detached HEAD mode?", commit.ShortHash),
		detail:       commit.Subject + "\n\nCreate a branch afterward to keep commits made from this point.",
		confirmLabel: "Checkout commit",
		run: func() {
			a.beginRepositoryActionMessage(trf("Checked out %s", commit.ShortHash), "",
				func(ctx context.Context, session repository.Session) error {
					return session.CheckoutCommit(ctx, commit)
				}, nil)
		},
	})
}

// openCommitTagForm asks for a tag name for commit, then creates the tag.
func (a *app) openCommitTagForm(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.commitTagCommit = commit
	a.commitTagName = ""
	a.commitTagMessage = ""
	a.commitTagAnnotated = false
	a.showCommitTagForm = true
}

func (a *app) beginCommitTag() {
	name := strings.TrimSpace(a.commitTagName)
	if name == "" {
		return
	}
	commit := a.commitTagCommit
	annotated := a.commitTagAnnotated
	message := strings.TrimSpace(a.commitTagMessage)
	detail := trf("The tag is lightweight and points at %s.", commit.ShortHash)
	if annotated {
		detail = trf("The tag is annotated: %s", message)
	}
	a.askConfirm(confirmRequest{
		title:        "Create Tag",
		message:      trf("Create tag %s at %s?", name, commit.ShortHash),
		detail:       detail,
		confirmLabel: "Add tag",
		run: func() {
			a.beginRepositoryActionMessage(trf("Tagged %s as %s", commit.ShortHash, name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.CreateTag(ctx, name, commit.Hash, message, annotated)
				}, func() {
					a.showCommitTagForm = false
					a.commitTagName = ""
					a.commitTagMessage = ""
					a.selectedTag = selectedTagIndex(a.snapshot.Tags, name)
				})
		},
	})
}

// openCommitBranchForm asks for a branch name to start at commit.
func (a *app) openCommitBranchForm(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.commitBranchCommit = commit
	a.commitBranchName = ""
	a.showCommitBranchForm = true
}

// beginCommitBranch creates the branch at the commit and checks it out, as
// "create a branch from here" does in Git clients.
func (a *app) beginCommitBranch() {
	name := strings.TrimSpace(a.commitBranchName)
	if name == "" {
		return
	}
	commit := a.commitBranchCommit
	a.askConfirm(confirmRequest{
		title:        "Create Branch",
		message:      trf("Create branch %s at %s and check it out?", name, commit.ShortHash),
		detail:       "The working tree switches to the new branch.",
		confirmLabel: "Create branch",
		run: func() {
			a.beginRepositoryActionMessage(trf("Checked out new branch %s", name), "",
				func(ctx context.Context, session repository.Session) error {
					if err := session.CreateBranch(ctx, name, commit.Hash); err != nil {
						return err
					}
					return session.CheckoutBranch(ctx, name)
				}, func() {
					a.showCommitBranchForm = false
					a.commitBranchName = ""
					a.selectedBranch = selectedBranchIndex(a.snapshot.Branches, name)
					a.syncBranchRow()
				})
		},
	})
}

// commitActionModals draws the small name forms the commit actions open, over
// the workspace.
func (a *app) commitActionModals(c *ui.Context) {
	// The question a form's submit asked is drawn over the workspace instead
	// of the form, so the two overlays never compete for the click that
	// answers it; cancelling brings the form back as it was.
	if a.confirm != nil {
		return
	}
	if a.showCommitRewordForm {
		a.commitRewordModal(c)
		return
	}
	if !a.showCommitTagForm && !a.showCommitBranchForm {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(480).MaxWidthPercent(92).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Children(func() {
				if a.showCommitTagForm {
					commit := a.commitTagCommit
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Icon(c, tagsIcon).FontSize(18).TextColor(theme.Accent)
						ui.Column(c).Gap(1).Grow(1).Children(func() {
							ui.Text(c, tr("Create a tag")).FontSize(17).Bold()
							ui.Textf(c, tr("at %s · %s"), commit.ShortHash, commit.Subject).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
								SingleLine().Tooltip(commit.Subject)
						})
						iconButton(c, "Close the tag form", "Escape", closeIcon, false, func() {
							a.showCommitTagForm = false
						})
					})
					ui.TextInput(c, &a.commitTagName).Label(tr("Tag name")).Placeholder(tr("v1.2.0"))
					ui.Checkbox(c, &a.commitTagAnnotated, tr("Annotated tag"))
					if a.commitTagAnnotated {
						ui.TextInput(c, &a.commitTagMessage).Label(tr("Tag message"))
					}
					ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
						if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
							a.showCommitTagForm = false
						}
						invalid := strings.TrimSpace(a.commitTagName) == "" ||
							(a.commitTagAnnotated && strings.TrimSpace(a.commitTagMessage) == "")
						if ui.PrimaryButton(c, tr("Create tag")).Disabled(a.actionBusy || invalid).Clicked() {
							a.beginCommitTag()
						}
					})
				} else {
					commit := a.commitBranchCommit
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Icon(c, branchIcon).FontSize(18).TextColor(theme.Accent)
						ui.Column(c).Gap(1).Grow(1).Children(func() {
							ui.Text(c, tr("Create a branch here")).FontSize(17).Bold()
							ui.Textf(c, tr("start at %s · %s"), commit.ShortHash, commit.Subject).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
								SingleLine().Tooltip(commit.Subject)
						})
						iconButton(c, "Close the branch form", "Escape", closeIcon, false, func() {
							a.showCommitBranchForm = false
						})
					})
					ui.TextInput(c, &a.commitBranchName).Label(tr("Branch name")).Placeholder(tr("feature/my-work"))
					ui.Text(c, tr("The branch is created at this commit and checked out.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
						if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
							a.showCommitBranchForm = false
						}
						if ui.PrimaryButton(c, tr("Create and checkout")).
							Disabled(a.actionBusy || strings.TrimSpace(a.commitBranchName) == "").Clicked() {
							a.beginCommitBranch()
						}
					})
				}
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showCommitTagForm, a.showCommitBranchForm = false, false
			}
		})
	})
}
