package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type cockpitUIState struct {
	scroll ui.ScrollState
	// tileInRow says the card being built shares a row with another, so it takes
	// an equal share of the width; otherwise it fills the width of its column.
	tileInRow bool
	// contributors is the history statistics the Overview's Contributors card
	// shows, read in the background when the Overview is entered.
	contributors cockpitContributors
}

// contributorSummarySession is the optional capability behind the card;
// sessions without it leave the card out.
type contributorSummarySession interface {
	ContributorSummary(context.Context) (repository.ContributorSummary, error)
}

type cockpitContributors struct {
	loading     bool
	loaded      bool
	unavailable bool
	err         string
	summary     repository.ContributorSummary
	generation  uint64
	loadedAt    time.Time
	token       uint64
}

// cockpitContributorsMaxAge is how long a read of the history is reused when
// the Overview is entered again.
const cockpitContributorsMaxAge = 2 * time.Minute

// loadCockpitContributors reads the contributor statistics in the background,
// unless a recent read of this repository is already held.
func (a *app) loadCockpitContributors() {
	state := &a.cockpit.contributors
	if state.loading || a.session == nil {
		return
	}
	session, ok := a.session.(contributorSummarySession)
	if !ok {
		state.unavailable = true
		return
	}
	state.unavailable = false
	if state.loaded && state.generation == a.generation && time.Since(state.loadedAt) < cockpitContributorsMaxAge {
		return
	}
	state.loading = true
	state.token++
	token := state.token
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		summary, err := session.ContributorSummary(ctx)
		a.update(func() {
			current := &a.cockpit.contributors
			if generation != a.generation || current.token != token {
				return
			}
			current.loading = false
			if err != nil {
				current.err = safeError(err)
				return
			}
			current.err = ""
			current.summary, current.loaded = summary, true
			current.generation, current.loadedAt = generation, time.Now()
		})
	}()
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
			ui.Column(c).FillWidth().MinWidth(0).Gap(0).Padding(0, 2, 6, 2).Children(func() {
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
						ui.Row(c).FillWidth().MinWidth(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
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
				a.cockpitContributorsCard(c, width >= 760)
				a.cockpitRecentCard(c, summary)
			})
		})
	})
}

func (a *app) cockpitRepositoryHeader(c *ui.Context, summary cockpitSummary) {
	theme := c.Theme()
	islandCard(c, ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).Padding(6, 10)).Children(func() {
		ui.Box(c).Size(34, 34).Radius(9).Center().Shrink(0).Background(theme.Accent.Alpha(0.14)).Children(func() {
			ui.Icon(c, appIcon).FontSize(18).TextColor(theme.Accent)
		})
		ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, summary.Repository.Name).FontSize(18).Bold().SingleLine().MinWidth(0)
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
	})
}

