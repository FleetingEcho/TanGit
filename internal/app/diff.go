package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// buildDiffState turns a diff into the editor's viewer state, laid out the
// way the diff toggles choose: unified or side by side, and long lines
// wrapped or scrolled sideways.
func buildDiffState(layout editor.DiffLayout, wrap bool, file repository.File, diff repository.Diff, options repository.DiffOptions) (*editor.DiffState, error) {
	state := &editor.DiffState{
		Layout:       layout,
		ContextLines: options.ContextLines,
		Wrap:         wrap,
		Inline:       editor.DiffInlineWordAlt,
	}
	if diff.Patch != "" {
		patches, err := editor.ParsePatch(diff.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse file diff: %w", err)
		}
		if len(patches) == 0 {
			return nil, fmt.Errorf("file diff contained no patches")
		}
		// Git prints no lines of a binary change, so there is no viewer for
		// it; the pane says there is no text to show instead of drawing an
		// empty one.
		if patches[0].Binary {
			return nil, nil
		}
		state.SetPatch(patches[0])
		state.Annotations = movedLineAnnotations(patches[0])
		if len(state.Annotations) > 0 {
			state.RenderAnnotation = renderMovedLine
		}
	} else {
		state.Old = editor.NewBuffer(diff.OldText)
		state.New = editor.NewBuffer(diff.NewText)
	}

	language := editor.LanguageFor(file.Path)
	state.Old.SetLanguage(language)
	state.New.SetLanguage(language)
	return state, nil
}

// diffLayout is how every diff viewer lays its two texts out: side by side
// when the layout toggle is on, unified otherwise.
func (a *app) diffLayout() editor.DiffLayout {
	if a.diffSplit {
		return editor.DiffSplit
	}
	return editor.DiffUnified
}

// toggleDiffLayout switches every diff on screen between unified and side by
// side, and the diffs read from now on stay with it.
func (a *app) toggleDiffLayout() {
	a.diffSplit = !a.diffSplit
	a.settings.DiffSplit = a.diffSplit
	a.applyDiffOptions()
	a.saveSettings()
}

// toggleDiffWrap switches every diff on screen between wrapping long lines
// and scrolling them sideways, and the diffs read from now on stay with it.
func (a *app) toggleDiffWrap() {
	a.diffWrap = !a.diffWrap
	a.settings.DiffWrap = a.diffWrap
	a.applyDiffOptions()
	a.saveSettings()
}

// selectedBufferFont leaves the family empty for System, so MyGo uses the
// platform's monospaced family. Size and line spacing stay independent of the
// interface theme's font size and layout spacing.
func selectedBufferFont(family string, fontSize float64) ui.Font {
	family = strings.TrimSpace(family)
	if family == "System" {
		family = ""
	}
	return ui.Font{Family: family, Size: float32(fontSize)}
}

// applyBufferFonts updates buffers already on screen as well as those a later
// pane chooses. New states loaded in the background receive the same setting
// on the frame that publishes them.
func (a *app) applyBufferFonts() {
	font := selectedBufferFont(a.settings.BufferFont, a.settings.BufferFontSize)
	lineHeight := float32(a.settings.BufferLineHeight)
	for _, state := range a.liveDiffs() {
		if state != nil {
			state.Font = font
			state.LineHeight = lineHeight
		}
	}
	states := []*editor.State{a.fileEditState, a.conflictView, a.blameState}
	states = append(states, a.conflictStageViews[:]...)
	for _, document := range a.code.documents {
		if document != nil {
			states = append(states, document.state)
		}
	}
	if a.conflictSides != nil {
		states = append(states, a.conflictSides.view)
	}
	for _, state := range states {
		if state != nil {
			state.Font = font
			state.LineHeight = lineHeight
		}
	}
}

// applyDiffOptions gives every diff the app already holds the current layout
// and wrapping, so a toggle shows at once in the pane on screen.
func (a *app) applyDiffOptions() {
	for _, state := range a.liveDiffs() {
		if state != nil {
			state.Layout = a.diffLayout()
			state.Wrap = a.diffWrap
		}
	}
	// The markers of a conflicted file are not a diff, but they wrap with
	// them: the header's wrap control sits beside the review of the sides.
	if a.conflictView != nil {
		a.conflictView.SoftWrap = a.diffWrap
	}
}

