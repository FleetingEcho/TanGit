package app

import (
	"context"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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
	detail := tr("lazygitGo finds the latest commit, reset, pull, checkout, or completed rebase in the reflog and reverses it. Tracked work is auto-stashed around hard resets. Finish or abort any operation already in progress first.")
	confirmLabel := "Undo"
	success := tr("Git operation undone")
	if redo {
		title = "Redo Git Operation"
		message = tr("Redo the latest undone Git operation?")
		if description, ok := repository.ReflogReplayDescription(a.snapshot.Reflog, a.snapshot.Operations, true); ok {
			message = trf("Redo %s?", description)
		}
		detail = tr("lazygitGo reapplies the latest action reversed by Undo. Tracked work is auto-stashed around hard resets. Finish or abort any operation already in progress first.")
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
						return requireHistorySession(session).RedoExpected(ctx, identity)
					}
					return requireHistorySession(session).UndoExpected(ctx, identity)
				}, nil)
		},
	})
}

// latestUndoableRecord is the newest recorded change Undo can still reverse:
// not undone yet, undoable, and made on the HEAD that is checked out now. A
// record from an older HEAD is stale (a commit, pull or checkout came after
// it), and the reflog's own latest action is the right one to offer instead.
func (a *app) latestUndoableRecord() (repository.OperationRecord, bool) {
	for _, record := range a.snapshot.Operations {
		if record.UndoneAt != 0 || !record.CanUndo() {
			continue
		}
		if record.HeadAfter != "" && record.HeadAfter != a.snapshot.HeadHash {
			continue
		}
		return record, true
	}
	return repository.OperationRecord{}, false
}

// latestConflictRecovery is the newest conflict resolution that can still be
// put back as unresolved. It only counts while the operation that raised the
// conflict is running: afterwards the conflict is history, not a pending choice.
func (a *app) latestConflictRecovery() (repository.FileRecoveryRecord, bool) {
	if a.snapshot.OperationState == nil {
		return repository.FileRecoveryRecord{}, false
	}
	var latest repository.FileRecoveryRecord
	found := false
	for _, record := range a.snapshot.FileRecoveries {
		if !fileRecoveryCanRestore(record) || !fileRecoveryHasUnresolved(record) {
			continue
		}
		if !found || record.CreatedAt > latest.CreatedAt {
			latest, found = record, true
		}
	}
	return latest, found
}

// confirmUndo reverses the latest change and always says which one: staging,
// discarding, resolving a conflict, committing, or a finished operation. The
// journal knows each exactly; only a change it did not record (a pull or
// checkout) falls back to Git's reflog. With nothing to reverse it says so
// instead of asking an open-ended question.
func (a *app) confirmUndo() {
	if a.actionBusy || a.session == nil {
		return
	}
	// The list on screen can trail the change that was just made: its details
	// load after the action. Deciding from it could offer an older change, or
	// none, so the journal is read again first.
	session := a.session
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		operations, err := session.Operations(ctx)
		a.update(func() {
			if generation != a.generation || a.actionBusy {
				return
			}
			if err == nil && len(operations) > 0 {
				a.snapshot.Operations = operations
			}
			a.offerUndo()
		})
	}()
}

// undoHint names what Ctrl+Z would reverse right now, using the same choice
// offerUndo makes, or reports that nothing can be undone.
func (a *app) undoHint() (string, bool) {
	record, haveRecord := a.latestUndoableRecord()
	if recovery, ok := a.latestConflictRecovery(); ok && (!haveRecord || recovery.CreatedAt >= record.CreatedAt) {
		return trf("Undo %s", recovery.Path), true
	}
	if haveRecord {
		return trf("Undo %s", record.Summary), true
	}
	if _, ok := repository.ReflogReplayIdentity(a.snapshot.Reflog, false); ok {
		if description, ok := repository.ReflogReplayDescription(a.snapshot.Reflog, a.snapshot.Operations, false); ok {
			return trf("Undo %s", description), true
		}
	}
	return "", false
}

// offerUndo asks to reverse the latest change, from the journal as the app
// holds it now.
func (a *app) offerUndo() {
	if a.actionBusy || a.session == nil {
		return
	}
	record, haveRecord := a.latestUndoableRecord()
	// Resolving a conflict keeps its own durable record of the unresolved file.
	// While the operation runs, the newer of the two is the latest change.
	if recovery, ok := a.latestConflictRecovery(); ok && (!haveRecord || recovery.CreatedAt >= record.CreatedAt) {
		a.confirmRestoreFileRecovery(recovery)
		return
	}
	if haveRecord {
		a.confirmUndoOperation(record)
		return
	}
	if _, ok := repository.ReflogReplayIdentity(a.snapshot.Reflog, false); ok {
		a.confirmReflogReplay(false)
		return
	}
	detail := tr("No recent change can be undone safely. Stage, discard, commit and conflict actions are remembered here.")
	if !repository.SafetyJournalEnabled() {
		detail = tr("The safety journal is off, so staging and discarding are not recorded. Turn it on in Settings to use Undo for them.")
	}
	a.notify("", tr("Nothing to undo"), detail)
}

func (a *app) confirmRedo() {
	a.confirmReflogReplay(true)
}
