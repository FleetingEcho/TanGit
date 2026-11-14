package app

import (
	"context"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (a *app) beginCreateWorktree() {
	path := strings.TrimSpace(a.worktreePath)
	base := strings.TrimSpace(a.worktreeBase)
	branch := strings.TrimSpace(a.worktreeBranch)
	detach := a.worktreeDetach || a.worktreeTemporary
	if detach {
		branch = ""
	}
	if a.worktreeGuessRemote || a.worktreeOrphan {
		base = ""
	}
	options := repository.WorktreeCreateOptions{
		Path: path, Base: base, Branch: branch, Detach: detach,
		Force: a.worktreeForce, Track: a.worktreeTrack, GuessRemote: a.worktreeGuessRemote,
		Lock: a.worktreeLock, LockReason: a.worktreeLockReason,
		Orphan: a.worktreeOrphan, NoCheckout: a.worktreeNoCheckout,
		SparsePaths: splitWorktreePaths(a.worktreeSparsePaths),
	}
	a.beginRepositoryActionMessage(trf("Added worktree at %s", path), "",
		func(ctx context.Context, session repository.Session) error {
			return session.CreateWorktree(ctx, options)
		}, func() {
			a.selectedWorktree = selectedWorktreeIndex(a.snapshot.Worktrees, path)
			a.syncWorktreeRow()
			a.worktreePath = ""
			a.worktreeBase = ""
			a.worktreeBranch = ""
			a.worktreeDetach = false
			a.worktreeForce = false
			a.worktreeTrack = false
			a.worktreeGuessRemote = false
			a.worktreeLock = false
			a.worktreeLockReason = ""
			a.worktreeOrphan = false
			a.worktreeNoCheckout = false
			a.worktreeSparsePaths = ""
			a.worktreeTemporary = false
			a.showWorktreeForm = false
		})
}

func (a *app) worktreeRefOptions() []string {
	options := []string{"HEAD"}
	seen := map[string]bool{"HEAD": true}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			options = append(options, value)
		}
	}
	for _, branch := range a.snapshot.Branches {
		add(branch.Name)
	}
	for _, remote := range a.snapshot.Remotes {
		for _, branch := range remote.Branches {
			add(remote.Name + "/" + branch.Name)
		}
	}
	for _, tag := range a.snapshot.Tags {
		add(tag.Name)
	}
	return options
}

func worktreePathSlug(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "/\\")
	value = strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(value)
	if value == "" || value == "HEAD" {
		return "worktree"
	}
	return value
}

func (a *app) suggestedWorktreePath() string {
	name := strings.TrimSpace(a.worktreeBranch)
	if name == "" {
		name = strings.TrimSpace(a.worktreeBase)
	}
	if a.worktreeTemporary {
		name = "tmp-" + worktreePathSlug(name)
	}
	name = worktreePathSlug(name)
	if a.location.IsRemote() {
		root := path.Clean(a.snapshot.Root)
		return path.Join(path.Dir(root), path.Base(root)+"-"+name)
	}
	root := filepath.Clean(a.snapshot.Root)
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+name)
}

func (a *app) beginTemporaryWorktree() {
	a.worktreeBase = "HEAD"
	a.worktreeTemporary = true
	a.worktreeDetach = false
	a.worktreeOrphan = false
	a.worktreeBranch = ""
	a.worktreePath = a.suggestedWorktreePath()
	a.showWorktreeForm = true
}

func (a *app) openSelectedWorktree() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	a.openWorktree(a.snapshot.Worktrees[a.selectedWorktree])
}

func (a *app) selectedWorktreeValue() (repository.Worktree, bool) {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return repository.Worktree{}, false
	}
	return a.snapshot.Worktrees[a.selectedWorktree], true
}

func (a *app) openSelectedWorktreeInEditor() {
	worktree, ok := a.selectedWorktreeValue()
	if !ok || worktree.Missing {
		return
	}
	if a.location.IsRemote() {
		a.openWorktree(worktree)
		return
	}
	a.launchEditor(worktree.Path, 0)
}

