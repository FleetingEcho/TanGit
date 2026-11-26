package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func (a *app) beginCommit() {
	if strings.TrimSpace(a.commitSummary) == "" || !hasStagedFiles(a.snapshot.Files) {
		return
	}
	summary := strings.TrimSpace(a.commitSummary)
	body := strings.TrimSpace(a.commitBody)
	amend := a.commitAmend
	message := trf("Committed %s", summary)
	if amend {
		message = trf("Amended %s", summary)
	}
	run := func() {
		a.beginRepositoryActionMessage(message, "", func(ctx context.Context, session repository.Session) error {
			return requireChangesSession(session).Commit(ctx, summary, body, amend)
		}, func() {
			a.commitSummary = ""
			a.commitBody = ""
			a.commitAmend = false
			a.showCommitForm = false
		})
	}
	if amend {
		a.confirmAttention("Amend commit", summary, "This rewrites the latest commit with the staged changes and the entered message. Published history will require a force push.", run)
	} else {
		run()
	}
}

// toggleStageSelected stages the selected file when it has unstaged changes,
// and unstages it otherwise, as Space does in the Lazygit TUI. A selection of
// lines in the pane's diff acts on those lines instead, leaving the rest of
// the file where it is.
func (a *app) toggleStageSelected() {
	if _, selected := a.selectedLineCount(); selected {
		a.beginLineAction()
		return
	}
	change, ok := a.selectedChange()
	if !ok {
		return
	}
	if change.staged {
		a.beginStageFile(change.file, false)
	} else {
		a.beginStageFile(change.file, true)
	}
}

// lineSelectionAvailable reports whether the pane's diff can stage or unstage
// a selection of its lines.
func (a *app) lineSelectionAvailable() bool {
	return a.diffState != nil && a.diffState.LineSelection && a.diffPatch != ""
}

// currentLineSelections returns the independent ranges picked in multiple-line
// mode, or the viewer's one range during an ordinary selection.
func (a *app) currentLineSelections() []editor.DiffLineRange {
	if a.lineSelectMode && len(a.lineSelectRanges) > 0 {
		return a.lineSelectRanges
	}
	state := a.diffState
	if a.customPatchLineSelectionAvailable() {
		state = a.commitDiffState
	}
	if state == nil {
		return nil
	}
	selection, ok := state.SelectedLines()
	if !ok || selection.End < selection.Start {
		return nil
	}
	return []editor.DiffLineRange{selection}
}

// selectedLineCount is how many lines the pane's diff has selected, and
// whether it has any. Disjoint lines count individually, not as their bounds.
func (a *app) selectedLineCount() (int, bool) {
	if !a.lineSelectionAvailable() {
		return 0, false
	}
	selections := a.currentLineSelections()
	count := 0
	for _, selection := range selections {
		count += selection.End - selection.Start + 1
	}
	return count, count > 0
}

// selectedLinesLabel says how many lines are selected, in the singular for one.
func selectedLinesLabel(count int) string {
	if count == 1 {
		return tr("1 line selected")
	}
	return trf("%d lines selected", count)
}

func (a *app) clearMultipleLineSelection() {
	a.lineSelectRange, a.lineSelectHas = editor.DiffLineRange{}, false
	a.lineSelectRanges = nil
}

// startLineSelection enters the mode where each plain click adds one line and
// Shift-click adds a contiguous range. It starts afresh, so the button never
// adds to a selection the user has forgotten.
func (a *app) startLineSelection() {
	if !a.lineSelectionAvailable() && !a.customPatchLineSelectionAvailable() {
		return
	}
	a.lineSelectMode = true
	a.clearMultipleLineSelection()
	a.clearExtraSelectedLines()
	state := a.diffState
	if a.customPatchLineSelectionAvailable() {
		state = a.commitDiffState
	}
	if state != nil {
		state.SetSelectedLines(editor.DiffLineRange{}, false)
	}
}

// stopLineSelection leaves the mode and drops the selection, which is what its
// Cancel button and Escape do.
func (a *app) stopLineSelection() {
	a.lineSelectMode = false
	a.clearMultipleLineSelection()
	a.clearExtraSelectedLines()
	state := a.diffState
	if a.customPatchActive() && a.commitDiffState != nil {
		state = a.commitDiffState
	}
	if state != nil {
		state.SetSelectedLines(editor.DiffLineRange{}, false)
	}
}

// syncLineSelection preserves independent selections across frames. The native
// viewer range remains the Shift-click anchor, not a replacement for the model.
func (a *app) syncLineSelection() {
	state := a.diffState
	if a.customPatchActive() && a.commitDiffState != nil {
		state = a.commitDiffState
	}
	if !a.lineSelectMode || state == nil {
		a.clearMultipleLineSelection()
		if state != nil {
			state.SetExtraSelectedLines(nil)
		}
		return
	}
	if len(a.lineSelectRanges) == 0 {
		if selection, ok := state.SelectedLines(); ok {
			a.lineSelectRanges = []editor.DiffLineRange{selection}
			a.lineSelectRange, a.lineSelectHas = selection, true
		}
	}
	// The viewer extends one range; every independent selection is painted
	// through the extra ranges so the lines counted are the lines shown.
	if len(a.lineSelectRanges) > 1 {
		state.SetExtraSelectedLines(a.lineSelectRanges)
	} else {
		state.SetExtraSelectedLines(nil)
	}
}

