package repository

import (
	"strings"
	"testing"
)

func TestShortcutLogDoesNotStealAnotherCommandsResult(t *testing.T) {
	log := NewCommandLog()
	complete := log.Begin("git status")
	first := log.RecordShortcut(ShortcutInvocation{ID: "stage/all", Chord: "a", Label: "Stage all changes"})
	if first.Sequence == 0 {
		t.Fatal("accepted shortcut did not receive an identity")
	}
	first.Label = "mutated outside the invocation"
	complete("staged successfully", false)
	log.Note("background synchronisation failed")

	entries := log.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want the shortcut and the shell command", len(entries))
	}
	var short *ShortcutInvocation
	for _, entry := range entries {
		if entry.Shortcut != nil {
			short = entry.Shortcut
			continue
		}
		if !entry.Failed || !strings.Contains(entry.Output, "staged successfully") ||
			!strings.Contains(entry.Output, "background synchronisation failed") {
			t.Fatalf("real command lost its own output/failure: %+v", entry)
		}
	}
	if short == nil {
		t.Fatal("keyboard invocation was not retained")
	}
	if short.Label != "Stage all changes" {
		t.Fatalf("keyboard metadata was mutated through its returned copy: %+v", short)
	}
	if entries[len(entries)-1].Shortcut == nil || entries[len(entries)-1].Failed {
		t.Fatalf("unrelated Git failure was attributed to the shortcut: %+v", entries[len(entries)-1])
	}
}