// liveDiffs are the diff viewers the app keeps, one per section that shows a
// diff, plus the file-history drawer's.
func (a *app) liveDiffs() []*editor.DiffState {
	return []*editor.DiffState{
		a.diffState,
		a.stashDiffState,
		a.commitDiffState,
		a.branchCommitDiffState,
		a.fileHistoryDiffState,
		a.conflictDiffState,
		a.compare.diffState,
		a.agents.review.diffState,
	}
}

// diffLayoutButton is the icon above a diff that toggles its layout. As the
// wrap toggle beside it, it shows the state on screen: the side-by-side icon,
// filled with the accent, while the diff is split, and the unified icon on a
// plain button otherwise. Its tooltip names the layout a click gives.
func (a *app) diffLayoutButton(c *ui.Context) {
	tooltip := "Switch to side-by-side diff"
	icon := diffUnifiedIcon
	// One button only: the constructor appends the node, so choose the
	// look before building it.
	var button ui.Element
	if a.diffSplit {
		tooltip = "Switch to unified diff"
		icon = diffSplitIcon
		button = compactIconButton(ui.PrimaryButton(c, ""))
	} else {
		button = compactIconButton(ui.Button(c, ""))
	}
	clicked := button.Label(tr("Toggle diff layout")).Tooltip(tr(tooltip)).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(14)
	})
	if clicked {
		a.toggleDiffLayout()
	}
}

// diffWrapButton is the icon above a diff that toggles wrapping, for reading
// long lines without scrolling sideways. It sits beside the layout toggle and,
// as that one, shows the state on and the other state in its tooltip.
func (a *app) diffWrapButton(c *ui.Context) {
	tooltip := "Long lines scroll sideways — click to wrap them"
	icon := diffWrapIcon
	// One button only: the constructor appends the node, so choose the
	// look before building it.
	var button ui.Element
	if a.diffWrap {
		tooltip = "Long lines are wrapped — click to scroll them sideways"
		button = compactIconButton(ui.PrimaryButton(c, ""))
	} else {
		button = compactIconButton(ui.Button(c, ""))
	}
	clicked := button.Label(tr("Toggle diff line wrapping")).Tooltip(tr(tooltip)).Clicked()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(14)
	})
	if clicked {
		a.toggleDiffWrap()
	}
}

// shownDiff is the diff a pane is showing, with what the full-window view
// calls it.
type shownDiff struct {
	title    string
	subtitle string
	// path is the file the diff changes, as the download names its file, and
	// patch is the diff itself, as it was read, for saving.
	path  string
	patch string
	state *editor.DiffState
	media *mediaReview
}

