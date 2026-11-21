package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type cockpitUIState struct {
	scroll ui.ScrollState
}

func (a *app) cockpitInput() cockpitInput {
	reviewed, reviewTotal := a.reviewProgress()
	input := cockpitInput{
		Snapshot: a.snapshot, DetailsLoading: a.repositoryDetailsLoading,
		Crash: a.crash, ActionError: a.actionError, CommandFailure: a.actionError,
		Reviewed: reviewed, ReviewTotal: reviewTotal, ReviewLoading: a.review.loading,
		Agents: agentTaskClones(a.agents.tasks), AgentsLoading: a.agents.loading,
		AgentsError: a.agents.err, ChecksText: a.prChecksText, ChecksError: a.prChecksError,
		ChecksLoading: a.prChecksLoading, Remote: a.location.IsRemote(),
		Indexing: a.code.filesLoading || a.code.searchRunning,
	}
	input.AgentsLoaded = a.agents.loadedRepo != "" && a.agents.loadedRepo == a.agentRepositoryID()
	input.DirtyEditors = len(a.dirtyCodePaths())
	if a.fileEditState != nil && a.fileEditState.Buffer != nil && a.fileEditState.Buffer.Modified() {
		input.DirtyEditors++
	}
	if a.conflictView != nil && a.conflictView.Buffer != nil && a.conflictView.Buffer.Modified() {
		input.DirtyEditors++
	}
	input.LSPBusy = a.lsp.goStarting
	for _, server := range a.lsp.additional {
		if server != nil && (server.starting || server.analyzing) {
			input.LSPBusy = true
			break
		}
	}
	if activity, ok := a.activityInput(); ok {
		input.Transfers = activity.Transfers
		input.Recent = (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{activity}}, activityQuery{Limit: 5})
	}
	return input
}

func (a *app) cockpitSummary() cockpitSummary {
	return buildCockpitSummary(a.cockpitInput())
}

func (s *shell) commandStateFor(id commandID) commandState {
	ctx := s.commandContext()
	for _, spec := range s.catalogFor(ctx).specs {
		if spec.ID != id {
			continue
		}
		state := commandState{Visible: true, Enabled: true}
		if spec.Resolve != nil {
			state = spec.Resolve(ctx)
		}
		state.DisabledReason = tr(state.DisabledReason)
		return state
	}
	return commandState{DisabledReason: tr("Command is unavailable")}
}

func (a *app) runCockpitCommand(id commandID) {
	if a.invokeCommand == nil {
		return
	}
	if result := a.invokeCommand(id); !result.Accepted() && result.Reason != "" {
		a.notify("warning", "Command unavailable", result.Reason)
	}
}

func cockpitCommandIcon(id commandID) *ui.SVG {
	switch id {
	case commandOpenFetch:
		return fetchIcon
	case commandOpenPull:
		return pullIcon
	case commandOpenPush:
		return pushIcon
	case commandOpenRefresh:
		return refreshIcon
	case commandOpenLog:
		return logIcon
	case commandCommitStaged:
		return commitIcon
	case commandContinue:
		return continueIcon
	case commandOpenRecovery, commandOpenPRChecks:
		return historyIcon
	case commandOpenAgents:
		return appIcon
	case commandOpenChanges, commandOpenCockpit:
		return folderIcon
	default:
		return openRepoIcon
	}
}

// cockpitCommandButton resolves availability for this exact frame and invokes
// by stable ID, so cards and the palette share both reasons and execution.
func (a *app) cockpitCommandButton(c *ui.Context, id commandID, label string, primary bool) {
	state := commandState{Visible: true, Enabled: true}
	if a.resolveCommand != nil {
		state = a.resolveCommand(id)
	}
	if !state.Visible {
		return
	}
	tooltip := tr(label)
	var button ui.Element
	if primary {
		button = ui.PrimaryButton(c, "")
	} else {
		button = ui.Button(c, "")
	}
	button = button.Label(tr(label)).Disabled(!state.Enabled)
	if state.DisabledReason != "" {
		button = button.Tooltip(tooltip + " · " + state.DisabledReason)
	} else {
		button = button.Tooltip(tooltip)
	}
	button.Children(func() {
		ui.Icon(c, cockpitCommandIcon(id)).FontSize(14)
		ui.Text(c, tr(label)).SingleLine()
	})
	if button.Clicked() && state.Enabled {
		a.runCockpitCommand(id)
	}
}

