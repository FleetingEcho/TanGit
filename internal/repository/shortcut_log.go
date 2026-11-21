package repository

import (
	"strings"
	"time"
)

// ShortcutInvocation records one accepted keyboard invocation, not the result
// of an operation. Its immutable copy can travel through the invocation's own
// callbacks without pretending later asynchronous work happened under a
// mutable "last pressed key" marker.
type ShortcutInvocation struct {
	ID       string
	Chord    string
	Label    string
	Sequence uint64
}

// RecordShortcut appends an independently owned keyboard event to the
// repository's user-command log. It deliberately does not replace the current
// command writer, so a running Git command keeps ownership of its output.
func (l *CommandLog) RecordShortcut(inv ShortcutInvocation) ShortcutInvocation {
	if l == nil {
		return inv
	}
	inv.ID = strings.TrimSpace(RedactSecrets(inv.ID))
	inv.Chord = strings.TrimSpace(RedactSecrets(inv.Chord))
	inv.Label = strings.TrimSpace(RedactSecrets(inv.Label))
	if inv.Label == "" {
		return inv
	}
	l.mu.Lock()
	inv.Sequence = l.nextID + 1
	l.appendEntryLocked(CommandEntry{
		At: time.Now(), Command: inv.Label, Origin: CommandOriginUser, Shortcut: &inv,
		InvocationID: inv.ID, InvocationSource: "shortcut",
	})
	l.mu.Unlock()
	l.notifyListener()
	return inv
}

// RecordPalette records an accepted Command Catalog invocation in the user
// command log. Unlike RecordShortcut it has no chord and emits no shortcut
// acknowledgement toast; actual Git/custom commands keep their own entries.
func (l *CommandLog) RecordPalette(id, label string) {
	if l == nil {
		return
	}
	id = strings.TrimSpace(RedactSecrets(id))
	label = strings.TrimSpace(RedactSecrets(label))
	if label == "" {
		return
	}
	l.mu.Lock()
	l.appendEntryLocked(CommandEntry{
		At: time.Now(), Command: label, Origin: CommandOriginUser,
		InvocationID: id, InvocationSource: "palette",
	})
	l.mu.Unlock()
	l.notifyListener()
}
