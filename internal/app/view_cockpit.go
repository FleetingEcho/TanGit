package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type cockpitUIState struct {
	scroll ui.ScrollState
	// tileInRow says the card being built shares a row with another, so it takes
	// an equal share of the width; otherwise it fills the width of its column.
	tileInRow bool
	// tileGrow is the share of a row the card being built takes, 1 when unset.
	tileGrow float32
	// width is the Overview's measured width, so charts can size their bars to it.
	width float32
	// contributors is the history statistics the Overview's Contributors card
	// shows, read in the background when the Overview is entered.
	contributors cockpitContributors
	// insights are the heat map, code volume and branch health views.
	insights cockpitInsights
}

// contributorSummarySession is the optional capability behind the card;
// sessions without it leave the card out. An empty branch is every branch.
type contributorSummarySession interface {
	ContributorSummary(ctx context.Context, branch string) (repository.ContributorSummary, error)
}

// contributorSinceSession reads only the commits since a time, which keeps the
// Overview quick on a huge history; sessions without it read everything.
type contributorSinceSession interface {
	ContributorSummarySince(ctx context.Context, branch string, since int64) (repository.ContributorSummary, error)
}

// readContributorSummary reads the history since a Unix time (0 for all of it),
// as narrowly as the session allows.
func readContributorSummary(ctx context.Context, session contributorSummarySession, branch string, since int64) (repository.ContributorSummary, error) {
	if narrow, ok := session.(contributorSinceSession); ok {
		return narrow.ContributorSummarySince(ctx, branch, since)
	}
	return session.ContributorSummary(ctx, branch)
}

// contributorReadSince is the Unix time the Overview's read of a period starts
// at: twice the period back, so the period before it can be compared. All time
// reads everything.
func contributorReadSince(period int, now time.Time) int64 {
	if period < 0 || period >= len(contributorRanges) || contributorRanges[period].days == 0 {
		return 0
	}
	return now.AddDate(0, 0, -2*contributorRanges[period].days).Unix()
}

// syncCockpitBranch makes the Overview follow the checked-out branch until the
// user picks another one in the branch menu.
func (a *app) syncCockpitBranch() {
	state := &a.cockpit.contributors
	if state.branchChosen {
		return
	}
	// While the branches are still being read there is no checked-out branch to
	// follow, and an empty name would mean "every branch": keep what was chosen.
	if len(a.snapshot.Branches) == 0 {
		return
	}
	current := a.currentBranchName()
	label := ""
	if current != "" {
		label = current + " (" + tr("current") + ")"
	}
	// Only another branch resets the read. The picker's text is just a label
	// the user may have edited, so a different label alone never does.
	if state.branch != current {
		state.branch = current
		state.loading, state.loaded = false, false
		state.token++
	}
	state.branchInput = label
}

type contributorCacheEntry struct {
	summary    repository.ContributorSummary
	generation uint64
	loadedAt   time.Time
}

type cockpitContributors struct {
	loading     bool
	loaded      bool
	unavailable bool
	err         string
	summary     repository.ContributorSummary
	// period is the chosen range and stats the history counted for it.
	period int
	stats  contributorRangeStats
	// branch is the branch whose history is counted, "" for every branch;
	// branchInput is the text of the branch picker.
	branch      string
	branchInput string
	// branchChosen is set once the user picks a branch; until then the Overview
	// follows the checked-out one. windowed says the read holds only the
	// recent part of the history.
	branchChosen bool
	windowed     bool
	// cache keeps each branch's read for a while, so going back to a branch is
	// instant.
	cache      map[string]contributorCacheEntry
	generation uint64
	loadedAt   time.Time
	token      uint64
}

// cockpitContributorsMaxAge is how long a read of the history is reused when
// the Overview is entered again.
const cockpitContributorsMaxAge = 2 * time.Minute