// activeDiff is the diff the active section shows, so the expand button and
// the full-window view follow the pane the user is looking at. It names the
// file even while the diff is being read, so the full-window header stays put
// as another file is chosen; state is nil until that read lands.
func (a *app) activeDiff() (shownDiff, bool) {
	if a.fileHistoryOpen {
		if a.fileHistoryDiffPath == "" {
			return shownDiff{}, false
		}
		return shownDiff{
			title:    a.fileHistoryDiffPath,
			subtitle: fileHistorySubtitle(a.fileHistoryCommits, a.fileHistoryFilesHash),
			path:     a.fileHistoryDiffPath,
			patch:    a.fileHistoryDiffPatch,
			state:    a.fileHistoryDiffState,
			media:    a.fileHistoryDiffMedia,
		}, true
	}
	switch a.workspaceTab {
	case sectionAgents:
		path := a.agentReviewFileLabel()
		if !a.agents.review.open || path == "" {
			return shownDiff{}, false
		}
		return shownDiff{
			title:    path,
			subtitle: a.agents.review.title + " · " + agentShortHash(a.agents.review.base),
			path:     path,
			patch:    a.agents.review.diffPatch,
			state:    a.agents.review.diffState,
		}, true
	case sectionChanges:
		change, ok := a.selectedChange()
		if !ok {
			return shownDiff{}, false
		}
		if change.file.HasMergeConflicts {
			// A conflicted file is reviewed as the two sides of its merge:
			// the current branch's version against the incoming one.
			if a.conflictDiffState == nil {
				return shownDiff{}, false
			}
			return shownDiff{
				title:    change.file.Path,
				subtitle: tr("Merge conflict"),
				path:     change.file.Path,
				patch:    a.conflictDiffPatch,
				state:    a.conflictDiffState,
			}, true
		}
		subtitle := trf("%s changes", tr(a.diffKind))
		if a.diffKind == "" {
			// The diff is still being read: say which side of the file
			// this is from the section it sits in.
			side := tr("Unstaged")
			if change.staged {
				side = tr("Staged")
			}
			subtitle = trf("%s changes", side)
		}
		return shownDiff{
			title:    change.file.Path,
			subtitle: subtitle,
			path:     change.file.Path,
			patch:    a.diffPatch,
			state:    a.diffState,
			media:    a.diffMedia,
		}, true
	case sectionHistory, sectionGraph:
		if a.commitDiffPath == "" {
			return shownDiff{}, false
		}
		return shownDiff{
			title:    a.commitDiffPath,
			subtitle: a.commitSubject(a.selectedCommit),
			path:     a.commitDiffPath,
			patch:    a.commitDiffPatch,
			state:    a.commitDiffState,
			media:    a.commitDiffMedia,
		}, true
	case sectionBranches:
		if a.branchCommitDiffPath == "" {
			return shownDiff{}, false
		}
		subject := ""
		if a.selectedBranchCommit >= 0 && a.selectedBranchCommit < len(a.branchCommits) {
			subject = a.branchCommits[a.selectedBranchCommit].Subject
		}
		return shownDiff{
			title:    a.branchCommitDiffPath,
			subtitle: subject,
			path:     a.branchCommitDiffPath,
			patch:    a.branchCommitDiffPatch,
			state:    a.branchCommitDiffState,
			media:    a.branchCommitDiffMedia,
		}, true
	case sectionCompare:
		if a.compare.selected < 0 || a.compare.selected >= len(a.compare.files) {
			return shownDiff{}, false
		}
		file := a.compare.files[a.compare.selected]
		return shownDiff{
			title:    file.Path,
			subtitle: a.compare.appliedFrom + " → " + a.compare.appliedTo,
			path:     file.Path,
			patch:    a.compare.diffPatch,
			state:    a.compare.diffState,
		}, true
	case sectionStash:
		if a.selectedStash < 0 || a.selectedStash >= len(a.snapshot.Stashes) ||
			a.selectedStashFile < 0 || a.selectedStashFile >= len(a.stashFiles) {
			return shownDiff{}, false
		}
		return shownDiff{
			title:    a.snapshot.Stashes[a.selectedStash].Name,
			subtitle: a.stashFiles[a.selectedStashFile].Path,
			path:     a.stashFiles[a.selectedStashFile].Path,
			patch:    a.stashDiffPatch,
			state:    a.stashDiffState,
			media:    a.stashDiffMedia,
		}, true
	}
	return shownDiff{}, false
}

// activeDiffLoading reports whether the section on screen is still reading
// the diff the full-window view shows.
func (a *app) activeDiffLoading() bool {
	if a.fileHistoryOpen {
		return a.fileHistoryDiffLoading
	}
	switch a.workspaceTab {
	case sectionAgents:
		return a.agents.review.diffLoading
	case sectionChanges:
		return a.diffLoading
	case sectionHistory, sectionGraph:
		return a.commitDiffLoading
	case sectionBranches:
		return a.branchCommitDiffLoading
	case sectionStash:
		return a.stashDiffLoading
	case sectionCompare:
		return a.compare.diffLoading
	}
	return false
}