// mergeLineSelections coalesces adjacent clicks into one range while retaining
// gaps between independent clicks. Patch construction and the displayed count
// therefore operate on precisely the chosen lines.
func mergeLineSelections(ranges []editor.DiffLineRange) []editor.DiffLineRange {
	ordered := append([]editor.DiffLineRange(nil), ranges...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Side != ordered[j].Side {
			return ordered[i].Side < ordered[j].Side
		}
		return ordered[i].Start < ordered[j].Start
	})
	merged := make([]editor.DiffLineRange, 0, len(ordered))
	for _, next := range ordered {
		if next.End < next.Start {
			continue
		}
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.Side == next.Side && next.Start <= last.End+1 {
				last.End = max(last.End, next.End)
				if next.Start < last.Start {
					last.Start = next.Start
				}
				continue
			}
		}
		merged = append(merged, next)
	}
	return merged
}

// extendLineSelection enters multiple-line mode on the first line-number
// click. A plain click adds just that line; only an actual Shift/drag range
// adds the unchanged lines between its endpoints.
func (a *app) extendLineSelection(state *editor.DiffState, clicked editor.DiffLineRange) {
	ordinary := state != nil && state == a.diffState && a.lineSelectionAvailable()
	custom := state != nil && state == a.commitDiffState && a.customPatchLineSelectionAvailable()
	if (!ordinary && !custom) || clicked.End < clicked.Start {
		return
	}
	if !a.lineSelectMode {
		a.clearMultipleLineSelection()
		a.lineSelectMode = true
	}
	a.lineSelectRanges = mergeLineSelections(append(a.currentLineSelections(), clicked))
	a.lineSelectRange, a.lineSelectHas = clicked, true
	// MyGo paints one range. Keep the latest click/range as its Shift anchor
	// without highlighting the gaps between independent selections.
	state.SetSelectedLines(clicked, true)
	if len(a.lineSelectRanges) == 1 {
		state.SetSelectedLines(a.lineSelectRanges[0], true)
	}
}

// lineSelectionControls draws what the pane says and offers about staging
// lines: how many are picked and the action when there are some, the way into
// the click-to-extend mode, and the way back out of it.
func (a *app) lineSelectionControls(c *ui.Context, withHint bool) {
	theme := c.Theme()
	count, selected := a.selectedLineCount()
	// One selection mode serves hunks and lines: its row exists only after
	// the toolbar's Select changes button, a line-number click or a selection.
	if !a.hunkMode && !a.lineSelectMode && !selected {
		// An empty placeholder keeps the viewer's position in the column, so
		// the row appearing never makes MyGo see a new diff viewer and drop its
		// keyboard focus.
		ui.Column(c)
		return
	}
	ui.Column(c).FillWidth().Gap(4).Padding(3, 8).Radius(theme.Radius+2).
		Border(1, theme.Accent.Alpha(0.35)).Background(theme.Accent.Alpha(0.05)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
				label := "Select lines to stage"
				if a.lineSelectMode {
					label = "Selecting multiple lines"
				}
				ui.Icon(c, selectLinesIcon).FontSize(14).TextColor(theme.Accent).
					Label(tr(label)).Tooltip(tr(label)).Shrink(0)
				if selected {
					summary := selectedLinesSummary(a.currentLineSelections())
					ui.Text(c, selectedLinesLabel(count)).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 2).SingleLine().Shrink(0).Tooltip(summary)
					if summary != "" {
						ui.Text(c, "("+summary+")").TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 2).SingleLine().Shrink(1).MinWidth(0).Tooltip(summary)
					}
					a.diffLineButton(c)
				} else if withHint {
					var hint string
					switch {
					case a.hunkMode && a.lineSelectMode:
						hint = tr("Click line numbers to pick lines (Shift-click for a range), or use the hunk actions above.")
					case a.hunkMode:
						hint = tr("Click line numbers to pick lines, or use the hunk actions above.")
					case a.lineSelectMode:
						hint = tr("Click line numbers to pick lines (Shift-click for a range).")
					default:
						hint = tr("Click line numbers to pick lines.")
					}
					ui.Text(c, hint).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).
						SingleLine().Width(0).Grow(1).Tooltip(hint)
				}
			})
			if a.lineSelectMode {
				button := ui.ButtonBase(c).Size(22, 22).Padding(3).Radius(4).
					Label(tr("Cancel")).Tooltip(tr("Leave the selection alone and stop choosing lines"))
				button.Children(func() {
					ui.Icon(c, closeIcon).FontSize(12).TextColor(theme.TextMuted)
				})
				if button.Clicked() {
					a.stopLineSelection()
					a.hunkMode = false
				}
			}
		})
	})
}

// diffLineButton offers staging/unstaging and stashing for exactly the lines
// selected in the pane. Stashing belongs only to the working-tree side.
func (a *app) diffLineButton(c *ui.Context) {
	label, caption := tr("Stage selected lines"), tr("Stage")
	tooltip := tr("Stage only the selected lines, leaving the rest of the file in the working tree")
	if a.diffKind == "Staged" {
		label, caption = tr("Unstage selected lines"), tr("Unstage")
		tooltip = tr("Take only the selected lines out of the index, leaving them in the working tree")
	}
	compact := func(button ui.Element) ui.Element {
		return button.Height(22).Padding(2, 7).FontSize(c.Theme().FontSize - 2).Shrink(0).Center()
	}
	if compact(ui.PrimaryButton(c, caption)).Label(label).
		Tooltip(tooltip).Disabled(a.actionBusy).Clicked() {
		a.beginLineAction()
	}
	if a.diffKind != "Staged" {
		if compact(ui.Button(c, tr("Stash")+"…")).Label(tr("Stash selected lines…")).
			Tooltip(tr("Save only the selected lines in a stash")).Disabled(a.actionBusy).Clicked() {
			a.openLineStash()
		}
		if compact(ui.Button(c, tr("Discard")+"…")).Label(tr("Discard selected lines…")).
			Tooltip(tr("Remove only the selected lines from the working tree after confirmation")).Disabled(a.actionBusy).Clicked() {
			a.confirmDiscardSelectedLines()
		}
	}
	if compact(ui.Button(c, tr("Copy"))).Label(tr("Copy selected lines")).
		Tooltip(tr("Copy the selected lines as a patch")).Clicked() {
		a.copySelectedLines()
	}
}

