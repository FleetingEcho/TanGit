package app

import (
	"context"
	"errors"
	"sort"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// stackSyncUIState stores immutable plans/results, never MyGo elements or a
// UI context. One token rejects both obsolete Provide reads and Apply commits.
type stackSyncUIState struct {
	open       bool
	loading    bool
	applying   bool
	plan       *repository.StackSyncPlan
	result     *repository.StackSyncResult
	planError  string
	applyError string
	mode       string
	publish    bool
	token      uint64
}

func (s *stackSyncUIState) reset() {
	s.token++
	s.open = false
	s.loading = false
	s.applying = false
	s.plan = nil
	s.result = nil
	s.planError = ""
	s.applyError = ""
	s.mode = ""
	s.publish = false
}

func (a *app) openStackSync() {
	if a.actionBusy || a.session == nil {
		return
	}
	a.stackSync.open = true
	a.stackSync.applyError = ""
	if a.stackSync.mode == "" {
		a.stackSync.mode = string(repository.StackSyncRebase)
		a.stackSync.publish = true
	}
	a.requestStackSyncPlan()
}

func (a *app) closeStackSync() {
	if a.stackSync.applying {
		return
	}
	a.stackSync.token++
	a.stackSync.open = false
	a.stackSync.loading = false
	a.stackSync.plan = nil
	a.stackSync.result = nil
	a.stackSync.planError = ""
	a.stackSync.applyError = ""
}

// requestStackSyncPlan is the UI's Provide boundary. It deliberately reads
// through the fresh-provider entry point every time, including option changes
// and stale-plan recovery; no cached Snapshot or selected branch substitutes
// for live OIDs, topology, worktrees, or operation metadata.
func (a *app) requestStackSyncPlan() {
	if !a.stackSync.open || a.stackSync.applying || a.session == nil {
		return
	}
	sync, ok := a.session.(repository.StackSyncSession)
	if !ok {
		a.stackSync.loading = false
		a.stackSync.plan = nil
		a.stackSync.planError = "this repository session does not support stack sync plans"
		return
	}
	mode := repository.StackSyncMode(a.stackSync.mode)
	if mode == "" {
		mode = repository.StackSyncRebase
	}
	a.stackSync.mode = string(mode)
	options := repository.StackSyncOptions{Mode: mode, Publish: a.stackSync.publish, Fetch: true}
	generation := a.generation
	a.stackSync.token++
	token := a.stackSync.token
	a.stackSync.loading = true
	a.stackSync.plan = nil
	a.stackSync.result = nil
	a.stackSync.planError = ""
	go func() {
		defer a.recoverBackground()
		plan, err := sync.ProvideStackSync(context.Background(), options)
		a.updateChanged(func() bool {
			if generation != a.generation || token != a.stackSync.token {
				return false
			}
			a.stackSync.loading = false
			if err != nil {
				a.stackSync.planError = safeError(err)
				return a.stackSync.open
			}
			// plan is a fresh immutable value from this Provide call. Retain
			// an independent copy so later UI requests cannot overwrite it.
			provided := plan
			a.stackSync.plan = &provided
			return a.stackSync.open
		})
	}()
}

func (a *app) beginApplyStackSync() {
	if !a.stackSync.open || a.stackSync.loading || a.stackSync.applying || a.actionBusy || a.stackSync.plan == nil || !a.stackSync.plan.CanApply() {
		return
	}
	sync, ok := a.session.(repository.StackSyncSession)
	if !ok {
		a.stackSync.applyError = "this repository session cannot apply stack sync plans"
		return
	}
	// Apply captures this exact plan, not a branch name or mutable UI option.
	plan := *a.stackSync.plan
	session := a.session
	generation := a.generation
	a.stackSync.token++
	token := a.stackSync.token
	a.stackSync.applying = true
	a.stackSync.applyError = ""
	a.stackSync.result = nil
	a.actionBusy = true
	a.actionError = ""
	go func() {
		defer a.recoverBackground()
		ctx := context.Background()
		result, applyErr := sync.ApplyStackSync(ctx, plan)
		snapshot, snapshotErr := session.Snapshot(ctx)
		a.update(func() {
			if generation != a.generation || token != a.stackSync.token {
				return
			}
			a.stackSync.applying = false
			a.actionBusy = false
			// Every Apply attempt consumes the preview. Successful results are
			// shown separately; failures and CAS errors require a new source
			// read before another mutation can be approved.
			a.stackSync.plan = nil
			if len(result.Branches) > 0 || len(result.Pushes) > 0 {
				kept := result
				a.stackSync.result = &kept
			}
			if snapshotErr == nil {
				a.repositoryDetailsLoading = false
				a.snapshot = snapshot
				a.snapshotSeq++
				a.resetBranchCommits()
				a.resetCommitDetails()
				a.resetCommitPaging(snapshot)
			}
			if applyErr != nil {
				a.stackSync.applyError = safeError(applyErr)
				a.actionError = safeError(applyErr)
				if errors.Is(applyErr, repository.ErrStackSyncChanged) {
					a.stackSync.result = nil
					// Rejecting an old plan is not permission to keep displaying
					// it. Recompute immediately, preserving the visible reason.
					a.requestStackSyncPlan()
				}
				return
			}
			if snapshotErr != nil {
				a.stackSync.applyError = safeError(snapshotErr)
				a.actionError = safeError(snapshotErr)
			}
		})
	}()
}

func stackSyncShortOID(oid string) string {
	if len(oid) > 10 {
		return oid[:10]
	}
	if oid == "" {
		return "—"
	}
	return oid
}

func stackSyncRelationLabel(relation repository.StackSyncRelation) string {
	switch relation {
	case repository.StackSyncFastForward:
		return tr("ff")
	case repository.StackSyncRemoteRewritten:
		return tr("remote rewritten")
	case repository.StackSyncLocalAheadOnly:
		return tr("local ahead only")
	case repository.StackSyncUpToDate:
		return tr("up-to-date")
	case repository.StackSyncDiverged:
		return tr("diverged")
	case repository.StackSyncNoUpstream:
		return tr("no upstream")
	case repository.StackSyncUpstreamMissing:
		return tr("upstream missing")
	default:
		return string(relation)
	}
}

func stackSyncActionLabel(branch repository.StackSyncBranch) string {
	switch branch.Action {
	case repository.StackSyncActionUpdateRef:
		return trf("CAS update-ref → %s", stackSyncShortOID(branch.TargetOID))
	case repository.StackSyncActionMerge:
		if branch.Relation == repository.StackSyncFastForward {
			return trf("Real merge --ff-only → %s", stackSyncShortOID(branch.TargetOID))
		}
		return trf("Real merge → %s", stackSyncShortOID(branch.TargetOID))
	case repository.StackSyncActionRebase:
		return trf("Real rebase onto %s from %s", stackSyncShortOID(branch.TargetOID), stackSyncShortOID(branch.RebaseFromOID))
	case repository.StackSyncActionSkip:
		return tr("Skip local update")
	default:
		if branch.Publish {
			return tr("Publish with preview-OID lease")
		}
		return tr("No local change")
	}
}

func stackSyncMessageView(c *ui.Context, text string, danger bool) {
	if text == "" {
		return
	}
	color := c.Theme().Warning
	if danger {
		color = c.Theme().Danger
	}
	ui.Text(c, text).TextColor(color).FontSize(c.Theme().FontSize - 1).Selectable()
}

func stackSyncPlanBranchView(c *ui.Context, branch repository.StackSyncBranch) {
	theme := c.Theme()
	c.Key("stack-sync-plan-branch:" + branch.Name)
	ui.Column(c).FillWidth().Gap(4).Padding(9, 10).
		Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, branchIcon).FontSize(14).TextColor(theme.Accent)
			ui.Text(c, branch.Name).Bold().Grow(1).SingleLine().Tooltip(branch.Ref)
			ui.Badge(c, stackSyncRelationLabel(branch.Relation))
			if branch.Current {
				ui.Badge(c, tr("Current"))
			}
		})
		ui.Text(c, stackSyncActionLabel(branch)).TextColor(theme.Text).SingleLine().
			Tooltip("local " + branch.LocalOID + "\ntarget " + branch.TargetOID)
		ui.Row(c).Gap(10).Children(func() {
			ui.Text(c, trf("%s → %s", stackSyncShortOID(branch.LocalOID), stackSyncShortOID(branch.TargetOID))).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Tooltip(branch.LocalOID + "\n" + branch.TargetOID)
			if branch.Parent != "" {
				ui.Text(c, trf("parent %s", branch.Parent)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
					SingleLine().Tooltip(branch.ParentOID)
			}
			if branch.Ahead != 0 || branch.Behind != 0 {
				ui.Text(c, trf("ahead %d · behind %d", branch.Ahead, branch.Behind)).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		})
		if branch.Upstream.Ref != "" {
			ui.Text(c, trf("upstream %s/%s · %s", branch.Upstream.Remote, branch.Upstream.RemoteBranch, stackSyncShortOID(branch.Upstream.OID))).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Tooltip(branch.Upstream.Ref + "\n" + branch.Upstream.OID)
		}
		if branch.WorktreePath != "" {
			operation := ""
			if branch.LiveOperation != "" {
				operation = trf(" · live %s", branch.LiveOperation)
			}
			ui.Text(c, trf("worktree %s%s", branch.WorktreePath, operation)).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Tooltip(branch.WorktreePath)
		}
		if branch.MayUseUpdateRefs {
			ui.Text(c, tr("rebase.updateRefs enabled: Git's real TODO owns matching refs; the CAS batch will not update them again.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		for _, warning := range branch.Warnings {
			stackSyncMessageView(c, warning, false)
		}
		stackSyncMessageView(c, branch.Error, true)
	})
}

func stackSyncPlanView(c *ui.Context, plan repository.StackSyncPlan) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, trf("Base %s · %s", plan.MainBranch, stackSyncShortOID(plan.MainOID))).
			TextColor(theme.TextMuted).Tooltip(plan.MainOID)
		ui.Text(c, trf("Current %s · plan %s", plan.CurrentBranch, stackSyncShortOID(plan.SourceID))).
			TextColor(theme.TextMuted).SingleLine().Tooltip(plan.SourceID)
		if len(plan.Merged) > 0 {
			ui.Text(c, trf("Excluded %d branch%s already merged into %s.", len(plan.Merged), pluralS(len(plan.Merged)), plan.MainBranch)).
				TextColor(theme.TextMuted)
		}
		if plan.RebaseUpdateRefs {
			ui.Text(c, tr("rebase.updateRefs is enabled. Refs actually listed in Git's generated TODO will be reported as rebase-driven.")).
				TextColor(theme.TextMuted)
		}
		if len(plan.Branches) == 0 {
			ui.Text(c, tr("No unmerged local branches need review.")).TextColor(theme.TextMuted)
			return
		}
		for _, branch := range plan.Branches {
			stackSyncPlanBranchView(c, branch)
		}
	})
}

