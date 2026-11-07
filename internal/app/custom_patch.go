package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"

	"lazygitapp/internal/repository"
)

// customPatchEntry is one immutable commit/file selection. Whole entries keep
// Git's canonical file patch. Partial entries keep that same source plus
// content-based line identities, so viewer layout and display options never
// become patch coordinates.
type customPatchEntry struct {
	commit      repository.Commit
	file        repository.CommitFile
	sourcePatch string
	whole       bool
	lines       []patch.LineIdentity
	order       uint64
}

type customPatchState struct {
	open         bool
	entries      []customPatchEntry
	nextOrder    uint64
	revision     uint64
	pending      int
	addToken     uint64
	previewOpen  bool
	previewLoad  bool
	preview      *repository.PatchPreview
	previewError string
	previewToken uint64
}

func (a *app) customPatchActive() bool {
	return a.session != nil && isCommitSection(a.workspaceTab)
}

func customPatchEntryKey(entry customPatchEntry) string {
	return entry.commit.Hash + "\x00" + entry.file.Path
}

func customPatchPaths(file repository.CommitFile) []string {
	paths := []string{file.Path}
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		paths = append(paths, file.PreviousPath)
	}
	return paths
}

func customPatchesCollide(left, right customPatchEntry) bool {
	if left.commit.Hash == right.commit.Hash {
		return false
	}
	for _, a := range customPatchPaths(left.file) {
		for _, b := range customPatchPaths(right.file) {
			if a == b {
				return true
			}
		}
	}
	return false
}

func mergeCustomPatchIdentities(existing, added []patch.LineIdentity) []patch.LineIdentity {
	seen := make(map[patch.LineIdentity]bool, len(existing)+len(added))
	merged := make([]patch.LineIdentity, 0, len(existing)+len(added))
	for _, group := range [][]patch.LineIdentity{existing, added} {
		for _, line := range group {
			if seen[line] {
				continue
			}
			seen[line] = true
			merged = append(merged, line)
		}
	}
	return merged
}

// mergeCustomPatchEntries validates a whole batch before changing the basket.
// Historical patches to one path from different commits have different bases
// and cannot safely be concatenated, so such collections are rejected whole.
func mergeCustomPatchEntries(existing, added []customPatchEntry, nextOrder uint64) ([]customPatchEntry, uint64, error) {
	merged := make([]customPatchEntry, len(existing))
	copy(merged, existing)
	for i := range merged {
		merged[i].lines = append([]patch.LineIdentity(nil), existing[i].lines...)
	}
	indices := make(map[string]int, len(merged))
	for i := range merged {
		indices[customPatchEntryKey(merged[i])] = i
	}
	for _, candidate := range added {
		if candidate.sourcePatch == "" {
			return nil, nextOrder, fmt.Errorf("%s has no textual patch to add", candidate.file.Path)
		}
		for _, other := range merged {
			if customPatchesCollide(candidate, other) {
				return nil, nextOrder, fmt.Errorf("cannot combine changes to %s from different commits; apply or clear the existing selection first", candidate.file.Path)
			}
		}
		key := customPatchEntryKey(candidate)
		if index, ok := indices[key]; ok {
			entry := &merged[index]
			entry.whole = entry.whole || candidate.whole
			entry.lines = mergeCustomPatchIdentities(entry.lines, candidate.lines)
			if entry.whole {
				entry.lines = nil
			}
			continue
		}
		candidate.order = nextOrder
		nextOrder++
		indices[key] = len(merged)
		merged = append(merged, candidate)
	}
	return merged, nextOrder, nil
}

// patchTextForIdentities keeps exactly the selected changes in one canonical
// file patch. Covering every change promotes it to the complete file patch,
// which is required for whole-file additions and deletions.
func patchTextForIdentities(sourcePatch, path string, lines []patch.LineIdentity) (string, error) {
	if !customPatchSourceSupportsPartial(sourcePatch) {
		return "", fmt.Errorf("%s contains mode or type changes and must be sent as a whole file", path)
	}
	filePatch := patch.Parse(sourcePatch)
	indices := patch.ChangeLineIndicesForLines(filePatch, lines)
	sort.Ints(indices)
	unique := indices[:0]
	for _, index := range indices {
		if len(unique) == 0 || unique[len(unique)-1] != index {
			unique = append(unique, index)
		}
	}
	if len(unique) == 0 {
		return "", fmt.Errorf("the selection for %s contains no changed lines", path)
	}
	if len(unique) >= changeLineCount(filePatch) {
		return sourcePatch, nil
	}
	return filePatch.Transform(patch.TransformOpts{IncludedLineIndices: unique}).FormatPlain(), nil
}