func (a *app) revealSelectedWorktree() {
	worktree, ok := a.selectedWorktreeValue()
	if !ok || worktree.Missing || a.location.IsRemote() {
		return
	}
	revealCodeItemInFolder(worktree.Path)
}

func (a *app) openSelectedWorktreeTerminal() {
	worktree, ok := a.selectedWorktreeValue()
	if !ok || worktree.Missing {
		return
	}
	if a.location.IsRemote() {
		a.openWorktree(worktree)
		return
	}
	if err := launchWorktreeTerminal(worktree.Path); err != nil {
		a.notify("error", "Could not open terminal", safeError(err))
	}
}

func (a *app) compareSelectedWorktree() {
	worktree, ok := a.selectedWorktreeValue()
	if !ok || strings.TrimSpace(worktree.Head) == "" {
		return
	}
	a.openCompare()
	a.compare.from = "HEAD"
	a.compare.to = worktree.Head
	a.beginCompare()
}

func (a *app) fetchSelectedWorktree() {
	worktree, ok := a.selectedWorktreeValue()
	if !ok || worktree.Missing || a.actionBusy {
		return
	}
	a.beginRepositoryActionMessage(trf("Fetched %s", worktree.Name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.FetchWorktree(ctx, worktree.Path)
		}, nil)
}

func (a *app) confirmPullSelectedWorktree() {
	worktree, ok := a.selectedWorktreeValue()
	if !ok || worktree.Missing || a.actionBusy {
		return
	}
	a.confirmAttention("Pull Worktree", worktree.Name,
		"This fast-forwards the selected worktree and changes its checked-out files. Review its local changes first.", func() {
			a.beginRepositoryActionMessage(trf("Pulled %s", worktree.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.PullWorktree(ctx, worktree.Path)
				}, nil)
		})
}

func (a *app) openWorktree(worktree repository.Worktree) {
	if worktree.Missing {
		return
	}
	a.path = a.worktreeOpenPath(worktree.Path)
	a.beginOpen()
}

// worktreeOpenPath keeps a linked worktree on the source repository's host.
// Snapshot worktree paths are native POSIX paths for SSH repositories, not
// local paths to pass back to the opener.
func (a *app) worktreeOpenPath(worktreePath string) string {
	if !a.location.IsRemote() {
		return worktreePath
	}
	location := a.location
	location.Root = path.Clean(worktreePath)
	return location.String()
}

func (a *app) confirmDeleteSelectedWorktree() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	a.confirmDeleteWorktree(a.snapshot.Worktrees[a.selectedWorktree])
}

func (a *app) confirmDeleteWorktree(worktree repository.Worktree) {
	a.confirmRemoveWorktree(worktree, false)
}

func (a *app) confirmForceDeleteSelectedWorktree() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	a.confirmRemoveWorktree(a.snapshot.Worktrees[a.selectedWorktree], true)
}

func (a *app) confirmRemoveWorktree(worktree repository.Worktree, force bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	if worktree.Current || worktree.Main {
		return
	}
	title := "Remove Worktree"
	detail := trf("Git will remove the checkout at %s. Uncommitted changes prevent removal.", worktree.Path)
	label := "Remove"
	if force {
		title = "Force Remove Worktree"
		detail = trf("Force removal deletes the checkout at %s even when it has uncommitted changes. This cannot be undone.", worktree.Path)
		label = "Force remove"
	}
	a.askConfirm(confirmRequest{
		title:        title,
		message:      trf("Remove worktree %s?", worktree.Name),
		detail:       detail,
		confirmLabel: label,
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Removed worktree %s", worktree.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.DeleteWorktree(ctx, worktree.Path, force)
				}, nil)
		},
	})
}

func (a *app) toggleSelectedWorktreeLock() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	a.toggleWorktreeLock(a.snapshot.Worktrees[a.selectedWorktree])
}

func (a *app) toggleWorktreeLock(worktree repository.Worktree) {
	if a.actionBusy || a.session == nil || worktree.Main {
		return
	}
	if worktree.Locked {
		a.beginRepositoryActionMessage(trf("Unlocked worktree %s", worktree.Name), "",
			func(ctx context.Context, session repository.Session) error {
				return session.UnlockWorktree(ctx, worktree.Path)
			}, nil)
		return
	}
	a.worktreeLockTarget = worktree.Path
	a.worktreeLockTargetName = worktree.Name
	a.worktreeLockReason = strings.TrimSpace(worktree.LockReason)
	if a.worktreeLockReason == "" {
		a.worktreeLockReason = "TanGit"
	}
	a.showWorktreeLockForm = true
}

