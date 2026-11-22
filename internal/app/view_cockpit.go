package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type cockpitUIState struct {
	scroll ui.ScrollState
	// tileInRow says the card being built shares a row with another, so it takes
	// an equal share of the width; otherwise it fills the width of its column.
	tileInRow bool
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

func cockpitSeverityColor(theme *ui.Theme, severity attentionSeverity) ui.Color {
	switch severity {
	case attentionSeverityError:
		return theme.Danger
	case attentionSeverityWarning:
		return theme.Warning
	case attentionSeveritySuccess:
		return theme.Success
	}
	return theme.Accent
}

func cockpitSeverityGlyph(severity attentionSeverity) *ui.SVG {
	switch severity {
	case attentionSeverityError, attentionSeverityWarning:
		return conflictIcon
	case attentionSeveritySuccess:
		return commandSuccessIcon
	}
	return infoIcon
}

// cockpitChip is a small rounded label: a status the eye can read in passing.
func cockpitChip(c *ui.Context, text string, color ui.Color, icon *ui.SVG) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(5).Padding(2, 9).Radius(999).Shrink(0).
		Background(color.Alpha(0.14)).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(12).TextColor(color)
		}
		ui.Text(c, text).FontSize(theme.FontSize - 1).TextColor(color).SingleLine()
	})
}

// cockpitIconCommand is a compact icon button for a catalog command, with the
// same availability reason in its tooltip as the palette gives.
func (a *app) cockpitIconCommand(c *ui.Context, id commandID, label string) {
	state := commandState{Visible: true, Enabled: true}
	if a.resolveCommand != nil {
		state = a.resolveCommand(id)
	}
	if !state.Visible {
		return
	}
	tooltip := tr(label)
	if state.DisabledReason != "" {
		tooltip += " · " + state.DisabledReason
	}
	button := compactIconButton(ui.Button(c, "")).Center().Label(tr(label)).Tooltip(tooltip).Disabled(!state.Enabled)
	button.Children(func() {
		ui.Icon(c, cockpitCommandIcon(id)).FontSize(15)
	})
	if button.Clicked() && state.Enabled {
		a.runCockpitCommand(id)
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
			ui.Column(c).FillWidth().MinWidth(0).Gap(12).Padding(2, 4, 12, 4).Children(func() {
				a.cockpitRepositoryHeader(c, summary)
				a.cockpitAttentionCard(c, theme, summary)
				// Four status tiles: two across when there is room, one otherwise.
				tiles := []func(){
					func() { a.cockpitWorkingTreeCard(c, summary) },
					func() { a.cockpitCollaborationCard(c, summary) },
					func() { a.cockpitOperationCard(c, summary) },
					func() { a.cockpitAgentsCard(c, summary) },
				}
				a.cockpit.tileInRow = width >= 760
				if width >= 760 {
					for row := 0; row < len(tiles); row += 2 {
						c.Key(fmt.Sprintf("cockpit-tile-row:%d", row))
						ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Stretch).Children(func() {
							tiles[row]()
							tiles[row+1]()
						})
					}
				} else {
					for _, tile := range tiles {
						tile()
					}
				}
				a.cockpit.tileInRow = false
				a.cockpitRecentCard(c, summary)
			})
		})
	})
}

