package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"

	"lazygitapp/internal/repository"
)

// changeCourierState freezes source bytes, then binds one fresh preview to the
// chosen destination. A preview can never be applied through another session or
// after the destination selection has changed.
type changeCourierState struct {
	open             bool
	sourceLoading    bool
	source           string
	patchText        string
	destinations     []repository.Worktree
	destination      string
	previewLoading   bool
	preview          *repository.PatchPreview
	previewError     string
	destinationError string
	target           repository.Session
	token            uint64
}

func courierDestinations(snapshot repository.Snapshot) []repository.Worktree {
	current := filepath.Clean(snapshot.Root)
	var destinations []repository.Worktree
	for _, worktree := range snapshot.Worktrees {
		if worktree.Missing || worktree.OperationLive || filepath.Clean(worktree.Path) == current {
			continue
		}
		destinations = append(destinations, worktree)
	}
	return destinations
}

func courierDestinationLabel(worktree repository.Worktree) string {
	name := strings.TrimSpace(worktree.Branch)
	if name == "" {
		name = worktree.Name
	}
	if name == "" {
		name = filepath.Base(worktree.Path)
	}
	if worktree.Head != "" && strings.TrimSpace(worktree.Branch) == "" {
		name += " @ " + worktree.Head
	}
	return name
}

func (a *app) courierEligible() bool {
	return a.session != nil && !a.actionBusy
}

func (a *app) courierButtonTooltip(tooltip string) string {
	if a.actionBusy {
		return tr("Wait for the current action to finish, then send these changes to another worktree.")
	}
	if len(courierDestinations(a.snapshot)) == 0 {
		return tr("No other available worktrees. Create a worktree for the destination branch first.")
	}
	return tooltip
}

func (a *app) openChangeCourier(text, source string) {
	if !a.courierEligible() || strings.TrimSpace(text) == "" {
		return
	}
	a.courier.token++
	a.courier.open = true
	a.courier.sourceLoading = false
	a.courier.source = source
	a.courier.patchText = text
	a.courier.destinations = courierDestinations(a.snapshot)
	a.courier.destination = ""
	a.courier.previewLoading = false
	a.courier.preview = nil
	a.courier.previewError = ""
	a.courier.destinationError = ""
	a.courier.target = nil
	if len(a.courier.destinations) > 0 {
		a.beginCourierDestination(a.courier.destinations[0].Path)
	}
}

func (a *app) closeChangeCourier() {
	if a.actionBusy {
		return
	}
	a.resetChangeCourier()
}

func (a *app) resetChangeCourier() {
	a.courier.token++
	a.courier.open = false
	a.courier.sourceLoading = false
	a.courier.source = ""
	a.courier.patchText = ""
	a.courier.destinations = nil
	a.courier.destination = ""
	a.courier.previewLoading = false
	a.courier.preview = nil
	a.courier.previewError = ""
	a.courier.destinationError = ""
	a.courier.target = nil
}

func (a *app) beginCourierDestination(path string) {
	if !a.courier.open || a.actionBusy {
		return
	}
	found := false
	for _, worktree := range a.courier.destinations {
		if worktree.Path == path {
			found = true
			break
		}
	}
	if !found {
		return
	}
	a.courier.token++
	token := a.courier.token
	generation := a.generation
	frozen := a.courier.patchText
	a.courier.destination = path
	a.courier.previewLoading = true
	a.courier.preview = nil
	a.courier.previewError = ""
	a.courier.destinationError = ""
	a.courier.target = nil
	go func() {
		defer a.recoverBackground()
		target, err := a.opener.Open(context.Background(), path)
		var preview repository.PatchPreview
		if err == nil {
			preview, err = target.PreviewPatchText(context.Background(), frozen)
		}
		a.update(func() {
			if generation != a.generation || token != a.courier.token || !a.courier.open {
				return
			}
			a.courier.previewLoading = false
			if err != nil {
				a.courier.destinationError = safeError(err)
				return
			}
			a.courier.target = target
			a.courier.preview = &preview
		})
	}()
}

