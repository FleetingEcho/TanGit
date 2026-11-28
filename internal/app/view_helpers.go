package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
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

// toolbarSeparator groups related icon actions without adding another face.
func toolbarSeparator(c *ui.Context) {
	ui.Box(c).Width(1).Height(16).Margin(0, 2).Background(c.Theme().Border)
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

// collapsibleContentPaneSplit is collapsiblePaneSplit for a workspace page,
// whose panes are island cards: its rail spans what those cards do.
func (a *app) collapsibleContentPaneSplit(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWithInset(c, collapsed, expandLabel, islandGap/2, first, second,
		pixelSplitter(size, func(size *float32) { paneSplit(c, size, first, second).Fill() }))
}

func collapsiblePaneSplitLane(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWith(c, collapsed, expandLabel, first, second,
		pixelSplitter(size, func(size *float32) { paneSplit(c, size, first, second).Fill() }))
}

// collapsiblePaneSplitNested is collapsiblePaneSplit for a split inside an
// island card: its divider's masked ends take the card's color, not the
// darker backdrop that shows between cards.
func collapsiblePaneSplitNested(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWith(c, collapsed, expandLabel, first, second,
		pixelSplitter(size, func(size *float32) { paneSplitInCard(c, size, first, second).Fill() }))
}

// paneSplitter draws a collapsible split. extent is the first pane's full width
// for a host of the given width (nil when the split does not animate); draw
// builds the split, with the first pane at width, or at its own size when width
// is negative, so that a drag still changes it.
type paneSplitter struct {
	extent func(hostWidth float32) float32
	draw   func(width, hostWidth float32)
}

// pixelSplitter is a split whose first pane has a size in DIPs.
func pixelSplitter(size *float32, build func(size *float32)) paneSplitter {
	return paneSplitter{
		extent: func(float32) float32 { return *size },
		draw: func(width, _ float32) {
			if width < 0 {
				build(size)
				return
			}
			build(&width)
		},
	}
}

// percentSplitter is a split whose first pane has a share of the host.
func percentSplitter(percent *float32, build func(percent *float32)) paneSplitter {
	return paneSplitter{
		extent: func(hostWidth float32) float32 { return *percent / 100 * hostWidth },
		draw: func(width, hostWidth float32) {
			if width < 0 || hostWidth <= 0 {
				build(percent)
				return
			}
			share := width / hostWidth * 100
			build(&share)
		},
	}
}

// collapsiblePaneSplitPercent keeps a responsive percentage split while using
// the same slim restore rail as pixel-sized workspace panes.
func collapsiblePaneSplitPercent(c *ui.Context, percent *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitPercentLane(c, percent, collapsed, expandLabel, first, second)
}

func (a *app) collapsibleContentPaneSplitPercent(c *ui.Context, percent *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWithInset(c, collapsed, expandLabel, islandGap/2, first, second,
		percentSplitter(percent, func(percent *float32) { paneSplitPercent(c, percent, first, second).Fill() }))
}

func collapsiblePaneSplitPercentLane(c *ui.Context, percent *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWith(c, collapsed, expandLabel, first, second,
		percentSplitter(percent, func(percent *float32) { paneSplitPercent(c, percent, first, second).Fill() }))
}

const (
	// paneRailWidth is a collapsed pane's rail: its toggle and its gap.
	paneRailWidth = paneToggleSize + islandGap
	// paneRailShowsBelow is how far open a sliding pane is when it turns into its
	// rail: the part of the slide in which its content is too squeezed to read.
	paneRailShowsBelow float32 = 0.4
)

// paneCollapseDuration is how long a pane takes to slide shut or open. It is a
// variable so tests, which assert the settled layout, can turn the motion off.
var paneCollapseDuration = 200 * time.Millisecond

func b2f(on bool) float32 {
	if on {
		return 1
	}
	return 0
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

// paneSplitVerticalReserve keeps a structural minimum height for the second
// pane while the user drags, independently of either pane's content.
func paneSplitVerticalReserve(c *ui.Context, size *float32, secondMin, inset float32, first, second func()) ui.Element {
	withSpace := func(before bool, content func()) func() {
		return func() {
			pane := ui.Box(c).Fill()
			if before {
				pane.Padding(0, 0, inset, 0)
			} else {
				pane.Padding(inset, 0, 0, 0)
			}
			pane.Children(content)
		}
	}
	// The requested minimum belongs to the second pane's content, not to
	// the breathing room above its divider.
	return paneSplitCore(c, size, true, false, secondMin+inset, true,
		withSpace(true, first), withSpace(false, second))
}

func paneSplitVerticalPercent(c *ui.Context, percent *float32, first, second func()) ui.Element {
	return paneSplitWithGrip(c, percent, true, true, first, second)
}

func paneSplitWithGrip(c *ui.Context, value *float32, vertical, percent bool,
	first, second func()) ui.Element {
	return paneSplitWithGripReserve(c, value, vertical, percent, 0, first, second)
}

// paneSecondMinWidth is the room a side-by-side split always leaves its second
// pane when the window can spare it, so a wide first pane cannot squeeze the
// details beside it into an unusable strip on a narrow window.
const paneSecondMinWidth float32 = 200

func paneSplitWithGripReserve(c *ui.Context, value *float32, vertical, percent bool, secondMin float32,
	first, second func()) ui.Element {
	return paneSplitCore(c, value, vertical, percent, secondMin, false, first, second)
}

// paneSplitInCard is paneSplit for a split inside an island card, where the
// divider's masked ends take the card's color.
func paneSplitInCard(c *ui.Context, size *float32, first, second func()) ui.Element {
	return paneSplitCore(c, size, false, false, 0, true, first, second)
}

// paneSplitVerticalInCard is paneSplitVertical for a split inside an island
// card, where the divider's ends are masked in the card's own color rather than
// the darker backdrop that shows between cards.
func paneSplitVerticalInCard(c *ui.Context, size *float32, first, second func()) ui.Element {
	return paneSplitCore(c, size, true, false, 0, true, first, second)
}

// paneSplitCore is the split every paneSplit* builds; inCard says the split
// sits inside a card, so its divider's masked ends take the card's color.
func paneSplitCore(c *ui.Context, value *float32, vertical, percent bool, secondMin float32,
	inCard bool, first, second func()) ui.Element {
	theme := c.Theme()
	mask := islandBackdrop(theme)
	if inCard {
		mask = theme.Background
	}
	if !vertical && !percent && secondMin == 0 {
		secondMin = paneSecondMinWidth
	}
	host := ui.Box(c).Grow(1)
	if vertical {
		host.FillWidth()
	} else {
		host.FillHeight()
	}
	host.Children(func() {
		bounds := host.Bounds()
		total := bounds.W
		if vertical {
			total = bounds.H
		}
		capFirst := func(v float32) float32 {
			if secondMin > 0 && total > 0 {
				maximum := max(total-1-secondMin, theme.Space(10))
				return min(v, maximum)
			}
			return v
		}
		controlled := capFirst(*value)
		size := value
		if secondMin > 0 && !percent {
			size = &controlled
		}
		splitter := ui.Element{}
		switch {
		case vertical && percent:
			splitter = ui.SplitVerticalPercent(c, value, first, second).Fill()
		case vertical:
			splitter = ui.SplitVertical(c, size, first, second).Fill()
		case percent:
			splitter = ui.SplitPercent(c, value, first, second).Fill()
		default:
			splitter = ui.Split(c, size, first, second).Fill()
		}
		if secondMin > 0 && !percent && splitter.Changed() {
			*value = capFirst(controlled)
		}

		shown := capFirst(*value)
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
				Background(mask).PassThrough()
			ui.Box(c).Absolute().Top(shown).Right(0).Width(paneResizeLineInset).Height(1).
				Background(mask).PassThrough()
		} else {
			ui.Box(c).Absolute().Left(shown).Top(0).Height(paneResizeLineInset).Width(1).
				Background(mask).PassThrough()
			ui.Box(c).Absolute().Left(shown).Bottom(0).Height(paneResizeLineInset).Width(1).
				Background(mask).PassThrough()
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
			// A glyph's line box is taller than this grip and its baseline can
			// push the visual center down. The native icon has the same fixed
			// bounds as the grip and remains centered in either orientation.
			icon := ui.Icon(c, moreIcon).FontSize(12).TextColor(theme.TextMuted).Center()
			if !vertical {
				icon.Rotate(90)
			}
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
				*value = capFirst(next)
			}
		}
	})
	return host
}

