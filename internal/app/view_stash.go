package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func (a *app) beginStashChanges() {
	message := strings.TrimSpace(a.stashMessage)
	if message == "" {
		message = a.defaultStashMessage()
	}
	includeUntracked := a.stashIncludeUntracked
	selection := append([]repository.File(nil), a.stashSelection...)
	patchText := a.stashPatch
	removalPatch := a.stashRemovalPatch
	detail := "This saves changes in a stash and removes them from the working tree or index."
	success := "Stashed changes"
	if patchText != "" {
		detail = "Only the selected lines are saved and removed. The rest of this file and the index stay unchanged."
		success = "Stashed selected lines"
	} else if len(selection) > 0 {
		detail = "Only the selected file is saved and removed. Other changed files stay exactly as they are."
		success = "Stashed selected file"
	}
	a.confirmAttention("Stash", message, detail, func() {
		a.beginRepositoryActionMessage(tr(success), "",
			func(ctx context.Context, session repository.Session) error {
				switch {
				case patchText != "":
					return session.StashPatch(ctx, message, patchText, removalPatch)
				case len(selection) > 0:
					return session.StashSelectedFiles(ctx, message, selection, includeUntracked)
				default:
					return session.StashChanges(ctx, message, includeUntracked)
				}
			}, func() {
				a.stashMessage = ""
				a.stashIncludeUntracked = false
				a.stashSelection = nil
				a.stashPatch = ""
				a.stashRemovalPatch = ""
				a.showStashForm = false
			})
	})
}

// stashStagedChanges saves only the index away. The unstaged work stays in
// the tree, so the staged files leave the panel and the rest remains to be
// reviewed. The stash is named like an unnamed one from the form: the branch
// and the moment it was taken.
func (a *app) stashStagedChanges() {
	if a.actionBusy || !hasStagedFiles(a.snapshot.Files) {
		return
	}
	message := a.defaultStashMessage()
	a.confirmAttention("Stash", message, "This saves changes in a stash and removes them from the working tree or index.", func() {
		a.beginRepositoryActionMessage(tr("Stashed staged changes"), "",
			func(ctx context.Context, session repository.Session) error {
				return session.StashStagedChanges(ctx, message)
			}, nil)
	})
}

// defaultStashMessage names a stash the user left unnamed: the branch it was
// taken on and the moment, so the Saved work list tells one from another.
func (a *app) defaultStashMessage() string {
	branch := a.snapshot.Branch
	if branch == "" {
		branch = "HEAD"
	}
	return "WIP on " + branch + " at " + time.Now().Format("2006-01-02 15:04:05")
}

// openStashForm opens the modal that saves the working tree away. With
// nothing changed there is nothing to save, so it says so instead.
func (a *app) openStashForm() {
	if a.actionBusy {
		return
	}
	if len(a.snapshot.Files) == 0 {
		a.notify("", "Nothing to stash", "The working tree has no changes to save.")
		return
	}
	a.stashMessage = ""
	a.stashIncludeUntracked = false
	a.stashSelection = nil
	a.stashPatch = ""
	a.stashRemovalPatch = ""
	a.showStashForm = true
}

func (a *app) openSelectedFileStash(file repository.File) {
	if a.actionBusy || a.session == nil || file.HasMergeConflicts {
		return
	}
	a.stashMessage = ""
	a.stashIncludeUntracked = !file.Tracked
	a.stashSelection = []repository.File{file}
	a.stashPatch = ""
	a.stashRemovalPatch = ""
	a.showStashForm = true
}

func (a *app) openSelectedLinesStash(file repository.File, patchText, removalPatch string) {
	if a.actionBusy || a.session == nil || file.HasMergeConflicts || patchText == "" || removalPatch == "" {
		return
	}
	a.stashMessage = ""
	a.stashIncludeUntracked = !file.Tracked
	a.stashSelection = nil
	a.stashPatch = patchText
	a.stashRemovalPatch = removalPatch
	a.showStashForm = true
}

// stashFormDescription keeps scope explicit before the confirmation dialog.
func (a *app) stashFormDescription() string {
	if a.stashPatch != "" {
		return "Save only the selected lines to restore them later."
	}
	if len(a.stashSelection) > 0 {
		return "Save only the selected file to restore it later."
	}
	return "Save the working tree away to restore it later."
}

