package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// previewAndApplyPatch is the single app-side mutation path for a generated
// patch. The repository returns one opaque approval containing private bytes,
// options, and its state digest; only that same value can subsequently apply.
func previewAndApplyPatch(ctx context.Context, session repository.Session, request repository.PatchRequest) error {
	preview, err := requireChangesSession(session).PreviewPatchRequest(ctx, request)
	if err != nil {
		return err
	}
	return requireChangesSession(session).ApplyPatch(ctx, preview)
}

// changedFileNavigation moves through actual file rows, leaving section and
// directory headers out. It wraps so the same buttons work at either end.
func (a *app) changedFileNavigation(direction int) {
	if len(a.changeRows) == 0 || direction == 0 {
		return
	}
	for step := 1; step <= len(a.changeRows); step++ {
		index := (a.selectedFile + direction*step) % len(a.changeRows)
		if index < 0 {
			index += len(a.changeRows)
		}
		if !a.changeRows[index].isFile() {
			continue
		}
		a.selectedFile = index
		a.resetFileDiff()
		return
	}
}

// changedFileAndHunkNavigationControls shares one toolbar row with hunk
// actions. In particular, conflict help must not take another row and squeeze
// the existing conflict editor at narrow widths.
func (a *app) changedFileAndHunkNavigationControls(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
		a.hunkActionControls(c)
		a.changedFileNavigationControls(c)
	})
}

// changedFileNavigationControls stays available while the newly chosen diff
// loads, so repeated next/previous actions never depend on network-speed work.
func (a *app) changedFileNavigationControls(c *ui.Context) {
	ui.Row(c).Justify(ui.End).Children(func() {
		flatIconButton(c, "Previous changed file", "Go to the previous changed file (Alt+K)", chevronUpIcon, 14, 4, false, func() {
			a.changedFileNavigation(-1)
		})
		flatIconButton(c, "Next changed file", "Go to the next changed file (Alt+J)", chevronDownIcon, 14, 4, false, func() {
			a.changedFileNavigation(1)
		})
	})
}

type hunkActionStatus struct {
	staged bool
	reason string
	hunk   int
	count  int
}

// currentHunkActionStatus is derived from the focused Changes row and the
// actual canonical patch it loaded. Unsupported shapes stay visible as
// disabled controls with a concrete reason rather than silently disappearing.
func (a *app) currentHunkActionStatus() hunkActionStatus {
	change, ok := a.selectedChange()
	if !ok {
		return hunkActionStatus{hunk: -1, reason: "Choose a changed file first."}
	}
	status := hunkActionStatus{staged: change.staged, hunk: -1}
	if a.diffKind == "Staged" || a.diffKind == "Unstaged" {
		status.staged = a.diffKind == "Staged"
	}
	if change.file.HasMergeConflicts {
		status.reason = "Combined conflict diffs cannot be staged or discarded by hunk. Resolve the conflict or use a whole-file action."
		return status
	}
	if a.diffLoading {
		status.reason = "Wait for this file's diff to load."
		return status
	}
	if a.diffError != "" {
		status.reason = "The current diff could not be read. Refresh it before changing a hunk."
		return status
	}
	if status.staged && change.file.Deleted {
		status.reason = "A staged deletion can only be unstaged as a whole file."
		return status
	}
	if a.diffState == nil {
		status.reason = "The current file has no textual diff hunks."
		if a.diffPatch != "" {
			parsed, err := editor.ParsePatch(a.diffPatch)
			if err == nil && len(parsed) == 1 && parsed[0].Binary {
				status.reason = "Binary changes cannot be divided into hunks. Use the whole-file actions instead."
			}
		}
		return status
	}
	status.count = len(a.diffState.Hunks())
	if status.count == 0 {
		status.reason = "This diff has no content hunks to change."
		if strings.Contains(a.diffPatch, "old mode ") || strings.Contains(a.diffPatch, "new mode ") {
			status.reason = "Mode-only changes cannot be divided into hunks. Use the whole-file actions instead."
		}
		return status
	}
	status.hunk, _ = a.diffState.CurrentHunk()
	return status
}

// currentHunkPatch reuses the existing hunk identity mapping and partial patch
// transformer. It never reconstructs lines from rendered row coordinates.
func (a *app) currentHunkPatch(discard bool) (repository.File, string, bool, bool) {
	change, ok := a.selectedChange()
	if !ok {
		return repository.File{}, "", false, false
	}
	status := a.currentHunkActionStatus()
	if status.reason != "" {
		a.notify("warning", tr("Hunk action unavailable"), tr(status.reason))
		return repository.File{}, "", false, false
	}
	if status.hunk < 0 || status.hunk >= status.count {
		a.notify("", tr("No current hunk"), tr("Move the caret into a diff hunk first."))
		return repository.File{}, "", false, false
	}
	parsed, err := editor.ParsePatch(a.diffPatch)
	if err != nil || len(parsed) != 1 {
		if err == nil {
			err = fmt.Errorf("the diff covers %d files, not one", len(parsed))
		}
		a.notifyError(fmt.Errorf("read the current hunk: %w", err))
		return repository.File{}, "", false, false
	}
	file := change.diffFile()
	lines := customPatchHunkRunIdentities(parsed[0], status.hunk)
	patchText, wholeFile, err := partialPatchForIdentities(a.diffPatch, file, lines, discard || status.staged)
	if err != nil {
		a.notifyError(err)
		return repository.File{}, "", false, false
	}
	if patchText == "" && !wholeFile {
		a.notify("", tr("Nothing to change"), tr("Move the caret into a diff hunk first."))
		return repository.File{}, "", false, false
	}
	// Whole staging/unstaging retains the existing add/delete file semantics.
	// Discard must never take that route: session.Discard would also reset the
	// index. Its original full patch reverses only working-tree content.
	if discard && wholeFile {
		patchText = a.diffPatch
	}
	return file, patchText, wholeFile, true
}

