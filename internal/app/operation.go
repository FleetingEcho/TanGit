package app

import (
	"context"
	"fmt"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func snapshotOperationFingerprint(snapshot repository.Snapshot) string {
	if snapshot.OperationState == nil {
		return ""
	}
	return snapshot.OperationState.Fingerprint
}

// driveOperation uses the optional CAS driver when available. The fallback is
// deliberately limited to legacy snapshots: a rich fingerprinted confirmation
// must never silently degrade into an unchecked legacy Continue or Abort.
func driveOperation(ctx context.Context, session repository.Session, action repository.OperationAction, expectedFingerprint string) error {
	if operations, ok := session.(repository.OperationSession); ok {
		return operations.DriveOperation(ctx, repository.OperationDriveOptions{
			Action: action, ExpectedFingerprint: expectedFingerprint,
		})
	}
	if expectedFingerprint != "" {
		return fmt.Errorf("%w: session cannot validate the operation fingerprint", repository.ErrOperationUnsupported)
	}
	switch action {
	case repository.OperationActionContinue:
		return session.ContinueOperation(ctx)
	case repository.OperationActionAbort:
		return session.AbortOperation(ctx)
	default:
		return fmt.Errorf("%w: legacy sessions cannot %s an operation", repository.ErrOperationUnsupported, action)
	}
}

func (a *app) operationCan(action repository.OperationAction) bool {
	if a.snapshot.Operation == "" {
		return false
	}
	if state := a.snapshot.OperationState; state != nil {
		return state.Can(action)
	}
	// Legacy snapshots expose only the operation name and the two original
	// Session entry points. Skip is shown only for a rich, actionable state.
	return action == repository.OperationActionContinue || action == repository.OperationActionAbort
}

func (a *app) confirmSkipOperation() {
	if a.actionBusy || a.session == nil || !a.operationCan(repository.OperationActionSkip) {
		return
	}
	operation := a.snapshot.Operation
	expected := snapshotOperationFingerprint(a.snapshot)
	a.askConfirm(confirmRequest{
		title:        "Skip Operation Step",
		message:      trf("Skip the current %s step?", operation),
		detail:       "Git will discard the stopped step and its conflict resolutions, then resume the remaining operation.",
		confirmLabel: "Skip",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(tr("Skipped the operation step"), "", func(ctx context.Context, session repository.Session) error {
				return driveOperation(ctx, session, repository.OperationActionSkip, expected)
			}, nil)
		},
	})
}