func stackSyncResultBranchesView(c *ui.Context, result repository.StackSyncResult) {
	theme := c.Theme()
	if len(result.Branches) == 0 {
		return
	}
	ui.Text(c, tr("Local branch results")).Bold()
	for _, branch := range result.Branches {
		c.Key("stack-sync-result-branch:" + branch.Name)
		ui.Column(c).FillWidth().Gap(3).Padding(7, 9).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
			title := branch.Name
			if branch.Updated {
				title += trf(" · %s → %s", stackSyncShortOID(branch.OldOID), stackSyncShortOID(branch.NewOID))
			} else if branch.Skipped {
				title += " · " + tr("skipped")
			}
			ui.Text(c, title).Bold().SingleLine().Tooltip(branch.OldOID + "\n" + branch.NewOID)
			if branch.Driver != "" {
				ui.Text(c, branch.Driver).TextColor(theme.Accent).FontSize(theme.FontSize - 1)
			}
			if branch.UpdateRefsTODO != "" {
				ui.Text(c, trf("Driven by actual rebase TODO: %s", branch.UpdateRefsTODO)).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Tooltip(branch.UpdateRefsTODO)
			}
			for _, warning := range branch.Warnings {
				stackSyncMessageView(c, warning, false)
			}
			stackSyncMessageView(c, branch.Error, true)
		})
	}
}