func collapsiblePaneSplitWith(c *ui.Context, collapsed *bool,
	expandLabel string, first, second func(), split paneSplitter) {
	collapsiblePaneSplitWithInset(c, collapsed, expandLabel, 0, first, second, split)
}

// collapsiblePaneSplitWithInset leaves inset of room above and below the
// collapsed rail: a workspace page's cards carry that margin, so their rail
// matches them, while the panes nested inside a card are plain content that
// the rail should span fully.
func collapsiblePaneSplitWithInset(c *ui.Context, collapsed *bool,
	expandLabel string, inset float32, first, second func(), split paneSplitter) {
	collapsiblePaneSplitWithAxis(c, collapsed, expandLabel, false,
		railInset{side: inset, top: inset, bottom: inset}, first, second, split)
}

// railInset places a collapsed pane's rail against its host: side is the room
// at the rail's outer edge, top and bottom the room above and below. A negative
// top lets the rail climb into the gap above the host.
type railInset struct{ side, top, bottom float32 }

const (
	// cardRailGap is the room a rail inside a card keeps from the card's border
	// and from the divider above it, enough for its rounded corners to show.
	cardRailGap float32 = 6
	// cardPanePadding is the card's own padding, which such a pane now carries
	// itself so that its rail can run out to the card's edge.
	cardPanePadding float32 = 16
)

