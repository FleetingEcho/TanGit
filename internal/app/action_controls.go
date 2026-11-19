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
	conflictActionStyleGuided      = "guided"
)

func normalizeConflictActionStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case conflictActionStyleIconsLabels:
		return conflictActionStyleIconsLabels
	case conflictActionStyleGuided:
		return conflictActionStyleGuided
	default:
		return conflictActionStyleIcons
	}
}

func conflictActionStyleChoice(style string) int {
	switch normalizeConflictActionStyle(style) {
	case conflictActionStyleIconsLabels:
		return 1
	case conflictActionStyleGuided:
		return 2
	default:
		return 0
	}
}

func conflictActionStyleForChoice(choice int) string {
	switch choice {
	case 1:
		return conflictActionStyleIconsLabels
	case 2:
		return conflictActionStyleGuided
	default:
		return conflictActionStyleIcons
	}
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
	style := normalizeConflictActionStyle(a.settings.ConflictActionStyle)
	a.conflictActionButtonWithLabels(c, action,
		style == conflictActionStyleIconsLabels || style == conflictActionStyleGuided)
}

func (a *app) conflictActionButtonWithLabels(c *ui.Context, action actionControl, showLabel bool) {
	if action.run != nil {
		run := action.run
		action.run = func() {
			run()
			c.Invalidate()
		}
	}
	renderActionControl(c, action, showLabel)
}

// conflictActionToolbar lays out an ordinary hunk-action group. It must not
// infer the group's room from the window: Changes can place it in either a
// wide or narrow splitter child. The conflict resolver adds its own measured
// group layout below, while this shared path retains the established wrap.
func (a *app) conflictActionToolbar(c *ui.Context, label string, actions []actionControl) {
	ui.Row(c).MinWidth(0).MaxWidthPercent(100).AlignItems(ui.Center).Gap(6).Wrap().Label(tr(label)).Children(func() {
		for _, action := range actions {
			a.conflictActionButton(c, action)
		}
	})
}

// conflictCompactActionGroup is one indivisible unit in the measured narrow
// conflict toolbar. The group button keeps Hunk / View / Apply visible while
// its Popover supplies the same labels and unavailable reasons as the full
// controls. Frequently used Apply choices remain direct icon buttons beside
// it; the user's label preference therefore never removes their tooltips.
func (a *app) conflictCompactActionGroup(c *ui.Context, label string, icon *ui.SVG,
	menu, direct []actionControl, showGroupLabel bool,
) {
	ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(6).Label(tr(label) + " group").Children(func() {
		if len(menu) > 0 {
			renderActionControl(c, actionControl{
				label: label, visibleLabel: label, icon: icon,
				description: "Show this action group with labels and unavailable reasons", menu: menu,
			}, showGroupLabel)
		} else if showGroupLabel {
			ui.Text(c, tr(label)).SingleLine()
		}
		for _, action := range direct {
			a.conflictActionButtonWithLabels(c, action, false)
		}
	})
}

// paneLayoutState schedules a frame after the parent splitter has committed
// a new size. Element.Bounds is deliberately the previous frame's geometry,
// so the measured pane itself chooses the layout; window and splitter values
// only detect when another measurement is needed.
type paneLayoutState struct {
	ready                       bool
	paneWidth, windowWidth      float32
	fileWidth, navWidth         float32
	fileCollapsed, navCollapsed bool
}

const (
	conflictCompactPaneWidth = float32(600)
	conflictTinyPaneWidth    = float32(240)
)

func (a *app) measuredPaneWidth(c *ui.Context, element ui.Element) float32 {
	paneWidth := element.Bounds().W
	windowWidth, _ := c.Size()
	state := ui.Local(element, paneLayoutState{}, func() paneLayoutState { return paneLayoutState{} })
	measurementChanged := !state.ready || state.paneWidth != paneWidth ||
		state.windowWidth != windowWidth || state.fileWidth != a.filePaneWidth ||
		state.navWidth != a.navPaneWidth || state.fileCollapsed != a.filePaneCollapsed ||
		state.navCollapsed != a.navPaneCollapsed
	if measurementChanged {
		// A bounded, changed-only redraw is what makes resize and splitter input
		// settle immediately instead of leaving the previous mode on screen.
		c.Invalidate()
		state.ready = true
		state.paneWidth = paneWidth
		state.windowWidth = windowWidth
		state.fileWidth = a.filePaneWidth
		state.navWidth = a.navPaneWidth
		state.fileCollapsed = a.filePaneCollapsed
		state.navCollapsed = a.navPaneCollapsed
	}
	return paneWidth
}

func (a *app) measuredActionGroupWidth(c *ui.Context, row ui.Element) float32 {
	return a.measuredPaneWidth(c, row)
}

func (a *app) conflictToolbarRow(c *ui.Context) (ui.Element, bool, bool) {
	row := ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).Wrap().Label(tr("Conflict toolbar"))
	paneWidth := a.measuredActionGroupWidth(c, row)
	compact := paneWidth <= 0 || paneWidth < conflictCompactPaneWidth
	tiny := paneWidth <= 0 || paneWidth < conflictTinyPaneWidth
	if compact {
		row.Gap(6)
	}
	return row, compact, tiny
}

// conflictCompactApplyGroup keeps the two common side choices directly
// reachable and overflows the remaining mutations. If a whole-file conflict
// has only those two actions, its group menu repeats them rather than leaving
// an inert Apply label beside an unexplained pair of icons.
func (a *app) conflictCompactApplyGroup(c *ui.Context, actions []actionControl, tiny bool) {
	if tiny {
		a.conflictCompactActionGroup(c, "Apply", applyFileIcon, actions, nil, false)
		return
	}
	var direct, overflow []actionControl
	for _, action := range actions {
		if action.label == "Use current" || action.label == "Use incoming" {
			direct = append(direct, action)
		} else {
			overflow = append(overflow, action)
		}
	}
	if len(overflow) == 0 {
		overflow = actions
	}
	a.conflictCompactActionGroup(c, "Apply", applyFileIcon, overflow, direct, true)
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
			disabled: undoReason != "", disabledReason: undoReason, run: a.confirmUndoConflictResolution,
		})
	}
	return actions
}
