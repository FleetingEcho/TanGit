package repository

import "sync/atomic"

// safetyJournalOff is the user's choice to run actions without taking a
// recovery point first. It is process-wide because it is a preference of the
// window, and every repository tab and SSH workspace reads the same one.
var safetyJournalOff atomic.Bool

// SetSafetyJournalEnabled turns the recovery point and journal entry that
// precede most workspace actions on or off. It is on by default.
//
// With it off, stash, apply, discard, checkout, merge, commit, revert, branch
// deletion and similar actions just run: nothing is saved for Undo, and the
// action is not listed in the Safety Journal. History rewrites that are hard to
// redo by hand (rebase, reset, patch import and editing) keep their recovery
// point regardless.
func SetSafetyJournalEnabled(enabled bool) {
	safetyJournalOff.Store(!enabled)
}

// SafetyJournalEnabled reports whether actions take a recovery point first.
func SafetyJournalEnabled() bool {
	return !safetyJournalOff.Load()
}