// collapsiblePaneSplitInCard is collapsiblePaneSplit for panes that fill the
// bottom of an island card whose padding stops at its bottom edge. The panes
// carry that padding themselves, so the rail beside them runs the card's whole
// remaining height, leaving cardRailGap above and below, instead of stopping at
// the padding.
func collapsiblePaneSplitInCard(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	padded := func(content func()) func() {
		return func() {
			ui.Column(c).Fill().Padding(0, 0, cardPanePadding, 0).Children(content)
		}
	}
	first, second = padded(first), padded(second)
	// The rail climbs 4 DIPs into the 10 DIP gap over the split, which leaves
	// cardRailGap clear of the divider above it.
	collapsiblePaneSplitWithAxis(c, collapsed, expandLabel, false,
		railInset{top: -(10 - cardRailGap), bottom: cardRailGap}, first, second,
		pixelSplitter(size, func(size *float32) { paneSplitInCard(c, size, first, second).Fill() }))
}

func collapsiblePaneSplitVertical(c *ui.Context, size *float32, collapsed *bool,
	expandLabel string, first, second func()) {
	collapsiblePaneSplitWithAxis(c, collapsed, expandLabel, true, railInset{}, first, second,
		pixelSplitter(size, func(size *float32) { paneSplitVerticalInCard(c, size, first, second).Fill() }))
}

func collapsiblePaneSplitWithAxis(c *ui.Context, collapsed *bool,
	expandLabel string, vertical bool, inset railInset, first, second func(), split paneSplitter) {
	theme := c.Theme()
	host := ui.Box(c).Grow(1)
	// open eases between 0 (collapsed) and 1 (expanded) when a side-by-side pane
	// is toggled. It starts at its target, so a pane that is built collapsed or
	// open does not animate, and it moves at once where the desktop asks for less
	// motion. Stacked panes switch without it.
	open := b2f(!*collapsed)
	if paneCollapseDuration > 0 && !vertical {
		open = host.Animate("pane-open", open, paneCollapseDuration)
	}
	if vertical {
		host.FillWidth()
	} else {
		host.FillHeight()
	}
	host.Children(func() {
		if vertical {
			if *collapsed {
				const restoreHeight = paneToggleHeight + islandGap
				ui.Box(c).Absolute().Left(inset.side).Right(inset.side).Top(inset.top).
					Height(restoreHeight-inset.top).Background(collapsedRailBackground(theme)).
					Border(1, theme.Border).Radius(theme.Radius + 4).PassThrough()
				ui.Column(c).Fill().Padding(restoreHeight, 0, 0, 0).Children(second)
				button := paneToggle(c, ui.ButtonBase(c).Absolute()).
					Left(islandGap / 2).Top(islandGap / 2).
					Label(tr(expandLabel)).Tooltip(tr(expandLabel))
				button.Children(func() {
					ui.Icon(c, paneCollapsedIcon).FontSize(15).TextColor(theme.TextMuted)
				})
				if button.Clicked() {
					*collapsed = false
				}
				return
			}
			split.draw(-1, 0)
			return
		}

		// drawRail is the slim rail a collapsed pane leaves, width DIPs wide, with
		// the second pane beside it. Each host already begins after its parent's
		// restore rail, so every collapsed pane owns exactly one identical rail of
		// its own. The rail leaves the same room above and below as the cards
		// beside it, or none when the panes beside it are plain content.
		drawRail := func(width float32) {
			ui.Box(c).Absolute().Left(inset.side).Top(inset.top).Bottom(inset.bottom).
				Width(width-inset.side).Background(collapsedRailBackground(theme)).
				Border(1, theme.Border).Radius(theme.Radius + 4).PassThrough()
			ui.Column(c).Fill().Padding(0, 0, 0, width).Children(second)
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
		}
		if *collapsed && open <= 0 {
			drawRail(paneRailWidth)
			return
		}
		hostWidth := host.Bounds().W
		if open < 1 && hostWidth > 0 && split.extent != nil {
			// Mid-slide the pane is as wide as its rail plus what is left of it.
			// Once it is narrow the rail is drawn at that width instead, over the
			// pane's squeezed content, so the pane turns into the rail as it
			// shrinks and the rail never pops in at its end.
			width := paneRailWidth + max(split.extent(hostWidth)-paneRailWidth, 0)*open
			if open < paneRailShowsBelow {
				drawRail(width)
				return
			}
			split.draw(width, hostWidth)
			return
		}
		split.draw(-1, hostWidth)
	})
}