// confirmDiscardSelectedLines removes just the selected lines' working-tree
// changes after asking, the way Discard hunk does for a hunk: the index is left
// alone and a recovery copy is kept.
func (a *app) confirmDiscardSelectedLines() {
	change, ok := a.selectedChange()
	if a.actionBusy || a.session == nil || !ok || a.diffKind == "Staged" || !a.lineSelectionAvailable() {
		return
	}
	file := change.diffFile()
	count, _ := a.selectedLineCount()
	patchText, wholeFile, err := partialPatchSelections(a.diffPatch, file, a.currentLineSelections(), true)
	if err != nil {
		a.notifyError(err)
		return
	}
	if patchText == "" && !wholeFile {
		a.notify("", tr("Nothing to discard"), tr("Select changed lines, not unchanged ones."))
		return
	}
	if wholeFile {
		// A whole-file selection keeps the original patch, so discard reverses
		// only the working tree and never resets the index.
		patchText = a.diffPatch
	}
	a.askConfirm(confirmRequest{
		title:        "Discard Lines",
		message:      trf("Discard the %d selected lines in %s?", count, file.Path),
		detail:       "Only these lines' working-tree changes are removed; the index and staged changes stay untouched. A recovery copy is kept for 7 days in Saved work.",
		confirmLabel: "Discard lines",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Discarded selected lines in %s", file.Path), file.Path,
				func(ctx context.Context, session repository.Session) error {
					return previewAndApplyPatch(ctx, session, repository.PatchRequest{
						Text: patchText, Target: repository.PatchWorktree, Reverse: true,
					})
				}, a.stopLineSelection)
		},
	})
}

// copySelectedLines puts the selected lines on the clipboard as the patch that
// would stage them.
func (a *app) copySelectedLines() {
	change, ok := a.selectedChange()
	if !ok || !a.lineSelectionAvailable() {
		return
	}
	patchText, _, ok := a.selectedLinePatch(change.diffFile(), a.diffKind == "Staged")
	if !ok {
		return
	}
	if patchText == "" {
		patchText = a.diffPatch
	}
	count, _ := a.selectedLineCount()
	a.services.WriteClipboard(patchText)
	a.notify("success", tr("Copied selected lines"), selectedLinesLabel(count))
}

// selectedLinePatch builds the patch shared by selected-line stage and stash.
func (a *app) selectedLinePatch(file repository.File, reverse bool) (string, bool, bool) {
	selections := a.currentLineSelections()
	if len(selections) == 0 {
		return "", false, false
	}
	patchText, wholeFile, err := partialPatchSelections(a.diffPatch, file, selections, reverse)
	if err != nil {
		a.notifyError(err)
		return "", false, false
	}
	if patchText == "" && !wholeFile {
		// Only context lines were selected: there is no change to move.
		a.notify("", tr("Nothing to move"), tr("Select changed lines, not unchanged ones."))
		return "", false, false
	}
	return patchText, wholeFile, true
}

// openLineStash names the selected-line patch before saving it. Selecting the
// whole file uses the existing selected-file path, including untracked files.
func (a *app) openLineStash() {
	change, ok := a.selectedChange()
	if a.actionBusy || a.session == nil || !ok || a.diffKind == "Staged" || !a.lineSelectionAvailable() {
		return
	}
	file := change.diffFile()
	patchText, wholeFile, ok := a.selectedLinePatch(file, false)
	if !ok {
		return
	}
	if wholeFile {
		a.openSelectedFileStash(file)
		return
	}
	// Creating the stash starts from HEAD, while removing the selected work
	// must match the actual working tree, including its unselected edits.
	removalPatch, _, err := partialPatchSelections(a.diffPatch, file, a.currentLineSelections(), true)
	if err != nil {
		a.notifyError(err)
		return
	}
	a.openSelectedLinesStash(file, patchText, removalPatch)
}

// beginLineAction stages or unstages the lines selected in the pane's diff,
// building from them the partial patch the index takes. A selection covering
// every change of the file is the file itself, and is staged or unstaged as a
// whole: a file that is wholly added or deleted is described by its diff, not
// by taking its lines one way or the other.
func (a *app) beginLineAction() {
	change, ok := a.selectedChange()
	if a.actionBusy || a.session == nil || !ok || !a.lineSelectionAvailable() {
		return
	}
	file := change.diffFile()
	reverse := a.diffKind == "Staged"
	patchText, wholeFile, ok := a.selectedLinePatch(file, reverse)
	if !ok {
		return
	}
	if wholeFile {
		a.beginStageFile(file, !reverse)
		return
	}
	a.beginRepositoryAction(file.Path, func(ctx context.Context, session repository.Session) error {
		return previewAndApplyPatch(ctx, session, repository.PatchRequest{
			Text: patchText, Target: repository.PatchIndex, Reverse: reverse,
		})
	}, nil)
}

func (a *app) confirmDiscardFile(file repository.File) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Discard Changes",
		message:      trf("Discard all changes to %s?", file.Path),
		detail:       "A recovery copy is kept for 7 days in Saved work. The current changes will be removed.",
		confirmLabel: "Discard",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Discarded changes to %s", file.Path), file.Path,
				func(ctx context.Context, session repository.Session) error {
					return requireChangesSession(session).Discard(ctx, file)
				}, func() { a.syncDiscardedCodeFiles([]repository.File{file}) })
		},
	})
}

