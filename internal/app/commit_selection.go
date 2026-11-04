package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (a *app) selectedCommitsForBatch() []repository.Commit {
	if !a.commitSelectionMode {
		return nil
	}
	var selected []repository.Commit
	for _, commit := range a.visibleCommits() {
		if a.commitSelection[commit.Hash] {
			selected = append(selected, commit)
		}
	}
	return selected
}

// commitSelectionModeButton reveals the commit checkboxes only while batch
// selection is wanted. Leaving the mode also clears its selection, so hidden
// commits can never be acted on accidentally.
func (a *app) commitSelectionModeButton(c *ui.Context) {
	theme := c.Theme()
	button := ui.Button(c, "").Padding(2, 3)
	clicked := button.Label(tr("Select multiple commits")).Tooltip(tr("Show or hide commit selection checkboxes")).Disabled(a.actionBusy).Clicked()
	button.Children(func() {
		icon := ui.Icon(c, selectCommitsIcon).FontSize(13)
		if a.commitSelectionMode {
			icon.TextColor(theme.Accent)
		}
	})
	if a.commitSelectionMode {
		button.Background(theme.Accent.Alpha(0.14))
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if clicked {
		a.commitSelectionMode = !a.commitSelectionMode
		if !a.commitSelectionMode {
			a.commitSelection = nil
		}
	}
}

func (a *app) commitSelectionCheckbox(c *ui.Context, commit repository.Commit) {
	if !a.commitSelectionMode {
		return
	}
	selected := a.commitSelection[commit.Hash]
	c.Key("batch:" + commit.Hash)
	ui.Checkbox(c, &selected, "").Label(tr("Select commit for batch actions")).Disabled(a.actionBusy).OnClick(func() {
		a.toggleCommitForBatch(commit)
	})
}

// toggleCommitForBatch also enters selection mode for the context menu and
// Ctrl+Space, keeping those alternatives usable without the toolbar button.
func (a *app) toggleCommitForBatch(commit repository.Commit) {
	a.commitSelectionMode = true
	if a.commitSelection == nil {
		a.commitSelection = map[string]bool{}
	}
	a.commitSelection[commit.Hash] = !a.commitSelection[commit.Hash]
}

func (a *app) commitSelectionBar(c *ui.Context) {
	selected := a.selectedCommitsForBatch()
	if len(selected) == 0 {
		return
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, trf("%d commits selected", len(selected))).SingleLine()
		ui.Row(c).Gap(8).Children(func() {
			ui.PopoverMenuButton(c, tr("Batch actions"), func(m *ui.Menu) { a.commitBatchMenu(m) }).Disabled(a.actionBusy)
			ui.Button(c, tr("Clear selection")).Disabled(a.actionBusy).OnClick(func() { a.commitSelection = nil })
		})
	})
}

func (a *app) commitBatchMenu(m *ui.Menu) {
	selected := a.selectedCommitsForBatch()
	if len(selected) == 0 {
		return
	}
	if m.Item(tr("Cherry-pick selected commits…")).Disabled(a.actionBusy).Chosen() {
		a.confirmBatchCommits("", true)
	}
	for _, entry := range []struct {
		label  string
		action repository.CommitRangeAction
	}{
		{"Squash selected commits…", repository.RangeSquash},
		{"Fixup selected commits…", repository.RangeFixup},
		{"Move selected commits toward HEAD…", repository.RangeMoveNewer},
		{"Move selected commits toward older history…", repository.RangeMoveOlder},
		{"Drop selected commits…", repository.RangeDrop},
	} {
		if m.Item(tr(entry.label)).Disabled(a.actionBusy || len(selected) < 2).Chosen() {
			a.confirmBatchCommits(entry.action, false)
		}
	}
}

func (a *app) confirmBatchCommits(action repository.CommitRangeAction, cherry bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	selected := a.selectedCommitsForBatch()
	if len(selected) == 0 {
		return
	}
	lines := make([]string, 0, len(selected))
	for _, commit := range selected {
		lines = append(lines, commit.ShortHash+" "+commit.Subject)
	}
	label := "Cherry-pick selected commits…"
	if !cherry {
		label = map[repository.CommitRangeAction]string{
			repository.RangeSquash:    "Squash selected commits…",
			repository.RangeFixup:     "Fixup selected commits…",
			repository.RangeDrop:      "Drop selected commits…",
			repository.RangeMoveNewer: "Move selected commits toward HEAD…",
			repository.RangeMoveOlder: "Move selected commits toward older history…",
		}[action]
	}
	detail := tr("Rewriting requires consecutive commits on the current branch. Squash and fixup keep the oldest selected commit; fixup keeps only its message. Later commits receive new hashes.")
	if cherry {
		detail = tr("Commits are applied oldest first. If a conflict occurs, resolve it and continue, or abort the operation.")
	}
	a.askConfirm(confirmRequest{
		title: "Batch Commit Action", message: trf("Apply this action to %d selected commits?", len(selected)),
		detail:       tr(label) + "\n" + strings.Join(lines, "\n") + "\n\n" + detail,
		confirmLabel: "Apply", destructive: !cherry,
		run: func() {
			a.beginRepositoryActionMessage(tr("Applied batch commit action"), "", func(ctx context.Context, s repository.Session) error {
				if cherry {
					return s.CherryPickCommits(ctx, selected)
				}
				return s.EditCommitRange(ctx, selected, action)
			}, func() { a.commitSelection = nil })
		},
	})
}

func (a *app) confirmForcePushWithLease() {
	if a.actionBusy || a.session == nil || a.snapshot.Branch == "" {
		return
	}
	branch := a.snapshot.Branch
	a.askConfirm(confirmRequest{
		title: "Force Push with Lease", message: trf("Force-push %s with a lease?", branch),
		detail:       tr("This replaces published branch history. Git refuses if the remote branch differs from your remote-tracking reference. Review fetched changes before confirming."),
		confirmLabel: "Force push with lease", destructive: true,
		run: func() {
			a.beginRepositoryActionMessage(tr("Force-pushed the branch with a lease"), "", func(ctx context.Context, s repository.Session) error { return s.ForcePushWithLease(ctx) }, nil)
		},
	})
}

func (a *app) toggleSelectedCommitForBatch() {
	if a.actionBusy {
		return
	}
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.toggleCommitForBatch(commit)
	}
}
