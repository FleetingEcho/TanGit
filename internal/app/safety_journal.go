package app

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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
		detail:       tr("lazygitGo will first verify that HEAD, the branch and the working tree still match the operation. Newer work stops Undo before anything is overwritten."),
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
	if record.Status == repository.OperationUndoing {
		return tr("Undoing")
	}
	if record.UndoStartedAt != 0 {
		return tr("Undo interrupted")
	}
	if record.ActiveOperation != repository.OperationTypeNone || record.RebasePaused {
		return tr("In progress")
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

// operationScopeDetail keeps journal provenance visible when several tabs or
// linked worktrees share a journal. Older records without a branch still show
// whichever immutable scope they captured.
func operationScopeDetail(record repository.OperationRecord) string {
	branch := record.Branch
	if record.BranchAfter != "" && record.BranchAfter != branch {
		if branch == "" {
			branch = record.BranchAfter
		} else {
			branch += " → " + record.BranchAfter
		}
	}
	if branch != "" && record.Worktree != "" {
		return branch + " · " + record.Worktree
	}
	if branch != "" {
		return branch
	}
	return record.Worktree
}

// emptyJournalState is the centered note of a list with nothing in it.
func emptyJournalState(c *ui.Context, theme *ui.Theme, title, hint string) {
	ui.Column(c).FillWidth().Center().Gap(8).Padding(34, 20).Children(func() {
		ui.Icon(c, softResetIcon).FontSize(26).TextColor(theme.TextMuted)
		ui.Text(c, tr(title)).Bold()
		if hint != "" {
			ui.Text(c, tr(hint)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
				TextAlign(ui.Center).MaxWidth(440)
		}
	})
}

// safetyJournalBody lists what the journal recorded as cards: a dot for what
// the entry means, the action and when it ran, and an Undo where one is safe.
func (a *app) safetyJournalBody(c *ui.Context) {
	theme := c.Theme()
	reveal := a.journalReveal
	a.journalReveal = ""
	if a.journalNotice != "" {
		ui.Text(c, a.journalNotice).TextColor(theme.Warning).Selectable()
	}
	if len(a.snapshot.Operations) == 0 {
		if !a.safetyJournalEnabled() {
			emptyJournalState(c, theme, "Nothing recorded yet", "Nothing is recorded while the safety journal is off.")
			return
		}
		emptyJournalState(c, theme, "Nothing recorded yet",
			"Discards, resets, rebases, branch deletions, dropped stashes and patch applications appear here. Safe recovery is revalidated before every Undo.")
		return
	}
	for _, item := range a.snapshot.Operations {
		record := item
		c.Key("journal-entry:" + record.ID)
		targeted := reveal != "" && record.ID == reveal
		row := ui.Row(c).FillWidth().MinHeight(58).AlignItems(ui.Center).Gap(12).Padding(9, 12).
			Background(theme.Surface).Radius(theme.Radius)
		if targeted {
			row = row.Border(2, theme.Accent).
				ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
		}
		row.Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(journalStatusColor(theme, record))
			ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, record.Summary).SingleLine().Ellipsis("…").Tooltip(record.Summary)
				detail := time.Unix(record.CreatedAt, 0).Format("2006-01-02 15:04")
				if scope := a.journalScopeDetail(record); scope != "" {
					detail += " · " + scope
				}
				if heads := operationHeadDetail(record); heads != "" {
					detail += " · " + heads
				}
				ui.Text(c, detail).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).
					SingleLine().Ellipsis("…").Tooltip(detail)
				if record.Error != "" {
					ui.Text(c, record.Error).TextColor(theme.Danger).FontSize(theme.FontSize - 2).
						SingleLine().Ellipsis("…").Tooltip(record.Error)
				}
			})
			tooltip := tr("A recovery copy is also listed under Recovery & checkpoints.")
			if record.Snapshot == nil {
				tooltip = operationStatusLabel(record)
			} else if record.UndoStartedAt != 0 && record.AfterSnapshot != nil {
				tooltip = tr("Both before and after recovery points remain available for review.")
			}
			ui.Badge(c, operationStatusLabel(record)).Tooltip(tooltip)
			if record.CanUndo() {
				iconButton(c, tr("Undo")+" "+record.ID, "Recheck the repository, then undo this exact operation", undoIcon, a.actionBusy, func() {
					a.confirmUndoOperation(record)
				})
			}
		})
	}
	ui.Text(c, fmt.Sprintf(tr("%d journal entries"), len(a.snapshot.Operations))).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 2)
}

// journalScopeDetail is where an entry ran. The checkout's path is shown only
// when it is not the repository on screen, since it repeats on every row of
// the usual single-repository list.
func (a *app) journalScopeDetail(record repository.OperationRecord) string {
	if record.Worktree == a.snapshot.Root {
		record.Worktree = ""
	}
	return operationScopeDetail(record)
}
