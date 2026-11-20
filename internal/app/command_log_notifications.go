package app

import "lazygitapp/internal/repository"

// attachRepositoryCommandLog ties the logger for the newly opened repository
// to the window. The listener never reads mutable app fields on its own thread;
// atomic visibility selects whether a live command-log repaint is necessary.
func (a *app) attachRepositoryCommandLog(log *repository.CommandLog) {
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

// openCommandLog shows background Git commands by default. User commands stay
// one explicit tab away, and command errors land directly in that section.
func (a *app) openCommandLog() {
	a.commandLogOpen = true
	a.commandLogVisible.Store(true)
	a.commandLogUser = false
	a.commandLogQuery = ""
}

func (a *app) openGitCommandLog() {
	a.commandLogOpen = true
	a.commandLogVisible.Store(true)
	a.commandLogUser = false
	a.commandLogQuery = ""
}

func (a *app) closeCommandLog() {
	a.commandLogOpen = false
	a.commandLogVisible.Store(false)
}