// loadCockpitContributors counts the contributors of the chosen branch: from
// the cache when a recent read is held, otherwise from Git in the background.
func (a *app) loadCockpitContributors() {
	state := &a.cockpit.contributors
	if a.session == nil {
		return
	}
	a.syncCockpitBranch()
	if state.loading {
		return
	}
	session, ok := a.session.(contributorSummarySession)
	if !ok {
		state.unavailable = true
		return
	}
	state.unavailable = false
	branch := state.branch
	now := time.Now()
	since := contributorReadSince(state.period, now)
	key := contributorCacheKey(branch, state.period)
	state.windowed = since > 0
	if entry, ok := state.cache[key]; ok && entry.generation == a.generation && time.Since(entry.loadedAt) < cockpitContributorsMaxAge {
		if !state.loaded || state.summary.TotalCommits != entry.summary.TotalCommits || state.generation != entry.generation {
			state.summary, state.loaded, state.generation, state.loadedAt = entry.summary, true, entry.generation, entry.loadedAt
		}
		state.err = ""
		a.recomputeContributorStats()
		return
	}
	state.loading, state.loaded = true, false
	state.token++
	token := state.token
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		summary, err := readContributorSummary(ctx, session, branch, since)
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
			if current.cache == nil {
				current.cache = map[string]contributorCacheEntry{}
			}
			now := time.Now()
			current.cache[key] = contributorCacheEntry{summary: summary, generation: generation, loadedAt: now}
			current.summary, current.loaded = summary, true
			current.generation, current.loadedAt = generation, now
			a.recomputeContributorStats()
		})
	}()
}

// selectContributorBranch counts another branch's history ("" is every
// branch). A read of the previous branch still in flight is dropped.
func (a *app) selectContributorBranch(branch, label string) {
	state := &a.cockpit.contributors
	if state.branch == branch && state.branchInput == label {
		return
	}
	state.branch, state.branchInput = branch, label
	state.branchChosen = true
	state.loading = false
	state.token++
	state.loaded = false
	a.cockpit.insights.heatLoaded = false
	a.loadCockpitContributors()
	a.loadCockpitCodeVolume()
	a.loadCockpitHeatmap()
}

// contributorBranchChoices are the branches the picker offers: every branch,
// the current one, then the other local branches and the remote ones.
func (a *app) contributorBranchChoices() []contributorBranchChoice {
	choices := []contributorBranchChoice{{label: tr("All branches")}}
	current := a.currentBranchName()
	if current != "" {
		choices = append(choices, contributorBranchChoice{branch: current, label: current + " (" + tr("current") + ")"})
	}
	for _, branch := range a.snapshot.Branches {
		if branch.Name == current {
			continue
		}
		choices = append(choices, contributorBranchChoice{branch: branch.Name, label: branchDisplay(branch)})
	}
	for _, remote := range a.snapshot.Remotes {
		for _, branch := range remote.Branches {
			name := remote.Name + "/" + branch.Name
			choices = append(choices, contributorBranchChoice{branch: name, label: name})
		}
	}
	return choices
}

type contributorBranchChoice struct {
	branch string
	label  string
}

const contributorBranchChoiceLimit = 60

// contributorBranchPickerWidth sizes the branch input to the branch name it
// holds, within bounds, so a long name is readable.
func contributorBranchPickerWidth(c *ui.Context, text string) float32 {
	width := float32(len([]rune(text)))*c.Theme().FontSize*0.58 + 40
	return min(max(width, 150), 420)
}

