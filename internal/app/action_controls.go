package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Conflict/hunk action buttons share one compact presentation preference.
// Keep the values stable: they are persisted in settings.json.
const (
	conflictActionStyleIcons       = "icons"
	conflictActionStyleIconsLabels = "icons+labels"
)

func normalizeConflictActionStyle(style string) string {
	if strings.EqualFold(strings.TrimSpace(style), conflictActionStyleIconsLabels) {
		return conflictActionStyleIconsLabels
	}
	return conflictActionStyleIcons
}

func conflictActionStyleChoice(style string) int {
	if normalizeConflictActionStyle(style) == conflictActionStyleIconsLabels {
		return 1
	}
	return 0
}

func conflictActionStyleForChoice(choice int) string {
	if choice == 1 {
		return conflictActionStyleIconsLabels
	}
	return conflictActionStyleIcons
}

// actionControl is the complete discoverability contract for one conflict or
// hunk action. Both presentations use the same accessible label and Tooltip;
// disabledReason explains why an action cannot run instead of only dimming it.
type actionControl struct {
	label          string
	visibleLabel   string // optional compact visual text; label stays accessible
	shortcut       string
	description    string
	tooltip        string // optional complete tooltip used by existing iconButton callers
	icon           *ui.SVG
	disabled       bool
	disabledReason string
	run            func()
	menu           []actionControl
	primary        bool
	textOnly       bool
}

func (action actionControl) localizedTooltip() string {
	base := action.tooltip
	if base == "" {
		base = action.label
	}
	parts := []string{tr(base)}
	if action.shortcut != "" {
		parts = append(parts, action.shortcut)
	}
	if action.description != "" {
		parts = append(parts, tr(action.description))
	}
	if action.disabled && action.disabledReason != "" {
		parts = append(parts, tr(action.disabledReason))
	}
	return strings.Join(parts, " · ")
}

// renderActionControl is the single MyGo button path used by compact icons,
// icons with labels, and the primary text action. A disabled MyGo button does
// not hover, so its wrapper owns the same Tooltip without taking input.
func renderActionControl(c *ui.Context, action actionControl, showLabel bool) {
	tooltip := action.localizedTooltip()
	visibleLabel := action.label
	if action.visibleLabel != "" {
		visibleLabel = action.visibleLabel
	}
	showIcon := action.icon != nil && !action.textOnly
	if action.textOnly {
		showLabel = true
	}
	render := func() {
		var button ui.Element
		if action.primary {
			button = ui.PrimaryButton(c, "")
		} else {
			button = ui.Button(c, "")
		}
		if showLabel {
			button = compactTextButton(button)
			if showIcon {
				button = button.Gap(6)
			}
		} else {
			button = compactIconButton(button)
		}
		button.Label(tr(action.label)).Tooltip(tooltip).Disabled(action.disabled)
		if len(action.menu) > 0 {
			button.PopoverMenuButton(func(m *ui.Menu) {
				for _, item := range action.menu {
					menuItem := m.Item(tr(item.label)).Disabled(item.disabled)
					if item.disabled && item.disabledReason != "" {
						menuItem.Trailing(tr(item.disabledReason))
					}
					if menuItem.Chosen() && item.run != nil {
						item.run()
						c.Invalidate()
					}
				}
			})
		}
		clicked := button.Clicked()
		button.Children(func() {
			if showIcon {
				ui.Icon(c, action.icon).FontSize(14)
			}
			if showLabel {
				ui.Text(c, tr(visibleLabel)).SingleLine()
			}
		})
		if len(action.menu) == 0 && clicked && !action.disabled && action.run != nil {
			action.run()
		}
	}
	if action.disabled {
		ui.Row(c).Tooltip(tooltip).Children(render)
		return
	}
	render()
}

// conflictActionButton applies the user's action-style preference. Primary
// text actions, such as Mark resolved, retain their established appearance.
func (a *app) conflictActionButton(c *ui.Context, action actionControl) {
	showLabel := normalizeConflictActionStyle(a.settings.ConflictActionStyle) == conflictActionStyleIconsLabels
	if action.run != nil {
		run := action.run
		action.run = func() {
			run()
			c.Invalidate()
		}
	}
	renderActionControl(c, action, showLabel)
}

