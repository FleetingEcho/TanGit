package app

import (
	"strings"

	"github.com/FleetingEcho/lazygitGo/internal/agentwork"
	"github.com/egoist/mygo/ui"
)

// This file contains the second Command Catalog migration batch. It adapts
// existing actionControl methods rather than copying their Git operations.

func actionCommandSpec(id commandID, group commandGroup, actionFor func(*app) actionControl, allowed func(*app) bool, initial ...*app) commandSpec {
	var action actionControl
	if len(initial) > 0 && initial[0] != nil {
		action = actionFor(initial[0])
	} else {
		action = actionFor(nil)
	}
	return commandSpec{
		ID: id, Label: action.label, Detail: action.description, Group: group,
		Chord: action.shortcut, Icon: commandIconDefault, Keywords: []string{string(id), action.label},
		Resolve: func(ctx commandContext) commandState {
			if ctx.App == nil || (allowed != nil && !allowed(ctx.App)) {
				return commandState{Visible: false}
			}
			current := actionFor(ctx.App)
			if current.label == "" {
				return commandState{Visible: false}
			}
			return commandState{
				Visible: true, Enabled: !current.disabled && current.run != nil,
				// These controls are specific to the current workflow, so they
				// must not fall out of the bounded empty-query list behind global commands.
				DisabledReason: current.disabledReason, ContextScore: 10000,
			}
		},
		Run: func(ctx commandContext) {
			if current := actionFor(ctx.App); !current.disabled && current.run != nil {
				current.run()
			}
		},
	}
}

func (a *app) hunkMutationAvailability() (hunkActionStatus, string) {
	status := a.currentHunkActionStatus()
	reason := status.reason
	if reason == "" && status.hunk < 0 {
		reason = "Move the caret into a diff hunk first."
	}
	if reason == "" {
		switch {
		case a.actionBusy:
			reason = "Wait for the current Git action to finish."
		case a.session == nil:
			reason = "Open a repository before changing hunks."
		}
	}
	return status, reason
}

func (a *app) hunkNavigationDisabledReason(direction int) string {
	if a.actionBusy {
		return "Wait for the current Git action to finish."
	}
	status := a.currentHunkActionStatus()
	if status.count == 0 {
		return "No diff hunk is available."
	}
	if direction < 0 && status.hunk <= 0 {
		return "Already at the first diff hunk."
	}
	if direction > 0 && status.hunk >= status.count-1 {
		return "Already at the last diff hunk."
	}
	return ""
}

func hunkActionControlsForCatalog(a *app) []commandSpec {
	if a == nil {
		return nil
	}
	status, _ := a.hunkMutationAvailability()
	label := "Stage hunk"
	if status.staged {
		label = "Unstage hunk"
	}
	mutation := func(*app) actionControl {
		_, currentReason := a.hunkMutationAvailability()
		return actionControl{label: label, shortcut: "s", description: "Change only the current diff hunk",
			disabled: currentReason != "", disabledReason: currentReason, run: a.beginCurrentHunkAction}
	}
	discard := func(*app) actionControl {
		currentStatus, currentReason := a.hunkMutationAvailability()
		if currentStatus.staged {
			return actionControl{label: "Discard hunk", shortcut: "d", disabled: true,
				disabledReason: "Unstage this hunk before discarding its working-tree changes.", run: a.confirmDiscardCurrentHunk}
		}
		return actionControl{label: "Discard hunk", shortcut: "d", description: "Discard the current hunk after confirmation; keep the index untouched",
			disabled: currentReason != "", disabledReason: currentReason, run: a.confirmDiscardCurrentHunk}
	}
	navigation := func(direction int) func(*app) actionControl {
		return func(*app) actionControl {
			label, chord := "Previous hunk", "["
			if direction > 0 {
				label, chord = "Next hunk", "]"
			}
			reason := a.hunkNavigationDisabledReason(direction)
			return actionControl{label: label, shortcut: chord, description: "Navigate the canonical diff hunks",
				disabled: reason != "", disabledReason: reason, run: func() { navigateDiffHunk(a.diffState, direction) }}
		}
	}
	allowed := func(candidate *app) bool { return candidate != nil && candidate.workspaceTab == sectionChanges }
	return []commandSpec{
		actionCommandSpec("changes/hunk-stage-stage-or-unstage", commandGroupChanges, mutation, allowed, a),
		actionCommandSpec("changes/hunk-discard", commandGroupChanges, discard, allowed, a),
		actionCommandSpec("changes/hunk-previous", commandGroupChanges, navigation(-1), allowed, a),
		actionCommandSpec("changes/hunk-next", commandGroupChanges, navigation(1), allowed, a),
	}
}