func (a *app) beginLockWorktree() {
	path := strings.TrimSpace(a.worktreeLockTarget)
	name := a.worktreeLockTargetName
	reason := strings.TrimSpace(a.worktreeLockReason)
	if path == "" {
		return
	}
	a.beginRepositoryActionMessage(trf("Locked worktree %s", name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.LockWorktree(ctx, path, reason)
		}, func() {
			a.showWorktreeLockForm = false
			a.worktreeLockTarget = ""
			a.worktreeLockTargetName = ""
			a.worktreeLockReason = ""
		})
}

func (a *app) repairWorktrees() {
	if a.actionBusy || a.session == nil {
		return
	}
	a.showWorktreeRepairForm = true
}

func splitWorktreePaths(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == '\r' || r == ';' })
}

func (a *app) beginRepairWorktrees() {
	paths := splitWorktreePaths(a.worktreeRepairPaths)
	a.beginRepositoryActionMessage(tr("Repaired worktree metadata"), "",
		func(ctx context.Context, session repository.Session) error {
			return session.RepairWorktrees(ctx, paths)
		}, func() {
			a.showWorktreeRepairForm = false
			a.worktreeRepairPaths = ""
		})
}

func (a *app) confirmPruneWorktrees() {
	if a.actionBusy || a.session == nil {
		return
	}
	a.showWorktreePruneForm = true
	a.beginWorktreePrunePreview()
}

func (a *app) beginWorktreePrunePreview() {
	if a.session == nil || a.worktreePruneLoading {
		return
	}
	a.worktreePruneToken++
	token, generation := a.worktreePruneToken, a.generation
	expire := strings.TrimSpace(a.worktreePruneExpire)
	session := a.session
	a.worktreePruneLoading = true
	a.worktreePrunePreview = nil
	a.worktreePruneError = ""
	go func() {
		defer a.recoverBackground()
		lines, err := session.PreviewPruneWorktrees(context.Background(), expire)
		a.update(func() {
			if token != a.worktreePruneToken || generation != a.generation || !a.showWorktreePruneForm {
				return
			}
			a.worktreePruneLoading = false
			if err != nil {
				a.worktreePruneError = safeError(err)
				return
			}
			a.worktreePrunePreview = lines
		})
	}()
}

func (a *app) beginPruneWorktrees() {
	expire := strings.TrimSpace(a.worktreePruneExpire)
	a.beginRepositoryActionMessage(tr("Pruned stale worktree records"), "",
		func(ctx context.Context, session repository.Session) error {
			return session.PruneWorktrees(ctx, expire)
		}, func() {
			a.showWorktreePruneForm = false
			a.worktreePrunePreview = nil
			a.worktreePruneError = ""
		})
}

func (a *app) openSelectedWorktreeMoveForm() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	worktree := a.snapshot.Worktrees[a.selectedWorktree]
	if worktree.Main || worktree.Current || worktree.Missing {
		return
	}
	a.worktreeMovePath = worktree.Path
	a.worktreeMoveForce = false
	a.showWorktreeMoveForm = true
}

