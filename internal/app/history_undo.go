package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// confirmReflogReplay asks before moving HEAD because both undo and redo can
// rewrite the checked-out branch. The repository determines the exact target
// from a fresh reflog after this confirmation, so it cannot go stale while the
// dialog is open.
func (a *app) confirmReflogReplay(redo bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	title := "Undo Git Operation"
	message := tr("Undo the latest eligible Git operation?")
	detail := tr("TanGit finds the latest commit, reset, pull, checkout, or completed rebase in the reflog and reverses it. Tracked work is auto-stashed around hard resets. Finish or abort any operation already in progress first.")
	confirmLabel := "Undo"
	success := tr("Git operation undone")
	if redo {
		title = "Redo Git Operation"
		message = tr("Redo the latest undone Git operation?")
		detail = tr("TanGit reapplies the latest action reversed by Undo. Tracked work is auto-stashed around hard resets. Finish or abort any operation already in progress first.")
		confirmLabel = "Redo"
		success = tr("Git operation redone")
	}
	a.askConfirm(confirmRequest{
		title:        title,
		message:      message,
		detail:       detail,
		confirmLabel: confirmLabel,
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(success, "",
				func(ctx context.Context, session repository.Session) error {
					if redo {
						return session.Redo(ctx)
					}
					return session.Undo(ctx)
				}, nil)
		},
	})
}

func (a *app) confirmUndo() {
	a.confirmReflogReplay(false)
}

func (a *app) confirmRedo() {
	a.confirmReflogReplay(true)
}