// activeDiffError is why the section on screen has no diff to show, "" when
// the read has not failed.
func (a *app) activeDiffError() string {
	if a.fileHistoryOpen {
		return a.fileHistoryDiffError
	}
	switch a.workspaceTab {
	case sectionAgents:
		return a.agents.review.diffError
	case sectionChanges:
		return a.diffError
	case sectionHistory, sectionGraph:
		return a.commitDiffError
	case sectionBranches:
		return a.branchCommitDiffError
	case sectionStash:
		return a.stashDiffError
	case sectionCompare:
		return a.compare.diffError
	}
	return ""
}

func (a *app) commitSubject(index int) string {
	commits := a.visibleCommits()
	if index < 0 || index >= len(commits) {
		return ""
	}
	commit := commits[index]
	subject := commit.ShortHash
	if commit.Subject != "" {
		subject += " · " + commit.Subject
	}
	return subject
}

const (
	diffFullscreenDetailsMinHeight  = float32(60)
	diffFullscreenDetailsSplitInset = float32(2)
	diffRegionMinHeight             = float32(200)
)

// openDiffFullscreen shows the active diff over the whole window, for reading
// a large change without the panes around it.
func (a *app) openDiffFullscreen() {
	if _, ok := a.activeDiff(); !ok {
		return
	}
	a.diffFullscreenDetailsHeight = diffFullscreenDetailsMinHeight
	a.diffFullscreenDetailsScroll = ui.ScrollState{}
	a.diffFullscreenMessageScroll = ui.ScrollState{}
	a.diffFullscreenMessageHeight = 0
	a.diffFullscreen = true
}

// expandDiffButton is the icon every diff header carries to review the diff
// full size; it is disabled while the pane shows no diff.
func (a *app) expandDiffButton(c *ui.Context) {
	_, ok := a.activeDiff()
	iconButton(c, "Expand diff", "Review this diff in the whole window", expandIcon, !ok, a.openDiffFullscreen)
}

// diffViewOptions draws the diff-wide controls on the row a pane shows above
// its diff: the layout and wrapping toggles and the expand control. Keeping
// them with the diff they change, instead of the window's top bar, puts them
// where the user looks while reading the diff.
func (a *app) diffViewOptions(c *ui.Context) {
	a.diffViewOptionsWith(c, nil, true)
}

// diffViewOptionsWith adds pane-specific controls before the ordinary diff
// actions and optionally includes their expand control. Commit details lead
// with the file/diff orientation switch; their resizable message/file panes
// use middle dividers instead of a maximize control.
func (a *app) diffViewOptionsWith(c *ui.Context, leading func(*ui.Context), includeExpand bool) {
	// Own the gap here rather than inheriting each pane's header spacing. Text
	// buttons keep their natural width, so translations never collide with the
	// fixed-width icon controls beside them.
	ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
		if leading != nil {
			leading(c)
		}
		a.diffWhitespaceButton(c)
		a.diffContextButton(c)
		a.diffLayoutButton(c)
		a.diffWrapButton(c)
		a.exportDiffButton(c)
		if includeExpand {
			a.expandDiffButton(c)
		}
	})
}

func (a *app) compareDiffControlsActive() bool {
	return a.workspaceTab == sectionCompare && !a.fileHistoryOpen
}

// toggleDiffWhitespace changes the patch, rather than just its presentation.
// Compare owns independent options; every ordinary diff shares the others.
func (a *app) toggleDiffWhitespace() {
	if a.compareDiffControlsActive() {
		a.compare.options.IgnoreWhitespace = !a.compare.options.IgnoreWhitespace
		a.refreshAppliedCompare()
		return
	}
	a.diffOptions.IgnoreWhitespace = !a.diffOptions.IgnoreWhitespace
	a.reloadOrdinaryDiffs()
}

// cycleDiffContext keeps the control compact in six diff headers while
// offering the useful no-context, narrow, default and generous readings.
func (a *app) cycleDiffContext() {
	choices := []int{0, 1, 3, 5, 10}
	current := a.diffOptions.ContextLines
	if a.compareDiffControlsActive() {
		current = a.compare.options.ContextLines
	}
	next := 3
	for i, choice := range choices {
		if current == choice {
			next = choices[(i+1)%len(choices)]
			break
		}
	}
	if a.compareDiffControlsActive() {
		a.compare.options.ContextLines = next
		a.compare.context = fmt.Sprintf("%d", next)
		a.refreshAppliedCompare()
		return
	}
	a.diffOptions.ContextLines = next
	a.reloadOrdinaryDiffs()
}

