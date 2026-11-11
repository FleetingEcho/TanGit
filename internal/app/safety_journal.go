package app

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// notifyRepositorySuccess attaches an immutable operation ID to immediate
// Undo. Later clicks resolve that ID in the latest snapshot and revalidate the
// repository in UndoOperation, never replaying an arbitrary reflog entry.
func (a *app) notifyRepositorySuccess(message string, previous map[string]bool) {
	if message == "" {
		return
	}
	for _, record := range a.snapshot.Operations {
		if previous[record.ID] || !record.CanUndo() {
			continue
		}
		id := record.ID
		a.pendingToasts = append(a.pendingToasts, appToast{
			kind: "success", title: tr(message), action: tr("Undo"), timeout: 8 * time.Second,
			onAction: func() { a.confirmUndoOperationID(id) },
		})
		return
	}
	a.notify("success", message, "")
}

func (a *app) confirmUndoOperationID(id string) {
	for _, record := range a.snapshot.Operations {
		if record.ID == id {
			a.confirmUndoOperation(record)
			return
		}
	}
	a.notify("", tr("Undo is unavailable"), tr("The safety journal entry is no longer available in this repository."))
}

func (a *app) confirmUndoOperation(record repository.OperationRecord) {
	if a.actionBusy || a.session == nil || !record.CanUndo() {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Undo Repository Operation",
		message:      trf("Undo %s?", record.Summary),
		detail:       tr("TanGit will first verify that HEAD, the branch and the working tree still match the operation. Newer work stops Undo before anything is overwritten."),
		confirmLabel: "Undo",
		run: func() {
			id := record.ID
			a.beginRepositoryActionMessage(trf("Undid %s", record.Summary), "", func(ctx context.Context, session repository.Session) error {
				return session.UndoOperation(ctx, id)
			}, nil)
		},
	})
}

func operationStatusLabel(record repository.OperationRecord) string {
	if record.UndoneAt != 0 {
		return tr("Undone")
	}
	switch record.Status {
	case repository.OperationSucceeded:
		if record.CanUndo() {
			return tr("Recoverable")
		}
		return tr("Completed")
	case repository.OperationInterrupted, repository.OperationPending:
		return tr("Interrupted")
	case repository.OperationFailed:
		return tr("Failed")
	default:
		return record.Status
	}
}

func operationHeadDetail(record repository.OperationRecord) string {
	const short = 7
	before, after := record.HeadBefore, record.HeadAfter
	if len(before) > short {
		before = before[:short]
	}
	if len(after) > short {
		after = after[:short]
	}
	if before == "" && after == "" {
		return ""
	}
	if before == "" {
		return "→ " + after
	}
	if after == "" || before == after {
		return before
	}
	return before + " → " + after
}

func (a *app) safetyJournalBody(c *ui.Context) {
	theme := c.Theme()
	if len(a.snapshot.Operations) == 0 {
		ui.Text(c, tr("Discards, resets, rebases, branch deletions, dropped stashes and patch applications appear here. Safe recovery is revalidated before every Undo.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		return
	}
	height := float32(min(len(a.snapshot.Operations), 6)) * 62
	ui.Scroll(c).Height(height).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(5).Children(func() {
			for _, item := range a.snapshot.Operations {
				record := item
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(9).Padding(7, 8).
					Background(theme.Surface).Radius(theme.Radius).Children(func() {
					ui.Column(c).Gap(3).Grow(1).Children(func() {
						ui.Text(c, record.Summary).SingleLine().Tooltip(record.Summary)
						detail := time.Unix(record.CreatedAt, 0).Format("2006-01-02 15:04")
						if heads := operationHeadDetail(record); heads != "" {
							detail += " · " + heads
						}
						ui.Text(c, detail).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).SingleLine()
						if record.Error != "" {
							ui.Text(c, record.Error).TextColor(theme.Danger).FontSize(theme.FontSize - 2).SingleLine().Tooltip(record.Error)
						}
					})
					ui.Badge(c, operationStatusLabel(record))
					if record.Snapshot != nil {
						ui.Badge(c, tr("Saved work")).Tooltip(tr("A recovery copy is also listed under Recovery & checkpoints."))
					}
					if record.CanUndo() {
						if compactTextButton(ui.Button(c, tr("Undo…"))).Label(tr("Undo") + " " + record.ID).
							Tooltip(tr("Recheck the repository, then undo this exact operation")).Disabled(a.actionBusy).Clicked() {
							a.confirmUndoOperation(record)
						}
					}
				})
			}
		})
	})
	ui.Text(c, fmt.Sprintf(tr("%d journal entries"), len(a.snapshot.Operations))).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 2)
}