func (a *app) cockpitRepositoryHeader(c *ui.Context, summary cockpitSummary) {
	theme := c.Theme()
	islandCard(c, ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(14).Padding(14, 16)).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Center().Shrink(0).Background(theme.Accent.Alpha(0.14)).Children(func() {
			ui.Icon(c, appIcon).FontSize(24).TextColor(theme.Accent)
		})
		ui.Column(c).Gap(6).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, summary.Repository.Name).FontSize(22).Bold().SingleLine().MinWidth(0)
			ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
				branch := summary.Repository.Branch
				if branch == "" {
					branch = tr("Detached HEAD")
				}
				if summary.Collaboration.DivergenceKnown {
					branch += fmt.Sprintf("  ↑%d ↓%d", summary.Collaboration.Ahead, summary.Collaboration.Behind)
				}
				cockpitChip(c, branch, theme.Accent, branchIcon)
				if summary.Repository.Remote {
					cockpitChip(c, tr("SSH"), theme.TextMuted, nil)
				} else {
					cockpitChip(c, tr("Local"), theme.TextMuted, nil)
				}
				if summary.Repository.Clean {
					cockpitChip(c, tr("Clean"), theme.Success, nil)
				} else {
					cockpitChip(c, tr("Dirty"), theme.Warning, nil)
				}
				if summary.Repository.DetailsLoading {
					cockpitChip(c, tr("loading details"), theme.TextMuted, nil)
				}
			})
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Shrink(0).Children(func() {
			a.cockpitIconCommand(c, commandOpenFetch, "Fetch")
			a.cockpitIconCommand(c, commandOpenPull, "Pull")
			a.cockpitIconCommand(c, commandOpenPush, "Push")
			a.cockpitIconCommand(c, commandOpenRefresh, "Refresh")
		})
	})
}

// cockpitAttentionCard lists what needs doing next, most important first, each
// with a stripe in its severity color and one action.
func (a *app) cockpitAttentionCard(c *ui.Context, theme *ui.Theme, summary cockpitSummary) {
	c.Key("cockpit-attention")
	islandCard(c, ui.Column(c).FillWidth().Gap(10).Padding(14, 16)).Label(tr("Continue where you left off")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, tr("Continue where you left off")).Bold().FontSize(16).Grow(1)
			ui.Text(c, trf("%d items", len(summary.Attention))).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		})
		for index, attention := range summary.Attention {
			attention, index := attention, index
			c.Key("cockpit-attention:" + attention.ID)
			color := cockpitSeverityColor(theme, attention.Severity)
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(12).Padding(10, 12, 10, 0).
				Background(theme.Surface).Radius(theme.Radius).Clip().Children(func() {
				ui.Box(c).Width(4).Height(34).Radius(2).Background(color).Shrink(0)
				ui.Icon(c, cockpitSeverityGlyph(attention.Severity)).FontSize(16).TextColor(color).Shrink(0)
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, tr(attention.Title)).Bold().SingleLine().MinWidth(0)
					ui.Text(c, tr(attention.Description)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
						SingleLine().MinWidth(0).Tooltip(tr(attention.Description))
				})
				a.cockpitCommandButton(c, attention.Command, "Open", index == 0)
			})
		}
	})
}

// cockpitCard is one status tile: a tinted icon and title, then its content.
// Tiles in a row share its width and height.
func (a *app) cockpitCard(c *ui.Context, id, title string, icon *ui.SVG, body func()) {
	theme := c.Theme()
	c.Key("cockpit-card:" + id)
	card := ui.Column(c).MinWidth(0).Gap(10).Padding(14, 16)
	if a.cockpit.tileInRow {
		card = card.Width(0).Grow(1)
	} else {
		card = card.FillWidth()
	}
	islandCard(c, card).Label(tr(title)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(9).Children(func() {
			ui.Box(c).Size(26, 26).Radius(8).Center().Shrink(0).Background(theme.Accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, icon).FontSize(14).TextColor(theme.Accent)
			})
			ui.Text(c, tr(title)).Bold().FontSize(14).Grow(1)
		})
		body()
	})
}

// cockpitMetric is a tile's headline number with what it counts.
func cockpitMetric(c *ui.Context, value, caption string, color ui.Color) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.End).Gap(8).Children(func() {
		ui.Text(c, value).FontSize(28).Bold().TextColor(color).SingleLine()
		ui.Text(c, caption).FontSize(theme.FontSize-1).TextColor(theme.TextMuted).SingleLine().Margin(0, 0, 4, 0)
	})
}

