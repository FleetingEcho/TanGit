package app

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

// timeNow is a seam so relative-time formatting is testable.
var timeNow = func() int64 { return time.Now().Unix() }

// hasStagedFiles reports whether the index holds a change that can be
// committed or stashed. A conflicted file is staged while Git records the
// merge, but it is resolved first, so it does not count as a staged change.
func hasStagedFiles(files []repository.File) bool {
	for _, file := range files {
		if file.Staged && !file.HasMergeConflicts {
			return true
		}
	}
	return false
}

// hasUnstagedFiles reports whether a file can be staged as a whole. A
// conflicted file is resolved in its own section instead.
func hasUnstagedFiles(files []repository.File) bool {
	for _, file := range files {
		if file.Unstaged && !file.HasMergeConflicts {
			return true
		}
	}
	return false
}

func conflictedFileCount(files []repository.File) int {
	count := 0
	for _, file := range files {
		if file.HasMergeConflicts {
			count++
		}
	}
	return count
}

func hasConflictedFiles(files []repository.File) bool {
	return conflictedFileCount(files) > 0
}

func statusColor(theme *ui.Theme, file repository.File) ui.Color {
	if file.HasMergeConflicts {
		return theme.Danger
	}
	if file.Added {
		return theme.Success
	}
	if file.Deleted {
		return theme.Danger
	}
	return theme.Warning
}

// aheadBehind returns the counts of a branch when Git could determine them.
// Git hands lazygit the placeholder "?" instead of a count when the
// remote-tracking ref is not local yet or the upstream is gone, and that
// placeholder must never appear as a number beside "ahead" or "behind".
func aheadBehind(branch repository.Branch) (ahead string, behind string, known bool) {
	ahead, behind, known = branch.Ahead, branch.Behind, false
	if ahead == "" || ahead == "?" || behind == "" || behind == "?" {
		return "", "", false
	}
	return ahead, behind, true
}

// upstreamName is a branch's upstream as Git writes it, "origin/main", or ""
// when the branch tracks none.
func upstreamName(branch repository.Branch) string {
	if branch.UpstreamRemote == "" {
		return ""
	}
	if branch.UpstreamBranch != "" {
		return branch.UpstreamRemote + "/" + branch.UpstreamBranch
	}
	return branch.UpstreamRemote
}

// upstreamUnknown explains a missing count for the detail pane: why Git has
// no ahead/behind for a branch that does track a remote.
func upstreamUnknown(branch repository.Branch) string {
	upstream := upstreamName(branch)
	if upstream == "" {
		return ""
	}
	if branch.UpstreamGone {
		return trf("Upstream %s no longer exists.", upstream)
	}
	return trf("Git cannot tell ahead or behind until %s is fetched.", upstream)
}

// aheadTooltip explains the up arrow beside a branch: the commits are on
// this branch and not on the branch it tracks, until a push publishes them.
func aheadTooltip(branch repository.Branch) string {
	if upstream := upstreamName(branch); upstream != "" {
		return trf("Commits here are not on %s yet. Push to publish them.", upstream)
	}
	return tr("Commits here are not on the upstream yet. Push to publish them.")
}

// behindTooltip explains the down arrow beside a branch: the tracked branch
// has commits this one does not, until a pull brings them in.
func behindTooltip(branch repository.Branch) string {
	if upstream := upstreamName(branch); upstream != "" {
		return trf("%s has commits this branch does not. Pull to bring them in.", upstream)
	}
	return tr("The upstream has commits this branch does not. Pull to bring them in.")
}

// aheadCountView draws a count ahead of the tracked branch: an up arrow, the
// commits it stands for, and the tooltip that explains them.
func aheadCountView(c *ui.Context, n string, branch repository.Branch, color ui.Color, size float32) {
	countView(c, arrowUpIcon, n, aheadTooltip(branch), color, size)
}

// behindCountView draws a count behind the tracked branch: a down arrow, the
// commits it stands for, and the tooltip that explains them.
func behindCountView(c *ui.Context, n string, branch repository.Branch, color ui.Color, size float32) {
	countView(c, arrowDownIcon, n, behindTooltip(branch), color, size)
}

// countView is one ahead or behind count, the arrow before the number so the
// pair reads as one mark.
func countView(c *ui.Context, icon *ui.SVG, n, tooltip string, color ui.Color, size float32) {
	ui.Row(c).AlignItems(ui.Center).Gap(2).Tooltip(tooltip).Children(func() {
		ui.Icon(c, icon).FontSize(size - 1).TextColor(color)
		ui.Text(c, n).FontSize(size).TextColor(color)
	})
}