// contributorBranchPicker is the searchable branch menu of the contributor
// views. A repository with hundreds of branches shows the first matches and
// says how many more there are.
func (a *app) contributorBranchPicker(c *ui.Context, width float32) {
	theme := c.Theme()
	state := &a.cockpit.contributors
	ui.Row(c).AlignItems(ui.Center).Gap(6).Shrink(0).Children(func() {
		ui.Icon(c, branchIcon).FontSize(13).TextColor(theme.TextMuted)
		combo := ui.ComboboxBase(c, &state.branchInput)
		combo.Input.Width(width).Padding(3, 10).Background(theme.Surface).Border(1, theme.Border).
			Radius(999).Label(tr("Branch for contributors")).Placeholder(tr("All branches"))
		combo.Popup(func(panel ui.Element) {
			panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
			needle := ""
			if combo.Filtering() {
				needle = strings.ToLower(strings.TrimSpace(state.branchInput))
			}
			var matches []contributorBranchChoice
			for _, choice := range a.contributorBranchChoices() {
				if needle == "" || strings.Contains(strings.ToLower(choice.label), needle) {
					matches = append(matches, choice)
				}
			}
			shown := matches
			if len(shown) > contributorBranchChoiceLimit {
				shown = shown[:contributorBranchChoiceLimit]
			}
			filterPopupList(c, theme, func() {
				if len(matches) == 0 {
					ui.Row(c).Padding(7, 10).Children(func() {
						ui.Text(c, tr("No branches match")).TextColor(theme.TextMuted)
					})
					return
				}
				for _, choice := range shown {
					item := combo.Item(choice.branch+"\x00"+choice.label).Padding(7, 10)
					if item.Highlighted() {
						item.Background(theme.SurfaceHover)
					}
					item.Children(func() {
						ui.Text(c, choice.label).SingleLine().Tooltip(choice.label)
					})
				}
				if len(matches) > len(shown) {
					ui.Row(c).Padding(7, 10).Children(func() {
						ui.Text(c, trf("%d more — type to narrow the list", len(matches)-len(shown))).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
				}
			})
		})
		if value, ok := combo.Chosen(); ok {
			branch, label, _ := strings.Cut(value, "\x00")
			if branch == "" {
				label = ""
			}
			a.selectContributorBranch(branch, label)
		}
	})
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
	// A compact icon: the name is in the tooltip and the accessible label.
	button = compactIconButton(button).Label(tr(label)).Disabled(!state.Enabled)
	if state.DisabledReason != "" {
		button = button.Tooltip(tooltip + " · " + state.DisabledReason)
	} else {
		button = button.Tooltip(tooltip)
	}
	button.Children(func() {
		ui.Icon(c, cockpitCommandIcon(id)).FontSize(14)
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
	root := ui.Column(c).Grow(1).FillWidth().MinWidth(0)
	width := a.measuredActionGroupWidth(c, root)
	a.cockpit.width = width
	root.Children(func() {
		ui.Scroll(c).Grow(1).TrackScroll(&a.cockpit.scroll).Children(func() {
			ui.Column(c).FillWidth().MinWidth(0).Gap(0).Padding(0, 2, 0, 2).Children(func() {
				a.cockpit.tileInRow = false
				a.cockpitContributorsCard(c, width >= 760)
				// Commit activity and the languages share a row when it is wide.
				if width >= 1100 {
					a.cockpit.tileInRow, a.cockpit.tileGrow = true, 2
					c.Key("cockpit-activity-row")
					ui.Row(c).FillWidth().MinWidth(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
						a.cockpitHeatmapCard(c, width*2/3)
						a.cockpit.tileGrow = 1
						a.cockpitLanguagesCard(c)
					})
					a.cockpit.tileGrow = 0
				} else {
					a.cockpit.tileInRow = false
					a.cockpitHeatmapCard(c, width)
					a.cockpitLanguagesCard(c)
				}
				a.cockpit.tileInRow = width >= 760
				if width >= 760 {
					c.Key("cockpit-insight-row")
					ui.Row(c).FillWidth().MinWidth(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
						a.cockpitBranchHealthCard(c)
						a.cockpitCodeChangesCard(c)
					})
				} else {
					a.cockpitBranchHealthCard(c)
					a.cockpitCodeChangesCard(c)
				}
				a.cockpit.tileInRow = false
				// Two status tiles: two across when there is room, one otherwise.
				tiles := []func(){
					func() { a.cockpitWorkingTreeCard(c, summary) },
					func() { a.cockpitCollaborationCard(c, summary) },
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
				a.cockpitRecentCard(c, summary)
			})
		})
	})
}

// cockpitCard is one status tile: a tinted icon and title, then its content.
// Tiles in a row share its width and height.
func (a *app) cockpitCard(c *ui.Context, id, title string, icon *ui.SVG, body func()) {
	a.cockpitCardWithActions(c, id, title, icon, nil, body)
}

// cockpitCardWithActions is cockpitCard with filters and buttons sitting on the
// title's own row, after the title.
func (a *app) cockpitCardWithActions(c *ui.Context, id, title string, icon *ui.SVG, actions func(), body func()) {
	theme := c.Theme()
	c.Key("cockpit-card:" + id)
	card := ui.Column(c).MinWidth(0).Gap(4).Padding(7, 10)
	if a.cockpit.tileInRow {
		card = card.Width(0).Grow(max(a.cockpit.tileGrow, 1))
	} else {
		card = card.FillWidth()
	}
	islandCard(c, card).Label(tr(title)).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(7).Wrap().Children(func() {
			ui.Box(c).Size(20, 20).Radius(6).Center().Shrink(0).Background(theme.Accent.Alpha(0.14)).Children(func() {
				ui.Icon(c, icon).FontSize(12).TextColor(theme.Accent)
			})
			ui.Text(c, tr(title)).Bold().FontSize(14).Grow(1)
			if actions != nil {
				actions()
			}
		})
		body()
	})
}

// cockpitPeriodButton is a card's own time-range control: the toolkit's
// drop-down listing the periods, the current one shown. Each card keeps its own
// period. It returns the new period's index when the user picked another one. It returns the new
// period's index when the user picked another one.
func cockpitPeriodButton(c *ui.Context, period int, label string) (int, bool) {
	if period < 0 || period >= len(contributorRanges) {
		period = 0
	}
	labels := make([]string, len(contributorRanges))
	for index, spec := range contributorRanges {
		labels[index] = tr(spec.label)
	}
	chosen := labels[period]
	ui.Select(c, &chosen, labels).Label(label).Tooltip(label)
	if chosen == labels[period] {
		return -1, false
	}
	for index, name := range labels {
		if name == chosen {
			return index, true
		}
	}
	return -1, false
}

// cockpitTextLink is a small underlined word acting as a button.
func cockpitTextLink(c *ui.Context, text, label, tooltip string) bool {
	theme := c.Theme()
	button := ui.ButtonBase(c).Padding(2, 4).Radius(theme.Radius).Shrink(0).Label(label).Tooltip(tooltip)
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.Children(func() {
		ui.Text(c, text).FontSize(theme.FontSize - 1).TextColor(theme.Accent).Underline().SingleLine()
	})
	return button.Clicked()
}

// cockpitLoading is the placeholder a card shows while its data is read. It
// holds roughly the height the finished card has, so the page does not jump when
// the data arrives, and sits in the middle of that space.
func cockpitLoading(c *ui.Context, text string, height float32) {
	theme := c.Theme()
	ui.Row(c).FillWidth().MinWidth(0).Height(height).Justify(ui.Center).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Spinner(c).Label(text)
		ui.Text(c, text).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
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
	work := summary.WorkingTree
	// The buttons sit at the top right of the card, beside its title.
	actions := func() {
		a.cockpitCommandButton(c, commandOpenChanges, "Open Changes", false)
		if work.Staged > 0 {
			a.cockpitCommandButton(c, commandCommitStaged, "Commit staged", false)
		}
		if work.Reviewed < work.ReviewTotal {
			a.cockpitCommandButton(c, commandNextUnreviewed, "Next unreviewed", false)
		}
	}
	a.cockpitCardWithActions(c, "working-tree", "Working tree", folderIcon, actions, func() {
		theme := c.Theme()
		total := work.Staged + work.Unstaged + work.Untracked + work.Conflicted
		if total == 0 {
			// Nothing to count: say so once instead of listing zeros.
			ui.Row(c).AlignItems(ui.Center).Gap(8).Margin(4, 0, 4, 0).Children(func() {
				ui.Icon(c, commitIcon).FontSize(18).TextColor(theme.Success)
				ui.Text(c, tr("Working tree clean")).FontSize(theme.FontSize + 1).Bold()
			})
		} else {
			parts := []struct {
				count int
				label string
				color ui.Color
			}{
				{work.Staged, tr("Staged"), theme.Success},
				{work.Unstaged, tr("Modified"), theme.Warning},
				{work.Untracked, tr("Untracked"), theme.TextMuted},
			}
			if work.Conflicted > 0 {
				parts = append(parts, struct {
					count int
					label string
					color ui.Color
				}{work.Conflicted, tr("Conflicts"), theme.Danger})
			}
			number := theme.Text
			if work.Conflicted > 0 {
				number = theme.Danger
			}
			// Left: the total. Right: where those changes stand, one row per
			// state with its share as a thin bar. The split matches Branch health.
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Stretch).Gap(12).Children(func() {
				ui.Column(c).Width(110).Shrink(0).Justify(ui.Center).Gap(0).Children(func() {
					ui.Text(c, fmt.Sprintf("%d", total)).FontSize(34).Bold().TextColor(number).SingleLine()
					ui.Text(c, tr("changed files")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
				})
				ui.Box(c).Width(1).AlignSelf(ui.Stretch).Background(theme.Border.Alpha(0.6)).Shrink(0)
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					const track = 96
					for index, part := range parts {
						c.Key(fmt.Sprintf("work-part:%d", index))
						dot, text, value := part.color, theme.Text, theme.Text
						if part.count == 0 {
							dot, text, value = theme.TextMuted.Alpha(0.35), theme.TextMuted.Alpha(0.7), theme.TextMuted.Alpha(0.7)
						}
						ui.Row(c).FillWidth().MinWidth(0).Height(26).AlignItems(ui.Center).Gap(8).Padding(0, 4).Children(func() {
							ui.Box(c).Width(8).Height(8).Radius(4).Background(dot).Shrink(0)
							ui.Text(c, part.label).FontSize(theme.FontSize - 1).TextColor(text).SingleLine().Width(0).Grow(1).MinWidth(0)
							ui.Text(c, fmt.Sprintf("%d", part.count)).FontSize(theme.FontSize - 1).Bold().TextColor(value).SingleLine().Shrink(0)
							ui.Row(c).Width(track).Height(4).Radius(2).Clip().Shrink(0).Background(theme.Border.Alpha(0.5)).Children(func() {
								if part.count > 0 {
									ui.Box(c).Width(max(4, track*float32(part.count)/float32(total))).Height(4).Background(part.color)
								}
							})
						})
					}
				})
			})
		}
	})
}

