package app

import "errors"

// recoverBackground turns a panic on a background read into a reported
// failure, so a fault in the Git layer or in a third-party helper stays a
// failed read instead of taking the process down. Every goroutine the app
// starts defers it first.
func (a *app) recoverBackground() {
	r := recover()
	if r == nil {
		return
	}
	reason := describeFault(r)
	a.update(func() { a.reportBackgroundFault(reason) })
}

// reportBackgroundFault clears whatever the failed read was filling, records
// the reason in the pane it was reading for, and shows it, so nothing is left
// loading forever and the user can retry.
func (a *app) reportBackgroundFault(reason string) {
	if a.state == stateLoading {
		a.state = stateError
		a.errMessage = reason
	}
	// The pane the read was for shows the reason; one read runs at a time in
	// each pane, so the loading flag names it.
	switch {
	case a.diffLoading:
		a.diffError = reason
	case a.blameLoading:
		a.blameError = reason
	case a.commitsLoading:
		a.commitsError = reason
	case a.authorsLoading:
		a.authorsError = reason
	case a.commitFilesLoading:
		a.commitFilesError = reason
	case a.commitDiffLoading:
		a.commitDiffError = reason
	case a.stashDiffLoading:
		a.stashDiffError = reason
	case a.stashFilesLoading:
		a.stashFilesError = reason
	case a.branchCommitsLoading:
		a.branchCommitsError = reason
	case a.branchCommitFilesLoading:
		a.branchCommitFilesError = reason
	case a.branchCommitDiffLoading:
		a.branchCommitDiffError = reason
	case a.fileHistoryLoading:
		a.fileHistoryError = reason
	case a.fileHistoryLoadingMore:
		a.fileHistoryMoreError = reason
	case a.fileHistoryFilesLoading:
		a.fileHistoryFilesError = reason
	case a.fileHistoryDiffLoading:
		a.fileHistoryDiffError = reason
	case a.fileEditLoading:
		a.fileEditError = reason
	case a.term.loading:
		a.term.err = reason
	}
	if a.actionBusy {
		a.actionBusy = false
		a.actionError = reason
	}
	a.diffLoading = false
	a.blameLoading = false
	a.commitsLoading = false
	a.authorsLoading = false
	a.commitFilesLoading = false
	a.commitDiffLoading = false
	a.stashDiffLoading = false
	a.stashFilesLoading = false
	a.branchCommitsLoading = false
	a.branchCommitFilesLoading = false
	a.branchCommitDiffLoading = false
	a.fileHistoryLoading = false
	a.fileHistoryLoadingMore = false
	a.fileHistoryFilesLoading = false
	a.fileHistoryDiffLoading = false
	a.fileEditLoading = false
	a.term.loading = false
	// Failed opening or refresh work must free the way for a retry rather than
	// leaving its loading flags set for the rest of the session.
	a.repositoryDetailsLoading = false
	a.refreshBusy = false

	if a.commandLog != nil {
		a.commandLog.Note("Unexpected failure: " + reason)
	}
	a.notifyError(errors.New(reason))
}