// customPatchText assembles an immutable copy in insertion order. A partial
// selection that covers every change is promoted to the complete file patch,
// which is required for whole-file additions and deletions.
func customPatchText(entries []customPatchEntry) (string, error) {
	ordered := append([]customPatchEntry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })
	parts := make([]string, 0, len(ordered))
	for _, entry := range ordered {
		text := entry.sourcePatch
		if !entry.whole {
			partial, err := patchTextForIdentities(entry.sourcePatch, entry.file.Path, entry.lines)
			if err != nil {
				return "", err
			}
			text = partial
		}
		text = strings.TrimSuffix(text, "\n")
		if text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("add at least one changed file, hunk, or line first")
	}
	return strings.Join(parts, "\n") + "\n", nil
}

func (a *app) addCustomPatchEntries(entries []customPatchEntry) {
	merged, next, err := mergeCustomPatchEntries(a.customPatch.entries, entries, a.customPatch.nextOrder)
	if err != nil {
		a.notifyError(err)
		return
	}
	a.customPatch.entries = merged
	a.customPatch.nextOrder = next
	a.customPatch.revision++
	a.customPatch.previewToken++
	a.notify("success", tr("Added to Custom Patch"), trf("%d files selected", len(merged)))
}

func (a *app) beginAddCustomPatchFile(commit repository.Commit, file repository.CommitFile, lines []patch.LineIdentity) {
	if a.session == nil || a.actionBusy {
		return
	}
	if len(lines) > 0 && !customPatchSupportsPartial(file) {
		a.notify("", tr("Whole file required"), tr("Additions, deletions, renames, binary files, and mode-only changes must be added as a whole file."))
		return
	}
	session := a.session
	generation := a.generation
	addToken := a.customPatch.addToken
	a.customPatch.pending++
	go func() {
		defer a.recoverBackground()
		text, err := session.CommitFilePatch(context.Background(), commit, file)
		a.update(func() {
			if generation != a.generation || addToken != a.customPatch.addToken {
				return
			}
			a.customPatch.pending--
			if err != nil {
				a.notifyError(err)
				return
			}
			if len(lines) > 0 && !customPatchSourceSupportsPartial(text) {
				a.notify("", tr("Whole file required"), tr("Additions, deletions, renames, binary files, and mode-only changes must be added as a whole file."))
				return
			}
			a.addCustomPatchEntries([]customPatchEntry{{
				commit: commit, file: file, sourcePatch: text,
				whole: len(lines) == 0, lines: append([]patch.LineIdentity(nil), lines...),
			}})
		})
	}()
}

func customPatchSupportsPartial(file repository.CommitFile) bool {
	status := strings.ToUpper(strings.TrimSpace(file.Status))
	return status == "" || status == "M" || status == "T"
}

// A diff can combine content edits with chmod or a file-type transition. Its
// textual line selection cannot exclude the extended mode headers, so only a
// whole-file entry preserves exactly what the user selected.
func customPatchSourceSupportsPartial(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(line, "old mode ") || strings.HasPrefix(line, "new mode ") {
			return false
		}
	}
	return true
}

func (a *app) beginAddCustomPatchCommit(commit repository.Commit) {
	if a.session == nil || a.actionBusy {
		return
	}
	session := a.session
	generation := a.generation
	addToken := a.customPatch.addToken
	a.customPatch.pending++
	go func() {
		defer a.recoverBackground()
		files, err := session.CommitFiles(context.Background(), commit)
		entries := make([]customPatchEntry, 0, len(files))
		if err == nil {
			for _, file := range files {
				var text string
				text, err = session.CommitFilePatch(context.Background(), commit, file)
				if err != nil {
					break
				}
				entries = append(entries, customPatchEntry{commit: commit, file: file, sourcePatch: text, whole: true})
			}
		}
		a.update(func() {
			if generation != a.generation || addToken != a.customPatch.addToken {
				return
			}
			a.customPatch.pending--
			if err != nil {
				a.notifyError(err)
				return
			}
			if len(entries) == 0 {
				a.notify("", tr("Nothing to add"), tr("This commit has no changed files."))
				return
			}
			a.addCustomPatchEntries(entries)
		})
	}()
}