// statusLetter is the one letter a changed file wears at the end of its row:
// the first letter of Git's status, with an untracked file's "?" read as U.
// A name-status entry that carries a score, "R100", reads as its R.
func statusLetter(status string) string {
	if status == "" {
		return ""
	}
	if status[0] == '?' {
		return "U"
	}
	return status[:1]
}

// rowStatusColor keeps a status letter's own color at rest, and takes the
// selection's text color on a chosen row, where the accent fill would leave
// the letter hard to read, as the path does.
func rowStatusColor(theme *ui.Theme, row ui.ListRow, color ui.Color) ui.Color {
	if row.Selected() {
		return theme.AccentText
	}
	return color
}

// statusSlot draws a status letter at the end of a list row, right-aligned so
// the letters of a list keep one edge however wide their glyphs are.
func statusSlot(c *ui.Context, letter string, color ui.Color) {
	ui.Text(c, letter).TextColor(color).Bold().
		Width(c.Theme().FontSize + 2).TextAlign(ui.End)
}

// scrollbarGutter is the room a scrollable row keeps at its end for the
// list's scroll bar: the bar floats over the rows at the list's right edge
// while the pointer is on them, and without the room it covers the status
// letter at the row's end.
func scrollbarGutter(theme *ui.Theme) float32 {
	width := theme.ScrollbarWidth
	if width <= 0 {
		width = 6
	}
	// The thumb sits three DIPs in from the edge, and dragging widens it.
	return width + 5
}

// count is one branch count for a row: the value and whether it is worth
// showing, that is known and not zero.
func count(value string) (string, bool) {
	if value == "" || value == "?" || value == "0" {
		return "", false
	}
	return value, true
}

// primaryIconButton is iconButton with the theme's primary look, for the
// action a pane leads with.
func primaryIconButton(c *ui.Context, label, tooltip string, icon *ui.SVG, disabled bool, run func()) {
	button := compactIconButton(ui.PrimaryButton(c, ""))
	clicked := button.Label(tr(label)).Tooltip(tr(tooltip)).Disabled(disabled).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(14)
	})
	if clicked {
		run()
	}
}

// compactIconButton gives toolbar glyphs a smaller, consistent frame without
// changing their labels, tooltips, keyboard behavior or button semantics.
func compactIconButton(button ui.Element) ui.Element {
	return button.Size(34, 26).Padding(0).Center()
}

// compactTextButton gives labeled actions the same height and vertical rhythm
// as compactIconButton while letting their translated text choose the width.
func compactTextButton(button ui.Element) ui.Element {
	return button.Height(26).Padding(2, 8).Center()
}

// flatToggleIconButton keeps a view mode's active state visible and exposes
// it as a pressed toggle to assistive technology.
func flatToggleIconButton(c *ui.Context, label, tooltip string, icon *ui.SVG, size, pad float32, active bool, run func()) ui.Element {
	theme := c.Theme()
	button := ui.ButtonBase(c).Padding(pad).Radius(theme.Radius)
	button.Label(tr(label)).Tooltip(tr(tooltip)).Checked(active)
	color := theme.TextMuted
	if active {
		color = theme.Accent
	}
	button.Children(func() {
		ui.Icon(c, icon).FontSize(size).TextColor(color)
	})
	if active {
		button.Background(theme.Accent.Alpha(0.14))
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if button.Clicked() {
		run()
	}
	return button
}

// flatIconButton is a borderless icon button for a pane's header and its
// section bars: it takes no face until hovered, and stays out of the way
// while its action cannot run.
func flatIconButton(c *ui.Context, label, tooltip string, icon *ui.SVG, size, pad float32, disabled bool, run func()) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Padding(pad).Radius(theme.Radius)
	button.Label(tr(label)).Tooltip(tr(tooltip)).Disabled(disabled)
	button.Children(func() {
		ui.Icon(c, icon).FontSize(size).TextColor(theme.TextMuted)
	})
	if button.Hovered() && !disabled {
		button.Background(theme.SurfaceHover)
	}
	if button.Clicked() {
		run()
	}
}

const (
	paneToggleSize     float32 = 24
	paneToggleHeight   float32 = 20
	paneToggleTopInset float32 = 16
)

