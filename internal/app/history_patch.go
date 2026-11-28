package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// customPatchHistoryState owns only the review form. The authoritative history
// operation is repository.HistoryPatchState in the durable journal.
type customPatchHistoryState struct {
	open        bool
	kind        repository.HistoryPatchKind
	summary     string
	description string
}

func historyPatchKindLabel(kind repository.HistoryPatchKind) string {
	switch kind {
	case repository.HistoryPatchRemove:
		return tr("Remove from original commit")
	case repository.HistoryPatchMoveIndex:
		return tr("Move out into index")
	case repository.HistoryPatchMoveCommit:
		return tr("Move to selected commit")
	case repository.HistoryPatchNewBefore:
		return tr("New commit before original")
	case repository.HistoryPatchNewAfter:
		return tr("New commit after original")
	default:
		return tr("Custom Patch history action")
	}
}

func (a *app) customPatchSourceCommit() (repository.Commit, error) {
	if len(a.customPatch.entries) == 0 {
		return repository.Commit{}, fmt.Errorf("Custom Patch is empty")
	}
	source := a.customPatch.entries[0].commit
	for _, entry := range a.customPatch.entries[1:] {
		if entry.commit.Hash != source.Hash {
			return repository.Commit{}, fmt.Errorf("history edits require one source commit; apply different sources separately instead of mixing them into one commit")
		}
	}
	if len(source.Parents) > 1 {
		return repository.Commit{}, fmt.Errorf("merge commits cannot be a Custom Patch source")
	}
	return source, nil
}

func (a *app) customPatchHistoryDisabled(kind repository.HistoryPatchKind) string {
	if a.actionBusy {
		return tr("Another repository action is in progress")
	}
	if a.snapshot.Operation != "" || a.snapshot.OperationState != nil {
		return tr("Finish or abort the active Git operation first")
	}
	source, err := a.customPatchSourceCommit()
	if err != nil {
		return err.Error()
	}
	if _, ok := a.session.(repository.HistoryPatchSession); !ok {
		return tr("This session does not support resumable Custom Patch history edits")
	}
	if len(a.customPatch.entries) == 0 {
		return tr("Add at least one changed file, hunk, or line first")
	}
	if kind == repository.HistoryPatchNewBefore && len(source.Parents) == 0 {
		return tr("New-before a root commit is not supported")
	}
	if kind == repository.HistoryPatchMoveCommit {
		destination, ok := a.selectedVisibleCommit()
		if !ok {
			return tr("Select a destination commit in the current history first")
		}
		if destination.Hash == source.Hash {
			return tr("Select a destination other than the source commit")
		}
		if len(destination.Parents) > 1 {
			return tr("Merge commits cannot be a Custom Patch destination")
		}
	}
	return ""
}

func (a *app) customPatchHistoryMenu(m *ui.Menu) {
	source, sourceErr := a.customPatchSourceCommit()
	items := []struct {
		kind repository.HistoryPatchKind
		show bool
	}{
		{repository.HistoryPatchRemove, true},
		{repository.HistoryPatchMoveIndex, true},
		{repository.HistoryPatchMoveCommit, sourceErr == nil && func() bool {
			destination, ok := a.selectedVisibleCommit()
			return ok && destination.Hash != source.Hash
		}()},
		{repository.HistoryPatchNewBefore, true},
		{repository.HistoryPatchNewAfter, true},
	}
	m.Item(tr("History rewrite · one source commit")).Disabled(true)
	for _, item := range items {
		if !item.show {
			continue
		}
		kind := item.kind
		reason := a.customPatchHistoryDisabled(kind)
		label := historyPatchKindLabel(kind)
		if reason == "" && sourceErr == nil && kind == repository.HistoryPatchMoveCommit {
			if destination, ok := a.selectedVisibleCommit(); ok {
				label += " " + destination.ShortHash
			}
		}
		item := m.Item(label).Disabled(reason != "")
		if reason != "" {
			item.Tooltip(tr("Disabled") + ": " + reason)
		} else {
			item.Tooltip(tr("Review the exact source, destination and patch before rewriting history"))
		}
		if item.Chosen() {
			a.openCustomPatchHistoryReview(kind)
		}
	}
}

func (a *app) openCustomPatchHistoryReview(kind repository.HistoryPatchKind) {
	if reason := a.customPatchHistoryDisabled(kind); reason != "" {
		a.notify("", tr("Custom Patch history action unavailable"), reason)
		return
	}
	source, err := a.customPatchSourceCommit()
	if err != nil {
		a.notifyError(err)
		return
	}
	a.customPatch.history = customPatchHistoryState{open: true, kind: kind}
	if kind == repository.HistoryPatchNewBefore || kind == repository.HistoryPatchNewAfter {
		a.customPatch.history.summary = "Custom patch from " + source.ShortHash
	}
}

func (a *app) closeCustomPatchHistoryReview() {
	if a.actionBusy {
		return
	}
	a.customPatch.history.open = false
	a.customPatch.history.description = ""
}