func (a *app) cockpitCollaborationCard(c *ui.Context, summary cockpitSummary) {
	actions := func() {
		a.cockpitCommandButton(c, commandOpenPRChecks, "Open PR checks", false)
		a.cockpitCommandButton(c, commandOpenPull, "Pull", false)
		a.cockpitCommandButton(c, commandOpenPush, "Push", false)
	}
	a.cockpitCardWithActions(c, "collaboration", "Collaboration", branchesIcon, actions, func() {
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
	})
}

func (a *app) cockpitRecentCard(c *ui.Context, summary cockpitSummary) {
	actions := func() {
		if a.showActivityCenter != nil {
			iconButton(c, "View all activity", "Open the Activity Center for every open repository", historyIcon, false, a.showActivityCenter)
		}
		a.cockpitCommandButton(c, commandOpenLog, "Open Command log", false)
	}
	a.cockpitCardWithActions(c, "recent", "Recent activity", historyIcon, actions, func() {
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
	})
}

// cockpitContributorsCard shows who has been working in a period the user
// picks: totals and trend, a chart of commits over the period and the most
// active contributors. It is left out when the session cannot read contributors.
func (a *app) cockpitContributorsCard(c *ui.Context, wide bool) {
	state := a.cockpit.contributors
	if state.unavailable {
		return
	}
	actions := func() {
		a.contributorBranchPicker(c, contributorBranchPickerWidth(c, state.branchInput))
		if index, changed := cockpitPeriodButton(c, state.period, tr("Contributor period")); changed {
			a.cockpit.contributors.period = index
			// The read covers twice the period, so a longer one needs a new read
			// (kept per branch and period, so going back is instant).
			a.recomputeContributorStats()
			a.loadCockpitContributors()
		}
		if a.showContributors != nil {
			tip := tr("Open the Activity Center's contributor list")
			if state.loaded && state.stats.Total > 5 {
				tip = trf("View all contributors (%d more)", state.stats.Total-5)
			}
			if cockpitTextLink(c, tr("All"), tr("View all contributors"), tip) {
				a.showContributors()
			}
		}
	}
	a.cockpitCardWithActions(c, "contributors", "Contributors", personIcon, actions, func() {
		theme := c.Theme()
		switch {
		case state.err != "":
			ui.Text(c, tr("Could not read the contributors")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, state.err).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !state.loaded:
			cockpitLoading(c, tr("Reading the history…"), 150)
			return
		case state.summary.TotalCommits == 0:
			ui.Text(c, tr("No commits yet")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			return
		}
		stats := state.stats
		period := contributorRanges[stats.Range]
		summary := state.summary
		statsColumn := func() {
			caption := trf("of %d contributors were active", stats.Total)
			if state.windowed {
				caption = tr("contributors were active")
			}
			cockpitMetric(c, fmt.Sprintf("%d", stats.Active), caption, theme.Text)
			ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
				cockpitChip(c, contributorCountLabel(stats.Commits), theme.Success, nil)
				if period.days != 0 && stats.PreviousCommits > 0 {
					change := (stats.Commits - stats.PreviousCommits) * 100 / stats.PreviousCommits
					color := theme.TextMuted
					text := tr("same as the period before")
					switch {
					case change > 0:
						color, text = theme.Success, trf("%s more than the period before", fmt.Sprintf("%d%%", change))
					case change < 0:
						color, text = theme.Warning, trf("%s fewer than the period before", fmt.Sprintf("%d%%", -change))
					}
					cockpitChip(c, text, color, nil)
				}
				if first := contributorDate(summary.FirstCommit); first != "" && period.days == 0 {
					cockpitChip(c, trf("since %s", first), theme.TextMuted, nil)
				}
			})
			cockpitCommitChart(c, stats)
		}
		topColumn := func() {
			if len(stats.Top) == 0 {
				ui.Text(c, tr("No commits in this period")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				return
			}
			heading := tr("Most active")
			if len(stats.Top) > 5 {
				heading = trf("Top 5 of %d active · their share of commits %s", len(stats.Top), fmt.Sprintf("%d%%", stats.TopFiveShare))
			}
			ui.Text(c, heading).Bold().FontSize(theme.FontSize - 1)
			most := 1
			for _, share := range stats.Top {
				most = max(most, share.Commits)
			}
			for index, share := range stats.Top {
				if index == 5 {
					break
				}
				share := share
				c.Key("cockpit-contributor:" + contributorKey(share.Contributor))
				name := authorLabel(share.Contributor.Name, share.Contributor.Email)
				row := ui.ButtonBase(c).FillWidth().MinWidth(0).Gap(8).Padding(2, 4).AlignItems(ui.Center).
					Radius(theme.Radius).Role(ui.RoleOption).Label(name + " — " + contributorCountLabel(share.Commits))
				if row.Hovered() {
					row.Background(theme.SurfaceHover)
				}
				row.Children(func() {
					drawAuthorAvatar(c, share.Contributor.Name, share.Contributor.Email, 20, nil)
					ui.Text(c, name).SingleLine().Width(0).Grow(1).MinWidth(0)
					ui.Box(c).Height(4).Width(float32(max(4, 90*share.Commits/most))).Radius(2).
						Background(theme.Accent.Alpha(0.6)).Shrink(0)
					ui.Text(c, fmt.Sprintf("%d", share.Commits)).FontSize(theme.FontSize - 1).
						TextColor(theme.TextMuted).SingleLine().Width(40).TextAlign(ui.End).Shrink(0)
				})
				if row.Clicked() && a.showContributors != nil {
					a.showContributors()
				}
			}
		}
		if wide {
			ui.Row(c).FillWidth().MinWidth(0).Gap(16).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(6).Children(statsColumn)
				ui.Box(c).Width(1).Shrink(0).Background(theme.Border.Alpha(0.5))
				ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(2).Children(topColumn)
			})
		} else {
			statsColumn()
			topColumn()
		}
	})
}