func cockpitSeverityLabel(severity attentionSeverity) string {
	switch severity {
	case attentionSeverityError:
		return tr("Error")
	case attentionSeverityWarning:
		return tr("Attention")
	case attentionSeveritySuccess:
		return tr("Ready")
	default:
		return tr("Info")
	}
}

func (a *app) cockpitView(c *ui.Context) {
	if a.editorOnlyWorkspace() {
		return
	}
	summary := a.cockpitSummary()
	theme := c.Theme()
	root := ui.Column(c).Grow(1).FillWidth().MinWidth(0)
	width := a.measuredActionGroupWidth(c, root)
	root.Children(func() {
		ui.Scroll(c).Grow(1).TrackScroll(&a.cockpit.scroll).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Gap(12).Padding(2, 4).Children(func() {
				a.cockpitRepositoryHeader(c, summary)
				c.Key("cockpit-attention")
				islandCard(c, ui.Column(c).FillWidth().Gap(10).Padding(14)).Label(tr("Continue where you left off")).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, tr("Continue where you left off")).Bold().FontSize(16).Grow(1)
						ui.Badge(c, trf("%d items", len(summary.Attention)))
					})
					for _, attention := range summary.Attention {
						attention := attention
						c.Key("cockpit-attention:" + attention.ID)
						color := theme.TextMuted
						if attention.Severity == attentionSeverityError {
							color = theme.Danger
						} else if attention.Severity == attentionSeverityWarning {
							color = theme.Warning
						} else if attention.Severity == attentionSeveritySuccess {
							color = theme.Success
						}
						ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).Padding(8, 10).
							Background(theme.Surface).Radius(theme.Radius).Children(func() {
							ui.Icon(c, infoIcon).FontSize(15).TextColor(color)
							ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
								ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
									ui.Text(c, tr(attention.Title)).Bold().Grow(1).SingleLine().MinWidth(0)
									ui.Text(c, cockpitSeverityLabel(attention.Severity)).TextColor(color).
										FontSize(theme.FontSize - 2)
								})
								ui.Text(c, tr(attention.Description)).TextColor(theme.TextMuted).
									FontSize(theme.FontSize - 1).MaxWidth(760)
							})
							a.cockpitCommandButton(c, attention.Command, "Open", attention.ID == summary.Attention[0].ID)
						})
					}
				})
				wide := width >= 980
				if wide {
					ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Start).Gap(12).Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
							a.cockpitWorkingTreeCard(c, summary)
							a.cockpitAgentsCard(c, summary)
						})
						ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
							a.cockpitOperationCard(c, summary)
							a.cockpitCollaborationCard(c, summary)
							a.cockpitRecentCard(c, summary)
						})
					})
				} else {
					ui.Column(c).FillWidth().MinWidth(0).Gap(12).Children(func() {
						a.cockpitWorkingTreeCard(c, summary)
						a.cockpitOperationCard(c, summary)
						a.cockpitCollaborationCard(c, summary)
						a.cockpitAgentsCard(c, summary)
						a.cockpitRecentCard(c, summary)
					})
				}
			})
		})
	})
}