func (a *app) beginCustomPatchHistoryApply() {
	if a.actionBusy || a.session == nil {
		return
	}
	history := a.customPatch.history
	if reason := a.customPatchHistoryDisabled(history.kind); reason != "" {
		a.notify("", tr("Custom Patch history action unavailable"), reason)
		return
	}
	source, err := a.customPatchSourceCommit()
	if err != nil {
		a.notifyError(err)
		return
	}
	text, err := customPatchText(a.customPatch.entries)
	if err != nil {
		a.notifyError(err)
		return
	}
	request := repository.HistoryPatchRequest{
		Kind: history.kind, Patch: text, SourceSHA: source.Hash,
		Summary: strings.TrimSpace(history.summary), Description: history.description,
		ExpectedHeadSHA: a.snapshot.HeadHash, ExpectedHeadRef: "refs/heads/" + a.snapshot.Branch,
	}
	if history.kind == repository.HistoryPatchMoveCommit {
		destination, ok := a.selectedVisibleCommit()
		if !ok {
			a.notify("", tr("Custom Patch history action unavailable"), tr("Select a destination commit first"))
			return
		}
		request.DestinationSHA = destination.Hash
	}
	historyPatches, ok := a.session.(repository.HistoryPatchSession)
	if !ok {
		return
	}
	// Close the proposal as Git starts. Conflicts are then owned by the live
	// Operation card, and a successful continued operation clears the basket.
	a.customPatch.history.open = false
	a.beginRepositoryActionMessage(historyPatchKindLabel(history.kind), "", func(ctx context.Context, _ repository.Session) error {
		return historyPatches.ApplyHistoryPatch(ctx, request)
	}, a.clearCustomPatch)
}

func (a *app) customPatchHistoryModal(c *ui.Context) {
	if !a.customPatch.open || !a.customPatch.history.open {
		return
	}
	theme := c.Theme()
	history := a.customPatch.history
	source, sourceErr := a.customPatchSourceCommit()
	destination := repository.Commit{}
	if history.kind == repository.HistoryPatchMoveCommit {
		destination, _ = a.selectedVisibleCommit()
	}
	text, textErr := customPatchText(a.customPatch.entries)
	reason := a.customPatchHistoryDisabled(history.kind)
	if sourceErr != nil {
		reason = sourceErr.Error()
	}
	if textErr != nil {
		reason = textErr.Error()
	}
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.45)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(840).Height(650).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(12).Background(theme.Background).Border(1, theme.Border).
				Radius(theme.Radius + 4).Role(ui.RoleDialog).Label(tr("Review Custom Patch history edit"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Column(c).Grow(1).Gap(3).Children(func() {
						ui.Text(c, historyPatchKindLabel(history.kind)).FontSize(18).Bold()
						ui.Text(c, tr("Persistent replay · full hashes and patch digest are journaled before Git starts")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close Custom Patch history review", "Escape", closeIcon, a.actionBusy, a.closeCustomPatchHistoryReview)
				})
				ui.Divider(c)
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(9).Children(func() {
						ui.Text(c, tr("Source")+": "+source.Hash).Selectable()
						ui.Text(c, source.Subject).TextColor(theme.TextMuted)
						if destination.Hash != "" {
							ui.Text(c, tr("Destination")+": "+destination.Hash).Selectable()
							ui.Text(c, destination.Subject).TextColor(theme.TextMuted)
						}
						ui.Text(c, tr("Expected HEAD")+": "+a.snapshot.HeadHash+" (refs/heads/"+a.snapshot.Branch+")").Selectable()
						ui.Text(c, tr("Patch SHA-256")+": "+fmt.Sprintf("%x", sha256Summary(text))).TextColor(theme.TextMuted)
						if history.kind == repository.HistoryPatchNewBefore || history.kind == repository.HistoryPatchNewAfter {
							ui.TextInput(c, &a.customPatch.history.summary).Label(tr("New commit summary")).Disabled(a.actionBusy)
							ui.TextArea(c, &a.customPatch.history.description).Label(tr("Description")).Height(90).Disabled(a.actionBusy)
						}
						if reason != "" {
							ui.Text(c, tr("Disabled")+": "+reason).TextColor(theme.Danger).Selectable()
						}
						ui.Text(c, tr("Patch preview")).Bold()
						ui.Text(c, text).Selectable().FontSize(theme.FontSize - 1)
					})
				})
				ui.Divider(c)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
						a.closeCustomPatchHistoryReview()
					}
					if ui.PrimaryButton(c, tr("Rewrite history")).Disabled(a.actionBusy || reason != "" || (history.kind == repository.HistoryPatchNewBefore || history.kind == repository.HistoryPatchNewAfter) && strings.TrimSpace(a.customPatch.history.summary) == "").Clicked() {
						a.beginCustomPatchHistoryApply()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCustomPatchHistoryReview()
			}
		})
	})
}

// sha256Summary is kept tiny so the UI package does not expose the journal's
// byte storage. It returns a byte slice suitable for the existing %x formatter.
func sha256Summary(text string) []byte {
	digest := sha256.Sum256([]byte(text))
	return digest[:]
}