func (a *app) confirmDiscardSelected() {
	change, ok := a.selectedChange()
	if !ok {
		return
	}
	a.confirmDiscardFile(change.file)
}

// confirmDiscardAllChanges asks before throwing away every change in the
// working tree, staged ones included, since the section's discard action
// cannot be undone.
func (a *app) confirmDiscardAllChanges() {
	if a.actionBusy || a.session == nil {
		return
	}
	files := append([]repository.File(nil), a.snapshot.Files...)
	if len(files) == 0 {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Discard All Changes",
		message:      trf("Discard all %d changes?", len(files)),
		detail:       "Staged and unstaged changes are removed. One recovery copy is kept for 7 days in Saved work.",
		confirmLabel: "Discard all",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(tr("Discarded all changes"), "",
				func(ctx context.Context, session repository.Session) error {
					return requireChangesSession(session).DiscardFiles(ctx, files)
				}, func() { a.syncDiscardedCodeFiles(files) })
		},
	})
}

// fileRowMenu gives a working-tree row the actions its file supports, as a
// right-click menu. Actions run on the clicked row, whether or not it is
// selected, and every file offers both of its paths to copy.
func (a *app) fileRowMenu(m *ui.Menu, index int, services ui.Services) {
	if index < 0 || index >= len(a.changeRows) || !a.changeRows[index].isFile() {
		return
	}
	change := a.changeRows[index]
	file := change.file
	root := a.snapshot.Root
	if m.Item(tr("Open in built-in editor")).Chosen() {
		a.openGitFileInBuiltInEditor(root, file.Path)
	}
	m.Separator()
	// A right-click selects the row it acts on, as VS Code's Source Control
	// view does, so the details pane follows what the menu changes.
	a.selectedFile = index

	reviewLabel := "Mark reviewed"
	if a.review.files[file.Path] {
		reviewLabel = "Mark unreviewed"
	}
	if m.Item(tr(reviewLabel)).Shortcut(0, ui.KeyR).Disabled(a.actionBusy).Chosen() {
		a.setFileReviewed(file, !a.review.files[file.Path])
	}
	m.Separator()
	if !file.HasMergeConflicts && m.Item(tr("Send file to worktree…")).Disabled(a.actionBusy || len(courierDestinations(a.snapshot)) == 0).Chosen() {
		a.beginWorkingTreeCourier(file, nil, trf("File · %s", file.Path))
	}
	m.Separator()
	if file.HasMergeConflicts {
		if m.Item(tr("Use current")).Disabled(a.actionBusy).Chosen() {
			a.resolveConflictFile(file, true)
		}
		if m.Item(tr("Use incoming")).Disabled(a.actionBusy).Chosen() {
			a.resolveConflictFile(file, false)
		}
		m.Separator()
		if m.Item(tr("Mark resolved (stage as is)")).Disabled(a.actionBusy).Chosen() {
			a.beginStageFile(file, true)
		}
	} else {
		if change.staged {
			if m.Item(tr("Unstage")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
				a.beginStageFile(file, false)
			}
		} else {
			if m.Item(tr("Stage")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
				a.beginStageFile(file, true)
			}
		}
		if file.Tracked && !file.Deleted {
			if m.Item(tr("Blame")).Shortcut(0, ui.KeyB).Disabled(a.actionBusy).Chosen() {
				a.beginBlame()
			}
		}
		m.Separator()
		if !file.Tracked && !file.Staged {
			if m.Item(tr("Add to .gitignore")).Shortcut(0, ui.KeyI).Disabled(a.actionBusy).Chosen() {
				a.ignoreFile(file, false)
			}
			if m.Item(tr("Add to .git/info/exclude")).Shortcut(ui.Shift, ui.KeyI).Disabled(a.actionBusy).Chosen() {
				a.ignoreFile(file, true)
			}
			m.Separator()
		}
		if m.Item(tr("Stash selected file…")).Disabled(a.actionBusy).Chosen() {
			a.openSelectedFileStash(file)
		}
		if m.Item(tr("Discard changes…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.confirmDiscardFile(file)
		}
	}
	m.Separator()
	if m.Item(tr("Find file histories")).Disabled(a.actionBusy || a.session == nil).Chosen() {
		a.findFileHistories(file.Path)
	}
	m.Separator()
	a.copyPathItems(m, services, file.Path)
}

// copyPathItems adds the two ways to copy a file's path: the relative one Git
// shows, and the absolute one a terminal or editor can open.
func (a *app) copyPathItems(m *ui.Menu, services ui.Services, path string) {
	if m.Item(tr("Copy relative path")).Chosen() {
		a.notifyCopied(services, "relative path", path)
	}
	if m.Item(tr("Copy absolute path")).Chosen() {
		a.notifyCopied(services, "absolute path", a.repositoryPath(path))
	}
}

// repositoryPath turns a path relative to the repository root into the
// absolute one the user can paste elsewhere.
func (a *app) repositoryPath(path string) string {
	if a.snapshot.Root == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.snapshot.Root, path)
}

func (a *app) commitForm(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(14).Padding(20)).Children(func() {
		ui.Text(c, tr("Create commit")).FontSize(20).Bold()
		summary := ui.TextInput(c, &a.commitSummary).Label(tr("Summary")).Placeholder(tr("Describe the change")).AutoFocus()
		summary.DescribeShortcut(ui.ShortcutDescriptor{ID: "changes/commit-staged", Label: tr("Commit the staged changes")}, 0, ui.KeyEnter)
		summary.OnSubmit(a.beginCommit)
		ui.TextArea(c, &a.commitBody).Label(tr("Description")).Placeholder(tr("Optional details")).Height(150)
		ui.Checkbox(c, &a.commitAmend, tr("Amend the latest commit"))
		ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
			if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
				a.showCommitForm = false
			}
			if ui.PrimaryButton(c, tr("Commit")).Disabled(!a.canCommit()).Clicked() {
				a.beginCommit()
			}
		})
		ui.Row(c).Children(func() {
			ui.Text(c, tr("Esc closes this form")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
	})
}

