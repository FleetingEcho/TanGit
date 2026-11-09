package app

import (
	"fmt"
	"strings"

	"lazygitapp/internal/repository"
)

// signSnapshotForWorkspace keeps the local metadata signatures intact while
// deliberately omitting every host-filesystem stat for an SSH repository. A
// remote working tree cannot be fingerprinted through snapshot.Root locally.
func signSnapshotForWorkspace(snapshot repository.Snapshot, remote bool) snapshotSigns {
	if !remote {
		return signSnapshot(snapshot)
	}
	return snapshotSigns{
		files:      remoteFilesSignature(snapshot),
		refs:       refsSignature(snapshot),
		codeFiles:  codeFilesSignature(snapshot),
		operations: operationsSignature(snapshot),
		recovery:   recoverySignature(snapshot),
	}
}

// remoteFilesSignature contains only Git-reported facts. Equal signatures do
// not claim equal remote contents: refreshRemoteQuietDetails conservatively
// rereads the selected diff when these signatures match.
func remoteFilesSignature(snapshot repository.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00", snapshot.Root, snapshot.Operation)
	for _, file := range snapshot.Files {
		fmt.Fprintf(&b, "%+v\x00\x00", file)
	}
	return b.String()
}

// refreshRemoteQuietDetails is the conservative counterpart of the local
// size/mtime signature check. SSH snapshots do not expose host file metadata,
// and equal Git metadata can hide a timestamp-only or same-size content edit.
// Reread the displayed diff behind the scenes, without flashing a spinner or
// repainting an identical result.
func (a *app) refreshRemoteQuietDetails() bool {
	change, ok := a.selectedChange()
	if !ok {
		return false
	}
	if change.diffFile().HasMergeConflicts {
		// Conflict editing keeps its existing reload and selection behavior.
		a.beginDiff()
		return true
	}
	a.requestQuietFileDiff()
	// The asynchronous read repaints if its result actually changes.
	return false
}