func canonicalDiffOptions(options repository.DiffOptions) bool {
	return !options.IgnoreWhitespace && options.ContextLines == 3
}

// reloadOrdinaryDiffs drops only patches, never the file/commit lists around
// them. Existing identity checks and tokens reject every superseded response.
func (a *app) reloadOrdinaryDiffs() {
	a.lineSelectMode = false
	if change, ok := a.selectedChange(); !ok || !change.file.HasMergeConflicts {
		a.beginDiff()
	}
	a.commitDiffHash, a.commitDiffPath = "", ""
	a.commitDiffPatch, a.commitDiffError = "", ""
	a.commitDiffState = nil
	a.commitDiffMedia = nil
	a.commitDiffLoading = false

	a.branchCommitToken++
	a.branchCommitDiffHash, a.branchCommitDiffPath = "", ""
	a.branchCommitDiffPatch, a.branchCommitDiffError = "", ""
	a.branchCommitDiffState = nil
	a.branchCommitDiffMedia = nil
	a.branchCommitDiffLoading = false

	a.stashDiffHash, a.stashDiffPath = "", ""
	a.stashDiffPatch, a.stashDiffError = "", ""
	a.stashDiffState = nil
	a.stashDiffMedia = nil
	a.stashDiffLoading = false

	if a.fileHistoryOpen && a.fileHistorySelected >= 0 &&
		a.fileHistorySelected < len(a.fileHistoryCommits) &&
		a.fileHistorySelectedFile >= 0 && a.fileHistorySelectedFile < len(a.fileHistoryFiles) {
		a.loadFileHistoryDiff(a.fileHistoryCommits[a.fileHistorySelected],
			a.fileHistoryFiles[a.fileHistorySelectedFile], true)
	}
}

func (a *app) diffWhitespaceButton(c *ui.Context) {
	// A constructor appends its button, so choose the active look before
	// building it rather than replacing a plain button with a primary one.
	options := a.diffOptions
	if a.compareDiffControlsActive() {
		options = a.compare.options
	}
	var button ui.Element
	if options.IgnoreWhitespace {
		button = compactIconButton(ui.PrimaryButton(c, ""))
	} else {
		button = compactIconButton(ui.Button(c, ""))
	}
	clicked := button.Label(tr("Toggle whitespace-only diff changes")).
		Tooltip(tr("Alt+W · Hide or show whitespace-only changes")).Clicked()
	button.Children(func() {
		ui.Icon(c, diffWhitespaceIcon).FontSize(14)
	})
	if clicked {
		a.toggleDiffWhitespace()
	}
}

func (a *app) diffContextButton(c *ui.Context) {
	options := a.diffOptions
	if a.compareDiffControlsActive() {
		options = a.compare.options
	}
	button := compactIconButton(ui.Button(c, ""))
	clicked := button.Label(tr("Change diff context lines")).
		Tooltip(tr("Alt+U · Cycle through 0, 1, 3, 5, and 10 context lines")).Clicked()
	button.Children(func() {
		// Keep the current count visible without a long text-only button in
		// the icon toolbar.
		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, diffContextIcon).FontSize(14)
			ui.Text(c, fmt.Sprintf("%d", options.ContextLines)).FontSize(c.Theme().FontSize - 2)
		})
	})
	if clicked {
		a.cycleDiffContext()
	}
}

// fullscreenCommitDetail returns the selected commit and its existing
// detail state for the metadata pane above a commit's whole-window diff.
func (a *app) fullscreenCommitDetail() (repository.Commit, commitDetailViewState, bool) {
	if a.fileHistoryOpen {
		return repository.Commit{}, commitDetailViewState{}, false
	}
	switch a.workspaceTab {
	case sectionHistory, sectionGraph, sectionReflog:
		commit, ok := a.selectedVisibleCommit()
		return commit, a.historyCommitDetailViewState(), ok
	case sectionBranches:
		if a.selectedBranchCommit < 0 || a.selectedBranchCommit >= len(a.branchCommits) {
			return repository.Commit{}, commitDetailViewState{}, false
		}
		return a.branchCommits[a.selectedBranchCommit], a.branchCommitDetailViewState(), true
	default:
		return repository.Commit{}, commitDetailViewState{}, false
	}
}

