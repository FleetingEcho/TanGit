package app

import "github.com/egoist/mygo/ui/editor"

// startRemoteConflictWrite serializes an SSH conflict save behind the stamped
// file version read by requestConflictFile. It only captures immutable data for
// its worker; all state and editor changes are published back on the UI thread.
// The repository generation and per-state sequence keep an old save from
// landing in a replacement repository or conflict pane.
func (a *app) startRemoteConflictWrite(state *conflictState, content string, version uint64, done func(error)) bool {
	if state == nil || state.remoteSave.running || a.actionBusy {
		return false
	}
	client := state.remoteClient
	if client == nil {
		var err error
		client, err = a.remoteWorkspaceClient()
		if err != nil {
			done(err)
			return true
		}
	}
	root, name := a.snapshot.Root, state.path
	generation := a.generation
	previous, previousKnown := state.remoteStamp, state.remoteStampKnown
	save := &state.remoteSave
	save.sequence++
	sequence := save.sequence
	save.running = true
	save.pending = false
	save.version = version
	// The existing action gates keep hunk buttons and Mark resolved from
	// staging bytes that are still on the wire, without blocking frame drawing.
	a.actionBusy = true
	data := []byte(content)

	go func() {
		defer a.recoverBackground()
		defer func() {
			if recovered := recover(); recovered != nil {
				a.update(func() {
					if generation == a.generation && sequence == save.sequence {
						save.invalidate()
						a.actionBusy = false
					}
				})
				panic(recovered)
			}
		}()
		stamp, writeErr := writeRemoteWorkingFile(client, root, name, data, previous, previousKnown)
		a.updateChanged(func() bool {
			if generation != a.generation || sequence != save.sequence {
				save.invalidate()
				return false
			}
			save.running = false
			save.pending = false
			a.actionBusy = false
			if writeErr == nil {
				state.remoteStamp, state.remoteStampKnown = stamp, true
			}
			// resetFileDiff may have replaced the conflict while this request was
			// on the wire. Its write is still stamped and safe, but its completion
			// must not rebuild or report errors over the replacement pane.
			if a.conflictState != state {
				return false
			}
			done(writeErr)
			return true
		})
	}()
	return true
}

// lockRemoteConflictResolution prevents a hunk action's asynchronous write
// from racing new keystrokes into an editor whose contents are about to be
// replaced. Ordinary typed saves remain editable and queue newer versions.
func lockRemoteConflictResolution(view *editor.State) {
	if view != nil {
		view.ReadOnly = true
	}
}

func unlockRemoteConflictResolution(view *editor.State) {
	if view != nil {
		view.ReadOnly = false
	}
}