func (a *app) beginCourierApply() {
	if a.actionBusy || !a.courier.open || a.courier.target == nil ||
		a.courier.preview == nil || !a.courier.preview.Clean {
		return
	}
	target := a.courier.target
	preview := *a.courier.preview
	generation := a.generation
	token := a.courier.token
	destination := courierDestinationLabel(repository.Worktree{Path: a.courier.destination})
	for _, worktree := range a.courier.destinations {
		if worktree.Path == a.courier.destination {
			destination = courierDestinationLabel(worktree)
			break
		}
	}
	a.actionBusy = true
	a.actionError = ""
	go func() {
		defer a.recoverBackground()
		err := target.ApplyPatch(context.Background(), preview)
		a.update(func() {
			if generation != a.generation {
				return
			}
			a.actionBusy = false
			if err != nil {
				a.actionError = safeError(err)
				if token == a.courier.token {
					// Any error can follow a completed Git write (for example, a
					// recovery-pruning failure). Require a fresh preview rather than
					// offering a blind retry of the old fingerprint.
					a.courier.preview = nil
					a.courier.previewError = safeError(err)
				}
				a.notify("error", tr("Could not deliver changes"), safeError(err))
				return
			}
			if token != a.courier.token {
				return
			}
			a.resetChangeCourier()
			a.notify("success", tr("Changes delivered"), trf("Applied to %s as unstaged changes. The source is unchanged.", destination))
		})
	}()
}

var errCourierSourceChanged = errors.New("the displayed diff is no longer current")

// courierSelectedLineContents binds numeric line identities to the change text
// the user actually selected. Canonical reads use full object IDs and binary
// headers that differ from the displayed patch, so comparing complete patch
// strings would reject an unchanged file. Comparing the selected change lines
// catches the dangerous case: the same line number now holding different work.
func courierSelectedLineContents(sourcePatch string, lines []patch.LineIdentity) ([]string, error) {
	parsed, err := editor.ParsePatch(sourcePatch)
	if err != nil || len(parsed) != 1 {
		return nil, fmt.Errorf("read the selected source patch")
	}
	filePatch := patch.Parse(sourcePatch)
	indices := patch.ChangeLineIndicesForLines(filePatch, lines)
	sort.Ints(indices)
	patchLines := filePatch.Lines()
	contents := make([]string, 0, len(indices))
	last := -1
	for _, index := range indices {
		if index < 0 || index >= len(patchLines) {
			return nil, fmt.Errorf("read the selected source lines")
		}
		if index == last {
			continue
		}
		contents = append(contents, patchLines[index].Content)
		last = index
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("the selection contains no changed lines")
	}
	return contents, nil
}

func (a *app) beginWorkingTreeCourier(file repository.File, lines []patch.LineIdentity, source string) {
	a.beginWorkingTreeCourierAtDiff(file, lines, "", source)
}

func (a *app) beginWorkingTreeCourierAtDiff(file repository.File, lines []patch.LineIdentity, displayedPatch, source string) {
	if !a.courierEligible() {
		return
	}
	if file.HasMergeConflicts {
		a.notify("", tr("Resolve conflicts first"), tr("Conflicted files cannot be sent as one patch."))
		return
	}
	session := a.session
	generation := a.generation
	a.courier.token++
	token := a.courier.token
	a.courier.open = true
	a.courier.sourceLoading = true
	a.courier.source = source
	a.courier.patchText = ""
	a.courier.destinations = courierDestinations(a.snapshot)
	a.courier.destination = ""
	a.courier.preview = nil
	a.courier.previewError = ""
	a.courier.destinationError = ""
	a.courier.target = nil
	go func() {
		defer a.recoverBackground()
		text, err := session.WorkingTreeFilePatch(context.Background(), file)
		if err == nil && len(lines) > 0 {
			if displayedPatch != "" {
				var displayedLines, currentLines []string
				displayedLines, err = courierSelectedLineContents(displayedPatch, lines)
				if err == nil {
					currentLines, err = courierSelectedLineContents(text, lines)
				}
				if err == nil && !slices.Equal(displayedLines, currentLines) {
					err = errCourierSourceChanged
				}
			}
			if err == nil {
				text, err = patchTextForIdentities(text, file.Path, lines)
			}
		}
		a.update(func() {
			if generation != a.generation || token != a.courier.token || !a.courier.open {
				return
			}
			a.courier.sourceLoading = false
			if err != nil {
				if errors.Is(err, errCourierSourceChanged) {
					a.courier.previewError = tr("The file changed after this diff was loaded. Review the new diff and select the changes again.")
				} else {
					a.courier.previewError = safeError(err)
				}
				return
			}
			if strings.TrimSpace(text) == "" {
				a.courier.previewError = tr("The selected file has no changes to send.")
				return
			}
			a.courier.patchText = text
			if len(a.courier.destinations) > 0 {
				a.beginCourierDestination(a.courier.destinations[0].Path)
			}
		})
	}()
}

