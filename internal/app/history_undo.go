package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// confirmReflogReplay asks before moving HEAD because both undo and redo can
// rewrite the checked-out branch. Acceptance re-reads the complete reflog and
// proceeds only if it still selects the exact action approved here.
func (a *app) confirmReflogReplay(redo bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	identity, _ := repository.ReflogReplayIdentity(a.snapshot.Reflog, redo)
	title := "Undo Git Operation"
	message := tr("Undo the latest eligible Git operation?")
	if description, ok := repository.ReflogReplayDescription(a.snapshot.Reflog, a.snapshot.Operations, false); ok {
		message = trf("Undo %s?", description)
	}
	detail := tr("TanGit finds the latest commit, reset, pull, checkout, or completed rebase in the reflog and reverses it. Tracked work is auto-stashed around hard resets. Finish or abort any operation already in progress first.")
	confirmLabel := "Undo"
	success := tr("Git operation undone")
	if redo {
		title = "Redo Git Operation"
		message = tr("Redo the latest undone Git operation?")
		if description, ok := repository.ReflogReplayDescription(a.snapshot.Reflog, a.snapshot.Operations, true); ok {
			message = trf("Redo %s?", description)
		}
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
						return session.RedoExpected(ctx, identity)
					}
					return session.UndoExpected(ctx, identity)
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
