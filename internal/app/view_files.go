package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
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
	a.beginRepositoryActionMessage(message, "", func(ctx context.Context, session repository.Session) error {
		return session.Commit(ctx, summary, body, amend)
	}, func() {
		a.commitSummary = ""
		a.commitBody = ""
		a.commitAmend = false
		a.showCommitForm = false
	})
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

// selectedLineCount is how many lines the pane's diff has selected, and
// whether it has any.
func (a *app) selectedLineCount() (int, bool) {
	if !a.lineSelectionAvailable() {
		return 0, false
	}
	selection, ok := a.diffState.SelectedLines()
	if !ok || selection.End < selection.Start {
		return 0, false
	}
	return selection.End - selection.Start + 1, true
}

// selectedLinesLabel says how many lines are selected, in the singular for one.
func selectedLinesLabel(count int) string {
	if count == 1 {
		return tr("1 line selected")
	}
	return trf("%d lines selected", count)
}

// startLineSelection enters the mode where each click on a line number adds the
// line to the selection, so a range is picked without holding Shift. It starts
// the range afresh, so the button never adds to a selection the user has
// forgotten.
func (a *app) startLineSelection() {
	if !a.lineSelectionAvailable() {
		return
	}
	a.lineSelectMode = true
	a.lineSelectRange, a.lineSelectHas = editor.DiffLineRange{}, false
	if a.diffState != nil {
		a.diffState.SetSelectedLines(editor.DiffLineRange{}, false)
	}
}

// stopLineSelection leaves the mode and drops the selection, which is what its
// Cancel button and Escape do.
func (a *app) stopLineSelection() {
	a.lineSelectMode = false
	a.lineSelectRange, a.lineSelectHas = editor.DiffLineRange{}, false
	if a.diffState != nil {
		a.diffState.SetSelectedLines(editor.DiffLineRange{}, false)
	}
}

// syncLineSelection remembers what the pane shows, so the next click extends
// the range the user is looking at. It is called while the frame builds, after
// the click that changed the selection has been handled.
func (a *app) syncLineSelection() {
	if !a.lineSelectMode || a.diffState == nil {
		a.lineSelectRange, a.lineSelectHas = editor.DiffLineRange{}, false
		return
	}
	selection, ok := a.diffState.SelectedLines()
	if !ok {
		a.lineSelectRange, a.lineSelectHas = editor.DiffLineRange{}, false
		return
	}
	a.lineSelectRange, a.lineSelectHas = selection, true
}

// extendLineSelection is what the pane reports when a line number is clicked
// while the mode is on. A plain click picks one line; a line outside the range
// grows the range to hold it, and a line inside it starts the range there
// again. Shift-click still builds a range of its own, and is taken as it is.
func (a *app) extendLineSelection(state *editor.DiffState, clicked editor.DiffLineRange) {
	if !a.lineSelectMode || state == nil || state != a.diffState || clicked.End < clicked.Start {
		return
	}
	if clicked.Start != clicked.End {
		// Shift-click already asked for a range from one end of the
		// selection to the clicked line.
		a.lineSelectRange, a.lineSelectHas = clicked, true
		return
	}
	line := clicked.Start
	seen := a.lineSelectRange
	if !a.lineSelectHas || seen.Side != clicked.Side ||
		(line >= seen.Start && line <= seen.End) {
		// The first line of a range, or one already in it.
		a.lineSelectRange, a.lineSelectHas = clicked, true
		return
	}
	grown := editor.DiffLineRange{Side: clicked.Side, Start: seen.Start, End: seen.End}
	if line < grown.Start {
		grown.Start = line
	} else {
		grown.End = line
	}
	state.SetSelectedLines(grown, true)
	a.lineSelectRange, a.lineSelectHas = grown, true
}