func (a *app) beginMoveWorktree() {
	if a.selectedWorktree < 0 || a.selectedWorktree >= len(a.snapshot.Worktrees) {
		return
	}
	worktree := a.snapshot.Worktrees[a.selectedWorktree]
	newPath := strings.TrimSpace(a.worktreeMovePath)
	force := a.worktreeMoveForce
	a.beginRepositoryActionMessage(trf("Moved worktree %s", worktree.Name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.MoveWorktree(ctx, worktree.Path, newPath, force)
		}, func() {
			a.showWorktreeMoveForm = false
			a.worktreeMovePath = ""
			a.selectedWorktree = selectedWorktreeIndex(a.snapshot.Worktrees, newPath)
			a.syncWorktreeRow()
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
	if !worktree.Missing {
		if m.Item(tr("Open in editor")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.openSelectedWorktreeInEditor()
		}
		if m.Item(tr("Open terminal here")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.openSelectedWorktreeTerminal()
		}
		if !a.location.IsRemote() && m.Item(tr("Show in file manager")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.revealSelectedWorktree()
		}
	}
	m.Separator()
	if m.Item(tr("Copy path")).Chosen() {
		a.notifyCopied(services, "worktree path", worktree.Path)
	}
	if !worktree.Main {
		label := "Lock worktree"
		if worktree.Locked {
			label = "Unlock worktree"
		}
		if m.Item(tr(label)).Shortcut(0, ui.KeyL).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.toggleWorktreeLock(worktree)
		}
	}
	if !worktree.Missing {
		m.Separator()
		if m.Item(tr("Compare with current worktree")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.compareSelectedWorktree()
		}
		if m.Item(tr("Fetch worktree")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.fetchSelectedWorktree()
		}
		if m.Item(tr("Pull worktree…")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.confirmPullSelectedWorktree()
		}
	}
	if !worktree.Current && !worktree.Main {
		m.Separator()
		if !worktree.Missing && m.Item(tr("Move worktree…")).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.openSelectedWorktreeMoveForm()
		}
		if m.Item(tr("Remove worktree…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.confirmDeleteWorktree(worktree)
		}
		if m.Item(tr("Force remove worktree…")).Shortcut(ui.Shift, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.selectWorktreeRow(row.Index)
			a.confirmRemoveWorktree(worktree, true)
		}
	}
}

func (a *app) worktreeFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showWorktreeForm, 680, "Create Worktree",
		"Check out another branch beside this repository.", worktreesIcon, func() {
			ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
				ui.TextInput(c, &a.worktreePath).Label(tr("Worktree path")).
					Placeholder(tr("/path/to/worktree")).AutoFocus().Grow(1)
				if ui.Button(c, tr("Use suggested path")).Clicked() {
					a.worktreePath = a.suggestedWorktreePath()
				}
			})
			if !a.worktreeOrphan && !a.worktreeGuessRemote {
				refs := a.worktreeRefOptions()
				if strings.TrimSpace(a.worktreeBase) == "" {
					a.worktreeBase = "HEAD"
				}
				ui.Select(c, &a.worktreeBase, refs).Label(tr("Base ref")).Grow(1)
			}
			if !a.worktreeDetach && !a.worktreeTemporary {
				ui.TextInput(c, &a.worktreeBranch).Label(tr("New branch (optional)")).
					Placeholder(tr("feature/my-work"))
			}
			ui.Row(c).Gap(14).Children(func() {
				ui.Checkbox(c, &a.worktreeTemporary, tr("Temporary detached worktree"))
				ui.Checkbox(c, &a.worktreeDetach, tr("Detached HEAD")).Disabled(a.worktreeTemporary || a.worktreeOrphan)
				ui.Checkbox(c, &a.worktreeOrphan, tr("Orphan branch")).Disabled(a.worktreeTemporary || a.worktreeDetach)
			})
			ui.Row(c).Gap(14).Children(func() {
				ui.Checkbox(c, &a.worktreeGuessRemote, tr("Guess remote branch")).Disabled(a.worktreeOrphan)
				ui.Checkbox(c, &a.worktreeTrack, tr("Track base branch")).Disabled(a.worktreeDetach || a.worktreeTemporary || a.worktreeOrphan)
				ui.Checkbox(c, &a.worktreeNoCheckout, tr("Do not checkout files"))
			})
			ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Checkbox(c, &a.worktreeLock, tr("Lock after creation"))
				if a.worktreeLock {
					ui.TextInput(c, &a.worktreeLockReason).Label(tr("Lock reason")).Placeholder(tr("Portable drive or long-running task")).Grow(1)
				}
				ui.Checkbox(c, &a.worktreeForce, tr("Override Git safeguards"))
			})
			ui.TextArea(c, &a.worktreeSparsePaths).Label(tr("Sparse checkout paths (optional)")).Lines(2, 4).
				Placeholder(tr("Enter one repository-relative path per line"))
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
					a.showWorktreeForm = false
				}
				invalid := strings.TrimSpace(a.worktreePath) == "" ||
					(!a.worktreeOrphan && !a.worktreeGuessRemote && strings.TrimSpace(a.worktreeBase) == "") ||
					(a.worktreeOrphan && strings.TrimSpace(a.worktreeBranch) == "")
				if ui.PrimaryButton(c, tr("Create worktree")).Disabled(a.actionBusy || invalid).Clicked() {
					a.beginCreateWorktree()
				}
			})
		})
}