// collapsiblePaneSplit is a draggable horizontal split with an always-visible
// pane-state button at its top-right. Hiding the first pane leaves a slim rail
// for its restore button, so the control never covers the remaining content.
func collapsiblePaneSplit(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitLane(c, size, collapsed, expandLabel, first, second)
}

func (a *app) collapsibleContentPaneSplit(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitLane(c, size, collapsed, expandLabel, first, second)
}

func collapsiblePaneSplitLane(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWith(c, collapsed, expandLabel, first, second,
		func() {
			paneSplit(c, size, first, second).Fill()
		})
}

// collapsiblePaneSplitPercent keeps a responsive percentage split while using
// the same slim restore rail as pixel-sized workspace panes.
func collapsiblePaneSplitPercent(c *ui.Context, percent *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitPercentLane(c, percent, collapsed, expandLabel, first, second)
}

func (a *app) collapsibleContentPaneSplitPercent(c *ui.Context, percent *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitPercentLane(c, percent, collapsed, expandLabel, first, second)
}

func collapsiblePaneSplitPercentLane(c *ui.Context, percent *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWith(c, collapsed, expandLabel, first, second,
		func() {
			paneSplitPercent(c, percent, first, second).Fill()
		})
}

const (
	paneResizeGripWidth  float32 = 12
	paneResizeGripLength float32 = 32
	paneResizeLineInset  float32 = 12
)

// paneSplit keeps MyGo's accessible split control and adds the small,
// centered sash grip used throughout the island workspace. The grip owns the
// pointer over its visible area, so it mirrors the split's drag into size.
func paneSplit(c *ui.Context, size *float32, first, second func()) ui.Element {
	return paneSplitWithGrip(c, size, false, false, first, second)
}

func paneSplitPercent(c *ui.Context, percent *float32, first, second func()) ui.Element {
	return paneSplitWithGrip(c, percent, false, true, first, second)
}

func paneSplitVertical(c *ui.Context, size *float32, first, second func()) ui.Element {
	return paneSplitWithGrip(c, size, true, false, first, second)
}

func paneSplitVerticalPercent(c *ui.Context, percent *float32, first, second func()) ui.Element {
	return paneSplitWithGrip(c, percent, true, true, first, second)
}

func paneSplitWithGrip(c *ui.Context, value *float32, vertical, percent bool,
	first, second func()) ui.Element {
	theme := c.Theme()
	host := ui.Box(c).Grow(1)
	if vertical {
		host.FillWidth()
	} else {
		host.FillHeight()
	}
	host.Children(func() {
		switch {
		case vertical && percent:
			ui.SplitVerticalPercent(c, value, first, second).Fill()
		case vertical:
			ui.SplitVertical(c, value, first, second).Fill()
		case percent:
			ui.SplitPercent(c, value, first, second).Fill()
		default:
			ui.Split(c, value, first, second).Fill()
		}

		bounds := host.Bounds()
		total := bounds.W
		if vertical {
			total = bounds.H
		}
		shown := *value
		if percent && total > 1 {
			shown = min(max(*value, 0), 100) * (total - 1) / 100
		}
		if total > 0 {
			minPane := theme.Space(10)
			shown = min(max(shown, minPane), max(total-1-minPane, minPane))
		}
		// MyGo's splitter reaches the host's full cross-axis, past the rounded
		// card borders. Mask only that overshoot, one DIP thick: a horizontal
		// divider starts after the corner radius at each side, just as a vertical
		// one starts below it. Repainting the whole seam with the opaque backdrop
		// also covered the cards' shadows and made a conspicuous dark stripe in
		// dark themes. The native divider stays visible between these masks,
		// including its accent hover feedback.
		if vertical {
			ui.Box(c).Absolute().Top(shown).Left(0).Width(paneResizeLineInset).Height(1).
				Background(islandBackdrop(theme)).PassThrough()
			ui.Box(c).Absolute().Top(shown).Right(0).Width(paneResizeLineInset).Height(1).
				Background(islandBackdrop(theme)).PassThrough()
		} else {
			ui.Box(c).Absolute().Left(shown).Top(0).Height(paneResizeLineInset).Width(1).
				Background(islandBackdrop(theme)).PassThrough()
			ui.Box(c).Absolute().Left(shown).Bottom(0).Height(paneResizeLineInset).Width(1).
				Background(islandBackdrop(theme)).PassThrough()
		}

		grip := ui.Box(c).Absolute().Center().Radius(theme.Radius).
			Label(tr("Drag to resize")).Tooltip(tr("Drag to resize"))
		if vertical {
			grip.LeftPercent(50).Top(shown-paneResizeGripWidth/2).
				Size(paneResizeGripLength, paneResizeGripWidth).
				Translate(-paneResizeGripLength/2, 0).Cursor(ui.CursorResizeNS)
		} else {
			grip.Left(shown-paneResizeGripWidth/2).TopPercent(50).
				Size(paneResizeGripWidth, paneResizeGripLength).
				Translate(0, -paneResizeGripLength/2).Cursor(ui.CursorResizeEW)
		}
		dx, dy, held := grip.Dragged()
		if grip.Hovered() || held {
			grip.Background(theme.SurfaceHover)
		}
		grip.Children(func() {
			mark := "⋮"
			if vertical {
				mark = "⋯"
			}
			ui.Text(c, mark).FontSize(12).TextColor(theme.TextMuted)
		})

		delta := dx
		if vertical {
			delta = dy
		}
		if held && delta != 0 {
			next := shown + delta
			if total > 0 {
				minPane := theme.Space(10)
				next = min(max(next, minPane), max(total-1-minPane, minPane))
			}
			if percent {
				if total > 1 {
					*value = next * 100 / (total - 1)
				}
			} else {
				*value = next
			}
		}
	})
	return host
}

