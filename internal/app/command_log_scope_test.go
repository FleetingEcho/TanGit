package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"lazygitapp/internal/repository"
)

func TestCommandLogSectionsAndSearch(t *testing.T) {
	a, tester, _ := realCommandHarness(t)
	a.commandLog.Record("git rev-parse HEAD", "abc123", false)
	a.commandLog.BeginUserCommand("git status --short")("?? untracked.txt\n", false)
	a.commandLog.BeginUserCommand("git describe --tags")("v1.0.0\n", false)
	a.openCommandLog()
	tester.Frame()

	if !tester.HasText("Your commands") || !tester.HasText("Background Git commands") {
		t.Fatalf("missing sections: %q", tester.Texts())
	}
	if !tester.HasText("$ git status --short") || tester.HasText("$ git rev-parse HEAD") {
		t.Fatalf("default section does not contain only user commands: %q", tester.Texts())
	}
	if err := tester.Click("Background Git commands"); err != nil {
		t.Fatalf("switch to Git section: %v", err)
	}
	tester.Frame()
	if !tester.HasText("$ git rev-parse HEAD") || tester.HasText("$ git status --short") {
		t.Fatalf("Git section includes the wrong entries: %q", tester.Texts())
	}

	if err := tester.Click("Your commands"); err != nil {
		t.Fatalf("return to user commands: %v", err)
	}
	tester.Frame()
	if err := tester.Click("Search commands"); err != nil {
		t.Fatalf("focus command search: %v", err)
	}
	tester.Type("STATUS")
	tester.Frame()
	if !tester.HasText("$ git ") || !tester.HasText("status") {
		t.Fatalf("query did not split and highlight the match: %q", tester.Texts())
	}
	if !tester.HasText("?? untracked.txt") {
		t.Fatalf("output from a matching entry was discarded: %q", tester.Texts())
	}
	if tester.HasText("describe") {
		t.Fatalf("unmatched entries were not filtered: %q", tester.Texts())
	}
	parts := commandHighlightParts("$ git status --short", "STATUS")
	var marked strings.Builder
	for _, part := range parts {
		if part.matched {
			marked.WriteString(part.text)
		}
	}
	if marked.String() != "status" {
		t.Fatalf("matched text was not preserved: %+v", parts)
	}

	a.commandLogQuery = "no-such-command"
	tester.Frame()
	if !tester.HasText("No commands match your search.") {
		t.Fatalf("missing filter-empty state: %q", tester.Texts())
	}
}

func TestHighlightPartsPreserveTextAndMatchEveryOccurrence(t *testing.T) {
	const input = "status beside status"
	parts := commandHighlightParts(input, "status")
	var reconstructed strings.Builder
	matches := 0
	for _, part := range parts {
		reconstructed.WriteString(part.text)
		if part.matched {
			matches++
		}
	}
	if reconstructed.String() != input || matches != 2 {
		t.Fatalf("parts = %#v, reconstructed %q, matches %d", parts, reconstructed.String(), matches)
	}
}

func TestHighlightPartsRespectUnicodeBoundaries(t *testing.T) {
	parts := commandHighlightParts("Kelvin and K other text", "k")
	matches := 0
	for _, part := range parts {
		if part.matched {
			matches++
			if part.text != "K" {
				t.Fatalf("match sliced a rune or lost its casing: %q", part.text)
			}
		}
	}
	if matches != 2 {
		t.Fatalf("got %d matches, want both Kelvin signs: %+v", matches, parts)
	}
}

func TestHighlightedCommandCopiesWithoutInsertedWhitespace(t *testing.T) {
	a, tester, _ := realCommandHarness(t)
	a.commandLog.Clear()
	a.commandLog.BeginUserCommand("git remote -v")("origin  https://example.com/team/project.git\n", false)
	a.commandLogQuery = "remote"
	a.openCommandLog()
	a.commandLogQuery = "remote"
	tester.Frame()
	link, ok := tester.Find("origin  https://example.com/team/project.git")
	if !ok {
		t.Fatalf("URL is not searchable: %q", tester.Texts())
	}
	tester.ClickAt(link.X+link.W/2, link.Y+link.H/2)
	tester.Frame()
	tester.Key(ui.Ctrl, ui.KeyA)
	tester.Frame()
	tester.Key(ui.Ctrl, ui.KeyC)
	tester.Frame()
	if copied := tester.Clipboard(); !strings.Contains(copied, "$ git remote -v") {
		t.Fatalf("command was not copied intact: %q", copied)
	} else if !strings.Contains(copied, "origin  https://example.com/team/project.git") {
		t.Fatalf("output was not copied intact: %q", copied)
	}
}

func TestVisibleCommandLogRequestsRepaintForNewCommands(t *testing.T) {
	a, _, _ := realCommandHarness(t)
	a.openCommandLog()
	invalidations := 0
	previous := a.update
	a.update = func(fn func()) {
		invalidations++
		previous(fn)
	}
	logger := repository.NewCommandLog()
	a.attachRepositoryCommandLog(logger)
	entries := logger.BeginUserCommand("printf queued-update")
	if invalidations == 0 {
		t.Fatal("a running command did not schedule a live repaint")
	}
	entries("completed", false)
	if invalidations < 2 {
		t.Fatalf("completion did not update the visible log; notifications = %d", invalidations)
	}
	a.closeCommandLog()
	logger.Record("git audit", "done", false)
	if got := invalidations; got != 2 {
		t.Fatalf("closed log was repainted unnecessarily: %d", got)
	}
}