func (a *app) beginSelectedLinesCourier() {
	change, ok := a.selectedChange()
	if !ok || !a.lineSelectionAvailable() || a.diffState == nil {
		return
	}
	parsed, err := editor.ParsePatch(a.diffPatch)
	if err != nil || len(parsed) != 1 {
		a.notifyError(fmt.Errorf("read selected lines for Change Courier: %w", err))
		return
	}
	lines := selectedLineIdentities(parsed[0], a.currentLineSelections()...)
	if len(lines) == 0 {
		a.notify("", tr("Nothing to send"), tr("Select changed lines, not unchanged ones."))
		return
	}
	file := change.diffFile()
	a.beginWorkingTreeCourierAtDiff(file, lines, a.diffPatch, trf("Selected lines · %s", file.Path))
}

func (a *app) beginCurrentHunkCourier() {
	change, ok := a.selectedChange()
	if !ok || a.diffState == nil || a.diffPatch == "" || a.customPatchActive() {
		return
	}
	hunk, _ := a.diffState.CurrentHunk()
	parsed, err := editor.ParsePatch(a.diffPatch)
	if err != nil || len(parsed) != 1 {
		a.notifyError(fmt.Errorf("read the current hunk for Change Courier: %w", err))
		return
	}
	lines := customPatchHunkIdentities(parsed[0], hunk)
	if len(lines) == 0 {
		a.notify("", tr("Nothing to send"), tr("Move the caret into a diff hunk first."))
		return
	}
	file := change.diffFile()
	a.beginWorkingTreeCourierAtDiff(file, lines, a.diffPatch, trf("Current hunk · %s", file.Path))
}

func (a *app) beginCustomPatchCourier() {
	text, err := customPatchText(a.customPatch.entries)
	if err != nil {
		a.notifyError(err)
		return
	}
	a.openChangeCourier(text, trf("Custom Patch · %d files", len(a.customPatch.entries)))
}

func (a *app) beginCustomPatchPreviewCourier() {
	if a.customPatch.preview == nil {
		return
	}
	data, err := a.customPatch.preview.Bytes()
	if err != nil {
		a.notifyError(err)
		return
	}
	a.openChangeCourier(string(data), trf("Custom Patch · %d files", len(a.customPatch.entries)))
}

func (a *app) changeCourierModal(c *ui.Context) {
	if !a.courier.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("change-courier-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(880).Height(680).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(12).Background(theme.Background).Border(1, theme.Border).
				Radius(theme.Radius + 4).Role(ui.RoleDialog).Label(tr("Change Courier")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, worktreesIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Change Courier")).FontSize(18).Bold()
						ui.Text(c, tr("Copy selected changes to another worktree without committing.")).TextColor(theme.TextMuted)
					})
					iconButton(c, "Close Change Courier", "Escape", closeIcon, a.actionBusy, a.closeChangeCourier)
				})
				ui.Text(c, a.courier.source).TextColor(theme.TextMuted).SingleLine().Tooltip(a.courier.source)
				ui.Divider(c)
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
					a.changeCourierBody(c)
				})
				ui.Divider(c)
				a.changeCourierFooter(c)
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeChangeCourier()
			}
		})
	})
}