func (a *app) cockpitWorkingTreeCard(c *ui.Context, summary cockpitSummary) {
	a.cockpitCard(c, "working-tree", "Working tree", folderIcon, func() {
		theme := c.Theme()
		work := summary.WorkingTree
		total := work.Staged + work.Unstaged + work.Untracked + work.Conflicted
		color := theme.Text
		if work.Conflicted > 0 {
			color = theme.Danger
		}
		cockpitMetric(c, fmt.Sprintf("%d", total), tr("changed files"), color)
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			cockpitChip(c, fmt.Sprintf(tr("%d staged"), work.Staged), theme.Success, nil)
			cockpitChip(c, fmt.Sprintf(tr("%d modified"), work.Unstaged), theme.Warning, nil)
			cockpitChip(c, fmt.Sprintf(tr("%d untracked"), work.Untracked), theme.TextMuted, nil)
			if work.Conflicted > 0 {
				cockpitChip(c, fmt.Sprintf(tr("%d conflicts"), work.Conflicted), theme.Danger, conflictIcon)
			}
		})
		if work.ReviewTotal > 0 {
			ui.Progress(c, float64(work.Reviewed)/float64(work.ReviewTotal)).Height(5)
		}
		review := fmt.Sprintf(tr("%d of %d files reviewed"), work.Reviewed, work.ReviewTotal)
		if work.ReviewLoading {
			review += " · " + tr("review marks loading")
		}
		ui.Text(c, review).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
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
	a.cockpitCard(c, "operation", "Operation & recovery", continueIcon, func() {
		theme := c.Theme()
		if summary.Operation == nil {
			cockpitMetric(c, tr("None"), tr("no interrupted Git operation"), theme.Success)
			ui.Row(c).Gap(7).Wrap().Children(func() {
				a.cockpitCommandButton(c, commandOpenRecovery, "Open Safety Journal", false)
			})
			return
		}
		cockpitMetric(c, strings.ToUpper(summary.Operation.Name), tr("in progress"), theme.Warning)
		ui.Text(c, tr(summary.Operation.Description)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		ui.Row(c).Gap(7).Wrap().Children(func() {
			a.cockpitCommandButton(c, summary.Operation.Command, "Review next step", true)
			a.cockpitCommandButton(c, commandOpenRecovery, "Open Safety Journal", false)
		})
	})
}

func (a *app) cockpitCollaborationCard(c *ui.Context, summary cockpitSummary) {
	a.cockpitCard(c, "collaboration", "Collaboration", branchesIcon, func() {
		theme := c.Theme()
		collaboration := summary.Collaboration
		if collaboration.DivergenceKnown {
			cockpitMetric(c, fmt.Sprintf("↑%d  ↓%d", collaboration.Ahead, collaboration.Behind), tr("ahead · behind"), theme.Text)
		} else {
			cockpitMetric(c, "—", tr("Upstream divergence unavailable"), theme.TextMuted)
		}
		if collaboration.Upstream != "" {
			ui.Text(c, collaboration.Upstream).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().MinWidth(0)
		}
		checksLabel := tr("Cached checks")
		if !collaboration.ChecksCached {
			checksLabel = tr("Checks not requested")
		}
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, checksLabel).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, tr(collaboration.ChecksSummary)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		ui.Row(c).Gap(7).Wrap().Children(func() {
			a.cockpitCommandButton(c, commandOpenPRChecks, "Open PR checks", true)
			a.cockpitCommandButton(c, commandOpenPull, "Pull", false)
			a.cockpitCommandButton(c, commandOpenPush, "Push", false)
		})
	})
}

