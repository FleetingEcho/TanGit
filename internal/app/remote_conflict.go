package app

import "github.com/egoist/mygo/ui/editor"

// startRemoteConflictWrite serializes an SSH conflict save behind the stamped
// file version read by requestConflictFile. It only captures immutable data for
// its worker; all state and editor changes are published back on the UI thread.
// The repository generation and per-state sequence keep an old save from
// landing in a replacement repository or conflict pane.
func (a *app) startRemoteConflictWrite(state *conflictState, content string, version uint64, done func(error)) bool {
	return a.startRemoteConflictWriteWithMutation(state, content, version, nil, done)
}

func (a *app) startRemoteConflictRecoveryWrite(state *conflictState, content string, done func(error)) bool {
	// Pin repository identity with the stamped remote client. A later repository
	// switch must not archive one session while writing through another.
	session := a.session
	ctx := a.repositoryWork.Context()
	return a.startRemoteConflictWriteWithMutation(state, content, 0,
		func(write func() error) error {
			return runFileRecoveryMutation(ctx, session, state.path, write)
		}, func(err error) {
			a.refreshFileRecoveries()
			done(err)
		})
}

// startRemoteConflictWriteWithMutation runs write inside an optional durable
// mutation envelope. The envelope and write both run on the SSH worker; only
// stamps, editor state and notifications return to the UI thread.
func (a *app) startRemoteConflictWriteWithMutation(state *conflictState, content string, version uint64,
	envelope func(func() error) error, done func(error)) bool {
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
		var stamp remoteFileStamp
		write := func() error {
			var err error
			stamp, err = writeRemoteWorkingFile(client, root, name, data, previous, previousKnown)
			return err
		}
		var writeErr error
		if envelope == nil {
			writeErr = write()
		} else {
			writeErr = envelope(write)
		}
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