func (a *app) changeCourierBody(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		if a.courier.sourceLoading {
			ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Children(func() {
				ui.Spinner(c).Label(tr("Freezing selected changes"))
				ui.Text(c, tr("Reading a canonical patch without changing the source…")).Bold()
			})
			return
		}
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, tr("Destination worktree or checked-out branch")).Bold()
			if len(a.courier.destinations) == 0 {
				ui.Text(c, tr("No other available worktrees. Create a worktree for the destination branch first.")).TextColor(theme.TextMuted)
				if ui.Button(c, tr("Create worktree…")).Disabled(a.actionBusy).Clicked() {
					a.resetChangeCourier()
					a.showWorktreeForm = true
					a.worktreeBase = "HEAD"
				}
				return
			}
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, worktree := range a.courier.destinations {
					target := worktree
					label := courierDestinationLabel(target)
					clicked := false
					if a.courier.destination == target.Path {
						clicked = compactTextButton(ui.PrimaryButton(c, label)).Label(trf("Send to %s", label)).Tooltip(target.Path).
							Disabled(a.actionBusy || a.courier.previewLoading).Clicked()
					} else {
						clicked = compactTextButton(ui.Button(c, label)).Label(trf("Send to %s", label)).Tooltip(target.Path).
							Disabled(a.actionBusy || a.courier.previewLoading).Clicked()
					}
					if clicked {
						a.beginCourierDestination(target.Path)
					}
				}
			})
			if a.courier.destination != "" {
				ui.Text(c, a.courier.destination).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).
					SingleLine().Tooltip(a.courier.destination)
			}
		})
		if a.courier.previewError != "" {
			ui.Column(c).Gap(6).Padding(10).Background(theme.Danger.Alpha(0.08)).Children(func() {
				ui.Text(c, tr("Could not prepare the delivery")).Bold().TextColor(theme.Danger)
				ui.Text(c, a.courier.previewError).Selectable()
			})
			return
		}
		if a.courier.destinationError != "" {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, tr("Could not inspect the destination")).Bold().TextColor(theme.Danger)
				ui.Text(c, a.courier.destinationError).Selectable()
			})
			return
		}
		if a.courier.previewLoading {
			ui.Row(c).Gap(8).Padding(20).Children(func() {
				ui.Spinner(c).Label(tr("Checking destination"))
				ui.Text(c, tr("Detecting conflicts without changing the destination…"))
			})
			return
		}
		preview := a.courier.preview
		if preview == nil {
			return
		}
		statusColor, status := theme.Success, tr("Delivery applies cleanly")
		detail := tr("Apply writes the destination working tree only. Nothing will be staged or committed.")
		if !preview.Clean {
			statusColor, status = theme.Danger, tr("Destination conflicts with these changes")
			detail = tr("Nothing was changed. Resolve the destination or choose another worktree, then preview again.")
		}
		ui.Column(c).Gap(5).Padding(10).Background(statusColor.Alpha(0.1)).
			Border(1, statusColor.Alpha(0.4)).Radius(theme.Radius).Children(func() {
			ui.Text(c, status).Bold().TextColor(statusColor)
			ui.Text(c, detail)
			ui.Text(c, trf("%d files · +%d · −%d", len(preview.Files), preview.Additions, preview.Deletions)).TextColor(theme.TextMuted)
		})
		if preview.Diagnostic != "" {
			ui.Text(c, preview.Diagnostic).Selectable().TextColor(theme.Danger)
		}
		for _, file := range preview.Files {
			ui.Row(c).Gap(8).Padding(6, 8).Background(theme.Surface).Children(func() {
				ui.Text(c, patchFileLabel(file)).Grow(1).SingleLine().Tooltip(patchFileLabel(file))
				if file.Binary {
					ui.Badge(c, tr("Binary"))
				} else {
					fileDiffStatCounts(c, file.Path, file.Additions, file.Deletions)
				}
			})
		}
		ui.Text(c, tr("Patch text")).Bold()
		ui.Text(c, preview.Text).Selectable().FontSize(theme.FontSize - 1)
		if preview.TextTruncated {
			ui.Text(c, tr("Patch text is truncated here; delivery still validates and uses the complete patch.")).TextColor(theme.TextMuted)
		}
	})
}

func (a *app) changeCourierFooter(c *ui.Context) {
	ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
		if ui.Button(c, tr("Close")).Disabled(a.actionBusy).Clicked() {
			a.closeChangeCourier()
		}
		if a.courier.preview != nil && a.courier.preview.Clean {
			if ui.PrimaryButton(c, tr("Send as unstaged changes")).Disabled(a.actionBusy || a.courier.previewLoading).Clicked() {
				a.beginCourierApply()
			}
		}
	})
}