func (a *app) fileDetailsView(c *ui.Context) {
	theme := c.Theme()
	// The editor handled the clicks of the last frame, so what it holds now is
	// what the next click extends.
	a.syncLineSelection()
	islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Label(tr("File details")).Children(func() {
		change, ok := a.selectedChange()
		if !ok {
			a.resetFileDiff()
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Select a changed file")).TextColor(theme.TextMuted)
			})
			return
		}

		file := change.file
		// The pane's editor works on the working tree, so only the row of a
		// file in the Changes section has text here to edit: the staged row
		// shows what the index holds, not what is on disk.
		side := change.diffFile()
		a.syncFileEdit(side)
		title := func() {
			fileTypeMark(c, file.Path, 18, false)
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, file.Path).FontSize(16).Bold().SingleLine().Tooltip(file.Path)
				if file.PreviousPath != "" {
					ui.Textf(c, tr("Renamed from %s"), file.PreviousPath).TextColor(theme.TextMuted).SingleLine()
				}
			})
		}
		controls := func() {
			if a.fileEditing {
				// The editor owns the file whole: while it shows, the header
				// keeps only the way back to the diff, so a click meant for
				// the file cannot stage or discard it half edited.
				iconButton(c, "Done", "Close the editor and return to the diff", commitIcon, false, a.closeFileEdit)
			} else if file.HasMergeConflicts {
				// The markers are edited in place, and the whole file is
				// edited here as any changed file is; the review of the
				// sides is a diff, so it brings the layout toggle with it.
				a.fileEditorButton(c, file)
				a.conflictReviewButton(c)
				if a.conflictReview {
					a.diffLayoutButton(c)
				}
				a.diffWrapButton(c)
				a.exportDiffButton(c)
				a.fileActionsMenu(c, theme, file)
			} else {
				// A file the working tree no longer holds has nothing for an
				// editor to open.
				if !file.Deleted {
					a.fileEditorButton(c, file)
				}
				a.diffViewOptionsWith(c, nil, false)
				a.fileActionsMenu(c, theme, file)
			}
		}
		// A conflicted file carries the same bar as a changed one, with the
		// way to review its sides beside the action that opens it in Code.
		// The way to mark it resolved is a resolution, so it sits on the
		// conflict toolbar below rather than beside the name: the extra
		// button would leave the name no room in the default window and make
		// the conflict's bar taller than a changed file's. Expansion stays in
		// the title's top-right corner; the other controls wrap on their own row
		// rather than let one fall past the edge in a narrow window.
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			title()
			if !a.fileEditing {
				a.expandDiffButton(c)
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			controls()
		})
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, file.Status).TextColor(statusColor(theme, file)).Bold().Width(30)
			if !file.HasMergeConflicts {
				if file.Staged {
					ui.Badge(c, tr("Staged"))
				}
				if file.Unstaged {
					ui.Badge(c, tr("Unstaged"))
				}
			}
			if file.HasMergeConflicts {
				ui.Badge(c, tr("Conflict")).Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
			}
			if file.LinesAdded > 0 || file.LinesDeleted > 0 {
				fileDiffStatCounts(c, file.Path, file.LinesAdded, file.LinesDeleted)
			}
		})
		if file.HasMergeConflicts {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(7, 9).
				Background(theme.Danger.Alpha(0.12)).Radius(theme.Radius).Children(func() {
				ui.Icon(c, conflictIcon).FontSize(14).TextColor(theme.Danger)
				ui.Text(c, tr(conflictExplanation(file))).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
			})
		}
		if a.fileEditing {
			a.fileEditPane(c, theme)
			return
		}
		a.requestFileDiff()
		a.changedFileAndHunkNavigationControls(c)
		if file.HasMergeConflicts {
			a.conflictPane(c, theme)
			return
		}
		if a.blameLoading {
			loadingPane(c, "Loading blame", "Loading blame…")
			return
		}
		if a.blameError != "" {
			ui.Text(c, a.blameError).TextColor(theme.Danger)
		}
		if a.blameState != nil {
			closed := false
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr("Blame at HEAD")).Bold().Grow(1)
				if ui.Button(c, tr("Close blame")).Clicked() {
					a.blameGeneration++
					a.blameState = nil
					closed = true
				}
			})
			// The click runs while this pass builds, so the state it just
			// cleared is nil here; drawing it would crash the editor.
			if !closed {
				// Blame is read-only, as a diff is: a click keeps the keyboard
				// where it is rather than putting a caret in its text.
				a.keyboardPane(c, "blame", editor.View(c, a.blameState).Grow(1)).Label(tr("Blame"))
			}
			return
		}
		if a.diffLoading {
			loadingPane(c, "Loading diff", "Loading diff…")
			return
		}
		if a.diffError != "" {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, a.diffError).TextColor(theme.Danger)
			})
			return
		}
		if a.diffState == nil {
			noTextualDiffOrMedia(c, a.diffMedia, change.file.Path)
			return
		}
		// Selecting lines turns this row into the stage or unstage action for
		// just those lines. The section and row above already name the side.
		if a.lineSelectionAvailable() {
			a.lineSelectionControls(c, true)
		}
		a.keyboardDiffViewer(c, a.diffState)
	})
}

