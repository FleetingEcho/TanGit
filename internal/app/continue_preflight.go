package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// continueTestRunner is the seam for deterministic execute tests. Production
// uses trustedContinueTestRunner, whose Run calls the same trusted shell
// helper as custom commands; tests must not treat a fake runner as evidence of
// real process execution.
type continueTestRunner interface {
	Run(context.Context, string, string) (string, error)
}

type continueTestRunnerFunc func(context.Context, string, string) (string, error)

func (f continueTestRunnerFunc) Run(ctx context.Context, root, command string) (string, error) {
	return f(ctx, root, command)
}

type trustedContinueTestRunner struct{}

func (trustedContinueTestRunner) Run(ctx context.Context, root, command string) (string, error) {
	return runTrustedShellCommand(ctx, root, command)
}

type continueCheckStatus string

const (
	continueCheckPending       continueCheckStatus = "pending"
	continueCheckNotConfigured continueCheckStatus = "not configured"
	continueCheckRunning       continueCheckStatus = "running"
	continueCheckPassed        continueCheckStatus = "passed"
	continueCheckFailed        continueCheckStatus = "failed"
	continueCheckCancelled     continueCheckStatus = "cancelled"
	continueCheckSkipped       continueCheckStatus = "skipped"
)

type continueCheck struct {
	command string
	status  continueCheckStatus
	output  string
	err     string
}

type continueCheckpointStatus string

const (
	continueCheckpointPending continueCheckpointStatus = "pending"
	continueCheckpointSkipped continueCheckpointStatus = "skipped"
	continueCheckpointCreated continueCheckpointStatus = "created"
	continueCheckpointFailed  continueCheckpointStatus = "failed"
)

type continueReview struct {
	required continueCheck
	optional continueCheck

	expectedFingerprint string
	generation          uint64
	root                string
	source              repository.ContinueSource
	reviewed            repository.ContinueSource
	hasSource           bool
	hasReviewed         bool
	changedFiles        []string

	capturePending  bool
	running         bool
	complete        bool
	cancelRequested bool
	stale           bool
	sourceErr       string

	checkpointStatus continueCheckpointStatus
	checkpointID     string
	checkpointErr    string
}

type continuePreflightUIState struct {
	review      *continueReview
	seq         uint64
	cancelRun   context.CancelFunc
	cancelFinal context.CancelFunc
	// Retain every attempt until shutdown. Closing or retrying does not join a
	// process on the UI thread, but final teardown still joins all of them.
	jobs []chan struct{}
}

func (s *continuePreflightUIState) cancel() {
	if s.cancelRun != nil {
		s.cancelRun()
	}
	if s.cancelFinal != nil {
		s.cancelFinal()
	}
}

func (a *app) closeContinueReview() {
	a.continuePreflight.seq++
	a.continuePreflight.cancel()
	a.continuePreflight.review = nil
	a.continuePreflight.cancelRun = nil
	a.continuePreflight.cancelFinal = nil
}

func (a *app) cancelContinueChecks() {
	review := a.continuePreflight.review
	if review == nil || review.complete || review.cancelRequested {
		return
	}
	review.cancelRequested = true
	if a.continuePreflight.cancelRun != nil {
		a.continuePreflight.cancelRun()
	}
}

// shutdownContinuePreflight invalidates every possible completion before
// joining the jobs. A retry or late callback can therefore never resurrect a
// modal, checkpoint, or DriveOperation in a newly opened repository.
func (a *app) shutdownContinuePreflight() {
	a.closeContinueReview()
	deadline := time.Now().Add(4 * time.Second)
	for _, done := range a.continuePreflight.jobs {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case <-done:
		case <-time.After(remaining):
			if a.commandLog != nil {
				a.commandLog.Note(tr("A Continue check did not stop in time; shutdown reached its wait limit."))
			}
			a.continuePreflight.jobs = nil
			return
		}
	}
	a.continuePreflight.jobs = nil
}