func (a *app) selectedCustomPatchFile() (repository.Commit, repository.CommitFile, bool) {
	if !a.customPatchActive() {
		return repository.Commit{}, repository.CommitFile{}, false
	}
	return a.selectedHistoricalFile()
}

func (a *app) addSelectedCustomPatchFile() {
	if commit, file, ok := a.selectedCustomPatchFile(); ok {
		a.beginAddCustomPatchFile(commit, file, nil)
	}
}

func (a *app) addSelectedCustomPatchCommit() {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.beginAddCustomPatchCommit(commit)
	}
}

func (a *app) customPatchLineSelectionAvailable() bool {
	return a.customPatchActive() && a.commitDiffState != nil && a.commitDiffState.LineSelection && a.commitDiffPatch != ""
}

func (a *app) currentCustomPatchSelections() []editor.DiffLineRange {
	if a.lineSelectMode && len(a.lineSelectRanges) > 0 {
		return append([]editor.DiffLineRange(nil), a.lineSelectRanges...)
	}
	if a.commitDiffState == nil {
		return nil
	}
	selection, ok := a.commitDiffState.SelectedLines()
	if !ok || selection.End < selection.Start {
		return nil
	}
	return []editor.DiffLineRange{selection}
}

func (a *app) addCustomPatchSelectedLines() {
	commit, file, ok := a.selectedCustomPatchFile()
	if !ok || !a.customPatchLineSelectionAvailable() {
		return
	}
	parsed, err := editor.ParsePatch(a.commitDiffPatch)
	if err != nil || len(parsed) != 1 {
		if err == nil {
			err = fmt.Errorf("the diff covers %d files, not one", len(parsed))
		}
		a.notify("", tr("Could not read the selected lines"), safeError(err))
		return
	}
	lines := selectedLineIdentities(parsed[0], a.currentCustomPatchSelections()...)
	if len(lines) == 0 {
		a.notify("", tr("Nothing to add"), tr("Select changed lines, not unchanged ones."))
		return
	}
	a.beginAddCustomPatchFile(commit, file, lines)
}

func customPatchHunkIdentities(file editor.FilePatch, wanted int) []patch.LineIdentity {
	if wanted < 0 || wanted >= len(file.Hunks) {
		return nil
	}
	hunk := file.Hunks[wanted]
	oldLine, newLine := hunk.OldStart, hunk.NewStart
	var lines []patch.LineIdentity
	for _, text := range hunk.Lines {
		if text == "" || text[0] == '\\' {
			continue
		}
		switch text[0] {
		case ' ':
			oldLine++
			newLine++
		case '-':
			lines = append(lines, patch.LineIdentity{LineNumber: oldLine, IsDeletion: true})
			oldLine++
		case '+':
			lines = append(lines, patch.LineIdentity{LineNumber: newLine})
			newLine++
		}
	}
	return lines
}

func (a *app) addCustomPatchCurrentHunk() {
	commit, file, ok := a.selectedCustomPatchFile()
	if !ok || a.commitDiffState == nil || a.commitDiffPatch == "" {
		return
	}
	hunk, _ := a.commitDiffState.CurrentHunk()
	parsed, err := editor.ParsePatch(a.commitDiffPatch)
	if err != nil || len(parsed) != 1 {
		if err == nil {
			err = fmt.Errorf("the diff covers %d files, not one", len(parsed))
		}
		a.notifyError(fmt.Errorf("read the current hunk: %w", err))
		return
	}
	lines := customPatchHunkIdentities(parsed[0], hunk)
	if len(lines) == 0 {
		a.notify("", tr("Nothing to add"), tr("Move the caret into a diff hunk first."))
		return
	}
	a.beginAddCustomPatchFile(commit, file, lines)
}

func (a *app) openCustomPatchBuilder() {
	if !a.customPatchActive() || a.actionBusy || a.customPatch.pending > 0 {
		return
	}
	if len(a.customPatch.entries) == 0 {
		a.notify("", tr("Custom Patch is empty"), tr("Add files, hunks, or changed lines from a commit first."))
		return
	}
	a.customPatch.open = true
}