func collapsiblePaneSplitWith(c *ui.Context, collapsed *bool,
	expandLabel string, first, second, split func()) {
	theme := c.Theme()
	host := ui.Box(c).Grow(1).FillHeight()
	host.Children(func() {
		if *collapsed {
			// Each host already begins after its parent's restore rail, so every
			// collapsed pane owns exactly one identical rail of its own.
			const restoreWidth = paneToggleSize + islandGap
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(restoreWidth).
				Background(islandBackdrop(theme)).PassThrough()
			ui.Column(c).Fill().Padding(0, 0, 0, restoreWidth).Children(second)
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(1).
				Background(theme.Border).PassThrough()
			// A collapsed pane has no width. Anchor its restore button to this
			// host's zero edge instead of the remembered expanded width.
			button := paneToggle(c, ui.ButtonBase(c).Absolute()).
				Left(islandGap / 2).Top(paneToggleTopInset).
				Label(tr(expandLabel)).Tooltip(tr(expandLabel))
			button.Children(func() {
				ui.Icon(c, paneCollapsedIcon).FontSize(15).TextColor(theme.TextMuted)
			})
			if button.Clicked() {
				*collapsed = false
			}
			return
		}

		split()
	})
}

// paneCollapseButton is part of the pane's own header action row, not an
// overlay: it cannot cover the filter, create, or overflow actions beside it.
func paneCollapseButton(c *ui.Context, collapsed *bool, label string) {
	theme := c.Theme()
	button := paneToggle(c, ui.ButtonBase(c)).Label(tr(label)).Tooltip(tr(label))
	button.Children(func() {
		ui.Icon(c, paneExpandedIcon).FontSize(15).TextColor(theme.TextMuted)
	})
	if button.Clicked() {
		*collapsed = true
	}
}

// paneToggle is a quiet header action that lives inside the island card. Its
// rectangular hit target appears only on hover, like the neighbouring icons.
func paneToggle(c *ui.Context, button ui.Element) ui.Element {
	theme := c.Theme()
	button.Size(paneToggleSize, paneToggleHeight).Center().Radius(min(theme.Radius, 4))
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	return button
}

// iconTextButton is a button with an icon and its label, for the picker,
// where the action deserves a word beside its glyph.
func iconTextButton(c *ui.Context, label, tooltip string, icon *ui.SVG, primary bool, run func()) {
	iconTextButtonState(c, label, tooltip, icon, primary, false, run)
}

func iconTextButtonState(c *ui.Context, label, tooltip string, icon *ui.SVG, primary, disabled bool, run func()) {
	var button ui.Element
	if primary {
		button = ui.PrimaryButton(c, "")
	} else {
		button = ui.Button(c, "")
	}
	button = compactTextButton(button)
	clicked := button.Label(tr(label)).Tooltip(tr(tooltip)).Gap(6).Disabled(disabled).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(14)
		ui.Text(c, tr(label))
	})
	if clicked {
		run()
	}
}