func (a *app) worktreeMoveFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showWorktreeMoveForm, 600, "Move Worktree",
		"Move the linked checkout and update Git's administrative metadata.", worktreesIcon, func() {
			ui.TextInput(c, &a.worktreeMovePath).Label(tr("New worktree path")).Placeholder(tr("/new/path/to/worktree")).AutoFocus()
			ui.Checkbox(c, &a.worktreeMoveForce, tr("Override locked or stale destination safeguards"))
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Clicked() {
					a.showWorktreeMoveForm = false
				}
				if ui.PrimaryButton(c, tr("Move worktree")).Disabled(a.actionBusy || strings.TrimSpace(a.worktreeMovePath) == "").Clicked() {
					a.beginMoveWorktree()
				}
			})
		})
}

func (a *app) worktreeLockFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showWorktreeLockForm, 560, "Lock Worktree",
		"Protect this worktree from pruning, moving, or removal.", worktreesIcon, func() {
			ui.TextInput(c, &a.worktreeLockReason).Label(tr("Lock reason")).Placeholder(tr("Portable drive or long-running task")).AutoFocus()
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Clicked() {
					a.showWorktreeLockForm = false
				}
				if ui.PrimaryButton(c, tr("Lock worktree")).Disabled(a.actionBusy).Clicked() {
					a.beginLockWorktree()
				}
			})
		})
}

func (a *app) worktreeRepairFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showWorktreeRepairForm, 620, "Repair Worktrees",
		"Repair links after the main repository or linked checkouts were moved.", worktreesIcon, func() {
			ui.TextArea(c, &a.worktreeRepairPaths).Label(tr("Moved worktree paths (optional)"))
			ui.Text(c, tr("Enter one new path per line. Leave empty for automatic repair.")).TextColor(c.Theme().TextMuted)
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Clicked() {
					a.showWorktreeRepairForm = false
				}
				if ui.PrimaryButton(c, tr("Repair worktrees")).Disabled(a.actionBusy).Clicked() {
					a.beginRepairWorktrees()
				}
			})
		})
}