func (a *app) openContinueReview() {
	a.openContinueReviewExpected(snapshotOperationFingerprint(a.snapshot))
}

// retryContinueReview deliberately re-bases a stale attempt on the newest
// complete source the user was just shown. Reusing the old snapshot's
// fingerprint would make every retry reject the same already-seen operation.
func (a *app) retryContinueReview() {
	if a.actionBusy || a.session == nil {
		return
	}
	expected := snapshotOperationFingerprint(a.snapshot)
	if review := a.continuePreflight.review; review != nil {
		if review.hasReviewed {
			expected = review.reviewed.Operation.Fingerprint
		} else if review.hasSource {
			expected = review.source.Operation.Fingerprint
		}
	}
	a.openContinueReviewExpected(expected)
}

func (a *app) openContinueReviewExpected(expectedFingerprint string) {
	a.closeContinueReview()
	requiredCommand := strings.TrimSpace(a.settings.ContinuePreflight.RequiredCommand)
	optionalCommand := strings.TrimSpace(a.settings.ContinuePreflight.OptionalCommand)
	review := &continueReview{
		required:            continueCheck{command: requiredCommand, status: continueCheckPending},
		optional:            continueCheck{command: optionalCommand, status: continueCheckPending},
		expectedFingerprint: expectedFingerprint,
		generation:          a.generation,
		root:                a.snapshot.Root,
		capturePending:      true,
		checkpointStatus:    continueCheckpointPending,
	}
	if requiredCommand == "" {
		review.required.status = continueCheckNotConfigured
	}
	if optionalCommand == "" {
		review.optional.status = continueCheckSkipped
	}
	a.continuePreflight.review = review
	a.continuePreflight.seq++
	seq := a.continuePreflight.seq
	generation := a.generation
	session := a.session
	root := a.snapshot.Root
	runner := a.continueTestRunner
	if runner == nil {
		runner = trustedContinueTestRunner{}
	}
	ctx, cancel := context.WithCancel(a.repositoryWork.Context())
	done := make(chan struct{})
	a.continuePreflight.cancelRun = cancel
	a.continuePreflight.jobs = append(a.continuePreflight.jobs, done)
	go func() {
		defer a.recoverBackground()
		defer close(done)
		defer cancel()
		a.executeContinueReview(ctx, session, root, generation, seq, review, runner)
	}()
}

func (a *app) publishContinueReview(generation, seq uint64, review *continueReview, update func()) {
	a.update(func() {
		if a.generation != generation || a.continuePreflight.seq != seq || a.continuePreflight.review != review {
			return
		}
		update()
	})
}

func continueCancelledCheck(check continueCheck, output string, err error) continueCheck {
	check.output = output
	check.status = continueCheckCancelled
	if err != nil {
		check.err = safeError(err)
	}
	return check
}

func runContinueCheck(ctx context.Context, runner continueTestRunner, root string, check continueCheck) continueCheck {
	check.output, check.err = "", ""
	output, err := runner.Run(ctx, root, check.command)
	check.output = output
	if ctx.Err() != nil {
		return continueCancelledCheck(check, output, err)
	}
	if err != nil {
		check.status = continueCheckFailed
		check.err = safeError(err)
		return check
	}
	check.status = continueCheckPassed
	return check
}