// paneCollapseButton is part of the pane's own header action row, not an
// overlay: it cannot cover the filter, create, or overflow actions beside it.
func paneCollapseButton(c *ui.Context, collapsed *bool, label string) ui.Element {
	theme := c.Theme()
	button := paneToggle(c, ui.ButtonBase(c)).Label(tr(label)).Tooltip(tr(label))
	button.Children(func() {
		ui.Icon(c, paneExpandedIcon).FontSize(15).TextColor(theme.TextMuted)
	})
	if button.Clicked() {
		*collapsed = true
	}
	return button
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
	a.ssh.reset()
	a.respondSSHHostKey(false)
	a.showCommitForm = false
	a.showBranchForm = false
	a.showRenameForm = false
	a.showRemoteForm = false
	a.showRemoteWebURLForm = false
	a.showRemoteCheckoutForm = false
	a.showRecoveryModal = false
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
	a.cancelAssistance()
	a.agents.formOpen = false
	a.showSubmoduleForm = false
	a.showCommitTagForm = false
	a.showCommitBranchForm = false
	a.closeCommitRewordForm()
	a.showRenameRecent = false
	a.closeHomeWorktrees()
	a.closeCodeFileForm()
	a.closeCodeWorkspaceFolderForm()
	a.closeTerminalRename()
	a.cancelCloneOperation()
	a.closeCloneForm()
	a.closePatchPreview()
	a.closeCustomPatchBuilder()
	a.resetChangeCourier()
	a.closeCodeBlameDetails()
	a.filterOpen = false
	a.closeHistoryChanges()
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
	a.settingsUI.closeCustomCommandForm()
}

// anyFormOpen reports whether a text-entry form is showing. Plain-letter
// shortcuts are registered only while it returns false, so they can never
// swallow keystrokes meant for a text input.
func (a *app) anyFormOpen() bool { return a.formOpen(false) }

// formOpen is anyFormOpen; with inline true it leaves out the surfaces that sit
// in a page rather than over it: the commit message box and the list filter.
func (a *app) formOpen(inline bool) bool { return a.openFormName(inline) != "" }