// cockpitAttentionCard lists what needs doing next, most important first, each
// with a stripe in its severity color and one action.
func (a *app) cockpitAttentionCard(c *ui.Context, theme *ui.Theme, summary cockpitSummary) {
	c.Key("cockpit-attention")
	islandCard(c, ui.Column(c).FillWidth().Gap(4).Padding(7, 10)).Label(tr("Continue where you left off")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, tr("Continue where you left off")).Bold().FontSize(16).Grow(1)
			ui.Text(c, trf("%d items", len(summary.Attention))).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		})
		for index, attention := range summary.Attention {
			attention, index := attention, index
			c.Key("cockpit-attention:" + attention.ID)
			color := cockpitSeverityColor(theme, attention.Severity)
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(8).Padding(4, 8, 4, 0).
				Background(theme.Surface).Radius(theme.Radius).Clip().Children(func() {
				ui.Box(c).Width(3).Height(24).Radius(2).Background(color).Shrink(0)
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
	card := ui.Column(c).MinWidth(0).Gap(4).Padding(7, 10)
	if a.cockpit.tileInRow {
		card = card.Width(0).Grow(1)
	} else {
		card = card.FillWidth()
	}
	islandCard(c, card).Label(tr(title)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Box(c).Size(20, 20).Radius(6).Center().Shrink(0).Background(theme.Accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, icon).FontSize(12).TextColor(theme.Accent)
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
		ui.Text(c, value).FontSize(22).Bold().TextColor(color).SingleLine()
		ui.Text(c, caption).FontSize(theme.FontSize-1).TextColor(theme.TextMuted).SingleLine().Margin(0, 0, 3, 0)
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
			row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(8).Padding(2, 8).
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

// cockpitContributorsCard shows who has contributed and how the history has
// moved lately: totals, recent activity, a weekly commit chart and the top
// contributors. It is left out when the session cannot read contributors.
func (a *app) cockpitContributorsCard(c *ui.Context, wide bool) {
	state := a.cockpit.contributors
	if state.unavailable {
		return
	}
	a.cockpitCard(c, "contributors", "Contributors", personIcon, func() {
		theme := c.Theme()
		switch {
		case state.err != "":
			ui.Text(c, tr("Could not read the contributors")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, state.err).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !state.loaded:
			ui.Text(c, tr("Reading the history…")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			return
		case state.summary.TotalCommits == 0:
			ui.Text(c, tr("No commits yet")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			return
		}
		summary := state.summary
		stats := func() {
			cockpitMetric(c, fmt.Sprintf("%d", len(summary.Contributors)), tr("contributors"), theme.Text)
			ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
				cockpitChip(c, trf("%d active in the last 30 days", summary.ActiveRecent), theme.Success, nil)
				cockpitChip(c, contributorCountLabel(summary.TotalCommits), theme.TextMuted, nil)
				if first := contributorDate(summary.FirstCommit); first != "" {
					cockpitChip(c, trf("since %s", first), theme.TextMuted, nil)
				}
			})
			cockpitWeeklyChart(c, summary.WeeklyCommits)
		}
		top := func() {
			ui.Text(c, tr("Top contributors")).Bold().FontSize(theme.FontSize - 1)
			most := 1
			for _, contributor := range summary.Contributors {
				most = max(most, contributor.Commits)
			}
			for index, contributor := range summary.Contributors {
				if index == 5 {
					break
				}
				contributor := contributor
				c.Key("cockpit-contributor:" + contributorKey(contributor))
				row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(8).Padding(2, 4).AlignItems(ui.Center).
					Radius(theme.Radius).Role(ui.RoleOption).
					Label(authorLabel(contributor.Name, contributor.Email) + " — " + contributorCountLabel(contributor.Commits))
				if row.Hovered() {
					row.Background(theme.SurfaceHover)
				}
				row.Children(func() {
					drawAuthorAvatar(c, contributor.Name, contributor.Email, 20, nil)
					ui.Text(c, authorLabel(contributor.Name, contributor.Email)).SingleLine().Width(0).Grow(1).MinWidth(0)
					ui.Box(c).Height(4).Width(float32(max(4, 90*contributor.Commits/most))).Radius(2).
						Background(theme.Accent.Alpha(0.6)).Shrink(0)
					ui.Text(c, fmt.Sprintf("%d", contributor.Commits)).FontSize(theme.FontSize - 1).
						TextColor(theme.TextMuted).SingleLine().Width(34).TextAlign(ui.End).Shrink(0)
				})
				if row.Clicked() && a.showContributors != nil {
					a.showContributors()
				}
			}
		}
		if wide {
			ui.Row(c).FillWidth().MinWidth(0).Gap(16).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(6).Children(stats)
				ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(2).Children(top)
			})
		} else {
			stats()
			top()
		}
		if a.showContributors != nil {
			ui.Row(c).Gap(7).Wrap().Children(func() {
				if ui.Button(c, tr("View all contributors")).Label(tr("View all contributors")).
					Tooltip(tr("Open the Activity Center's contributor list")).Clicked() {
					a.showContributors()
				}
			})
		}
	})
}

// cockpitWeeklyChart draws commits per week as small bars, newest last.
func cockpitWeeklyChart(c *ui.Context, weeks []int) {
	theme := c.Theme()
	most := 1
	for _, count := range weeks {
		most = max(most, count)
	}
	ui.Column(c).Gap(3).Children(func() {
		ui.Row(c).AlignItems(ui.End).Gap(3).Height(34).Label(tr("Commits per week, last 12 weeks")).Children(func() {
			for index, count := range weeks {
				count := count
				c.Key(fmt.Sprintf("cockpit-week:%d", index))
				height := float32(3)
				if count > 0 {
					height = float32(max(5, 34*count/most))
				}
				color := theme.Accent.Alpha(0.45)
				if index == len(weeks)-1 {
					color = theme.Accent
				}
				ui.Box(c).Width(12).Height(height).Radius(2).Background(color).Tooltip(contributorCountLabel(count))
			}
		})
		ui.Text(c, tr("Commits per week, last 12 weeks")).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
	})
}