// executeContinueReview performs I/O only. Every publication is bounded by
// repository generation, attempt sequence, and modal identity. Output is data:
// no string, including __IT_IS_SAFE__, changes a command's exit status.
func (a *app) executeContinueReview(
	ctx context.Context,
	session repository.Session,
	root string,
	generation, seq uint64,
	review *continueReview,
	runner continueTestRunner,
) {
	reviewer, ok := session.(repository.ContinueReviewSession)
	if !ok {
		a.publishContinueReview(generation, seq, review, func() {
			review.capturePending = false
			review.complete = true
			review.sourceErr = tr("This session cannot capture a canonical Continue source.")
		})
		return
	}
	source, err := reviewer.CaptureContinueSource(ctx)
	if err != nil {
		a.publishContinueReview(generation, seq, review, func() {
			review.capturePending = false
			review.complete = true
			if ctx.Err() != nil {
				review.required = continueCancelledCheck(review.required, "", ctx.Err())
				if review.optional.status == continueCheckPending {
					review.optional.status = continueCheckSkipped
				}
				return
			}
			review.stale = errors.Is(err, repository.ErrOperationChanged)
			review.sourceErr = safeError(err)
		})
		return
	}
	if review.expectedFingerprint == "" || source.Operation.Fingerprint != review.expectedFingerprint ||
		!source.Operation.Can(repository.OperationActionContinue) {
		a.publishContinueReview(generation, seq, review, func() {
			review.source = source
			review.hasSource = true
			review.capturePending = false
			review.complete = true
			review.stale = true
			review.sourceErr = tr("The operation changed before checks started. Refresh and review it again.")
		})
		return
	}
	a.publishContinueReview(generation, seq, review, func() {
		review.source = source
		review.hasSource = true
		review.capturePending = false
		if review.required.status == continueCheckPending {
			review.required.status = continueCheckRunning
			review.running = true
		} else {
			review.complete = true
			if review.optional.status == continueCheckPending {
				review.optional.status = continueCheckSkipped
			}
		}
	})
	if review.required.command == "" {
		return
	}

	required := runContinueCheck(ctx, runner, root, continueCheck{command: review.required.command, status: continueCheckRunning})
	optional := continueCheck{command: review.optional.command, status: review.optional.status}
	if required.status == continueCheckPassed {
		if optional.status != continueCheckSkipped {
			optional.status = continueCheckRunning
			a.publishContinueReview(generation, seq, review, func() {
				review.required = required
				review.optional = optional
				review.running = true
			})
			optional = runContinueCheck(ctx, runner, root, optional)
		}
	} else if optional.status == continueCheckPending || optional.status == continueCheckRunning {
		optional.status = continueCheckSkipped
	}
	if ctx.Err() != nil {
		if required.status != continueCheckCancelled && optional.status != continueCheckCancelled {
			required = continueCancelledCheck(required, required.output, ctx.Err())
		}
		a.publishContinueReview(generation, seq, review, func() {
			review.required = required
			review.optional = optional
			review.running = false
			review.complete = true
			review.checkpointStatus = continueCheckpointSkipped
		})
		return
	}

	reviewed, err := reviewer.CaptureContinueSource(ctx)
	if err != nil {
		a.publishContinueReview(generation, seq, review, func() {
			review.required = required
			review.optional = optional
			review.running = false
			review.complete = true
			review.stale = errors.Is(err, repository.ErrOperationChanged)
			review.sourceErr = safeError(err)
		})
		return
	}
	delta := repository.CompareContinueSources(source, reviewed)
	checkpointStatus := continueCheckpointSkipped
	checkpointID := ""
	checkpointErr := ""
	if !delta.OperationChanged && delta.ContentChanged() {
		checkpoint, checkpointFailure := session.CreateCheckpoint(ctx, "Before continuing "+string(source.Operation.Type))
		if checkpointFailure != nil {
			checkpointStatus = continueCheckpointFailed
			checkpointErr = safeError(checkpointFailure)
		} else {
			checkpointStatus = continueCheckpointCreated
			checkpointID = checkpoint.ID
		}
	}
	a.publishContinueReview(generation, seq, review, func() {
		review.required = required
		review.optional = optional
		review.reviewed = reviewed
		review.hasReviewed = true
		review.changedFiles = delta.ChangedFiles
		review.running = false
		review.complete = true
		review.stale = delta.OperationChanged
		review.checkpointStatus = checkpointStatus
		review.checkpointID = checkpointID
		review.checkpointErr = checkpointErr
		if delta.OperationChanged {
			review.sourceErr = tr("The operation, index stages, or metadata changed while checks ran.")
		}
	})
}

