package repository

import (
	"strings"
	"testing"
)

func TestCommandLogRecordsCommandsAndOutput(t *testing.T) {
	log := NewCommandLog()

	log.logCommand("git status --porcelain", true)
	if _, err := log.newWriter().Write([]byte(" M one.txt\n")); err != nil {
		t.Fatal(err)
	}

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	if entries[0].Command != "git status --porcelain" {
		t.Errorf("command %q", entries[0].Command)
	}
	if entries[0].Output != " M one.txt\n" {
		t.Errorf("output %q", entries[0].Output)
	}
	if entries[0].Failed {
		t.Error("a successful command was marked failed")
	}
}

func TestCommandLogNoteMarksTheFailure(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git push", true)

	log.Note("fatal: the remote hung up")

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	if !entries[0].Failed {
		t.Error("Note did not mark the command failed")
	}
	if !strings.Contains(entries[0].Output, "remote hung up") {
		t.Errorf("output %q does not hold the error", entries[0].Output)
	}

	// A note with no command still records what went wrong.
	empty := NewCommandLog()
	empty.Note("fatal: no repository")
	if entries := empty.Entries(); len(entries) != 1 || !entries[0].Failed {
		t.Fatalf("a note without a command was dropped: %+v", entries)
	}
}

func TestCommandLogKeepsOnlyTheNewestCommands(t *testing.T) {
	log := NewCommandLog()
	for i := 0; i < commandLogLimit+10; i++ {
		log.logCommand("git command", true)
	}
	entries := log.Entries()
	if len(entries) != commandLogLimit {
		t.Fatalf("%d entries, want %d", len(entries), commandLogLimit)
	}
}

func TestCommandLogClear(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git fetch", true)
	log.Clear()
	if entries := log.Entries(); len(entries) != 0 {
		t.Fatalf("Clear left %d entries", len(entries))
	}
	// A cleared log keeps working.
	log.logCommand("git pull", true)
	if entries := log.Entries(); len(entries) != 1 {
		t.Fatalf("%d entries after a new command, want 1", len(entries))
	}
}