func (a *app) beginCurrentHunkAction() {
	if a.actionBusy || a.session == nil {
		return
	}
	file, patchText, wholeFile, ok := a.currentHunkPatch(false)
	if !ok {
		return
	}
	reverse := a.diffKind == "Staged"
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

// confirmDiscardCurrentHunk freezes the exact hunk patch before asking. The
// confirmation protects a destructive click; ApplyPatch's later CAS protects
// against repository changes between the loaded diff and the actual mutation.
func (a *app) confirmDiscardCurrentHunk() {
	if a.actionBusy || a.session == nil || a.currentHunkActionStatus().staged {
		return
	}
	file, patchText, _, ok := a.currentHunkPatch(true)
	if !ok {
		return
	}
	status := a.currentHunkActionStatus()
	a.askConfirm(confirmRequest{
		title:        "Discard Hunk",
		message:      trf("Discard hunk %d of %d in %s?", status.hunk+1, status.count, file.Path),
		detail:       "Only this hunk's working-tree changes are removed; the index and staged changes stay untouched. A recovery copy is kept for 7 days in Saved work.",
		confirmLabel: "Discard hunk",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Discarded hunk in %s", file.Path), file.Path,
				func(ctx context.Context, session repository.Session) error {
					return previewAndApplyPatch(ctx, session, repository.PatchRequest{
						Text: patchText, Target: repository.PatchWorktree, Reverse: true,
					})
				}, nil)
		},
	})
}

func navigateDiffHunk(state *editor.DiffState, direction int) {
	if state == nil || direction == 0 || len(state.Hunks()) == 0 {
		return
	}
	hunk, _ := state.CurrentHunk()
	target := hunk + direction
	if hunk < 0 {
		// Preserve the existing diff navigation: either hunk key enters the
		// first hunk when the caret is before it.
		target = 0
	}
	if target < 0 || target >= len(state.Hunks()) {
		return
	}
	state.GoToHunk(target)
}

// hunkActionControls is the one-click hunk workflow for the focused Changes
// diff. It shares conflictActionToolbar with conflict Hunk/Apply actions;
// wrapping and narrow-window grouping keep labels reachable without squeezing text.
func (a *app) hunkActionControls(c *ui.Context) {
	status, mutationReason := a.hunkMutationAvailability()
	disabled := mutationReason != ""
	theme := c.Theme()
	row := ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(5)
	paneWidth := a.measuredActionGroupWidth(c, row)
	compactLabels := normalizeConflictActionStyle(a.settings.ConflictActionStyle) == conflictActionStyleIconsLabels &&
		(paneWidth <= 0 || paneWidth < conflictCompactPaneWidth)
	row.Children(func() {
		if mutationReason != "" {
			ui.Text(c, tr(mutationReason)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).
				SingleLine().Width(0).Grow(1).Tooltip(tr(mutationReason))
		} else {
			ui.Text(c, trf("Hunk %d/%d", status.hunk+1, status.count)).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).Grow(1)
		}
		actions := make([]actionControl, 0, 4)
		if status.staged {
			actions = append(actions, actionControl{
				label: "Unstage hunk", shortcut: "s", description: "Unstage only the current hunk; leave it in the working tree", icon: unstageIcon,
				disabled: disabled, disabledReason: mutationReason, run: a.beginCurrentHunkAction,
			})
		} else {
			actions = append(actions,
				actionControl{label: "Stage hunk", shortcut: "s", description: "Stage only the current hunk, leaving other hunks unstaged", icon: stageIcon,
					disabled: disabled, disabledReason: mutationReason, run: a.beginCurrentHunkAction},
				actionControl{label: "Discard hunk", shortcut: "d", description: "Discard the current hunk after confirmation; keep the index untouched", icon: revertIcon,
					disabled: disabled, disabledReason: mutationReason, run: a.confirmDiscardCurrentHunk},
			)
		}
		previousReason := a.hunkNavigationDisabledReason(-1)
		nextReason := a.hunkNavigationDisabledReason(1)
		actions = append(actions,
			actionControl{label: "Previous hunk", shortcut: "[", description: "Go to the previous diff hunk", icon: chevronUpIcon,
				disabled: previousReason != "", disabledReason: previousReason, run: func() { navigateDiffHunk(a.diffState, -1) }},
			actionControl{label: "Next hunk", shortcut: "]", description: "Go to the next diff hunk", icon: chevronDownIcon,
				disabled: nextReason != "", disabledReason: nextReason, run: func() { navigateDiffHunk(a.diffState, 1) }},
		)
		if compactLabels {
			renderActionControl(c, actionControl{
				label: "Diff hunk actions", visibleLabel: "More", icon: moreIcon,
				description: "Show this action group with labels and unavailable reasons", menu: actions,
			}, true)
		} else {
			a.conflictActionToolbar(c, "Diff hunk actions", actions)
		}
	})
}