func (a *app) closeCustomPatchBuilder() {
	if a.actionBusy {
		return
	}
	a.customPatch.open = false
	a.customPatch.previewOpen = false
	a.customPatch.previewLoad = false
	a.customPatch.preview = nil
	a.customPatch.previewError = ""
	a.customPatch.previewToken++
}

func (a *app) backToCustomPatchBuilder() {
	a.customPatch.previewOpen = false
	a.customPatch.previewLoad = false
	a.customPatch.preview = nil
	a.customPatch.previewError = ""
	a.customPatch.previewToken++
}

func (a *app) clearCustomPatch() {
	if a.actionBusy {
		return
	}
	a.customPatch.addToken++
	a.customPatch.pending = 0
	a.customPatch.entries = nil
	a.customPatch.revision++
	a.closeCustomPatchBuilder()
}

func (a *app) removeCustomPatchEntry(index int) {
	if a.actionBusy || index < 0 || index >= len(a.customPatch.entries) {
		return
	}
	a.customPatch.entries = append(a.customPatch.entries[:index], a.customPatch.entries[index+1:]...)
	a.customPatch.revision++
	if len(a.customPatch.entries) == 0 {
		a.closeCustomPatchBuilder()
	}
}

func (a *app) resetCustomPatch() {
	a.customPatch.addToken++
	a.customPatch.open = false
	a.customPatch.entries = nil
	a.customPatch.nextOrder = 0
	a.customPatch.revision++
	a.customPatch.pending = 0
	a.customPatch.previewOpen = false
	a.customPatch.previewLoad = false
	a.customPatch.preview = nil
	a.customPatch.previewError = ""
	a.customPatch.previewToken++
}

func (a *app) beginCustomPatchPreview() {
	if a.session == nil || a.actionBusy || a.customPatch.pending > 0 || len(a.customPatch.entries) == 0 {
		return
	}
	text, err := customPatchText(a.customPatch.entries)
	if err != nil {
		a.customPatch.previewToken++
		a.customPatch.previewOpen = true
		a.customPatch.previewLoad = false
		a.customPatch.preview = nil
		a.customPatch.previewError = safeError(err)
		return
	}
	session := a.session
	generation := a.generation
	a.customPatch.previewToken++
	token := a.customPatch.previewToken
	a.customPatch.previewOpen = true
	a.customPatch.previewLoad = true
	a.customPatch.preview = nil
	a.customPatch.previewError = ""
	go func() {
		defer a.recoverBackground()
		preview, err := session.PreviewPatchText(context.Background(), text)
		a.update(func() {
			if generation != a.generation || token != a.customPatch.previewToken || !a.customPatch.previewOpen {
				return
			}
			a.customPatch.previewLoad = false
			if err != nil {
				a.customPatch.previewError = safeError(err)
				return
			}
			a.customPatch.preview = &preview
		})
	}()
}

func (a *app) beginCustomPatchApply() {
	if a.actionBusy || a.session == nil || a.customPatch.preview == nil || !a.customPatch.preview.Clean {
		return
	}
	preview := *a.customPatch.preview
	a.beginRepositoryActionMessage(tr("Applied custom patch to the working tree"), "",
		func(ctx context.Context, session repository.Session) error {
			return session.ApplyPatch(ctx, preview)
		}, a.clearCustomPatch)
}

func (a *app) exportCustomPatch() {
	if a.customPatch.preview == nil || a.customPatch.previewLoad {
		return
	}
	preview := *a.customPatch.preview
	opts := mygo.SaveDialogOptions{
		Parent:      a.window,
		Title:       tr("Export Custom Patch"),
		DefaultPath: a.patchDialogDefaultPath("custom.patch"),
		ButtonLabel: tr("Export"),
		Filters:     []mygo.FileFilter{{Name: tr("Patch files"), Extensions: []string{"patch", "diff"}}},
	}
	go func() {
		defer a.recoverBackground()
		data, err := preview.Bytes()
		if err != nil {
			a.update(func() { a.notify("error", tr("Could not save the diff"), safeError(err)) })
			return
		}
		a.writePatchBytes(opts, data)
	}()
}