// fileActionsMenu puts the file-wide actions behind the detail header's ⋯
// button, at the end of the row: staging, blaming and discarding change the
// file whole, while the icons beside it change only what the pane shows.
func (a *app) fileActionsMenu(c *ui.Context, theme *ui.Theme, file repository.File) {
	more := ui.ButtonBase(c).Padding(5).Radius(theme.Radius)
	more.Label(tr("File actions")).Tooltip(tr("File actions"))
	more.Children(func() {
		ui.Text(c, "⋯").FontSize(16).TextColor(theme.TextMuted)
	})
	more.PopoverMenuButton(func(m *ui.Menu) {
		if file.HasMergeConflicts {
			// A conflicted file is resolved rather than staged or blamed:
			// the menu offers the two picks and marking it resolved, as its
			// row's menu does, then the two ways to copy its path.
			if m.Item(tr("Use current")).Disabled(a.actionBusy).Chosen() {
				a.resolveConflictFile(file, true)
			}
			if m.Item(tr("Use incoming")).Disabled(a.actionBusy).Chosen() {
				a.resolveConflictFile(file, false)
			}
			m.Separator()
			if m.Item(tr("Mark resolved (stage as is)")).Disabled(a.actionBusy).Chosen() {
				a.beginStageFile(file, true)
			}
			m.Separator()
			a.copyPathItems(m, c.Services(), file.Path)
			return
		}
		if file.Unstaged {
			if m.Item(tr("Stage")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
				a.beginFileAction(true)
			}
		}
		if file.Staged {
			if m.Item(tr("Unstage")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
				a.beginFileAction(false)
			}
		}
		if file.Tracked && !file.Deleted {
			if m.Item(tr("Blame")).Shortcut(0, ui.KeyB).Disabled(a.actionBusy).Chosen() {
				a.beginBlame()
			}
		}
		m.Separator()
		if !file.Tracked && !file.Staged {
			if m.Item(tr("Add to .gitignore")).Shortcut(0, ui.KeyI).Disabled(a.actionBusy).Chosen() {
				a.ignoreFile(file, false)
			}
			if m.Item(tr("Add to .git/info/exclude")).Shortcut(ui.Shift, ui.KeyI).Disabled(a.actionBusy).Chosen() {
				a.ignoreFile(file, true)
			}
			m.Separator()
		}
		if m.Item(tr("Stash selected file…")).Disabled(a.actionBusy).Chosen() {
			a.openSelectedFileStash(file)
		}
		if m.Item(tr("Discard changes…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.confirmDiscardSelected()
		}
	})
	if more.Hovered() {
		more.Background(theme.SurfaceHover)
	}
}

// conflictPane resolves a conflicted file in place. The toolbar keeps one hunk
// of the current conflict, writes the file and walks the conflicts that are
// left; the editor shows the whole file with its markers tinted, scrolls to the
// conflict the toolbar acts on, and takes hand edits that are written as typed.
func (a *app) conflictPane(c *ui.Context, theme *ui.Theme) {
	if a.diffLoading {
		loadingPane(c, "Loading conflict", "Loading conflict…")
		return
	}
	if a.diffError != "" {
		ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Icon(c, conflictIcon).FontSize(24).TextColor(theme.Danger)
			ui.Text(c, tr("The conflicted file could not be read.")).Bold()
			ui.Text(c, a.diffError).TextColor(theme.TextMuted).TextAlign(ui.Center)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.beginDiff()
			}
		})
		return
	}
	// Every view of an inline conflict shares the same toolbar. Selecting an
	// immutable Base/Current/Incoming stage replaces only the content below;
	// it must not collapse the Hunk and Apply groups back to a bare selector.
	hasConflictToolbar := a.conflictSides != nil || (a.conflictState != nil && a.conflictView != nil)
	if hasConflictToolbar {
		a.conflictToolbar(c, theme)
	} else {
		a.conflictStageSelector(c)
	}
	if normalizeConflictActionStyle(a.settings.ConflictActionStyle) == conflictActionStyleGuided {
		a.guidedConflictStageGuide(c)
	}
	if a.conflictStagePane(c, theme) {
		return
	}
	if a.conflictSides != nil {
		a.conflictSidesPane(c, theme)
		return
	}
	if a.conflictState == nil || a.conflictView == nil {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No textual conflict")).TextColor(theme.TextMuted)
		})
		return
	}
	if a.conflictSaveError != "" {
		ui.Text(c, trf("Could not save the file: %s", a.conflictSaveError)).
			TextColor(theme.Danger).FontSize(theme.FontSize - 1)
	}
	if a.conflictReview {
		a.conflictReviewPane(c, theme)
		return
	}
	a.conflictView.LineBackgrounds = a.conflictState.lineBackgrounds(theme)
	if a.conflictScrollTo >= 0 {
		a.conflictView.ScrollTo(a.conflictScrollTo)
		a.conflictScrollTo = -1
	}
	a.keyboardPane(c, "conflict-editor", editor.View(c, a.conflictView).Grow(1)).Label(tr("Conflict content"))
}

// conflictReviewPane shows the two sides of the conflict as a diff, laid out
// the way every diff is and with the same controls above it. The conflict is
// still resolved by the toolbar; this is the view that shows what each side
// changes.
func (a *app) conflictReviewPane(c *ui.Context, theme *ui.Theme) {
	if a.conflictDiffState == nil {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
		})
		return
	}
	a.keyboardDiffViewer(c, a.conflictDiffState)
}