// stashFormModal names the stash the working tree is saved as. The form is
// the confirmation: its button saves the changes, and Escape or a press
// outside closes it.
func (a *app) stashFormModal(c *ui.Context) {
	if !a.showStashForm || a.confirm != nil {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(480).MaxWidthPercent(92).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Stash Changes")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, stashIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr("Stash Changes")).FontSize(17).Bold()
							ui.Text(c, tr(a.stashFormDescription())).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close stash form", "Escape", closeIcon, false,
							func() { a.showStashForm = false })
					})
					input := ui.TextInput(c, &a.stashMessage).
						Label(tr("Stash message")).
						Placeholder(tr("Optional — a timestamped message is used")).
						AutoFocus()
					if input.Submitted() {
						a.beginStashChanges()
					}
					if a.stashPatch == "" {
						ui.Checkbox(c, &a.stashIncludeUntracked, tr("Include untracked"))
					}
					ui.Text(c, tr("Leave the message empty for a timestamped name.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
						if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
							a.showStashForm = false
						}
						if ui.PrimaryButton(c, tr("Stash changes")).Disabled(a.actionBusy).Clicked() {
							a.beginStashChanges()
						}
					})
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showStashForm = false
			}
		})
	})
}

func (a *app) beginRenameStash() {
	if a.selectedStash < 0 || a.selectedStash >= len(a.snapshot.Stashes) {
		return
	}
	index := a.snapshot.Stashes[a.selectedStash].Index
	name := strings.TrimSpace(a.stashRename)
	a.beginRepositoryActionMessage(trf("Renamed stash to %s", name), "",
		func(ctx context.Context, session repository.Session) error {
			return session.RenameStash(ctx, index, name)
		}, func() {
			a.stashRename = ""
			a.showStashRenameForm = false
		})
}

// stashKey names the stash a detail read is for. Stashes are numbered from
// the newest, so the number moves when another one is dropped or added; the
// commit hash stays with the saved work.
func stashKey(stash repository.Stash) string {
	if stash.Hash != "" {
		return stash.Hash
	}
	return fmt.Sprintf("index:%d", stash.Index)
}

// resetStashDetails drops what the saved-work pane read, so the next frame
// reads the chosen stash again after an action changed it.
func (a *app) resetStashDetails() {
	a.stashFiles = nil
	a.stashFilesHash = ""
	a.stashFilesLoading = false
	a.stashFilesError = ""
	a.selectedStashFileRow = -1
	a.selectedStashFile = -1
	a.stashDiffHash = ""
	a.stashDiffPath = ""
	a.stashDiffPatch = ""
	a.stashDiffState = nil
	a.stashDiffMedia = nil
	a.stashDiffLoading = false
	a.stashDiffError = ""
}

func (a *app) selectStashFileRow(row int) {
	selectFileTreeRow(a.stashFileRows, row, &a.selectedStashFileRow, &a.selectedStashFile)
	if a.selectedStashFile >= 0 {
		return
	}
	a.stashDiffHash = ""
	a.stashDiffPath = ""
	a.stashDiffPatch = ""
	a.stashDiffState = nil
	a.stashDiffMedia = nil
	a.stashDiffLoading = false
	a.stashDiffError = ""
}