func (a *app) customPatchBar(c *ui.Context) {
	if len(a.customPatch.entries) == 0 || !a.customPatchActive() {
		return
	}
	theme := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(7, 9).
		Background(theme.Accent.Alpha(0.08)).Border(1, theme.Accent.Alpha(0.3)).
		Radius(theme.Radius).Children(func() {
		ui.Icon(c, selectLinesIcon).FontSize(14).TextColor(theme.Accent)
		ui.Text(c, trf("Custom Patch · %d files", len(a.customPatch.entries))).Grow(1)
		if a.customPatch.pending > 0 {
			ui.Spinner(c).Label(tr("Adding to Custom Patch"))
		}
		if ui.Button(c, tr("Review")).Tooltip(tr("Review Custom Patch (B)")).Disabled(a.customPatch.pending > 0).Clicked() {
			a.openCustomPatchBuilder()
		}
		if ui.Button(c, tr("Clear")).Tooltip(tr("Clear every Custom Patch selection without changing the repository")).Clicked() {
			a.clearCustomPatch()
		}
	})
}

func (a *app) customPatchSelectionControls(c *ui.Context) {
	if !a.customPatchLineSelectionAvailable() {
		return
	}
	theme := c.Theme()
	selections := a.currentCustomPatchSelections()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Wrap().Children(func() {
		if a.lineSelectMode {
			ui.Icon(c, selectLinesIcon).FontSize(14).TextColor(theme.Accent)
			ui.Text(c, tr("Selecting Custom Patch lines")).TextColor(theme.TextMuted).Grow(1)
		} else {
			iconButton(c, "Select lines for Custom Patch", "Click line numbers to choose changed lines for Custom Patch",
				selectLinesIcon, false, a.startLineSelection)
		}
		if len(selections) > 0 {
			if ui.PrimaryButton(c, tr("Add selected lines")).Tooltip(tr("Add the selected changed lines to Custom Patch (Space)")).Clicked() {
				a.addCustomPatchSelectedLines()
			}
		} else {
			ui.Text(c, tr("Click changed line numbers to select them")).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).Grow(1)
		}
		if ui.Button(c, tr("Add hunk")).Tooltip(tr("Add the current diff hunk to Custom Patch (A)")).Clicked() {
			a.addCustomPatchCurrentHunk()
		}
		if a.lineSelectMode && ui.Button(c, tr("Cancel")).Clicked() {
			a.stopLineSelection()
		}
	})
}

func (a *app) customPatchModal(c *ui.Context) {
	if !a.customPatch.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(900).Height(690).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).Border(1, theme.Border).
				Radius(theme.Radius + 4).Role(ui.RoleDialog).Label(tr("Custom Patch Builder")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, selectLinesIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Custom Patch Builder")).FontSize(18).Bold()
						ui.Text(c, tr("Collect changes from commits, then apply or export one patch.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close Custom Patch Builder", "Escape", closeIcon, a.actionBusy, a.closeCustomPatchBuilder)
				})
				ui.Divider(c)
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
					if a.customPatch.previewOpen {
						a.customPatchPreviewBody(c)
					} else {
						a.customPatchEntriesBody(c)
					}
				})
				ui.Divider(c)
				if a.customPatch.previewOpen {
					a.customPatchPreviewFooter(c)
				} else {
					a.customPatchEntriesFooter(c)
				}
			})
			if !a.settings.DisableShortcuts {
				if back.OverlayShortcut(0, ui.KeyF1) {
					a.openKeybindings()
				}
				if !c.FocusTakesText() && back.OverlayShortcut(ui.Shift, ui.KeySlash) {
					a.openCurrentKeybindings()
				}
			}
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				if a.customPatch.previewOpen {
					a.backToCustomPatchBuilder()
				} else {
					a.closeCustomPatchBuilder()
				}
			}
		})
	})
}