func findAction(actions []actionControl, label string) actionControl {
	for _, action := range actions {
		if action.label == label {
			return action
		}
	}
	return actionControl{}
}

func conflictActionForLabel(label string) func(*app) actionControl {
	return func(a *app) actionControl {
		if a == nil {
			return actionControl{label: label}
		}
		if a.conflictState == nil {
			return actionControl{}
		}
		region, hasRegion := a.conflictState.current()
		return findAction(a.inlineConflictApplyActions(a.conflictState, region, hasRegion), label)
	}
}

func (a *app) markResolvedAction() actionControl {
	reason := ""
	if a != nil && a.actionBusy {
		reason = "Wait for the current Git action to finish."
	}
	return actionControl{
		label: "Mark resolved", description: "Stage the file as it is", primary: true, textOnly: true,
		disabled: reason != "", disabledReason: reason,
		run: func() {
			if change, ok := a.selectedChange(); ok {
				a.beginStageFile(change.file, true)
			}
		},
	}
}

func conflictActionControlsForCatalog(a *app) []commandSpec {
	if a == nil {
		return nil
	}
	allowed := func(candidate *app) bool {
		return candidate != nil && candidate.workspaceTab == sectionChanges && candidate.conflictState != nil
	}
	specs := make([]commandSpec, 0, 13)
	viewTabs := []struct {
		name string
		tab  int
	}{{"Base", 0}, {"Current", 1}, {"Incoming", 2}, {"Result", 3}}
	for _, view := range viewTabs {
		view := view
		specs = append(specs, actionCommandSpec(commandID("conflicts/view-"+strings.ToLower(view.name)), commandGroupChanges, func(candidate *app) actionControl {
			if candidate == nil || candidate.conflictState == nil {
				return actionControl{}
			}
			return actionControl{label: "View " + view.name, description: conflictStageTooltip(view.tab), run: func() { candidate.conflictStageTab = view.tab }}
		}, allowed, a))
	}
	for _, target := range []struct {
		id    commandID
		label string
	}{{"conflicts/use-current", "Use current"}, {"conflicts/use-base", "Use base"}, {"conflicts/use-incoming", "Use incoming"}, {"conflicts/use-both", "Use both"}, {"conflicts/restore-initial", "Restore initial conflict"}, {"conflicts/undo-resolution", "Undo resolution"}} {
		specs = append(specs, actionCommandSpec(target.id, commandGroupChanges, conflictActionForLabel(target.label), allowed, a))
	}
	specs = append(specs,
		actionCommandSpec("conflicts/mark-resolved", commandGroupChanges, (*app).markResolvedAction, allowed, a),
		actionCommandSpec("conflicts/assistant", commandGroupChanges, func(candidate *app) actionControl {
			if candidate == nil || candidate.conflictState == nil {
				return actionControl{}
			}
			reason := candidate.conflictMutationReason()
			if reason == "" && len(candidate.conflictState.regions) == 0 {
				reason = "No conflict markers remain for suggestions."
			}
			return actionControl{label: "Conflict Assistant", description: "Preview region suggestions on a clone; Accept writes Result only",
				disabled: reason != "", disabledReason: reason, run: candidate.openConflictAssistant}
		}, allowed, a),
	)
	return specs
}