// diffFullscreenView draws the active diff over the workspace, with the way
// back at its top right and, beside the diff, the files it can switch between.
// Commit diffs also have a compact details card above a draggable middle
// divider; pulling it down reveals the shared metadata/Markdown renderer.
func (a *app) diffFullscreenView(c *ui.Context, overlayTopInset float32) {
	if !a.diffFullscreen {
		return
	}
	theme := c.Theme()
	target, ok := a.activeDiff()
	ui.Overlay(c, func() {
		panel := ui.Column(c).Absolute().Left(0).Top(overlayTopInset).Right(0).Bottom(0).
			Padding(islandGap / 2).Gap(0).Background(islandBackdrop(theme)).Modal()
		a.registerTerminalModalShortcuts(panel)
		if !a.settings.DisableShortcuts {
			panel.OnShortcut(0, ui.KeyF6, func() { a.cycleKeyboardPane(c, 1) })
			panel.OnShortcut(ui.Shift, ui.KeyF6, func() { a.cycleKeyboardPane(c, -1) })
		}
		panel.Children(func() {
			a.headerViewWithID(c, "fullscreen-toolbar")
			commit, detailState, hasCommitDetails := a.fullscreenCommitDetail()
			showCommitDetails := ok && hasCommitDetails
			if showCommitDetails && a.diffFullscreenDetailsHeight < diffFullscreenDetailsMinHeight {
				a.diffFullscreenDetailsHeight = diffFullscreenDetailsMinHeight
			}
			renderHeader := func(details func()) {
				card := ui.Column(c).Gap(10).Padding(10, 14)
				if showCommitDetails {
					card.Grow(1)
				}
				islandCard(c, card).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Shrink(0).Children(func() {
						ui.Icon(c, diffSplitIcon).FontSize(18).TextColor(theme.Accent)
						ui.Column(c).Gap(1).Grow(1).Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Text(c, target.title).FontSize(17).Bold().SingleLine().Tooltip(target.title)
								if path := target.path; path != "" {
									services := c.Services()
									flatIconButton(c, "Copy file path", "Copy the file path", copyIcon, 14, 3, false,
										func() { a.notifyCopied(services, "relative path", path) })
								}
							})
							if target.subtitle != "" {
								ui.Text(c, target.subtitle).TextColor(theme.TextMuted).
									FontSize(theme.FontSize - 1).SingleLine()
							}
						})
						if a.compareDiffControlsActive() {
							a.compareDiffViewOptions(c, true)
						} else {
							a.diffWhitespaceButton(c)
							a.diffContextButton(c)
							a.diffLayoutButton(c)
							a.diffWrapButton(c)
							a.exportDiffButton(c)
							iconButton(c, "Restore diff view", "Restore diff view", shrinkIcon, false, a.closeDiffFullscreen)
						}
					})
					if details != nil {
						detailsScroll := ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Label(tr("Commit details"))
						if a.diffFullscreenDetailsScroll.MaxY > 0 {
							detailsScroll.KeepScrollbars(true)
						}
						detailsScroll.TrackScroll(&a.diffFullscreenDetailsScroll)
						detailsScroll.Children(details)
					}
				})
			}
			renderDiff := func() {
				if !ok {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("There is no diff to show.")).TextColor(theme.TextMuted)
					})
					return
				}
				files := a.fullscreenDiffFiles()
				a.diffFullscreenRows = files
				if len(files) == 0 {
					a.diffFullscreenDiff(c, theme, target)
					return
				}
				a.diffFullscreenFilesView(c, theme, target, files)
			}
			if showCommitDetails {
				paneSplitVerticalReserve(c, &a.diffFullscreenDetailsHeight, diffRegionMinHeight, diffFullscreenDetailsSplitInset, func() {
					if a.diffFullscreenDetailsHeight <= diffFullscreenDetailsMinHeight+1 {
						renderHeader(nil)
						return
					}
					renderHeader(func() {
						a.commitDetailMetadataBody(c, &commit, detailState, &a.diffFullscreenMessageScroll)
					})
				}, renderDiff).Grow(1)
				return
			}
			renderHeader(nil)
			// The file actions sit in an island of their own rather than on
			// the drawer's bare backdrop, matching the island language
			// every other pane uses.
			if a.workspaceTab == sectionChanges {
				islandCard(c, ui.Column(c).Padding(8, 10).Gap(4)).Children(func() {
					if a.hunkMode {
						selectionToolGroup(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
							a.hunkModeButton(c)
							a.hunkActionControls(c)
						})
					} else {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
							a.hunkModeButton(c)
						})
					}
					if a.lineSelectionAvailable() {
						a.syncLineSelection()
						a.lineSelectionControls(c, false)
					}
				})
			}
			renderDiff()
		})
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			a.closeDiffFullscreen()
		}
	})
}