// openFormName names the form that is open, "" when none is: the field it is
// kept in, so a form that stays open by mistake can be found.
func (a *app) openFormName(inline bool) string {
	for _, form := range []struct {
		on   bool
		name string
	}{
		{!inline && a.filterOpen, "filterOpen"},
		{a.historyChanges.open, "historyChanges"},
		{!inline && a.showCommitForm, "showCommitForm"},
		{a.showBranchForm, "showBranchForm"},
		{a.showRenameForm, "showRenameForm"},
		{a.showRemoteForm, "showRemoteForm"},
		{a.showRemoteWebURLForm, "showRemoteWebURLForm"},
		{a.showRemoteCheckoutForm, "showRemoteCheckoutForm"},
		{a.showRecoveryModal, "showRecoveryModal"},
		{a.showStashForm, "showStashForm"},
		{a.showStashRenameForm, "showStashRenameForm"},
		{a.showTagForm, "showTagForm"},
		{a.showTagPushForm, "showTagPushForm"},
		{a.showTagRemoteDeleteForm, "showTagRemoteDeleteForm"},
		{a.showWorktreeForm, "showWorktreeForm"},
		{a.showWorktreeMoveForm, "showWorktreeMoveForm"},
		{a.showWorktreeLockForm, "showWorktreeLockForm"},
		{a.showWorktreeRepairForm, "showWorktreeRepairForm"},
		{a.showWorktreePruneForm, "showWorktreePruneForm"},
		{a.assistance.conflict.open, "conflict assistance"},
		{a.agents.formOpen, "agent task form"},
		{a.showSubmoduleForm, "showSubmoduleForm"},
		{a.showCommitTagForm, "showCommitTagForm"},
		{a.showCommitBranchForm, "showCommitBranchForm"},
		{a.showCommitRewordForm, "showCommitRewordForm"},
		{a.rebasePlanEditorOpen || a.rebaseModalOpen, "rebase modal"},
		{a.showRenameRecent, "showRenameRecent"},
		{a.homeWorktrees.open && !a.homeWorktrees.dropdown, "homeWorktrees"},
		{a.ssh.profileFormOpen, "ssh profile form"},
		{a.cloneOpen, "cloneOpen"},
		{a.code.fileForm != codeFileFormNone, "code file form"},
		{a.code.workspaceFormOpen, "code workspace form"},
		{a.code.languageDialog != codeLanguageDialogNone, "language dialog"},
		{a.term.renameID != 0 || a.term.renameGroup != 0, "terminal rename"},
		{a.settingsUI.open && a.settingsUI.customCommandFormOpen, "custom command form"},
		{a.commandPromptOpen, "commandPromptOpen"},
		{a.showBisectForm, "showBisectForm"},
		{a.showExportPatchForm, "showExportPatchForm"},
		{a.showPRForm, "showPRForm"},
		{a.prChecksOpen, "prChecksOpen"},
	} {
		if form.on {
			return form.name
		}
	}
	return ""
}

// modalOpen reports whether a dialog or a full-window view owns the keyboard,
// so the window's tab shortcuts stand aside while it does.
func (a *app) modalOpen() bool {
	return a.modalOpenIncludingDiffFullscreen(true)
}

// modalOpenExceptDiffFullscreen lets a read-only maximized diff follow new
// commits while preserving the refresh protection every actual dialog has.
func (a *app) modalOpenExceptDiffFullscreen() bool {
	return a.modalOpenIncludingDiffFullscreen(false)
}

func (a *app) modalOpenIncludingDiffFullscreen(includeDiffFullscreen bool) bool {
	return a.dialogOpen(includeDiffFullscreen, false)
}

// paletteBlocked reports whether something that owns the whole window is open,
// so the Command Palette must not open over it. The commit message box, the
// list filter, a line selection and a maximized diff or branch view are part of
// a page, and the palette opens over them.
func (a *app) paletteBlocked() bool { return a.dialogOpen(false, true) }

// paletteBlockReason names what stops the Command Palette opening, for the
// message that says so.
func (a *app) paletteBlockReason() string {
	for _, item := range []struct {
		on   bool
		name string
	}{
		{a.confirm != nil, tr("a confirmation")},
		{a.settingsUI.open, tr("Settings")},
		{a.keybindingsOpen, tr("the keyboard shortcuts")},
		{a.commandLogOpen, tr("the command log")},
		{a.commandPromptOpen, tr("the command line")},
		{a.patchPreviewOpen, tr("the patch preview")},
		{a.stackSync.open, tr("the stack sync")},
		{a.performance.open, tr("Repository performance")},
		{a.customPatch.open, tr("the custom patch")},
		{a.courier.open, tr("the change courier")},
		{a.ssh.workspaceOpen || a.ssh.hostKeyPrompt != nil, tr("the SSH dialog")},
		{a.state == stateCloning, tr("the clone in progress")},
		{a.crash != "", tr("an error report")},
		{a.formOpen(true), tr("a form") + " (" + a.openFormName(true) + ")"},
	} {
		if item.on {
			return item.name
		}
	}
	return ""
}