func agentActionControlsForCatalog(a *app) []commandSpec {
	allowed := func(candidate *app) bool { return candidate != nil && candidate.workspaceTab == sectionAgents }
	test := func(candidate *app) actionControl {
		if candidate == nil {
			return actionControl{label: "Run tests"}
		}
		task, ok := candidate.selectedAgentTask()
		if !ok {
			return actionControl{label: "Run tests", disabled: true, disabledReason: "Select an Agent task with a test command."}
		}
		busy := candidate.agents.mutating[task.ID] || candidate.actionBusy
		testRunning := task.LastTest != nil && task.LastTest.Status == agentwork.TestStatusRunning
		reason := ""
		if busy {
			reason = "Wait for the current Agent operation to finish."
		} else if testRunning {
			reason = "Tests are already running for this task."
		} else if task.Profile.TestCommand == "" {
			reason = "This task has no test command configured."
		}
		return actionControl{label: "Run tests", description: "Run the selected task's profile tests", disabled: reason != "", disabledReason: reason, run: func() { candidate.runAgentTests(task.ID) }}
	}
	review := func(candidate *app) actionControl {
		if candidate == nil {
			return actionControl{label: "Review changes"}
		}
		task, ok := candidate.selectedAgentTask()
		if !ok {
			return actionControl{label: "Review changes", disabled: true, disabledReason: "Select an Agent task first."}
		}
		reason := ""
		if candidate.actionBusy {
			reason = "Wait for the current repository action to finish."
		} else if task.Status == agentwork.StatusArchived {
			reason = "Archived tasks cannot be reviewed."
		}
		return actionControl{label: "Review changes", description: "Review the fixed-Base final result", disabled: reason != "", disabledReason: reason, run: func() { candidate.openAgentReview(task) }}
	}
	return []commandSpec{
		actionCommandSpec("agents/run-selected-tests", commandGroupAgents, test, allowed, a),
		actionCommandSpec("agents/review-selected-result", commandGroupAgents, review, allowed, a),
	}
}

func workflowActionSpecs(ctx commandContext) []commandSpec {
	if ctx.App == nil || ctx.App.state != stateReady || ctx.App.editorOnlyWorkspace() {
		return nil
	}
	var specs []commandSpec
	specs = append(specs, hunkActionControlsForCatalog(ctx.App)...)
	specs = append(specs, conflictActionControlsForCatalog(ctx.App)...)
	specs = append(specs, agentActionControlsForCatalog(ctx.App)...)
	return specs
}