// fullscreenFile is one row of the whole-window diff's file list: what the row
// shows and what choosing it does. id tells apart the same path on both sides
// of a file, which the Changes section lists twice.
type fullscreenFile struct {
	id       string
	label    string
	status   string
	previous string
	staged   bool
	current  bool
	choose   func()
}

// fullscreenDiffFiles are the files the whole-window diff can switch between:
// the ones the pane it reads lists, with the choice that shows each. Choosing
// a row moves that pane's own selection, so reading another file there and
// here is the same act.
func (a *app) fullscreenDiffFiles() []fullscreenFile {
	if a.fileHistoryOpen {
		if a.fileHistorySelected < 0 || a.fileHistorySelected >= len(a.fileHistoryCommits) {
			return nil
		}
		commit := a.fileHistoryCommits[a.fileHistorySelected]
		files := make([]fullscreenFile, 0, len(a.fileHistoryFiles))
		for i, file := range a.fileHistoryFiles {
			files = append(files, fullscreenFile{
				id:       "file-history\x00" + file.Path,
				label:    file.Path,
				status:   file.Status,
				previous: file.PreviousPath,
				current:  i == a.fileHistorySelectedFile,
				choose: func() {
					a.fileHistorySelectedFile = i
					a.loadFileHistoryDiff(commit, file, false)
				},
			})
		}
		return files
	}
	switch a.workspaceTab {
	case sectionChanges:
		files := make([]fullscreenFile, 0, len(a.changeRows))
		for i, row := range a.changeRows {
			if !row.isFile() {
				continue
			}
			files = append(files, fullscreenFile{
				id:       row.sideKey() + "\x00" + row.file.Path,
				label:    row.file.Path,
				status:   row.file.Status,
				previous: row.file.PreviousPath,
				staged:   row.staged,
				current:  i == a.selectedFile,
				choose:   func() { a.selectedFile = i },
			})
		}
		return files
	case sectionHistory, sectionGraph:
		files := make([]fullscreenFile, 0, len(a.commitFiles))
		for i, file := range a.commitFiles {
			files = append(files, fullscreenFile{
				id:      "commit\x00" + file.Path,
				label:   file.Path,
				status:  file.Status,
				current: i == a.selectedCommitFile,
				choose:  func() { a.selectedCommitFile = i },
			})
		}
		return files
	case sectionBranches:
		files := make([]fullscreenFile, 0, len(a.branchCommitFiles))
		for i, file := range a.branchCommitFiles {
			files = append(files, fullscreenFile{
				id:      "branch-commit\x00" + file.Path,
				label:   file.Path,
				status:  file.Status,
				current: i == a.selectedBranchCommitFile,
				choose:  func() { a.selectedBranchCommitFile = i },
			})
		}
		return files
	case sectionCompare:
		files := make([]fullscreenFile, 0, len(a.compare.files))
		for i, file := range a.compare.files {
			files = append(files, fullscreenFile{
				id:       "compare\x00" + file.Path,
				label:    file.Path,
				status:   file.Status,
				previous: file.PreviousPath,
				current:  i == a.compare.selected,
				choose:   func() { a.selectCompareSource(i) },
			})
		}
		return files
	case sectionStash:
		files := make([]fullscreenFile, 0, len(a.stashFiles))
		for i, file := range a.stashFiles {
			files = append(files, fullscreenFile{
				id:       "stash\x00" + file.Path,
				label:    file.Path,
				status:   file.Status,
				previous: file.PreviousPath,
				current:  i == a.selectedStashFile,
				choose:   func() { a.selectedStashFile = i },
			})
		}
		return files
	}
	return nil
}