func (a *app) cockpitAgentsCard(c *ui.Context, summary cockpitSummary) {
	a.cockpitCard(c, "agents", "Agent tasks", appIcon, func() {
		theme := c.Theme()
		agents := summary.Agents
		switch {
		case agents.Loading:
			cockpitMetric(c, "…", tr("Loading Agent tasks…"), theme.TextMuted)
		case agents.Error != "":
			ui.Text(c, agents.Error).TextColor(theme.Danger).Selectable()
		case !agents.Loaded:
			cockpitMetric(c, "—", tr("not loaded"), theme.TextMuted)
			ui.Text(c, tr("Agent tasks have not been loaded for this repository")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		default:
			active := agents.Running + agents.NeedsInput + agents.Testing + agents.ReadyReview + agents.Blocked
			color := theme.Text
			if agents.NeedsInput > 0 || agents.FailedTests > 0 {
				color = theme.Warning
			}
			cockpitMetric(c, fmt.Sprintf("%d", active), tr("active tasks"), color)
			ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
				cockpitChip(c, fmt.Sprintf(tr("%d running"), agents.Running), theme.Accent, nil)
				cockpitChip(c, fmt.Sprintf(tr("%d need input"), agents.NeedsInput), theme.Warning, nil)
				cockpitChip(c, fmt.Sprintf(tr("%d testing"), agents.Testing), theme.TextMuted, nil)
				cockpitChip(c, fmt.Sprintf(tr("%d ready"), agents.ReadyReview), theme.Success, nil)
				cockpitChip(c, fmt.Sprintf(tr("%d blocked"), agents.Blocked), theme.Danger, nil)
			})
			if agents.FailedTests > 0 {
				ui.Text(c, fmt.Sprintf(tr("%d tasks have failed tests"), agents.FailedTests)).
					TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			} else if active == 0 {
				ui.Text(c, tr("No Agent task needs attention")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		}
		ui.Row(c).Gap(7).Wrap().Children(func() {
			a.cockpitCommandButton(c, commandOpenAgents, "Open Agent tasks", true)
		})
	})
}

func (a *app) cockpitRecentCard(c *ui.Context, summary cockpitSummary) {
	a.cockpitCard(c, "recent", "Recent activity", historyIcon, func() {
		theme := c.Theme()
		if len(summary.Recent) == 0 {
			ui.Text(c, tr("No activity has been recorded in this session")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		now := time.Now()
		for _, item := range summary.Recent {
			item := item
			c.Key("cockpit-activity:" + item.ID.String())
			row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(10).Padding(7, 8).
				AlignItems(ui.Center).Radius(theme.Radius).Role(ui.RoleOption).
				Label(item.Title + " — " + activityStateLabel(item.State))
			if row.Hovered() {
				row.Background(theme.SurfaceHover)
			}
			row.Children(func() {
				glyph, color := activitySeverityGlyph(theme, item.Severity)
				ui.Icon(c, glyph).FontSize(15).TextColor(color).Shrink(0)
				ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, item.Title).SingleLine().Grow(1).MinWidth(0)
					ui.Text(c, activitySourceLabel(item.Source)+" · "+activityPersistenceLabel(item.Persistence)).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 2)
				})
				if when := activityTimeLabel(activityTimestamp(item.OccurredAt, item.UpdatedAt), now); when != "" {
					ui.Text(c, when).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).SingleLine().Shrink(0)
				}
				ui.Icon(c, chevronRightIcon).FontSize(12).TextColor(theme.TextMuted).Shrink(0)
			})
			if row.Clicked() && a.openActivityTarget != nil {
				if result := a.openActivityTarget(item.Target); !result.Opened && result.Reason != "" {
					a.notify("warning", "Activity target unavailable", result.Reason)
				}
			}
		}
		ui.Row(c).Gap(7).Wrap().Children(func() {
			if a.showActivityCenter != nil {
				if ui.Button(c, tr("View all activity")).Label(tr("View all activity")).
					Tooltip(tr("Open the Activity Center for every open repository")).Clicked() {
					a.showActivityCenter()
				}
			}
			a.cockpitCommandButton(c, commandOpenLog, "Open Command log", false)
		})
	})
}