func (a *app) customPatchEntriesBody(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(9).Children(func() {
		ui.Text(c, tr("Selections keep their original commit patches. The same path cannot be collected from different commits.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		for i, entry := range a.customPatch.entries {
			index := i
			c.Key("custom-patch:" + customPatchEntryKey(entry))
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(9).
				Background(theme.Surface).Radius(theme.Radius).Children(func() {
				ui.Column(c).Gap(3).Grow(1).Children(func() {
					ui.Text(c, entry.file.Path).Bold().Tooltip(entry.file.Path)
					detail := entry.commit.ShortHash + " " + entry.commit.Subject
					if entry.file.PreviousPath != "" && entry.file.PreviousPath != entry.file.Path {
						detail = entry.file.PreviousPath + " → " + entry.file.Path + " · " + detail
					}
					ui.Text(c, detail).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).SingleLine().Tooltip(detail)
				})
				if entry.whole {
					ui.Badge(c, tr("Whole file"))
				} else {
					ui.Badge(c, tr("Partial"))
				}
				iconButton(c, "Remove from Custom Patch", "Remove this file selection", trashIcon, a.actionBusy, func() {
					a.removeCustomPatchEntry(index)
				})
			})
		}
	})
}

func (a *app) customPatchEntriesFooter(c *ui.Context) {
	ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
		if ui.Button(c, tr("Clear Builder")).Disabled(a.actionBusy).Clicked() {
			a.clearCustomPatch()
		}
		if ui.Button(c, tr("Send to worktree…")).Disabled(a.actionBusy || len(a.customPatch.entries) == 0 || len(courierDestinations(a.snapshot)) == 0).Clicked() {
			a.beginCustomPatchCourier()
		}
		if ui.PrimaryButton(c, tr("Preview Custom Patch")).Disabled(a.actionBusy || len(a.customPatch.entries) == 0).Clicked() {
			a.beginCustomPatchPreview()
		}
	})
}

func (a *app) customPatchPreviewBody(c *ui.Context) {
	theme := c.Theme()
	if a.customPatch.previewLoad {
		ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Children(func() {
			ui.Spinner(c).Label(tr("Checking Custom Patch"))
			ui.Text(c, tr("Checking Custom Patch without changing the repository…")).Bold()
		})
		return
	}
	if a.customPatch.previewError != "" {
		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, tr("Could not preview Custom Patch")).Bold().TextColor(theme.Danger)
			ui.Text(c, a.customPatch.previewError).Selectable()
		})
		return
	}
	preview := a.customPatch.preview
	if preview == nil {
		return
	}
	statusColor := theme.Success
	status := tr("Custom Patch applies cleanly")
	if !preview.Clean {
		statusColor = theme.Danger
		status = tr("Custom Patch cannot be applied")
	}
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c).Gap(5).Padding(10).Background(statusColor.Alpha(0.1)).
			Border(1, statusColor.Alpha(0.45)).Radius(theme.Radius).Children(func() {
			ui.Text(c, status).Bold().TextColor(statusColor)
			ui.Text(c, tr("Apply changes the working tree only; nothing will be staged."))
			ui.Text(c, trf("%d files · +%d · −%d", len(preview.Files), preview.Additions, preview.Deletions)).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		if preview.Diagnostic != "" {
			ui.Text(c, preview.Diagnostic).Selectable().TextColor(theme.Danger)
		}
		ui.Text(c, tr("Patch text")).Bold()
		ui.Text(c, preview.Text).Selectable().FontSize(theme.FontSize - 1)
		if preview.TextTruncated {
			ui.Text(c, tr("Patch text is truncated here; Apply and Export still use the complete patch.")).
				TextColor(theme.TextMuted)
		}
	})
}

func (a *app) customPatchPreviewFooter(c *ui.Context) {
	ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
		if ui.Button(c, tr("Back to Builder")).Disabled(a.actionBusy).Clicked() {
			a.backToCustomPatchBuilder()
		}
		if ui.Button(c, tr("Send to worktree…")).Disabled(a.actionBusy || a.customPatch.previewLoad || a.customPatch.preview == nil || len(courierDestinations(a.snapshot)) == 0).Clicked() {
			a.beginCustomPatchPreviewCourier()
		}
		if ui.Button(c, tr("Export .patch…")).Disabled(a.actionBusy || a.customPatch.previewLoad || a.customPatch.preview == nil).Clicked() {
			a.exportCustomPatch()
		}
		if a.customPatch.preview != nil && a.customPatch.preview.Clean {
			if ui.PrimaryButton(c, tr("Apply Custom Patch")).Disabled(a.actionBusy || a.customPatch.previewLoad).Clicked() {
				a.beginCustomPatchApply()
			}
		}
	})
}