// requestStashDetails lists the files a stash saved, unless the pane already
// holds them. It is asked for while the pane draws, so a read an action
// dropped, or a newer stash replaced, starts again on the next frame.
func (a *app) requestStashDetails(stash repository.Stash) {
	key := stashKey(stash)
	if a.session == nil || a.stashFilesHash == key {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	a.stashFilesHash = key
	a.stashFilesLoading = true
	a.stashFilesError = ""
	a.stashFiles = nil
	a.selectedStashFileRow = -1
	a.selectedStashFile = -1
	a.stashDiffHash = ""
	a.stashDiffPath = ""
	a.stashDiffPatch = ""
	a.stashDiffState = nil
	a.stashDiffMedia = nil
	a.stashDiffLoading = false
	a.stashDiffError = ""

	go func() {
		defer a.recoverBackground()
		files, err := session.StashFiles(context.Background(), stash.Index)
		a.update(func() {
			if repositoryGeneration != a.generation {
				// An action rewrote the repository: drop the request so the
				// next frame reads the stash again.
				if a.stashFilesHash == key {
					a.stashFilesHash = ""
					a.stashFilesLoading = false
				}
				return
			}
			if a.stashFilesHash != key {
				return
			}
			a.stashFilesLoading = false
			if err != nil {
				a.stashFilesError = safeError(err)
				return
			}
			a.stashFiles = files
			if len(files) == 0 {
				a.selectedStashFileRow = -1
				a.selectedStashFile = -1
				return
			}
			a.selectedStashFile = 0
		})
	}()
}

// requestStashFileDiff reads the diff of the file chosen in a stash, unless
// it is already the one the pane shows.
func (a *app) requestStashFileDiff(stash repository.Stash, file repository.CommitFile) {
	key := stashKey(stash)
	if a.session == nil || (a.stashDiffHash == key && a.stashDiffPath == file.Path &&
		a.stashDiffOptions == a.diffOptions) {
		return
	}
	session := a.session
	repositoryGeneration := a.generation
	a.stashDiffHash = key
	a.stashDiffPath = file.Path
	a.stashDiffOptions = a.diffOptions
	a.stashDiffLoading = true
	a.stashDiffError = ""
	a.stashDiffPatch = ""
	a.stashDiffState = nil
	a.stashDiffMedia = nil

	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
	go func() {
		defer a.recoverBackground()
		diff, err := session.StashFileDiff(context.Background(), stash.Index, file, options)
		var state *editor.DiffState
		var media *mediaReview
		if err == nil {
			state, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
			if err == nil && state == nil && isMediaPath(file.Path) {
				media, err = loadMediaReview(context.Background(), session, repository.MediaRequest{
					Source: repository.MediaStash, StashIndex: stash.Index, CommitFile: file,
				})
			}
		}
		a.update(func() {
			if repositoryGeneration != a.generation {
				if a.stashDiffHash == key && a.stashDiffPath == file.Path &&
					a.stashDiffOptions == options {
					a.stashDiffHash = ""
					a.stashDiffPath = ""
					a.stashDiffPatch = ""
					a.stashDiffLoading = false
				}
				return
			}
			if a.stashDiffHash != key || a.stashDiffPath != file.Path ||
				a.stashDiffOptions != options {
				return
			}
			a.stashDiffLoading = false
			if err != nil {
				a.stashDiffError = safeError(err)
				return
			}
			a.stashDiffPatch = diff.Patch
			a.stashDiffState = state
			a.stashDiffMedia = media
		})
	}()
}

func (a *app) confirmStashMutation(pop bool) {
	if a.selectedStash < 0 || a.selectedStash >= len(a.snapshot.Stashes) {
		return
	}
	a.confirmStashMutationFor(a.snapshot.Stashes[a.selectedStash], pop)
}

// confirmStashMutationFor previews a pop or drop of stash before running it.
func (a *app) confirmStashMutationFor(stash repository.Stash, pop bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	title := "Drop Stash"
	message := trf("Drop %s?", stash.Name)
	detail := "The saved changes will be removed. This operation cannot be undone from the app."
	button := "Drop"
	if pop {
		title = "Pop Stash"
		message = trf("Apply and remove %s?", stash.Name)
		detail = "The changes will be applied to the working tree and removed from the stash list."
		button = "Pop"
	}
	a.askConfirm(confirmRequest{
		title:        title,
		message:      message,
		detail:       detail,
		confirmLabel: button,
		destructive:  true,
		run: func() {
			message := trf("Dropped %s", stash.Name)
			if pop {
				message = trf("Popped %s", stash.Name)
			}
			a.beginRepositoryActionMessage(message, "",
				func(ctx context.Context, session repository.Session) error {
					if pop {
						return session.ApplyStash(ctx, stash.Index, true)
					}
					return session.DropStash(ctx, stash.Index)
				}, nil)
		},
	})
}

func (a *app) applyStash(stash repository.Stash) {
	a.confirmAttention("Apply", stash.Name, "This applies saved changes to the working tree and may cause conflicts. Applying one stash file can replace its current working-tree contents.", func() {
		a.beginRepositoryActionMessage(trf("Applied %s", stash.Name), "",
			func(ctx context.Context, session repository.Session) error {
				return session.ApplyStash(ctx, stash.Index, false)
			}, nil)
	})
}

// applyStashFile writes one saved file's version into the working tree,
// leaving the saved work itself in the stash.
func (a *app) applyStashFile(stash repository.Stash, file repository.CommitFile) {
	a.confirmAttention("Apply", file.Path+"\n"+stash.Name, "This applies saved changes to the working tree and may cause conflicts. Applying one stash file can replace its current working-tree contents.", func() {
		a.beginRepositoryActionMessage(trf("Applied %s", file.Path), file.Path,
			func(ctx context.Context, session repository.Session) error {
				return session.ApplyStashFile(ctx, stash.Index, file)
			}, nil)
	})
}

// confirmDiscardStashFile asks before rewriting the saved work without one
// file's change: what the stash held is gone for good once dropped.
func (a *app) confirmDiscardStashFile(stash repository.Stash, file repository.CommitFile) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Discard Saved File",
		message:      trf("Discard %s from %s?", file.Path, stash.Name),
		detail:       "The file's change will be removed from the saved work. This operation cannot be undone.",
		confirmLabel: "Discard",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Discarded %s from %s", file.Path, stash.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.DiscardStashFile(ctx, stash.Index, file, stash.Name)
				}, nil)
		},
	})
}