// closeAllForms drops every text-entry form the sections open, so leaving a
// repository never leaves one behind for the next.
func (a *app) closeAllForms() {
	a.closeSSHWorkspacePicker()
	a.respondSSHHostKey(false)
	a.showCommitForm = false
	a.showBranchForm = false
	a.showRenameForm = false
	a.showRemoteForm = false
	a.showRemoteWebURLForm = false
	a.showRemoteCheckoutForm = false
	a.showStashForm = false
	a.showStashRenameForm = false
	a.showTagForm = false
	a.showTagPushForm = false
	a.showWorktreeForm = false
	a.showWorktreeMoveForm = false
	a.showWorktreeLockForm = false
	a.showWorktreeRepairForm = false
	a.showWorktreePruneForm = false
	a.worktreePruneToken++
	a.showSubmoduleForm = false
	a.showCommitTagForm = false
	a.showCommitBranchForm = false
	a.closeCommitRewordForm()
	a.showRenameRecent = false
	a.closeCodeFileForm()
	a.closeTerminalRename()
	a.cancelCloneOperation()
	a.closeCloneForm()
	a.closePatchPreview()
	a.closeCustomPatchBuilder()
	a.resetChangeCourier()
	a.closeCodeBlameDetails()
	a.filterOpen = false
	a.closeCompare()
	a.showTagRemoteDeleteForm = false
	a.showMoveCommitsForm = false
	a.showBisectForm = false
	a.bisectFormError = ""
	a.showExportPatchForm = false
	a.exportPatchCommits = nil
	a.showPRForm = false
	a.prChecksOpen = false
	a.prChecksToken++
	a.commandPromptOpen = false
	a.customCommandFormOpen = false
}

// anyFormOpen reports whether a text-entry form is showing. Plain-letter
// shortcuts are registered only while it returns false, so they can never
// swallow keystrokes meant for a text input.
func (a *app) anyFormOpen() bool {
	return a.filterOpen ||
		a.showCommitForm ||
		a.showBranchForm ||
		a.showRenameForm ||
		a.showRemoteForm ||
		a.showRemoteWebURLForm ||
		a.showRemoteCheckoutForm ||
		a.showStashForm ||
		a.showStashRenameForm ||
		a.showTagForm ||
		a.showTagPushForm ||
		a.showTagRemoteDeleteForm ||
		a.showWorktreeForm ||
		a.showWorktreeMoveForm ||
		a.showWorktreeLockForm ||
		a.showWorktreeRepairForm ||
		a.showWorktreePruneForm ||
		a.showSubmoduleForm ||
		a.showCommitTagForm ||
		a.showCommitBranchForm ||
		a.showCommitRewordForm ||
		a.showRenameRecent ||
		a.sshProfileFormOpen ||
		a.cloneOpen ||
		a.code.fileForm != codeFileFormNone ||
		a.term.renameID != 0 || a.term.renameGroup != 0 ||
		a.customCommandFormOpen ||
		a.commandPromptOpen ||
		a.showBisectForm ||
		a.showExportPatchForm ||
		a.showPRForm ||
		a.prChecksOpen
}

// modalOpen reports whether a dialog or a full-window view owns the keyboard,
// so the window's tab shortcuts stand aside while it does.
func (a *app) modalOpen() bool {
	return a.confirm != nil ||
		a.settingsOpen ||
		a.keybindingsOpen ||
		a.commandLogOpen ||
		a.commandPromptOpen ||
		a.patchPreviewOpen ||
		a.customPatch.open ||
		a.courier.open ||
		a.sshWorkspaceOpen ||
		a.sshHostKeyPrompt != nil ||
		a.state == stateCloning ||
		a.diffFullscreen ||
		a.branchFullscreen ||
		a.anyFormOpen() ||
		a.lineSelectMode ||
		a.crash != ""
}

// commitHeaderButton is a compact icon-only button in a commit list header.
// Its small padding and glyph keep it within the title's line, and only the
// icon shows, so the label and tooltip carry its meaning.
func commitHeaderButton(c *ui.Context, label, tooltip string, icon *ui.SVG, run func()) {
	button := ui.Button(c, "").Padding(2, 3)
	clicked := button.Label(tr(label)).Tooltip(tr(tooltip)).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(13)
	})
	if clicked {
		run()
	}
}

