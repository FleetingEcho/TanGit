package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestPaletteHistoryPersistsSearchesCommandsAndPause(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.pathUserData = t.TempDir()
	s.openCommandPaletteWithQuery(nil, ">open history")
	if result := s.invokeCommand("workspace/history"); !result.Accepted() {
		t.Fatalf("open History: %+v", result)
	}
	s.rememberShellLine("git status")
	a.recordHistorySearch(repository.HistoryChangeQuery{Text: "timeout", Path: "config.go"})

	reopened := newShell(nil)
	reopened.loadPaletteHistory(a)
	entries := reopened.palette.history.Entries
	if len(entries) != 4 || entries[0].Kind != paletteHistorySearch || entries[1].Command != "workspace/history" ||
		entries[2].Text != "git status" || entries[3].CodeQuery == nil || entries[3].CodeQuery.Path != "config.go" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	s.openCommandPalette(nil)
	tt.Frame()
	if err := tt.Click("Recent"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Record history"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !s.palette.history.Paused {
		t.Fatal("pause did not take effect")
	}
	s.rememberShellLine("git log")
	s.recordPaletteHistory(a, paletteHistoryEntry{Kind: paletteHistorySearch, Text: "while paused"})
	again := newShell(nil)
	again.loadPaletteHistory(a)
	if !again.palette.history.Paused || len(again.palette.history.Entries) != 4 {
		t.Fatalf("pause was not persisted or accepted new records: %+v", again.palette.history.paletteHistoryStore)
	}
}

func TestPaletteHistoryDeletesSelectedAndClears(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.pathUserData = t.TempDir()
	s.rememberShellLine("git log")
	s.rememberShellLine("git status")
	s.rememberShellLine("git branch")
	s.openCommandPalette(nil)
	s.togglePaletteHistory(true)
	tt.Frame()
	if _, found := tt.Find("Select history entries git log"); found {
		t.Fatal("multi-select checkbox was visible by default")
	}
	if err := tt.Click("Select history entries"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"Select history entries git log", "Select history entries git status"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if len(s.palette.history.selected) != 2 {
		t.Fatalf("expected two selected history rows: %+v", s.palette.history.selected)
	}
	if err := tt.Click("Delete selected"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rows := s.palette.history.Entries; len(rows) != 1 || rows[0].Text != "git branch" {
		t.Fatalf("expected only git branch after deleting the selected rows: %+v", rows)
	}
	if len(s.palette.shellHistory) != 1 || s.palette.shellHistory[0] != "git branch" {
		t.Fatalf("shell recall kept deleted rows: %v", s.palette.shellHistory)
	}
	if err := tt.Click("Clear history"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !s.palette.history.clear || len(s.palette.history.Entries) != 1 {
		t.Fatal("clearing should ask for confirmation")
	}
	if err := tt.Click("Confirm clear history"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	reopened := newShell(nil)
	reopened.loadPaletteHistory(a)
	if len(reopened.palette.history.Entries) != 0 || len(loadAppSettings(a.pathUserData).ShellHistory) != 0 {
		t.Fatal("cleared entries reappeared after reopening")
	}
}

func TestPaletteHistoryRerunsTheClickedAndKeyboardSelectedEntry(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.pathUserData = t.TempDir()
	s.recordPaletteHistory(a, paletteHistoryEntry{Kind: paletteHistoryCommand, Text: "Open History", Command: "workspace/history"})
	s.recordPaletteHistory(a, paletteHistoryEntry{Kind: paletteHistorySearch, Text: "#branch:main"})
	s.openCommandPalette(nil)
	s.togglePaletteHistory(true)
	tt.Frame()
	if err := tt.Click("#branch:main"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.palette.history.open || s.palette.query != "#branch:main" {
		t.Fatalf("clicking did not run the search: query=%q", s.palette.query)
	}
	s.palette.history.lastQuery = s.palette.query
	s.togglePaletteHistory(true)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.palette.open || a.workspaceTab != sectionHistory {
		t.Fatalf("Enter did not run the selected command: cursor=%d workspace=%d", s.palette.history.cursor, a.workspaceTab)
	}
}

func TestPaletteHistoryKeepsOnlyTheNewestEntriesWithinItsLimit(t *testing.T) {
	s, _, a := readyPaletteShell(t)
	a.pathUserData = t.TempDir()
	s.loadPaletteHistory(a)
	s.palette.history.Limit = 100
	for i := range 130 {
		s.recordPaletteHistory(a, paletteHistoryEntry{Kind: paletteHistorySearch, Text: fmt.Sprintf("query %d", i)})
	}
	entries := s.palette.history.Entries
	if len(entries) != 100 || entries[0].Text != "query 30" || entries[99].Text != "query 129" {
		t.Fatalf("kept %d entries, first %q", len(entries), entries[0].Text)
	}
	again := newShell(nil)
	again.loadPaletteHistory(a)
	if len(again.palette.history.Entries) != 100 || again.palette.history.Limit != 100 {
		t.Fatalf("limit was not persisted: %d entries, limit %d", len(again.palette.history.Entries), again.palette.history.Limit)
	}
}
