package app

import (
	"strings"

	"lazygitapp/internal/agentwork"
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
				DisabledReason: current.disabledReason, ContextScore: 15,
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