// cockpitCommitChart draws the period's commits as small bars, newest last,
// each with its date range and count in its tooltip.
func cockpitCommitChart(c *ui.Context, stats contributorRangeStats) {
	theme := c.Theme()
	most := 1
	for _, count := range stats.Buckets {
		most = max(most, count)
	}
	ui.Column(c).FillWidth().MinWidth(0).Gap(3).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Stretch).Gap(3).MinHeight(44).Label(tr("Commits over the period")).Children(func() {
			for index, count := range stats.Buckets {
				c.Key(fmt.Sprintf("cockpit-bar:%d", index))
				percent := float32(3)
				if count > 0 {
					percent = float32(max(8, 100*count/most))
				}
				color := theme.Accent.Alpha(0.45)
				if index == len(stats.Buckets)-1 {
					color = theme.Accent
				}
				from := time.Unix(stats.BucketStart+int64(index)*stats.BucketSeconds, 0).Local()
				tip := from.Format("2006-01-02")
				if stats.BucketSeconds > secondsPerDay {
					tip += " – " + from.Add(time.Duration(stats.BucketSeconds-1)*time.Second).Format("2006-01-02")
				}
				// The bar and the space above it share the slot's height by ratio.
				ui.Column(c).Width(0).Grow(1).MinWidth(0).Children(func() {
					ui.Box(c).FillWidth().Height(0).Grow(100 - percent)
					ui.Box(c).FillWidth().Height(0).Grow(percent).Radius(2).Background(color).
						Tooltip(tip + " · " + contributorCountLabel(count))
				})
			}
		})
		ui.Text(c, tr("Commits over the period")).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
	})
}
