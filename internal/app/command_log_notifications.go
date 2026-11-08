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
		if !a.commandLogVisible.Load() {
			return
		}
		// Window.Invalidate coalesces bursts of Git output to one redraw. Tests
		// without a native window run the corresponding queued UI update.
		if a.window != nil {
			a.window.Invalidate()
			return
		}
		a.update(func() {})
	})
}

// openCommandLog shows commands entered by the user by default. Hosting or Git
// errors use openGitCommandLog to land in their actual section, not silently
// look missing behind the default tab after command history was filtered.
func (a *app) openCommandLog() {
	a.commandLogOpen = true
	a.commandLogVisible.Store(true)
	a.commandLogUser = true
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