func (a *app) cockpitRepositoryHeader(c *ui.Context, summary cockpitSummary) {
	theme := c.Theme()
	islandCard(c, ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(12).Padding(10, 14)).Children(func() {
		ui.Icon(c, appIcon).FontSize(22).TextColor(theme.Accent)
		ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).MinWidth(0).Children(func() {
				ui.Text(c, summary.Repository.Name).FontSize(18).Bold().SingleLine().Grow(1).MinWidth(0)
				if summary.Repository.Remote {
					ui.Badge(c, tr("SSH"))
				} else {
					ui.Badge(c, tr("Local"))
				}
				if summary.Repository.Clean {
					ui.Badge(c, tr("Clean"))
				} else {
					ui.Badge(c, tr("Dirty"))
				}
			})
			branch := summary.Repository.Branch
			if branch == "" {
				branch = tr("Detached HEAD")
			}
			line := branch
			if summary.Collaboration.DivergenceKnown {
				line += fmt.Sprintf(" · ↑%d ↓%d", summary.Collaboration.Ahead, summary.Collaboration.Behind)
			}
			if summary.Repository.DetailsLoading {
				line += " · " + tr("loading details")
			}
			ui.Text(c, line).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine()
		})
		a.cockpitCommandButton(c, commandOpenRefresh, "Refresh", false)
		if summary.Collaboration.Ahead > 0 {
			a.cockpitCommandButton(c, commandOpenPush, "Push", true)
		} else if summary.Collaboration.Behind > 0 {
			a.cockpitCommandButton(c, commandOpenPull, "Pull", true)
		}
	})
}

func cockpitCard(c *ui.Context, id, title string, icon *ui.SVG, body func()) {
	c.Key("cockpit-card:" + id)
	islandCard(c, ui.Column(c).FillWidth().MinWidth(0).Gap(10).Padding(14)).Label(tr(title)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, icon).FontSize(15).TextColor(c.Theme().Accent)
			ui.Text(c, tr(title)).Bold().FontSize(15).Grow(1)
		})
		body()
	})
}

func (a *app) cockpitWorkingTreeCard(c *ui.Context, summary cockpitSummary) {
	cockpitCard(c, "working-tree", "Working tree", folderIcon, func() {
		work := summary.WorkingTree
		ui.Text(c, fmt.Sprintf(tr("%d staged · %d modified · %d untracked · %d conflicts"),
			work.Staged, work.Unstaged, work.Untracked, work.Conflicted)).Selectable()
		review := fmt.Sprintf(tr("%d of %d files reviewed"), work.Reviewed, work.ReviewTotal)
		if work.ReviewLoading {
			review += " · " + tr("review marks loading")
		}
		ui.Text(c, review).TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1)
		ui.Row(c).Gap(7).Wrap().Children(func() {
			a.cockpitCommandButton(c, commandOpenChanges, "Open Changes", true)
			if work.Staged > 0 {
				a.cockpitCommandButton(c, commandCommitStaged, "Commit staged", false)
			}
			if work.Reviewed < work.ReviewTotal {
				a.cockpitCommandButton(c, commandNextUnreviewed, "Next unreviewed", false)
			}
		})
	})
}

func (a *app) cockpitOperationCard(c *ui.Context, summary cockpitSummary) {
	cockpitCard(c, "operation", "Operation & recovery", continueIcon, func() {
		if summary.Operation == nil {
			ui.Text(c, tr("No interrupted Git operation")).TextColor(c.Theme().TextMuted)
			a.cockpitCommandButton(c, commandOpenRecovery, "Open Safety Journal", false)
			return
		}
		ui.Text(c, strings.ToUpper(summary.Operation.Name)).Bold()
		ui.Text(c, tr(summary.Operation.Description)).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(7).Wrap().Children(func() {
			a.cockpitCommandButton(c, summary.Operation.Command, "Review next step", true)
			a.cockpitCommandButton(c, commandOpenRecovery, "Open Safety Journal", false)
		})
	})
}

