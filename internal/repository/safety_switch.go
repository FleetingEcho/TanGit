package repository

import "sync/atomic"

// safetyJournalOn is the user's explicit choice to take a recovery point
// first. Its zero value is off. It is process-wide because it is a preference
// of the window, and every repository tab and SSH workspace reads the same one.
var safetyJournalOn atomic.Bool

// SetSafetyJournalEnabled turns the recovery point and journal entry that
// precede most workspace actions on or off. It is off by default.
//
// With it off, stash, apply, discard, checkout, merge, commit, revert, branch
// deletion and similar actions just run: nothing is saved for Undo, and the
// action is not listed in the Safety Journal. History rewrites and operation
// transitions that Git's reflog cannot faithfully reconstruct (rebase, reset,
// patch import/editing, Continue, Skip and Abort) keep their recovery point
// regardless.
func SetSafetyJournalEnabled(enabled bool) {
	safetyJournalOn.Store(enabled)
}

// SafetyJournalEnabled reports whether actions take a recovery point first.
func SafetyJournalEnabled() bool {
	return safetyJournalOn.Load()
}