// stashFileMenu is the right-click menu of one saved file: the row's own
// apply and drop in front of the actions any changed file carries. Applying
// runs at once; discarding rewrites the saved work without the file, so it
// joins every discard path in asking first.
func (a *app) stashFileMenu(m *ui.Menu, stash repository.Stash, file repository.CommitFile, services ui.Services) {
	if m.Item(tr("Apply this file")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
		a.applyStashFile(stash, file)
	}
	if m.Item(tr("Discard this file…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
		a.confirmDiscardStashFile(stash, file)
	}
	m.Separator()
	a.commitFileMenu(m, nil, file, services)
}

func (a *app) visibleStashes() []int {
	return filteredSourceRows(len(a.snapshot.Stashes), a.stashFilter, func(source int) string {
		stash := a.snapshot.Stashes[source]
		return strings.Join([]string{stash.Name, stash.Hash, stash.Recency,
			fmt.Sprintf("stash@{%d}", stash.Index)}, "\x00")
	})
}

func (a *app) stashAt(row int) (repository.Stash, bool) {
	return sourceRow(a.visibleStashes(), a.snapshot.Stashes, row)
}

func (a *app) selectStashRow(row int) {
	selectFilteredRow(a.visibleStashes(), row, &a.selectedStashRow, &a.selectedStash)
	a.resetStashDetails()
	a.showStashRenameForm = false
}

func (a *app) syncStashRow() {
	syncFilteredRow(a.visibleStashes(), &a.selectedStashRow, &a.selectedStash)
}

// stashRowMenu is the right-click menu for a saved-work row.
func (a *app) stashRowMenu(m *ui.Menu, row ui.ListRow) {
	stash, ok := a.stashAt(row.Index)
	if !ok {
		return
	}
	if m.Item(tr("Apply to working tree")).Shortcut(0, ui.KeySpace).Disabled(a.actionBusy).Chosen() {
		a.selectStashRow(row.Index)
		a.applyStash(stash)
	}
	if m.Item(tr("Pop (apply and drop)…")).Shortcut(0, ui.KeyG).Disabled(a.actionBusy).Chosen() {
		a.selectStashRow(row.Index)
		a.confirmStashMutationFor(stash, true)
	}
	m.Separator()
	if m.Item(tr("Rename…")).Disabled(a.actionBusy).Chosen() {
		a.selectStashRow(row.Index)
		a.stashRename = stash.Name
		a.showStashRenameForm = true
	}
	if m.Item(tr("Drop…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
		a.selectStashRow(row.Index)
		a.confirmStashMutationFor(stash, false)
	}
}

func (a *app) stashView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Gap(0).Children(func() {
		a.recoveryPanel(c)
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionStash],
			"Expand list pane", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).
					Role(ui.RoleRegion).Label(tr("Saved work")).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
						ui.Icon(c, stashIcon).FontSize(14).TextColor(theme.Accent)
						ui.Text(c, tr("Saved work")).Bold().Grow(1)
						rowIconButton(c, "Stash changes…", "Save the working tree away",
							stashIcon, a.openStashForm)
						paneCollapseButton(c, &a.workspacePaneCollapsed[sectionStash], "Collapse list pane")
					})
					if len(a.snapshot.Stashes) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("No stashes")).TextColor(theme.TextMuted)
						})
						return
					}
					visible := a.visibleStashes()
					listSearchField(c, &a.sectionSearch[sectionStash], &a.stashFilter,
						"Search saved work", "Filter by message, hash, or stash name", "Clear stash search",
						len(visible), len(a.snapshot.Stashes), func() { a.selectStashRow(0) }, a.navigateStashSearch)
					visible = a.visibleStashes()
					if len(visible) == 0 {
						listSearchEmpty(c, "No stashes match this search")
						return
					}
					list := a.keyboardList(c, &a.stashList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
						stash, _ := sourceRow(visible, a.snapshot.Stashes, row.Index)
						element := ui.Column(row.Context).Gap(2).Padding(7, 8)
						// The row's actions show on the pointer's row, or the
						// chosen one, so the name keeps its room at rest, as
						// the file rows of the Changes panel do.
						reveal := element.Hovered() || row.Selected()
						element.Children(func() {
							ui.Row(row.Context).AlignItems(ui.Center).Gap(6).Children(func() {
								ui.Text(row.Context, stash.Name).Grow(1).SingleLine()
								if reveal {
									rowIconButton(row.Context, "Apply "+stash.Name, "Apply this saved work to the working tree", applyFileIcon, func() {
										a.selectStashRow(row.Index)
										a.applyStash(stash)
									})
									rowIconButton(row.Context, "Discard "+stash.Name, "Discard this saved work…", revertIcon, func() {
										a.selectStashRow(row.Index)
										a.confirmStashMutationFor(stash, false)
									})
								}
							})
							ui.Textf(row.Context, "stash@{%d} · %s", stash.Index, stash.Recency).TextColor(secondaryRowColor(theme, row))
						}).PopoverMenu(func(m *ui.Menu) { a.stashRowMenu(m, row) })
					})
					if a.keyboardListChanged(&a.stashList, list) {
						a.selectStashRow(a.selectedStashRow)
					}
					normalizeFilteredRow(visible, &a.selectedStashRow, &a.selectedStash)
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(16)).Children(func() {
					if a.selectedStash < 0 || a.selectedStash >= len(a.snapshot.Stashes) {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Select saved work")).TextColor(theme.TextMuted)
						})
						return
					}
					stash := a.snapshot.Stashes[a.selectedStash]
					ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
						ui.Text(c, stash.Name).FontSize(18).Bold().Grow(1).SingleLine().Tooltip(stash.Name)
						if ui.Button(c, tr("Rename…")).Disabled(a.actionBusy).Clicked() {
							a.showStashRenameForm = true
							a.stashRename = stash.Name
						}
						if ui.Button(c, tr("Apply")).Disabled(a.actionBusy).Clicked() {
							a.applyStash(stash)
						}
						if ui.Button(c, tr("Pop…")).Disabled(a.actionBusy).Clicked() {
							a.confirmStashMutation(true)
						}
						if ui.Button(c, tr("Drop…")).Disabled(a.actionBusy).Clicked() {
							a.confirmStashMutation(false)
						}
					})
					ui.Row(c).Justify(ui.End).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
						a.diffViewOptions(c)
					})
					ui.Textf(c, "stash@{%d} · %s", stash.Index, stash.Recency).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					if a.showStashRenameForm {
						ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
							ui.TextInput(c, &a.stashRename).Label(tr("New stash name")).Grow(1)
							if ui.Button(c, tr("Cancel rename")).Disabled(a.actionBusy).Clicked() {
								a.showStashRenameForm = false
							}
							if ui.PrimaryButton(c, tr("Rename stash")).Disabled(a.actionBusy || strings.TrimSpace(a.stashRename) == "").Clicked() {
								a.beginRenameStash()
							}
						})
					}
					a.requestStashDetails(stash)
					switch {
					case a.stashFilesLoading:
						ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
							ui.Spinner(c).Label(tr("Loading stash files"))
							ui.Text(c, tr("Reading the saved files…")).TextColor(theme.TextMuted)
						})
						return
					case a.stashFilesError != "":
						ui.Column(c).Grow(1).Gap(8).Children(func() {
							ui.Text(c, a.stashFilesError).TextColor(theme.Danger)
							if ui.Button(c, tr("Try again")).Clicked() {
								a.stashFilesHash = ""
								a.requestStashDetails(stash)
							}
						})
						return
					case len(a.stashFiles) == 0:
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("This stash saved no files.")).TextColor(theme.TextMuted)
						})
						return
					}
					paths := commitFilePaths(a.stashFiles)
					a.stashFileRows = a.syncFileTreeRows(fileTreeStashScope, paths, a.stashFileRows,
						&a.selectedStashFileRow, &a.selectedStashFile)
					// The saved files and the chosen file's diff share the rest
					// of the card. Their inset sash can be dragged like the commit
					// detail panes without touching the card's rounded border.
					paneSplitVertical(c, &a.stashFilesHeight, func() {
						ui.Column(c).Fill().Gap(6).Children(func() {
							ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
								ui.Icon(c, folderIcon).FontSize(13).TextColor(theme.Accent)
								ui.Text(c, tr("Changed files")).Bold().Grow(1)
								ui.Badge(c, fmt.Sprintf("%d", len(a.stashFiles)))
								a.fileTreeButtons(c, fileTreeStashScope, paths)
							})
							list := a.keyboardList(c, &a.stashFileList, len(a.stashFileRows)).Grow(1).Rows(func(row ui.ListRow) {
								item := a.stashFileRows[row.Index]
								if item.isDirectory() {
									a.fileTreeDirectoryRow(row.Context, fileTreeStashScope, item, row.Selected(), func() {
										a.selectStashFileRow(row.Index)
									})
									return
								}
								file := a.stashFiles[item.source]
								services := row.Context.Services()
								element := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(6, 8, 6, float32(8+16*item.depth))
								reveal := element.Hovered() || row.Selected()
								element.Children(func() {
									fileTypeMark(row.Context, file.Path, 13, row.Selected())
									ui.Text(row.Context, item.name).Grow(1).SingleLine().Tooltip(file.Path)
									if file.PreviousPath != "" {
										ui.Text(row.Context, "← "+file.PreviousPath).
											TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1).SingleLine()
									}
									fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
									if reveal {
										rowIconButton(row.Context, "Apply "+file.Path, "Apply this file", applyFileIcon, func() {
											a.applyStashFile(stash, file)
										})
										rowIconButton(row.Context, "Discard "+file.Path, "Discard this file", revertIcon, func() {
											a.confirmDiscardStashFile(stash, file)
										})
									}
									statusSlot(row.Context, statusLetter(file.Status),
										rowStatusColor(theme, row, diffFileStyle(theme, file.Status)))
								}).PopoverMenu(func(m *ui.Menu) {
									a.stashFileMenu(m, stash, file, services)
								})
							})
							if a.keyboardListChanged(&a.stashFileList, list) {
								a.selectStashFileRow(a.selectedStashFileRow)
							}
							normalizeFileTreeRow(a.stashFileRows, &a.selectedStashFileRow, &a.selectedStashFile)
						})
					}, func() {
						ui.Column(c).Fill().Children(func() {
							if a.selectedStashFile >= 0 && a.selectedStashFile < len(a.stashFiles) {
								a.requestStashFileDiff(stash, a.stashFiles[a.selectedStashFile])
							}
							switch {
							case a.stashDiffLoading:
								ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
									ui.Spinner(c).Label(tr("Loading diff"))
									ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
								})
							case a.stashDiffError != "":
								ui.Column(c).Gap(8).Children(func() {
									ui.Text(c, a.stashDiffError).TextColor(theme.Danger)
									if ui.Button(c, tr("Try again")).Clicked() {
										a.stashDiffHash = ""
										a.requestStashFileDiff(stash, a.stashFiles[a.selectedStashFile])
									}
								})
							case a.stashDiffState != nil:
								a.keyboardDiffViewer(c, a.stashDiffState)
							default:
								noTextualDiffOrMedia(c, a.stashDiffMedia, a.stashDiffPath)
							}
						})
					}).Grow(1)
				})
			})
	})
}