// conflictActionToolbar keeps the project's established grouping while
// allowing groups to wrap. Narrow icons+labels layouts use the project's
// PopoverMenuButton container instead: every menu item has the same label and
// disabled reason, and the group button retains a descriptive Tooltip.
// MyGo v0.9.2's Toolbar overflow is unsuitable because its More menu does not
// open under ui.NewTester, which would strand exactly the narrow-window tasks.
func (a *app) conflictActionToolbar(c *ui.Context, label string, actions []actionControl) {
	width, _ := c.Size()
	if width < 800 && normalizeConflictActionStyle(a.settings.ConflictActionStyle) == conflictActionStyleIconsLabels {
		renderActionControl(c, actionControl{
			label: label, visibleLabel: "More", description: "Show this action group with labels and unavailable reasons", icon: moreIcon, menu: actions,
		}, true)
		return
	}
	ui.Row(c).MinWidth(0).MaxWidthPercent(100).AlignItems(ui.Center).Gap(6).Wrap().Label(tr(label)).Children(func() {
		for _, action := range actions {
			a.conflictActionButton(c, action)
		}
	})
}

// conflictMutationReason applies to Result mutations, not to immutable stage
// buffers or navigation. Keeping it separate makes every disabled action
// explain the actual state rather than claiming that a hunk does not exist.
func (a *app) conflictMutationReason() string {
	if a.actionBusy {
		return "Wait for the current Git action to finish."
	}
	if a.conflictStageTab != 3 {
		return "Switch to Result to change the conflict."
	}
	return ""
}

func conflictNoMarkersReason() string {
	return "There are no conflict markers left to resolve."
}

func (a *app) inlineConflictApplyActions(state *conflictState, region conflictRegion, hasRegion bool) []actionControl {
	resolveReason := a.conflictMutationReason()
	if resolveReason == "" && len(state.regions) == 0 {
		resolveReason = conflictNoMarkersReason()
	}
	resolveDisabled := resolveReason != ""
	actions := []actionControl{
		{label: "Use current", shortcut: "Alt+1", description: "Keep the current side's hunk", icon: useCurrentIcon,
			disabled: resolveDisabled, disabledReason: resolveReason, run: func() { a.resolveConflictSide(conflictCurrent) }},
	}
	if hasRegion && region.hasBase() {
		actions = append(actions, actionControl{label: "Use base", shortcut: "Alt+2", description: "Keep the common ancestor's hunk", icon: historyIcon,
			disabled: resolveDisabled, disabledReason: resolveReason, run: func() { a.resolveConflictSide(conflictBase) }})
	}
	actions = append(actions,
		actionControl{label: "Use incoming", shortcut: "Alt+3", description: "Keep the incoming side's hunk", icon: useIncomingIcon,
			disabled: resolveDisabled, disabledReason: resolveReason, run: func() { a.resolveConflictSide(conflictIncoming) }},
		actionControl{label: "Use both", shortcut: "Alt+4", description: "Keep the hunks of both sides, without the ancestor", icon: diffSplitIcon,
			disabled: resolveDisabled, disabledReason: resolveReason, run: func() { a.resolveConflictSide(conflictBoth) }},
	)
	restoreReason := a.conflictMutationReason()
	if restoreReason == "" && state.isInitial() {
		restoreReason = "Result already matches the initial conflict."
	}
	actions = append(actions, actionControl{
		label: "Restore initial conflict", description: "Restore every conflict marker this file had before any resolutions", icon: hardResetIcon,
		disabled: restoreReason != "", disabledReason: restoreReason, run: a.restoreInitialConflict,
	})
	// Undo takes a slot only once a resolution actually exists, as before.
	if len(state.undo) > 0 {
		undoReason := a.conflictMutationReason()
		actions = append(actions, actionControl{
			label: "Undo resolution", shortcut: "Ctrl/Cmd+Z or Alt+Z", description: "Undo the last conflict resolution without changing the index", icon: revertIcon,
			disabled: undoReason != "", disabledReason: undoReason, run: a.undoConflictResolution,
		})
	}
	return actions
}