// continueGate returns the one reason Continue is unavailable. Required and
// optional are deliberately different: an optional failure alone can be
// overridden, while required/not-run/cancelled/stale/checkpoint failures never
// can. No unchecked __IT_IS_SAFE__ path exists here or in the runner.
func (a *app) continueGate(overrideOptional bool) (bool, string) {
	review := a.continuePreflight.review
	if review == nil {
		return false, tr("Wait for the Continue checks and source review to finish.")
	}
	if review.generation != a.generation || review.root != a.snapshot.Root {
		return false, tr("Operation changed during checks. Retry against fresh state.")
	}
	if !review.complete || review.capturePending || review.running {
		return false, tr("Wait for the Continue checks and source review to finish.")
	}
	if review.stale {
		return false, tr("Operation changed during checks. Retry against fresh state.")
	}
	if review.cancelRequested {
		return false, tr("Cancellation cannot be overridden. Retry the checks against fresh state.")
	}
	if review.sourceErr != "" {
		return false, review.sourceErr
	}
	if !review.hasSource || !review.hasReviewed {
		return false, tr("The Continue source has not been rechecked.")
	}
	switch review.required.status {
	case continueCheckNotConfigured:
		return false, tr("Required check is not configured. Configure a fully trusted shell command before continuing.")
	case continueCheckPassed:
	default:
		return false, tr("The required check must pass; failure and cancellation cannot be overridden.")
	}
	if review.optional.status == continueCheckFailed && !overrideOptional {
		return false, tr("The optional check failed. Review its output and explicitly override it to continue.")
	}
	if review.optional.status != continueCheckPassed && review.optional.status != continueCheckSkipped &&
		review.optional.status != continueCheckFailed {
		return false, tr("The optional check must pass, be skipped, or receive an explicit failure override.")
	}
	if review.checkpointStatus == continueCheckpointFailed {
		return false, tr("Checkpoint failed. Changes made by checks must be captured before Continue.")
	}
	if review.checkpointStatus != continueCheckpointCreated && review.checkpointStatus != continueCheckpointSkipped {
		return false, tr("Wait for checkpoint review to finish.")
	}
	return true, ""
}

// finishContinueReview is the single path for ordinary approval and an
// explicit optional-failure override. Both perform a fresh canonical source
// comparison and then call the operation driver's own expected-fingerprint
// CAS. Nothing here invokes Git continue directly.
func (a *app) finishContinueReview(overrideOptional bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	allowed, _ := a.continueGate(overrideOptional)
	if !allowed {
		return
	}
	review := a.continuePreflight.review
	approved := review.reviewed
	historyPatch := approved.Operation.HistoryPatch != nil
	ctx, cancel := context.WithCancel(a.repositoryWork.Context())
	a.continuePreflight.cancelFinal = cancel
	a.beginRepositoryActionMessage(tr("Continued the operation"), "", func(_ context.Context, session repository.Session) error {
		defer cancel()
		reviewer, ok := session.(repository.ContinueReviewSession)
		if !ok {
			return fmt.Errorf("%w: session cannot recheck the Continue source", repository.ErrOperationUnsupported)
		}
		current, err := reviewer.CaptureContinueSource(ctx)
		if err != nil {
			return err
		}
		if delta := repository.CompareContinueSources(approved, current); delta.OperationChanged || delta.ContentChanged() {
			return fmt.Errorf("%w: Continue source changed after review; retry the checks and checkpoint", repository.ErrOperationChanged)
		}
		return driveOperation(ctx, session, repository.OperationActionContinue, current.Operation.Fingerprint)
	}, func() {
		a.closeContinueReview()
		if historyPatch {
			a.clearCustomPatch()
		}
	})
}