func (a *app) cockpitCollaborationCard(c *ui.Context, summary cockpitSummary) {
	cockpitCard(c, "collaboration", "Collaboration", branchesIcon, func() {
		collaboration := summary.Collaboration
		divergence := tr("Upstream divergence unavailable")
		if collaboration.DivergenceKnown {
			divergence = fmt.Sprintf(tr("%d ahead · %d behind"), collaboration.Ahead, collaboration.Behind)
		}
		if collaboration.Upstream != "" {
			divergence += " · " + collaboration.Upstream
		}
		ui.Text(c, divergence).Selectable()
		checksLabel := tr("Cached checks")
		if !collaboration.ChecksCached {
			checksLabel = tr("Checks not requested")
		}
		ui.Text(c, checksLabel).Bold()
		ui.Text(c, tr(collaboration.ChecksSummary)).TextColor(c.Theme().TextMuted).MaxWidth(700)
		ui.Row(c).Gap(7).Wrap().Children(func() {
			a.cockpitCommandButton(c, commandOpenPRChecks, "Open PR checks", true)
			a.cockpitCommandButton(c, commandOpenPull, "Pull", false)
			a.cockpitCommandButton(c, commandOpenPush, "Push", false)
		})
	})
}

func (a *app) cockpitAgentsCard(c *ui.Context, summary cockpitSummary) {
	cockpitCard(c, "agents", "Agent tasks", appIcon, func() {
		agents := summary.Agents
		switch {
		case agents.Loading:
			ui.Text(c, tr("Loading Agent tasks…")).TextColor(c.Theme().TextMuted)
		case agents.Error != "":
			ui.Text(c, agents.Error).TextColor(c.Theme().Danger).Selectable()
		case !agents.Loaded:
			ui.Text(c, tr("Agent tasks have not been loaded for this repository")).
				TextColor(c.Theme().TextMuted)
		default:
			ui.Text(c, fmt.Sprintf(tr("%d running · %d need input · %d testing · %d ready · %d blocked"),
				agents.Running, agents.NeedsInput, agents.Testing, agents.ReadyReview, agents.Blocked)).Selectable()
			if agents.FailedTests > 0 {
				ui.Text(c, fmt.Sprintf(tr("%d tasks have failed tests"), agents.FailedTests)).
					TextColor(c.Theme().Danger)
			} else if agents.Running+agents.NeedsInput+agents.Testing+agents.ReadyReview+agents.Blocked == 0 {
				ui.Text(c, tr("No Agent task needs attention")).TextColor(c.Theme().TextMuted)
			}
		}
		a.cockpitCommandButton(c, commandOpenAgents, "Open Agent tasks", true)
	})
}

func (a *app) cockpitRecentCard(c *ui.Context, summary cockpitSummary) {
	cockpitCard(c, "recent", "Recent activity", historyIcon, func() {
		if len(summary.Recent) == 0 {
			ui.Text(c, tr("No activity has been recorded in this session")).
				TextColor(c.Theme().TextMuted)
		}
		for _, item := range summary.Recent {
			item := item
			c.Key("cockpit-activity:" + item.ID.String())
			row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(8).Padding(6, 7).
				AlignItems(ui.Center).Radius(c.Theme().Radius).Role(ui.RoleOption).
				Label(item.Title + " — " + activityStateLabel(item.State))
			if row.Hovered() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.Children(func() {
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, item.Title).SingleLine().Grow(1).MinWidth(0)
					ui.Text(c, activitySourceLabel(item.Source)+" · "+activityPersistenceLabel(item.Persistence)).
						TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 2)
				})
				ui.Icon(c, chevronRightIcon).FontSize(12).TextColor(c.Theme().TextMuted)
			})
			if row.Clicked() && a.openActivityTarget != nil {
				if result := a.openActivityTarget(item.Target); !result.Opened && result.Reason != "" {
					a.notify("warning", "Activity target unavailable", result.Reason)
				}
			}
		}
		a.cockpitCommandButton(c, commandOpenLog, "Open Command log", false)
	})
}
