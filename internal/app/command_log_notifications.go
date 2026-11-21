package app

import "lazygitapp/internal/repository"

type commandLogFailureTarget struct {
	log *repository.CommandLog
	id  uint64
}

// openFailedCommand follows an actual command identity, not a substring or
// current index. A stale or evicted target falls back to the ordinary log.
func (a *app) openFailedCommand(target *commandLogFailureTarget) {
	a.openGitCommandLog()
	if target == nil || target.log == nil || target.id == 0 || target.log != a.commandLog {
		return
	}
	for _, entry := range target.log.Entries() {
		if entry.ID() != target.id {
			continue
		}
		a.commandLogUser = entry.Origin == repository.CommandOriginUser
		selected := *target
		a.commandLogReveal = &selected
		return
	}
}

func (a *app) openFailedCommandAction(target *commandLogFailureTarget) func() {
	if target == nil {
		return a.openGitCommandLog
	}
	selected := *target
	return func() { a.openFailedCommand(&selected) }
}

// attachRepositoryCommandLog ties the logger for the newly opened repository
// to the window. The listener never reads mutable app fields on its own thread;
// atomic visibility selects whether a live command-log repaint is necessary.
func (a *app) attachRepositoryCommandLog(log *repository.CommandLog) {
	if log != a.commandLog {
		a.actionFailure = nil
		a.commandLogReveal = nil
		a.commandLogExpanded = nil
		a.resetCommandLogPaging()
	}
	a.commandLog = log
	if log == nil {
		return
	}
	log.SetListener(func() {
		if !a.commandLogVisible.Load() || !a.commandLogFramePending.CompareAndSwap(false, true) {
			return
		}
		// Queue one publication for a burst rather than allowing every output
		// append to repaint or fill the main-thread queue. Visibility is checked
		// again on the UI thread so a closed log or background tab stays quiet.
		a.updateChanged(func() bool {
			a.commandLogFramePending.Store(false)
			return a.commandLogVisible.Load()
		})
	})
}

// openCommandLog shows a fresh, bounded page of background Git commands. User
// commands stay one explicit tab away; no records are fetched during rendering.
func (a *app) openCommandLog() {
	a.commandLogOpen = true
	a.commandLogVisible.Store(true)
	a.commandLogReveal = nil
	a.commandLogUser = false
	a.commandLogQuery = ""
	a.commandLogExpanded = nil
	a.resetCommandLogFilters()
	a.resetCommandLogPaging()
}

func (a *app) openGitCommandLog() {
	a.openCommandLog()
}

// clearCommandLog removes the records and every view that still presents
// them. The status island's error and navigation target are command-log state,
// so neither may outlive the record the user explicitly cleared.
func (a *app) clearCommandLog() {
	if a.commandLog != nil {
		a.commandLog.Clear()
	}
	a.actionError = ""
	a.actionFailure = nil
	a.commandLogReveal = nil
	a.commandLogExpanded = nil
	a.resetCommandLogPaging()
}

func (a *app) closeCommandLog() {
	a.commandLogOpen = false
	a.commandLogReveal = nil
	a.commandLogVisible.Store(false)
}