func (a *app) worktreePruneFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showWorktreePruneForm, 680, "Prune Worktrees",
		"Preview stale administrative records before removing them.", worktreesIcon, func() {
			ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
				ui.TextInput(c, &a.worktreePruneExpire).Label(tr("Only records older than")).Placeholder(tr("now, 30.days.ago, or never")).Grow(1)
				if ui.Button(c, tr("Refresh preview")).Disabled(a.worktreePruneLoading).Clicked() {
					a.beginWorktreePrunePreview()
				}
			})
			switch {
			case a.worktreePruneLoading:
				ui.Text(c, tr("Inspecting stale worktree records…")).TextColor(c.Theme().TextMuted)
			case a.worktreePruneError != "":
				ui.Text(c, a.worktreePruneError).TextColor(c.Theme().Danger).Selectable()
			case len(a.worktreePrunePreview) == 0:
				ui.Text(c, tr("No stale worktree records match this expiration.")).TextColor(c.Theme().TextMuted)
			default:
				ui.Scroll(c).MaxHeight(220).FillWidth().Children(func() {
					ui.Column(c).Gap(4).Children(func() {
						for _, line := range a.worktreePrunePreview {
							ui.Text(c, line).Selectable()
						}
					})
				})
			}
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Clicked() {
					a.showWorktreePruneForm = false
					a.worktreePruneToken++
				}
				if ui.PrimaryButton(c, tr("Prune shown records")).Disabled(a.actionBusy || a.worktreePruneLoading || len(a.worktreePrunePreview) == 0).Clicked() {
					a.beginPruneWorktrees()
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
					ui.Row(c).Gap(8).Children(func() {
						if ui.Button(c, tr("New temporary…")).Disabled(a.actionBusy).Clicked() {
							a.beginTemporaryWorktree()
						}
						if ui.Button(c, tr("Repair metadata")).Disabled(a.actionBusy).Clicked() {
							a.repairWorktrees()
						}
						if ui.Button(c, tr("Prune stale records…")).Disabled(a.actionBusy).Clicked() {
							a.confirmPruneWorktrees()
						}
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
					list := a.keyboardList(c, &a.worktreeList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
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
							if worktree.Locked {
								ui.Badge(row.Context, tr("Locked"))
							}
							if worktree.Prunable {
								ui.Badge(row.Context, tr("Stale"))
							}
							if dirty := worktree.Staged + worktree.Modified + worktree.Untracked; dirty > 0 {
								ui.Badge(row.Context, trf("%d changes", dirty))
							}
							if worktree.Ahead > 0 {
								ui.Badge(row.Context, trf("↑%d", worktree.Ahead))
							}
							if worktree.Behind > 0 {
								ui.Badge(row.Context, trf("↓%d", worktree.Behind))
							}
						}).PopoverMenu(func(m *ui.Menu) { a.worktreeRowMenu(m, row, services) })
					})
					if a.keyboardListChanged(&a.worktreeList, list) {
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
						if !worktree.Main {
							lockLabel := "Lock"
							if worktree.Locked {
								lockLabel = "Unlock"
							}
							if ui.Button(c, tr(lockLabel)).Disabled(a.actionBusy).Clicked() {
								a.toggleSelectedWorktreeLock()
							}
						}
					})
					ui.Text(c, worktree.Path).TextColor(theme.TextMuted).Selectable()
					ui.Row(c).Gap(8).Children(func() {
						if ui.Button(c, tr("Open in editor")).Disabled(a.actionBusy || worktree.Missing).Clicked() {
							a.openSelectedWorktreeInEditor()
						}
						if ui.Button(c, tr("Terminal")).Disabled(a.actionBusy || worktree.Missing).Clicked() {
							a.openSelectedWorktreeTerminal()
						}
						if ui.Button(c, tr("Compare")).Disabled(a.actionBusy || worktree.Head == "").Clicked() {
							a.compareSelectedWorktree()
						}
						if !worktree.Current && !worktree.Main && !worktree.Missing {
							if ui.Button(c, tr("Move…")).Disabled(a.actionBusy).Clicked() {
								a.openSelectedWorktreeMoveForm()
							}
						}
					})
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
					if worktree.Locked {
						reason := strings.TrimSpace(worktree.LockReason)
						if reason == "" {
							reason = tr("No reason provided")
						}
						ui.Textf(c, tr("Locked: %s"), reason).TextColor(theme.Warning)
					}
					if worktree.Prunable {
						reason := strings.TrimSpace(worktree.PruneReason)
						if reason == "" {
							reason = tr("The administrative record is stale")
						}
						ui.Textf(c, tr("Prunable: %s"), reason).TextColor(theme.Warning)
					}
					if worktree.Branch != "" {
						ui.Textf(c, tr("Branch: %s"), worktree.Branch)
					} else {
						ui.Textf(c, tr("Detached at %s"), worktree.Head)
					}
					if worktree.OperationLive {
						ui.Text(c, tr("A rebase or bisect is active in this worktree.")).TextColor(theme.Warning)
					}
					ui.Textf(c, tr("Changes: %d staged · %d modified · %d untracked"), worktree.Staged, worktree.Modified, worktree.Untracked)
					if worktree.Ahead > 0 || worktree.Behind > 0 {
						ui.Textf(c, tr("Upstream: %d ahead · %d behind"), worktree.Ahead, worktree.Behind)
					}
					if worktree.LastSubject != "" {
						when := ""
						if worktree.LastCommitAt > 0 {
							when = time.Unix(worktree.LastCommitAt, 0).Format("2006-01-02 15:04") + " · "
						}
						ui.Text(c, when+worktree.LastSubject).TextColor(theme.TextMuted).Selectable()
					}
				})
			})
	})
}
