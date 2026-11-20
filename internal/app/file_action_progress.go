package app

import "github.com/egoist/mygo/ui"

// fileMutationPhase distinguishes an accepted mutation from its read-only
// preparation. In particular, checking an existing recovery record is not
// presented as writing the worktree.
type fileMutationPhase uint8

const (
	fileMutationIdle fileMutationPhase = iota
	fileMutationChecking
	fileMutationUpdating
)

type fileMutationState struct {
	serial uint64
	phase  fileMutationPhase
}

func (a *app) beginFileMutation() uint64 {
	a.fileMutation.serial++
	a.fileMutation.phase = fileMutationChecking
	return a.fileMutation.serial
}

// acceptFileMutation is invoked by a recovery worker only after its read-only
// checks succeed and immediately before its first durable state change.
// App-owned state transitions still run exclusively on the UI thread. The
// serial and phase checks deliberately do not consult shared actionBusy:
// unrelated background loads may clear it while this accepted mutation runs.
func (a *app) acceptFileMutation(serial, generation uint64) {
	a.update(func() {
		if serial != a.fileMutation.serial || generation != a.generation ||
			a.fileMutation.phase != fileMutationChecking {
			return
		}
		a.fileMutation.phase = fileMutationUpdating
	})
}

// finishFileMutation releases the indicator for both successful and failed
// writes, including when core verification or snapshot publication fails.
// finishFileMutationAsync is the worker's panic/failure safety net. It uses
// the originating repository generation so closing a repository never lets an
// older callback clear a newer mutation's state.
func (a *app) finishFileMutationAsync(serial, generation uint64) {
	a.update(func() {
		if generation == a.generation {
			a.finishFileMutation(serial)
		}
	})
}

func (a *app) finishFileMutation(serial uint64) {
	if a.fileMutation.serial == serial {
		a.fileMutation.phase = fileMutationIdle
	}
}

func (a *app) fileMutationIndicator(c *ui.Context) {
	phase := a.fileMutation.phase
	if phase == fileMutationIdle {
		return
	}
	label, hint := "Checking recovery…", "Verifying saved recovery data before staging"
	color := c.Theme().TextMuted
	if phase == fileMutationUpdating {
		label, hint, color = "Updating file list…", "Saving changes and refreshing the file list", c.Theme().Warning
	}
	c.Key("changes-header-mutation")
	ui.Row(c).AlignItems(ui.Center).Gap(5).Tooltip(tr(hint)).Children(func() {
		ui.Spinner(c).Label(tr(label)).TextColor(color)
		ui.Text(c, tr(label)).TextColor(color).FontSize(c.Theme().FontSize - 1).
			SingleLine().MaxWidth(128).Tooltip(tr(hint))
	})
}