// workspaceExtraSpecs puts actions that used to live only in toolbars and
// menus into the palette: patches, bulk change actions, the Overview's
// controls and the tab menu. Each uses the very method the button calls.
func workspaceExtraSpecs() []commandSpec {
	changes := func(ctx commandContext, need func(*app) string) commandState {
		state := repositoryState(ctx)
		if state.Enabled && ctx.App != nil && need != nil {
			if reason := need(ctx.App); reason != "" {
				state.Enabled = false
				state.DisabledReason = reason
			}
		}
		return state
	}
	overview := func(need func(*overviewState) string) func(commandContext) commandState {
		return func(ctx commandContext) commandState {
			if ctx.App == nil || ctx.App.overview == nil || ctx.App.state == stateReady {
				return commandState{Visible: false}
			}
			state := commandState{Visible: true, Enabled: true, ContextScore: 10000}
			if ctx.App.overview.cur() == nil {
				state.Enabled, state.DisabledReason = false, "No Overview is open"
			} else if reason := need(ctx.App.overview); reason != "" {
				state.Enabled, state.DisabledReason = false, reason
			}
			return state
		}
	}
	tabState := func(need func(*shell, *tab) string) func(commandContext) commandState {
		return func(ctx commandContext) commandState {
			state := shellState(ctx)
			if !state.Enabled {
				return state
			}
			t := ctx.Shell.activeTab()
			if t == nil {
				state.Enabled, state.DisabledReason = false, "No tab is open"
			} else if reason := need(ctx.Shell, t); reason != "" {
				state.Enabled, state.DisabledReason = false, reason
			}
			return state
		}
	}
	noChanges := func(a *app) string {
		if len(a.snapshot.Files) == 0 {
			return "There are no changes"
		}
		return ""
	}
	return []commandSpec{
		{ID: "changes/apply-patch", Label: "Apply patch…", Detail: "Apply a .patch or .diff file (from git diff) to the working tree as uncommitted changes", Keywords: []string{"patch", "diff", "apply", "import", "git apply"}, Group: commandGroupChanges, Chord: cmdShortcutChord() + "+I", Glyph: patchImportIcon, Resolve: func(ctx commandContext) commandState { return changes(ctx, nil) }, Run: func(ctx commandContext) { ctx.App.beginPatchImport() }},
		{ID: "changes/apply-mailbox-patch", Label: "Apply mailbox patch…", Detail: "Apply a git format-patch / .mbox series as commits, keeping the original authors and messages", Keywords: []string{"mailbox", "mbox", "am", "format-patch", "patch", "apply"}, Group: commandGroupChanges, Glyph: patchImportIcon, Resolve: func(ctx commandContext) commandState { return changes(ctx, nil) }, Run: func(ctx commandContext) { ctx.App.beginMailboxPatchImport() }},
		{ID: "changes/stash-staged", Label: "Stash staged changes", Detail: "Stash the staged files, leaving the rest in the tree", Keywords: []string{"stash", "staged", "shelve"}, Group: commandGroupStash, Glyph: stashIcon, Resolve: func(ctx commandContext) commandState {
			return changes(ctx, func(a *app) string {
				if !hasStagedFiles(a.snapshot.Files) {
					return "No staged changes to stash"
				}
				return ""
			})
		}, Run: func(ctx commandContext) { ctx.App.stashStagedChanges() }},
		{ID: "changes/discard-all", Label: "Discard all changes", Detail: "Throw away every change in the working tree, after a confirmation", Keywords: []string{"discard", "reset", "revert", "clean", "undo", "throw away"}, Group: commandGroupChanges, Glyph: undoIcon, Resolve: func(ctx commandContext) commandState { return changes(ctx, noChanges) }, Run: func(ctx commandContext) { ctx.App.confirmDiscardAllChanges() }},
		{ID: "overview/refresh", Label: "Refresh overview", Detail: "Read the repositories again (does not fetch)", Keywords: []string{"overview", "reload", "refresh", "update"}, Group: commandGroupApplication, Glyph: refreshIcon, Resolve: overview(func(o *overviewState) string {
			if len(o.followed()) == 0 {
				return "This Overview follows no repositories"
			}
			return ""
		}), Run: func(ctx commandContext) { ctx.App.reloadOverview() }},
		{ID: "overview/edit", Label: "Edit overview", Detail: "Rename this Overview or change its repositories", Keywords: []string{"overview", "rename", "repositories", "choose", "follow"}, Group: commandGroupApplication, Glyph: folderIcon, Resolve: overview(func(*overviewState) string { return "" }), Run: func(ctx commandContext) { ctx.App.openOverviewSetup() }},
		{ID: "overview/mark-read", Label: "Mark all as read", Detail: "Clear the New badges on the Overview's commits", Keywords: []string{"overview", "read", "new", "badge", "seen"}, Group: commandGroupApplication, Glyph: markReviewedIcon, Resolve: overview(func(o *overviewState) string {
			if o.newCommitCount() == 0 {
				return "No new commits"
			}
			return ""
		}), Run: func(ctx commandContext) { ctx.App.markOverviewRead(ctx.App.overview) }},
		{ID: "overview/export", Label: "Export commits", Detail: "Save the loaded commits as a Markdown or CSV file", Keywords: []string{"overview", "export", "save", "markdown", "csv", "report"}, Group: commandGroupApplication, Glyph: overviewExportIcon, Resolve: overview(func(o *overviewState) string {
			if len(o.visibleCommits()) == 0 {
				return "No commits are loaded"
			}
			return ""
		}), Run: func(ctx commandContext) { ctx.App.exportOverview(ctx.App.overview) }},
		{ID: "overview/period", Label: "Custom period", Detail: "Choose the dates the Overview reads", Keywords: []string{"overview", "period", "range", "dates", "time"}, Group: commandGroupApplication, Glyph: historyIcon, Resolve: overview(func(*overviewState) string { return "" }), Run: func(ctx commandContext) { ctx.App.openOverviewPeriod(ctx.App.overview) }},
		{ID: "overview/period-reset", Label: "Reset period", Detail: "Go back to the Overview's time range instead of a custom period", Keywords: []string{"overview", "period", "range", "reset", "clear"}, Group: commandGroupApplication, Glyph: closeIcon, Resolve: overview(func(o *overviewState) string {
			if o.period == nil {
				return "No custom period is set"
			}
			return ""
		}), Run: func(ctx commandContext) { ctx.App.selectOverviewPreset(ctx.App.overview, ctx.App.overview.rangeIdx) }},
		{ID: "tabs/rename", Label: "Rename tab", Keywords: []string{"tab", "title", "name"}, Group: commandGroupApplication, Glyph: editIcon, Resolve: tabState(func(_ *shell, t *tab) string {
			if !t.repository() && !t.isOverview() {
				return "This tab cannot be renamed"
			}
			return ""
		}), Run: func(ctx commandContext) {
			if t := ctx.Shell.activeTab(); t != nil {
				ctx.Shell.request(func() { ctx.Shell.renameTab(t) })
			}
		}},
		{ID: "tabs/color", Label: "Tab color…", Keywords: []string{"tab", "color", "accent", "paint"}, Group: commandGroupApplication, Glyph: themeIcon, Resolve: tabState(func(_ *shell, t *tab) string {
			if !t.repository() && !t.isOverview() {
				return "This tab has no color"
			}
			return ""
		}), Run: func(ctx commandContext) {
			if t := ctx.Shell.activeTab(); t != nil {
				s := ctx.Shell
				s.request(func() { s.tabColorTab = t; s.tabColorOpen = true; s.tabColorCustom = ui.Color{} })
			}
		}},
		{ID: "tabs/close-others", Label: "Close other tabs", Keywords: []string{"tab", "close", "others"}, Group: commandGroupApplication, Glyph: closeIcon, Resolve: tabState(func(s *shell, t *tab) string {
			if !s.hasOtherClosable(t) {
				return "There are no other tabs to close"
			}
			return ""
		}), Run: func(ctx commandContext) {
			if t := ctx.Shell.activeTab(); t != nil {
				ctx.Shell.request(func() { ctx.Shell.closeOthers(t) })
			}
		}},
		{ID: "tabs/close-right", Label: "Close tabs to the right", Keywords: []string{"tab", "close", "right"}, Group: commandGroupApplication, Glyph: closeIcon, Resolve: tabState(func(s *shell, t *tab) string {
			if !s.hasClosableRight(t) {
				return "There are no tabs to the right"
			}
			return ""
		}), Run: func(ctx commandContext) {
			if t := ctx.Shell.activeTab(); t != nil {
				ctx.Shell.request(func() { ctx.Shell.closeRight(t) })
			}
		}},
		{ID: "tabs/close-all", Label: "Close all tabs", Keywords: []string{"tab", "close", "all"}, Group: commandGroupApplication, Glyph: closeIcon, Resolve: tabState(func(s *shell, _ *tab) string {
			if len(s.tabs) < 2 {
				return "Only one tab is open"
			}
			return ""
		}), Run: func(ctx commandContext) { ctx.Shell.request(ctx.Shell.closeAll) }},
	}
}