// lineSelectionControls draws what the pane says and offers about staging
// lines: how many are picked and the action when there are some, the way into
// the click-to-extend mode, and the way back out of it.
func (a *app) lineSelectionControls(c *ui.Context, withHint bool) {
	theme := c.Theme()
	count, selected := a.selectedLineCount()
	if a.lineSelectMode {
		ui.Text(c, tr("Selecting multiple lines")).TextColor(theme.Accent).Bold().
			FontSize(theme.FontSize - 1)
		switch {
		case selected:
			ui.Text(c, selectedLinesLabel(count)).TextColor(theme.Accent).
				FontSize(theme.FontSize - 1)
			a.diffLineButton(c)
		case withHint:
			ui.Text(c, tr("Click a line number, then another, to choose a range.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		iconButton(c, "Cancel", "Leave the selection alone and stop choosing lines",
			abortIcon, false, a.stopLineSelection)
		return
	}
	if selected {
		ui.Text(c, selectedLinesLabel(count)).TextColor(theme.Accent).
			FontSize(theme.FontSize - 1)
		a.diffLineButton(c)
	} else if withHint {
		ui.Text(c, tr("Click line numbers to select lines")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
	iconButton(c, "Select multiple lines",
		"Click line numbers to add them to one selection, then stage them together",
		selectLinesIcon, false, a.startLineSelection)
}

// diffLineButton is the action for the lines selected in the pane's diff: it
// stages them when the unstaged side shows, and takes them back out of the
// index when the staged side does. The button says which way it goes.
func (a *app) diffLineButton(c *ui.Context) {
	label := tr("Stage selected lines")
	tooltip := tr("Stage only the selected lines, leaving the rest of the file in the working tree")
	if a.diffKind == "Staged" {
		label = tr("Unstage selected lines")
		tooltip = tr("Take only the selected lines out of the index, leaving them in the working tree")
	}
	if ui.PrimaryButton(c, label).Tooltip(tooltip).Disabled(a.actionBusy).Clicked() {
		a.beginLineAction()
	}
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
	selection, selected := a.diffState.SelectedLines()
	if !selected {
		return
	}
	file := change.diffFile()
	reverse := a.diffKind == "Staged"
	patchText, wholeFile, err := partialPatch(a.diffPatch, file, selection, reverse)
	if err != nil {
		a.notifyError(err)
		return
	}
	if wholeFile {
		a.beginStageFile(file, !reverse)
		return
	}
	if patchText == "" {
		// Only context lines were selected: there is no change to move.
		a.notify("", tr("Nothing to move"), tr("Select changed lines, not unchanged ones."))
		return
	}
	a.beginRepositoryAction(file.Path, func(ctx context.Context, session repository.Session) error {
		return session.ApplyCachedPatch(ctx, patchText, reverse)
	}, nil)
}

func (a *app) confirmDiscardFile(file repository.File) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Discard Changes",
		message:      trf("Discard all changes to %s?", file.Path),
		detail:       "This operation cannot be undone.",
		confirmLabel: "Discard",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Discarded changes to %s", file.Path), file.Path,
				func(ctx context.Context, session repository.Session) error {
					return session.Discard(ctx, file)
				}, nil)
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
		detail:       "Staged and unstaged changes are thrown away. This operation cannot be undone.",
		confirmLabel: "Discard all",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(tr("Discarded all changes"), "",
				func(ctx context.Context, session repository.Session) error {
					for _, file := range files {
						if err := session.Discard(ctx, file); err != nil {
							return err
						}
					}
					return nil
				}, nil)
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
	// A right-click selects the row it acts on, as VS Code's Source Control
	// view does, so the details pane follows what the menu changes.
	a.selectedFile = index

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
		if m.Item(tr("Discard changes…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
			a.confirmDiscardFile(file)
		}
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
		ui.TextInput(c, &a.commitSummary).Label(tr("Summary")).Placeholder(tr("Describe the change"))
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
	islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
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
				iconButton(c, "Edit", "Edit this file in the pane", editIcon, a.actionBusy, a.openFileEdit)
				a.fileEditorButton(c, file)
				a.conflictReviewButton(c)
				if a.conflictReview {
					a.diffLayoutButton(c)
				}
				a.diffWrapButton(c)
				a.exportDiffButton(c)
				a.expandDiffButton(c)
				a.fileActionsMenu(c, theme, file)
			} else {
				// The pane's editor works on the working tree, so only a row in
				// the Changes section offers it: a staged-only file shows what
				// the index holds, and has no Edit icon.
				if !side.Deleted && side.Unstaged {
					iconButton(c, "Edit", "Edit this file in the pane", editIcon, a.actionBusy, a.openFileEdit)
				}
				// A file the working tree no longer holds has nothing for an
				// editor to open.
				if !file.Deleted {
					a.fileEditorButton(c, file)
				}
				a.diffViewOptions(c)
				a.fileActionsMenu(c, theme, file)
			}
		}
		// A conflicted file carries the same bar as a changed one, with the
		// way to review its sides where a changed file keeps its Edit icon.
		// The way to mark it resolved is a resolution, so it sits on the
		// conflict toolbar below rather than beside the name: the extra
		// button would leave the name no room in the default window and make
		// the conflict's bar taller than a changed file's. The bar wraps
		// rather than let a control fall past the edge in a narrow window.
		ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			title()
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
		if file.HasMergeConflicts {
			a.conflictPane(c, theme)
			return
		}
		if a.blameLoading {
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading blame"))
				ui.Text(c, tr("Loading blame…")).TextColor(theme.TextMuted)
			})
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
				editor.View(c, a.blameState).Grow(1).KeepFocus()
			}
			return
		}
		if a.diffLoading {
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading diff"))
				ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
			})
			return
		}
		if a.diffError != "" {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, a.diffError).TextColor(theme.Danger)
			})
			return
		}
		if a.diffState == nil {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
			})
			return
		}
		// The row above already shows whether the file is staged; this names
		// which of its two diffs the pane shows, which matters for a file
		// that has both. Selecting lines turns the line into the stage or
		// unstage action for just those lines.
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, trf("%s changes", tr(a.diffKind))).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			if a.lineSelectionAvailable() {
				a.lineSelectionControls(c, true)
			}
		})
		diffViewer(c, a.diffState)
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
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading conflict"))
			ui.Text(c, tr("Loading conflict…")).TextColor(theme.TextMuted)
		})
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
	a.conflictToolbar(c, theme)
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
	editor.View(c, a.conflictView).Grow(1)
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
	diffViewer(c, a.conflictDiffState)
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
		// and the Edit icon beside this one opens the whole file.
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
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Badge(c, tr(conflictStatusLabel(sides.file.Status))).Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
		ui.Spacer(c).Grow(1)
		if ui.Button(c, tr("Use current")).Tooltip(tr("Resolve with the current side")).
			Disabled(a.actionBusy).Clicked() {
			a.resolveConflictFile(sides.file, true)
			c.Invalidate()
		}
		if ui.Button(c, tr("Use incoming")).Tooltip(tr("Resolve with the incoming side")).
			Disabled(a.actionBusy).Clicked() {
			a.resolveConflictFile(sides.file, false)
			c.Invalidate()
		}
		if ui.PrimaryButton(c, tr("Mark resolved")).Tooltip(tr("Stage the file as it is")).
			Disabled(a.actionBusy).Clicked() {
			a.beginStageFile(sides.file, true)
			c.Invalidate()
		}
	})
	ui.Column(c).Grow(1).Gap(6).Children(func() {
		if note := conflictSidesNote(sides.file.Status, !sides.current); note != "" {
			ui.Text(c, tr(note)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		if a.conflictReview {
			if a.conflictDiffState == nil {
				ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
				return
			}
			diffViewer(c, a.conflictDiffState)
			return
		}
		if sides.content != "" {
			editor.View(c, sides.viewState()).Grow(1).KeepFocus()
		}
	})
}

// conflictToolbar is the row above a conflicted file: which conflict is
// selected, the hunk to keep from it, the undo of the last pick, and the way
// to mark the file resolved once its markers are gone. The file's own
// controls, editing and the review, sit beside its name.
func (a *app) conflictToolbar(c *ui.Context, theme *ui.Theme) {
	state := a.conflictState
	count := len(state.regions)
	region, hasRegion := state.current()
	// The toolbar is two rows: which conflict is selected, then the hunks to
	// keep from it. Both wrap when the pane is too narrow for them, so the
	// resolution of a conflict never hides past the edge.
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			if count == 0 {
				ui.Badge(c, tr("No conflict markers")).Background(theme.Success)
			} else {
				ui.Badge(c, trf("Conflict %d of %d", state.selected+1, count)).Background(theme.Danger).TextColor(ui.Hex("#ffffff"))
				iconButton(c, "Previous conflict", "Select the conflict above (Shift+F7)", chevronUpIcon, state.selected == 0, func() {
					a.moveConflictSelection(-1)
					c.Invalidate()
				})
				iconButton(c, "Next conflict", "Select the conflict below (F7)", chevronDownIcon, state.selected == count-1, func() {
					a.moveConflictSelection(1)
					c.Invalidate()
				})
			}
			ui.Spacer(c).Grow(1)
			iconButton(c, "Undo resolution", "Put back the content before the last pick (Alt+Z)", revertIcon, a.actionBusy || len(state.undo) == 0, func() {
				a.undoConflictResolution()
				c.Invalidate()
			})
			// Marking the file resolved ends the resolution, so it stays on
			// this row, at its end, beside the conflict it belongs to: the
			// header above keeps the bar a changed file has.
			if ui.PrimaryButton(c, tr("Mark resolved")).Tooltip(tr("Stage the file as it is")).Disabled(a.actionBusy).Clicked() {
				if change, ok := a.selectedChange(); ok {
					a.beginStageFile(change.file, true)
				}
			}
		})
		resolveDisabled := count == 0 || a.actionBusy
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			if ui.Button(c, tr("Use current")).Tooltip(tr("Keep the current side's hunk") + " (Alt+1)").Disabled(resolveDisabled).Clicked() {
				a.resolveConflictSide(conflictCurrent)
				c.Invalidate()
			}
			if hasRegion && region.hasBase() {
				if ui.Button(c, tr("Use base")).Tooltip(tr("Keep the common ancestor's hunk") + " (Alt+2)").Disabled(resolveDisabled).Clicked() {
					a.resolveConflictSide(conflictBase)
					c.Invalidate()
				}
			}
			if ui.Button(c, tr("Use incoming")).Tooltip(tr("Keep the incoming side's hunk") + " (Alt+3)").Disabled(resolveDisabled).Clicked() {
				a.resolveConflictSide(conflictIncoming)
				c.Invalidate()
			}
			if ui.Button(c, tr("Use both")).Tooltip(tr("Keep the hunks of both sides, without the ancestor") + " (Alt+4)").Disabled(resolveDisabled).Clicked() {
				a.resolveConflictSide(conflictBoth)
				c.Invalidate()
			}
		})
	})
}