// conflictReviewButton switches the conflicted file between editing its
// markers and reviewing the two sides of its merge as a diff.
func (a *app) conflictReviewButton(c *ui.Context) {
	ready := a.conflictState != nil || a.conflictSides != nil
	label := "Review both sides"
	tooltip := "Compare the current and incoming versions of this file"
	icon := diffSplitIcon
	if a.conflictReview {
		// Back to the file, not to editing: the pane edits it either way,
		// and the Code action beside this one opens the whole file.
		label = "Show conflict markers"
		tooltip = "Show the file with its conflict markers"
		icon = conflictIcon
	}
	iconButton(c, label, tooltip, icon, !ready, func() {
		a.conflictReview = !a.conflictReview
		if a.conflictReview {
			a.refreshConflictDiff()
		}
		c.Invalidate()
	})
}

// conflictSidesPane shows a conflict Git wrote without markers: one side
// deleted the file, so Git kept the other side in the working tree. It names
// the side that has no version and shows the one that was kept, and its
// buttons resolve the whole file rather than one hunk.
func (a *app) conflictSidesPane(c *ui.Context, theme *ui.Theme) {
	sides := a.conflictSides
	// Resolution belongs to conflictToolbar above the content, exactly as it
	// is for inline files. This pane only explains and displays the kept side.
	ui.Column(c).Grow(1).Gap(6).Children(func() {
		if note := conflictSidesNote(sides.file.Status, !sides.current); note != "" {
			ui.Text(c, tr(note)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		if a.conflictReview {
			if a.conflictDiffState == nil {
				ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
				return
			}
			a.keyboardDiffViewer(c, a.conflictDiffState)
			return
		}
		if sides.content != "" {
			view := sides.viewState()
			view.Font = selectedBufferFont(a.settings.BufferFont, a.settings.BufferFontSize)
			view.LineHeight = float32(a.settings.BufferLineHeight)
			a.keyboardPane(c, "conflict-sides", editor.View(c, view).Grow(1))
		}
	})
}

// conflictSuccessBadgeText chooses a label color with enough contrast over
// the success fill, rather than assuming the theme's ordinary text is legible.
func conflictSuccessBadgeText(theme *ui.Theme) ui.Color {
	return accentText(theme.Success)
}

// conflictToolbarGroup builds one titled action group shared by inline and
// whole-file conflicts, so Deleted can never fall back to a different toolbar.
func conflictToolbarGroup(c *ui.Context, theme *ui.Theme, label string, content func()) {
	ui.Column(c).MinWidth(0).MaxWidthPercent(100).Gap(5).Label(tr(label) + " group").Children(func() {
		ui.Text(c, tr(label)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(content)
	})
}

// conflictToolbarSeparator is a fixed-height vertical line between groups.
func conflictToolbarSeparator(c *ui.Context, theme *ui.Theme, label string) {
	ui.Box(c).Width(1).Height(31).MaxHeight(31).Margin(0, 2).
		AlignSelf(ui.Center).Shrink(0).Background(theme.Border).Label(tr(label))
}

// conflictSidesToolbar resolves a conflict that has no textual hunks. Its
// status badge describes a whole-file resolution; Use both and initial-hunk
// Restore would have no meaning, so they do not consume slots here.
func (a *app) conflictSidesToolbar(c *ui.Context, theme *ui.Theme) {
	sides := a.conflictSides
	reason := a.conflictMutationReason()
	disabled := reason != ""
	applyActions := []actionControl{
		{label: "Use current", description: "Resolve with the current side", icon: useCurrentIcon,
			disabled: disabled, disabledReason: reason, run: func() { a.resolveConflictFile(sides.file, true) }},
		{label: "Use incoming", description: "Resolve with the incoming side", icon: useIncomingIcon,
			disabled: disabled, disabledReason: reason, run: func() { a.resolveConflictFile(sides.file, false) }},
	}
	busyReason := ""
	if a.actionBusy {
		busyReason = "Wait for the current Git action to finish."
	}
	markResolved := actionControl{
		label: "Mark resolved", description: "Stage the file as it is", primary: true, textOnly: true,
		disabled: a.actionBusy, disabledReason: busyReason, run: func() { a.beginStageFile(sides.file, true) },
	}
	row, compact, tiny := a.conflictToolbarRow(c)
	row.Children(func() {
		if compact {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Label(tr("Hunk") + " group").Children(func() {
				ui.Text(c, tr("Hunk")).SingleLine()
				ui.Badge(c, tr(conflictStatusLabel(sides.file.Status))).Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
			})
			a.conflictCompactActionGroup(c, "View", diffSplitIcon, a.conflictCompactViewActions(0, false), nil, !tiny)
			a.conflictCompactApplyGroup(c, applyActions, tiny)
			a.conflictActionButton(c, markResolved)
			return
		}
		conflictToolbarGroup(c, theme, "Hunk", func() {
			ui.Badge(c, tr(conflictStatusLabel(sides.file.Status))).Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
		})
		conflictToolbarSeparator(c, theme, "Hunk separator")
		conflictToolbarGroup(c, theme, "View", func() {
			a.conflictStageSelector(c)
		})
		conflictToolbarSeparator(c, theme, "View separator")
		conflictToolbarGroup(c, theme, "Apply", func() {
			a.conflictActionToolbar(c, "Whole-file conflict actions", applyActions)
		})
		ui.Spacer(c).Grow(1)
		conflictToolbarSeparator(c, theme, "Apply separator")
		a.conflictActionButton(c, markResolved)
	})
}

// conflictCompactViewActions is the Popover form of the same stage selector
// and assistant used by the full View group. A compact frame constructs only
// these menu actions; switching back rebuilds the ordinary tab buttons.
func (a *app) conflictCompactViewActions(count int, assistant bool) []actionControl {
	actions := make([]actionControl, 0, 5)
	for tab, label := range []string{"Base", "Current", "Incoming", "Result"} {
		tab, label := tab, label
		actions = append(actions, actionControl{
			label: label, description: conflictStageTooltip(tab),
			run: func() { a.conflictStageTab = tab },
		})
	}
	if assistant {
		assistReason := a.conflictMutationReason()
		if assistReason == "" && count == 0 {
			assistReason = "No conflict markers remain for suggestions."
		}
		actions = append(actions, actionControl{
			label: "Conflict Assistant", description: "Preview region suggestions on a clone; Accept writes Result only", icon: commandIcon,
			disabled: assistReason != "", disabledReason: assistReason, run: a.openConflictAssistant,
		})
	}
	return actions
}

// conflictToolbar is the row above a conflicted file: which conflict is
// selected, the hunk to keep from it, the undo of the last pick, and the way
// to mark the file resolved once its markers are gone. The file's own
// controls, editing and the review, sit beside its name.
func (a *app) conflictToolbar(c *ui.Context, theme *ui.Theme) {
	if a.conflictSides != nil {
		a.conflictSidesToolbar(c, theme)
		return
	}
	state := a.conflictState
	count := len(state.regions)
	region, hasRegion := state.current()
	var navigation []actionControl
	if count > 0 {
		navigation = []actionControl{
			{label: "Previous conflict", shortcut: "Shift+F7", description: "Select the conflict above", icon: chevronUpIcon,
				disabled: state.selected == 0, disabledReason: "Already at the first conflict.", run: func() { a.moveConflictSelection(-1) }},
			{label: "Next conflict", shortcut: "F7", description: "Select the conflict below", icon: chevronDownIcon,
				disabled: state.selected == count-1, disabledReason: "Already at the last conflict.", run: func() { a.moveConflictSelection(1) }},
		}
	}
	applyActions := a.inlineConflictApplyActions(state, region, hasRegion)
	busyReason := ""
	if a.actionBusy {
		busyReason = "Wait for the current Git action to finish."
	}
	markResolved := a.markResolvedAction()
	markResolved.disabledReason = busyReason
	statusBadge := func() {
		if count == 0 {
			ui.Row(c).Padding(0, theme.Space(1.5)).Radius(999).
				Background(theme.Success).Shrink(0).Children(func() {
				ui.Text(c, tr("No conflict markers")).FontSize(theme.FontSize - 2).
					TextColor(conflictSuccessBadgeText(theme)).SingleLine()
			})
		} else {
			ui.Badge(c, trf("%d / %d", state.selected+1, count)).
				Label(trf("Conflict %d of %d", state.selected+1, count)).
				Tooltip(trf("Conflict %d of %d", state.selected+1, count)).
				Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
		}
	}
	row, compact, tiny := a.conflictToolbarRow(c)
	row.Children(func() {
		if compact {
			if count == 0 {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Label(tr("Hunk") + " group").Children(func() {
					ui.Text(c, tr("Hunk")).SingleLine()
					statusBadge()
				})
			} else {
				ui.Row(c).AlignItems(ui.Center).Gap(6).Label(tr("Hunk") + " group").Children(func() {
					renderActionControl(c, actionControl{
						label: "Hunk", visibleLabel: "Hunk", icon: historyIcon,
						description: "Show this action group with labels and unavailable reasons", menu: navigation,
					}, !tiny)
					statusBadge()
				})
			}
			a.conflictCompactActionGroup(c, "View", diffSplitIcon, a.conflictCompactViewActions(count, true), nil, !tiny)
			a.conflictCompactApplyGroup(c, applyActions, tiny)
			a.conflictActionButton(c, markResolved)
			return
		}
		conflictToolbarGroup(c, theme, "Hunk", func() {
			statusBadge()
			if count > 0 {
				a.conflictActionToolbar(c, "Conflict navigation", navigation)
			}
		})
		conflictToolbarSeparator(c, theme, "Hunk separator")
		conflictToolbarGroup(c, theme, "View", func() {
			a.conflictStageSelector(c)
			assistReason := a.conflictMutationReason()
			if assistReason == "" && count == 0 {
				assistReason = "No conflict markers remain for suggestions."
			}
			a.conflictActionButton(c, actionControl{
				label: "Conflict Assistant", description: "Preview region suggestions on a clone; Accept writes Result only", icon: commandIcon,
				disabled: assistReason != "", disabledReason: assistReason, run: a.openConflictAssistant,
			})
		})
		conflictToolbarSeparator(c, theme, "View separator")
		conflictToolbarGroup(c, theme, "Apply", func() {
			a.conflictActionToolbar(c, "Conflict apply actions", applyActions)
		})
		ui.Spacer(c).Grow(1)
		conflictToolbarSeparator(c, theme, "Apply separator")
		a.conflictActionButton(c, markResolved)
	})
}

// selectedLinesSummary names the picked lines by number, 1-based, as
// "2, 6, 11–12"; lines of the old side carry a minus sign. The tooltip always
// holds the full text when the visible one is cut short by the pane's width.
func selectedLinesSummary(ranges []editor.DiffLineRange) string {
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		prefix := ""
		if r.Side == editor.DiffOld {
			prefix = "−"
		}
		if r.Start == r.End {
			parts = append(parts, fmt.Sprintf("%s%d", prefix, r.Start+1))
		} else {
			parts = append(parts, fmt.Sprintf("%s%d–%d", prefix, r.Start+1, r.End+1))
		}
	}
	return strings.Join(parts, ", ")
}

// clearExtraSelectedLines drops the viewer's extra highlighted ranges right
// away, on both viewers, so ending a selection never leaves lines painted
// until some later frame happens to rebuild the pane that syncs them.
func (a *app) clearExtraSelectedLines() {
	if a.diffState != nil {
		a.diffState.SetExtraSelectedLines(nil)
	}
	if a.commitDiffState != nil {
		a.commitDiffState.SetExtraSelectedLines(nil)
	}
}