// diffFullscreenFilesView draws the whole-window diff beside the files it
// reads: the pane's files on the left, the diff on the right, and a line
// between them the user drags to give one more room than the other.
func (a *app) diffFullscreenFilesView(c *ui.Context, theme *ui.Theme, target shownDiff, files []fullscreenFile) {
	current := -1
	for i, file := range files {
		if file.current {
			current = i
		}
	}
	collapsiblePaneSplit(c, &a.diffFullscreenFilesWidth, &a.diffFullscreenFilesCollapsed,
		"Expand changed files", func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					ui.Icon(c, folderIcon).FontSize(13).TextColor(theme.TextMuted)
					ui.Text(c, tr("Changed files")).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d", len(files)))
					paneCollapseButton(c, &a.diffFullscreenFilesCollapsed, "Collapse changed files")
				})
				// The list's choice follows the file the diff shows, so the
				// accent marks the one being read.
				a.diffFullscreenFile = current
				list := a.keyboardList(c, &a.diffFullscreenFileList, len(files)).Grow(1).Rows(func(row ui.ListRow) {
					file := files[row.Index]
					style := diffFileStyle(theme, file.status)
					ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8).Children(func() {
						fileTypeMark(row.Context, file.label, 13, row.Selected())
						ui.Text(row.Context, file.label).Grow(1).SingleLine().Tooltip(file.label)
						switch {
						case file.staged:
							ui.Text(row.Context, tr("Staged")).TextColor(secondaryRowColor(theme, row)).
								FontSize(theme.FontSize - 1)
						case file.previous != "":
							ui.Text(row.Context, "← "+file.previous).
								TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1).SingleLine()
						}
						statusSlot(row.Context, statusLetter(file.status), rowStatusColor(theme, row, style))
					}).OnClick(func() {
						a.diffFullscreenFile = row.Index
						file.choose()
					})
				})
				// The keys move the list too, and the row they land on is the
				// file to read.
				if a.keyboardListChanged(&a.diffFullscreenFileList, list) && a.diffFullscreenFile >= 0 && a.diffFullscreenFile < len(files) {
					files[a.diffFullscreenFile].choose()
				}
			})
		}, func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(10)).Children(func() {
				a.diffFullscreenDiff(c, theme, target)
			})
		})
}

// diffFullscreenDiff is the body of the whole-window diff: the file, the
// spinner of a read still running, why the read failed, or the word that there
// is no text.
func (a *app) diffFullscreenDiff(c *ui.Context, theme *ui.Theme, target shownDiff) {
	switch {
	case target.state != nil:
		// Scroll bars of a diff appear only while the pointer is over it, which
		// leaves a whole-window diff of thousands of lines with no sign of its
		// length or position. Keep them showing here.
		target.state.KeepScrollbars = true
		a.keyboardDiffViewerWithID(c, target.state, "fullscreen-diff")
	case target.media != nil:
		mediaReviewView(c, target.media, target.path)
	case a.activeDiffLoading():
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label(tr("Loading diff"))
			ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
		})
	case a.activeDiffError() != "":
		ui.Text(c, a.activeDiffError()).TextColor(theme.Danger).Selectable()
	default:
		ui.Text(c, tr("There is no diff to show.")).TextColor(theme.TextMuted)
	}
}

func (a *app) closeDiffFullscreen() {
	a.diffFullscreen = false
}
