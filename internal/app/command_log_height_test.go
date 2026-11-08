package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// Max-height alone lets the empty/search-empty states shrink the entire
// dialog. Every view should occupy the same modal height instead.
func TestCommandLogModalHeightDoesNotDependOnContent(t *testing.T) {
	a := newApp(fakeOpener{})
	a.commandLog = repository.NewCommandLog()
	for index := 0; index < 24; index++ {
		command := fmt.Sprintf("git log --oneline --skip=%d", index)
		a.commandLog.BeginOrigin(command, repository.CommandOriginGit)("many lines of output", false)
	}
	tester := ui.NewTester(a.view, 800, 600)
	a.openCommandLog()
	a.commandLogUser = false
	tester.Frame()
	populated, ok := tester.Find("Done")
	if !ok {
		t.Fatal("command log footer is missing")
	}

	// An empty user section must not collapse the modal to a small card.
	a.commandLogUser = true
	tester.Frame()
	empty, ok := tester.Find("Done")
	if !ok {
		t.Fatal("empty command log footer is missing")
	}
	if diff := absFloat32(populated.Y - empty.Y); diff > 1 {
		t.Fatalf("empty section changed the modal height: populated=%+v empty=%+v", populated, empty)
	}

	a.commandLogUser = false
	a.commandLogQuery = "no-results"
	tester.Frame()
	filtered, ok := tester.Find("Done")
	if !ok {
		t.Fatal("filtered command log footer is missing")
	}
	if diff := absFloat32(populated.Y - filtered.Y); diff > 1 {
		t.Fatalf("empty search results changed the modal height: populated=%+v filtered=%+v", populated, filtered)
	}
}