// commitFilterButton opens a commit list's filter. It wears the accent while
// a filter narrows the list, so a short list says why it is short without the
// filter having to be opened first.
func commitFilterButton(c *ui.Context, tooltip string, active bool, run func()) {
	theme := c.Theme()
	button := ui.Button(c, "").Padding(2, 3)
	clicked := button.Label(tr("Filter commits")).Tooltip(tr(tooltip)).Clicked()
	button.Children(func() {
		if active {
			ui.Icon(c, filterIcon).FontSize(13).TextColor(theme.Accent)
		} else {
			ui.Icon(c, filterIcon).FontSize(13)
		}
	})
	if active {
		button.Background(theme.Accent.Alpha(0.14))
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if clicked {
		run()
	}
}

// clearFilterButton clears every field of a commit list's filter. A list shows
// it beside the filter button only while a filter narrows it.
func clearFilterButton(c *ui.Context, tooltip string, run func()) {
	commitHeaderButton(c, "Clear all filters", tooltip, closeIcon, run)
}

// paneTitle is a section column's heading: its icon and its name.
func paneTitle(c *ui.Context, icon *ui.SVG, title string) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(theme.Accent)
		ui.Text(c, tr(title)).Bold()
	})
}

// paneTitleAction adds a compact create action to a list pane's own heading.
// Its wordy label stays in accessibility and the tooltip; the pane only shows
// the plus glyph so the action does not consume a separate toolbar row. Extra
// actions, such as the pane's collapse control, follow it in the same row.
func paneTitleAction(c *ui.Context, icon *ui.SVG, title, actionLabel, tooltip string,
	disabled bool, run func(), extra ...func()) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(theme.Accent)
		ui.Text(c, tr(title)).Bold().Grow(1)
		flatIconButton(c, actionLabel, tooltip, plusIcon, 14, 3, disabled, run)
		for _, action := range extra {
			action()
		}
	})
}

// repositoryFormModal gives repository-creation forms one shared dialog
// frame: a recognizable action header, background input blocking, and
// Escape/outside-click dismissal. The caller builds only its specific body.
func (a *app) repositoryFormModal(c *ui.Context, open *bool, width float32, title, subtitle string,
	icon *ui.SVG, body func()) {
	if !*open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(width).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr(title)).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, icon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr(title)).FontSize(17).Bold()
							ui.Text(c, tr(subtitle)).TextColor(theme.TextMuted).
								FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close "+title, "Escape", closeIcon, false,
							func() { *open = false })
					})
					body()
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				*open = false
			}
		})
	})
}

// repoName is the last element of a repository path, which the picker's cards
// show as their title.
func repoName(path string) string {
	if location, err := workspace.Parse(path); err == nil && location.IsRemote() {
		return location.Base()
	}
	clean := strings.TrimRight(path, string(filepath.Separator))
	name := filepath.Base(clean)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return clean
	}
	return name
}

// relTime renders a Unix timestamp as a compact age like "3h ago".
func relTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	age := int64(0)
	if now := timeNow(); now > unix {
		age = now - unix
	}
	switch {
	case age < 60:
		return tr("just now")
	case age < 3600:
		return humanizeCount(age/60, "min")
	case age < 86400:
		return humanizeCount(age/3600, "h")
	case age < 7*86400:
		return humanizeCount(age/86400, "d")
	case age < 30*86400:
		return humanizeCount(age/(7*86400), "w")
	case age < 365*86400:
		return humanizeCount(age/(30*86400), "mo")
	default:
		return humanizeCount(age/(365*86400), "y")
	}
}

func humanizeCount(value int64, unit string) string {
	return trf("%d%s ago", value, tr(unit))
}

// secondaryRowColor picks the secondary-text color for a list row: dimmed
// normally, and opaque against the accent fill when the row is selected.
func secondaryRowColor(theme *ui.Theme, row ui.ListRow) ui.Color {
	if row.Selected() {
		return theme.AccentText
	}
	return theme.TextMuted
}

// rowActionIconSize sizes a row's action glyphs to fit within a line of text.
// The glyphs appear on hover, so one taller than the line would grow the row
// the moment the pointer landed on it, and shrink it again on the way out.
// Scaling with the font keeps every row the same height at any font size the
// settings offer.
func rowActionIconSize(theme *ui.Theme) float32 {
	return min(max(theme.FontSize-1, 8), 13)
}

// rowActionIconSizeCompact is rowActionIconSize for the commit rows, whose
// action bar is denser and sits on the subject's line.
func rowActionIconSizeCompact(theme *ui.Theme) float32 {
	return min(max(theme.FontSize-2, 7), 12)
}