func (a *app) dialogOpen(includeDiffFullscreen, inline bool) bool {
	return a.confirm != nil ||
		a.settingsUI.open ||
		a.keybindingsOpen ||
		a.commandLogOpen ||
		a.commandPromptOpen ||
		a.patchPreviewOpen ||
		a.assistance.conflict.open ||
		a.stackSync.open ||
		a.performance.open ||
		a.customPatch.open ||
		a.courier.open ||
		a.ssh.workspaceOpen ||
		a.ssh.hostKeyPrompt != nil ||
		a.state == stateCloning ||
		(includeDiffFullscreen && a.diffFullscreen) ||
		(!inline && a.branchFullscreen) ||
		a.formOpen(inline) ||
		(!inline && a.lineSelectMode) ||
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

// commitFilterButton opens a commit list's filter. While a filter narrows the
// list it is filled with the accent, so a short list says why it is short at a
// glance, without the filter having to be opened first.
func commitFilterButton(c *ui.Context, tooltip string, active bool, run func()) {
	theme := c.Theme()
	button := ui.Button(c, "").Padding(3, 5)
	// The applied state is exposed as a pressed toggle, so assistive
	// technology reports it too, while the name stays "Filter commits".
	clicked := button.Label(tr("Filter commits")).Tooltip(tr(tooltip)).Checked(active).Clicked()
	button.Children(func() {
		if active {
			ui.Icon(c, filterIcon).FontSize(14).TextColor(theme.AccentText)
		} else {
			ui.Icon(c, filterIcon).FontSize(13)
		}
	})
	if active {
		fill := theme.Accent
		if button.Hovered() {
			fill = theme.AccentHover
		}
		button.Background(fill).Border(1, theme.Accent)
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if clicked {
		run()
	}
}

// commitCountBadge shows how many commits a list holds. When another page is
// waiting below, its tooltip says so, since the number is how far the list has
// loaded, not how many commits there are.
func commitCountBadge(c *ui.Context, count int, more bool) {
	tooltip := trf("%d commits shown.", count)
	if more {
		tooltip = trf("%d commits loaded so far. Scroll down to load more, or use the filter to search the commit history.", count)
	}
	ui.Badge(c, fmt.Sprintf("%d", count)).Tooltip(tooltip)
}

// clearFilterButton clears every field of a commit list's filter. A list shows
// it beside the filter button only while a filter narrows it.
func clearFilterButton(c *ui.Context, tooltip string, run func()) {
	commitHeaderButton(c, "Clear all filters", tooltip, closeIcon, run)
}

// commitRefreshButton puts the repository refresh the header offers where the
// commits are, so a list that looks stale can be re-read in place. It rests
// while that refresh runs, like the header button.
func (a *app) commitRefreshButton(c *ui.Context) {
	flatIconButton(c, "Refresh commits", "Reload the commit list", refreshIcon, 14, 4, a.actionBusy, a.beginRefresh)
}

// loadingPane centers one spinner and its message in the pane's remaining
// space. Inline progress in toolbars, forms, and footers deliberately stays
// compact instead of using this full-pane placeholder.
func loadingPane(c *ui.Context, label, message string) {
	ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
		ui.Spinner(c).Label(tr(label))
		ui.Text(c, tr(message)).TextColor(c.Theme().TextMuted).
			TextAlign(ui.Center).AlignSelf(ui.Center)
	})
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
	a.repositoryFormModalWith(c, open, width, title, subtitle, icon, body, nil)
}

// repositoryFormModalWith is repositoryFormModal with a hook that sees the
// modal's backdrop, for a shortcut the form needs while it is open.
func (a *app) repositoryFormModalWith(c *ui.Context, open *bool, width float32, title, subtitle string,
	icon *ui.SVG, body func(), onBackdrop func(back ui.Element)) {
	if !*open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		if onBackdrop != nil {
			onBackdrop(back)
		}
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

// collapsedRailBackground is the face of the slim rail a collapsed pane leaves
// behind. In a dark theme the islands' backdrop is half black, which made the
// rail read as a hole in the window; here it stays nearer the theme's own color.
func collapsedRailBackground(theme *ui.Theme) ui.Color {
	if theme.Dark {
		return theme.Background.Mix(black, 0.15)
	}
	return islandBackdrop(theme)
}