// stackSyncPushGroups is display-only grouping. Applying and recording pushes
// remains independent per branch, and no group claims remote atomicity.
func stackSyncPushGroups(pushes []repository.StackSyncPushResult) [][]repository.StackSyncPushResult {
	byRemote := map[string][]repository.StackSyncPushResult{}
	for _, push := range pushes {
		byRemote[push.Remote] = append(byRemote[push.Remote], push)
	}
	remotes := make([]string, 0, len(byRemote))
	for remote := range byRemote {
		remotes = append(remotes, remote)
	}
	sort.Strings(remotes)
	groups := make([][]repository.StackSyncPushResult, 0, len(remotes))
	for _, remote := range remotes {
		group := byRemote[remote]
		sort.SliceStable(group, func(i, j int) bool { return group[i].Branch < group[j].Branch })
		groups = append(groups, group)
	}
	return groups
}

func stackSyncPushResultsView(c *ui.Context, result repository.StackSyncResult) {
	theme := c.Theme()
	if len(result.Pushes) == 0 {
		return
	}
	ui.Text(c, tr("Remote publication · independent, not transactional")).Bold()
	for _, group := range stackSyncPushGroups(result.Pushes) {
		remote := group[0].Remote
		published := 0
		for _, push := range group {
			if push.Published {
				published++
			}
		}
		c.Key("stack-sync-push-remote:" + remote)
		ui.Column(c).FillWidth().Gap(5).Padding(8, 9).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Icon(c, remotesIcon).FontSize(14).TextColor(theme.Accent)
				ui.Text(c, remote).Bold().Grow(1)
				ui.Text(c, trf("%d/%d published", published, len(group))).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 1)
			})
			for _, push := range group {
				color := theme.Accent
				status := tr("Published")
				if !push.Published {
					color, status = theme.Danger, tr("Failed")
				}
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					ui.Textf(c, tr("%s · %s → %s/%s · %s"), status, push.Branch, push.Remote, push.RemoteBranch, stackSyncShortOID(push.LocalOID)).
						TextColor(color).SingleLine().Tooltip(push.LocalOID)
					ui.Text(c, trf("force-with-lease expected remote %s", stackSyncShortOID(push.ExpectedRemoteOID))).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Tooltip(push.ExpectedRemoteOID)
					if push.Error != "" {
						stackSyncMessageView(c, push.Error, true)
					}
				})
			}
		})
	}
	ui.Text(c, tr("Each remote branch was attempted independently. A failure does not roll back another remote or any local update.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
}

func (a *app) stackSyncModal(c *ui.Context) {
	if !a.stackSync.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(780).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(20).Gap(12).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Sync stack")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, branchesIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Sync stack")).Bold().FontSize(17)
						ui.Text(c, tr("Review the frozen Git operation for every branch.")).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close stack sync", "Escape", closeIcon, a.stackSync.applying, a.closeStackSync)
				})
				ui.Row(c).AlignItems(ui.End).Gap(12).Wrap().Children(func() {
					publishBefore, modeBefore := a.stackSync.publish, a.stackSync.mode
					ui.Checkbox(c, &a.stackSync.publish, tr("Publish changed branches")).
						Disabled(a.stackSync.loading || a.stackSync.applying || a.stackSync.result != nil)
					ui.Select(c, &a.stackSync.mode, []string{string(repository.StackSyncRebase), string(repository.StackSyncMerge)}).
						Label(tr("Divergence strategy")).Disabled(a.stackSync.loading || a.stackSync.applying || a.stackSync.result != nil)
					if publishBefore != a.stackSync.publish || modeBefore != a.stackSync.mode {
						a.stackSync.applyError = ""
						a.requestStackSyncPlan()
					}
				})
				if a.stackSync.publish && a.stackSync.result == nil {
					ui.Text(c, tr("Every publish uses force-with-lease bound to that preview's remote OID. Pushes are independent, not a remote transaction.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
				stackSyncMessageView(c, a.stackSync.planError, true)
				stackSyncMessageView(c, a.stackSync.applyError, true)
				if a.stackSync.loading {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Spinner(c).Label(tr("Building a fresh stack sync plan"))
						ui.Text(c, tr("Reading live refs, topology, upstreams, worktrees, and operations…")).
							TextColor(theme.TextMuted)
					})
				}
				ui.Scroll(c).Grow(1).Children(func() {
					ui.Column(c).FillWidth().Gap(8).Padding(0, 6, 0, 0).Children(func() {
						if a.stackSync.result != nil {
							failures := 0
							for _, push := range a.stackSync.result.Pushes {
								if !push.Published {
									failures++
								}
							}
							title := tr("Stack sync results")
							if failures > 0 {
								title = trf("Stack sync finished with %d publication failure%s", failures, pluralS(failures))
							}
							ui.Text(c, title).Bold().TextColor(map[bool]ui.Color{true: theme.Warning, false: theme.Accent}[failures > 0])
							stackSyncResultBranchesView(c, *a.stackSync.result)
							stackSyncPushResultsView(c, *a.stackSync.result)
						} else if a.stackSync.plan != nil {
							stackSyncPlanView(c, *a.stackSync.plan)
						} else if !a.stackSync.loading && a.stackSync.planError == "" {
							ui.Text(c, tr("Provide a fresh plan to review stack synchronization.")).TextColor(theme.TextMuted)
						}
					})
				})
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Close")).Disabled(a.stackSync.applying).Clicked() {
						a.closeStackSync()
					}
					refreshLabel := tr("Refresh plan")
					if a.stackSync.result != nil {
						refreshLabel = tr("New plan")
					}
					if ui.Button(c, refreshLabel).Disabled(a.stackSync.loading || a.stackSync.applying).Clicked() {
						a.stackSync.applyError = ""
						a.actionError = ""
						a.requestStackSyncPlan()
					}
					if ui.PrimaryButton(c, tr("Apply plan")).
						Disabled(a.stackSync.loading || a.stackSync.applying || a.stackSync.plan == nil || !a.stackSync.plan.CanApply()).
						Clicked() {
						a.beginApplyStackSync()
					}
				})
			})
			if (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) && !a.stackSync.applying {
				a.closeStackSync()
			}
		})
	})
}
