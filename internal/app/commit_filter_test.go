package app

import (
	"fmt"
	"image/color"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The History list's filter icon opens a modal that narrows the list through
// Git. The list opens on the branch checked out, and the Graph keeps its own
// filter while History is narrowed.
func TestHistoryFilterNarrowsTheCommitList(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the commit filter: %v", err)
	}
	h.tester.Frame()
	for _, want := range []string{"Filter commits", "Message", "Author", "Branch", "main (local)"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the filter modal is missing %q: %q", want, h.texts())
		}
	}
	// The page opens on the branch checked out, so its filter reads that
	// branch from the start rather than every branch.
	if got := h.app.historyFilter.applied.Branch; got != "main" {
		t.Fatalf("the filter reads %q, want the branch checked out", got)
	}
	if err := h.tester.Click("Search commit messages"); err != nil {
		t.Fatalf("focus the message search: %v", err)
	}
	h.tester.Type("Initial")
	h.tester.Frame()
	if err := h.tester.Click("Search commits"); err != nil {
		t.Fatalf("apply the filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the filtered history", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) == 1
	})
	h.tester.Frame()
	if h.app.filterOpen {
		t.Fatal("the filter modal stayed open after Search")
	}
	if !slices.Contains(h.texts(), "Initial commit") {
		t.Fatalf("the filtered history does not show the match: %q", h.texts())
	}
	if slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("the filtered history still shows a commit the filter left out: %q", h.texts())
	}

	// The Graph keeps its own filter, so narrowing History must not hide a
	// lane there.
	h.press(t, ui.Key3)
	if !slices.Contains(h.texts(), "Second commit") || !slices.Contains(h.texts(), "Initial commit") {
		t.Fatalf("the Graph lost commits when History was filtered: %q", h.texts())
	}

	// Clear puts every commit back, still from the same filter modal.
	h.press(t, ui.Key2)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("reopen the commit filter: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Clear filters"); err != nil {
		t.Fatalf("clear the filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.historyFilter.narrows() {
		t.Fatalf("Clear left the filter applied: %#v", h.app.historyFilter.applied)
	}
	if !slices.Contains(h.texts(), "Second commit") || !slices.Contains(h.texts(), "Initial commit") {
		t.Fatalf("clearing the filter did not show every commit: %q", h.texts())
	}
}

// checkOut moves the snapshot to another branch, as an action that checks a
// branch out does once its read lands.
func checkOut(h *harness, name string) {
	for i := range h.app.snapshot.Branches {
		branch := &h.app.snapshot.Branches[i]
		branch.Head = branch.Name == name && !branch.DetachedHead
	}
	h.tester.Frame()
}

// Both commit pages open on the branch checked out rather than on the whole
// repository, so History reads what the Branches page shows for the branch
// that is out. Each page holds its own default.
func TestCommitPagesOpenOnTheBranchCheckedOut(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	h.waitFor(t, "the branch's history", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) > 0
	})
	if got := h.app.historyFilter.applied.Branch; got != "main" {
		t.Fatalf("History reads %q, want the branch checked out", got)
	}
	h.press(t, ui.Key3)
	h.waitFor(t, "the graph's history", func() bool {
		return !h.app.graphFilter.loading && len(h.app.graphFilter.matches) > 0
	})
	if got := h.app.graphFilter.applied.Branch; got != "main" {
		t.Fatalf("the Graph reads %q, want the branch checked out", got)
	}
}

// The default follows a checkout: History names the branch that is out, not
// the one that was when the page first opened.
func TestTheDefaultFilterFollowsACheckout(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	h.waitFor(t, "the branch's history", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) > 0
	})
	checkOut(h, "feature")
	h.waitFor(t, "the new branch's history", func() bool {
		return h.app.historyFilter.applied.Branch == "feature" && !h.app.historyFilter.loading
	})
}

// Clearing the filter hands the list back: the branch default is the user's
// to remove and stays removed, even across a checkout.
func TestAClearedFilterStaysCleared(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	h.waitFor(t, "the branch's history", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) > 0
	})
	if err := h.tester.Click("Clear all filters"); err != nil {
		t.Fatalf("clear the filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.historyFilter.narrows() {
		t.Fatalf("Clear left the filter applied: %#v", h.app.historyFilter.applied)
	}
	checkOut(h, "feature")
	h.tester.Frame()
	h.settle(t)
	if h.app.historyFilter.narrows() {
		t.Fatalf("the branch default came back after a checkout: %#v", h.app.historyFilter.applied)
	}
}

// The filter button takes the accent while a filter narrows the list and
// gives it back when the list is whole, so the reason a list is short sits
// on screen and not behind a click.
func TestTheFilterButtonTakesTheAccentWhileFiltered(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	h.waitFor(t, "the branch's history", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) > 0
	})
	// Park the pointer on the window's corner before every ink read: a
	// click leaves it where the clear button was, and the button that moves
	// into that seat would sample as hovered.
	h.tester.Move(2, 2)
	narrowed := buttonInk(t, h.tester, "Filter commits", 0.5, 0.5)
	narrowedFill := buttonInk(t, h.tester, "Filter commits", 0.25, 0.25)

	if err := h.tester.Click("Clear all filters"); err != nil {
		t.Fatalf("clear the filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.tester.Move(2, 2)
	whole := buttonInk(t, h.tester, "Filter commits", 0.5, 0.5)
	wholeFill := buttonInk(t, h.tester, "Filter commits", 0.25, 0.25)

	if narrowed == whole {
		t.Fatalf("the filter button's glyph draws the same ink (%v) with and without a filter", narrowed)
	}
	if narrowedFill == wholeFill {
		t.Fatalf("the filter button's face draws the same ink (%v) with and without a filter", narrowedFill)
	}
}

// buttonInk is the colour the frame draws at fx,fy of a button's box, so a
// glyph (½,½) and the face beside it (¼,¼) both read what the button wears.
func buttonInk(t *testing.T, tt *ui.Tester, label string, fx, fy float32) color.RGBA {
	t.Helper()
	tt.Frame()
	rect, ok := tt.Find(label)
	if !ok {
		t.Fatalf("the frame draws no %q", label)
	}
	img := tt.Image()
	x, y := int(rect.X+rect.W*fx), int(rect.Y+rect.H*fy)
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		t.Fatalf("%q sits off the frame at %d,%d", label, x, y)
	}
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// A branch's commit list has the same filter, without a branch choice: the
// branch is already the one on screen.
func TestBranchCommitsFilterHasNoBranchChoice(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key4)
	h.settle(t)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the branch commit filter: %v", err)
	}
	h.tester.Frame()
	if slices.Contains(h.texts(), "All branches") {
		t.Fatalf("the branch commit filter offers a branch choice: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "Author") {
		t.Fatalf("the branch commit filter has no author field: %q", h.texts())
	}
	if err := h.tester.Click("Search commit messages"); err != nil {
		t.Fatalf("focus the message search: %v", err)
	}
	h.tester.Type("Initial")
	h.tester.Frame()
	if err := h.tester.Click("Search commits"); err != nil {
		t.Fatalf("apply the branch filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the filtered branch commits", func() bool {
		return !h.app.branchCommitsLoading && len(h.app.branchCommits) == 1
	})
	h.tester.Frame()
	if !h.app.branchFilterActive() {
		t.Fatalf("the branch filter was not applied: %#v", h.app.branchFilter)
	}
	if got := h.app.branchCommits[0].Subject; got != "Initial commit" {
		t.Fatalf("the branch's filtered commits start with %q, want Initial commit", got)
	}

	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("reopen the branch commit filter: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Clear filters"); err != nil {
		t.Fatalf("clear the branch filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the branch's whole history", func() bool {
		return !h.app.branchCommitsLoading && len(h.app.branchCommits) == 2
	})
	if h.app.branchFilterActive() {
		t.Fatalf("Clear left the branch filter applied: %#v", h.app.branchFilter)
	}
}

// The Graph has the same filter as History and keeps its own: narrowing the
// graph leaves History whole, and the graph stays narrowed until cleared.
func TestGraphFilterNarrowsTheGraph(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key3)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the graph filter: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Narrow the graph by message, author and branch.") {
		t.Fatalf("the graph filter does not name the graph: %q", h.texts())
	}
	if err := h.tester.Click("Search commit messages"); err != nil {
		t.Fatalf("focus the message search: %v", err)
	}
	h.tester.Type("Initial")
	h.tester.Frame()
	if err := h.tester.Click("Search commits"); err != nil {
		t.Fatalf("apply the graph filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the filtered graph", func() bool {
		return !h.app.graphFilter.loading && len(h.app.graphFilter.matches) == 1
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Initial commit") {
		t.Fatalf("the filtered graph does not show the match: %q", h.texts())
	}
	if slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("the filtered graph still shows a commit the filter left out: %q", h.texts())
	}

	// History is a different list, so nothing of the graph's narrowing
	// reaches it. It still holds only its own default, the branch checked
	// out.
	h.press(t, ui.Key2)
	got := h.app.historyFilter.applied
	if got.Query != "" || got.Author != "" || got.Branch != "main" {
		t.Fatalf("the graph filter reached History: %#v", got)
	}
	if !slices.Contains(h.texts(), "Second commit") || !slices.Contains(h.texts(), "Initial commit") {
		t.Fatalf("History lost commits when the Graph was filtered: %q", h.texts())
	}

	// The graph keeps its own filter across a visit to History.
	h.press(t, ui.Key3)
	if !h.app.graphFilter.narrows() {
		t.Fatal("the graph lost its filter on the way back")
	}
	if slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("the graph forgot its filter: %q", h.texts())
	}
}

// Clearing the graph's filter leaves History's alone: each page's empty
// state acts on the list that is empty.
func TestGraphEmptyFilterClearsOnlyTheGraph(t *testing.T) {
	h := menuTestApp(t)

	// Narrow History to one commit.
	h.press(t, ui.Key2)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the History filter: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Search commit messages"); err != nil {
		t.Fatalf("focus the message search: %v", err)
	}
	h.tester.Type("Initial")
	h.tester.Frame()
	if err := h.tester.Click("Search commits"); err != nil {
		t.Fatalf("apply the History filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the filtered History", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) == 1
	})

	// A graph filter with no matches shows the empty state.
	h.press(t, ui.Key3)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the graph filter: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Search commit messages"); err != nil {
		t.Fatalf("focus the message search: %v", err)
	}
	h.tester.Type("nothing matches this")
	h.tester.Frame()
	if err := h.tester.Click("Search commits"); err != nil {
		t.Fatalf("apply the graph filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the empty graph", func() bool {
		return !h.app.graphFilter.loading && len(h.app.graphFilter.matches) == 0
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "No commits match the filter.") {
		t.Fatalf("the empty graph explains nothing: %q", h.texts())
	}

	if err := h.tester.Click("Clear filters"); err != nil {
		t.Fatalf("clear the graph filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.graphFilter.narrows() {
		t.Fatalf("Clear left the graph filter applied: %#v", h.app.graphFilter.applied)
	}
	if slices.Contains(h.texts(), "No commits match the filter.") {
		t.Fatalf("the graph stayed empty after clearing: %q", h.texts())
	}

	// History's filter is untouched by the graph's empty state.
	h.press(t, ui.Key2)
	if !h.app.historyFilter.narrows() {
		t.Fatal("clearing the graph filter cleared History's too")
	}
	if slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("History forgot its own filter: %q", h.texts())
	}
}

// A repository can have more authors than fit in the window, so the author
// popup scrolls rather than running off the top of the page where its lower
// rows could not be reached.
func TestCommitFilterAuthorListScrolls(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the commit filter: %v", err)
	}
	h.settle(t)

	// Stand in for a long history: the modal reads the repository's authors
	// when it opens, and 40 do not fit under one field.
	authors := make([]string, 0, 40)
	for i := range 40 {
		authors = append(authors, fmt.Sprintf("Author %02d", i))
	}
	h.app.authors = authors
	h.app.authorsLoading = false
	h.tester.Frame()

	if err := h.tester.Click("Filter by author"); err != nil {
		t.Fatalf("open the author list: %v", err)
	}
	h.tester.Frame()
	first, ok := h.tester.Find("Author 00")
	if !ok {
		t.Fatalf("the author list does not show its first author: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "Author 39") {
		t.Fatalf("the author list does not build its last author: %q", h.texts())
	}
	// Scroll over the popup itself, as the wheel does, and watch the
	// first row move: rows below the panel are culled and sized later.
	// Move first, so the pointer's highlight settles before the wheel.
	x, y := first.X+first.W/2, first.Y+first.H/2
	h.tester.Move(x, y)
	h.tester.Frame()
	h.tester.Scroll(x, y, 0, 300)
	h.tester.Frame()
	scrolled, ok := h.tester.Find("Author 00")
	if !ok {
		t.Fatalf("the author list lost its first author while scrolling")
	}
	if scrolled.Y >= first.Y {
		t.Fatalf("the author list did not scroll: its first row stayed at y=%v", scrolled.Y)
	}
}

// The list header carries a small clear button beside the filter whenever a
// filter narrows the list, so every field can be dropped without reopening
// the modal. The branch the list opens on is a filter like any other, so the
// button is there from the start to drop that default with the rest.
func TestCommitHeaderClearsEveryFilter(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.Key2)
	if !h.tester.HasText("Clear all filters") {
		t.Fatalf("the header has no clear button beside the default filter: %q", h.texts())
	}
	if err := h.tester.Click("Filter commits"); err != nil {
		t.Fatalf("open the commit filter: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Search commit messages"); err != nil {
		t.Fatalf("focus the message search: %v", err)
	}
	h.tester.Type("Initial")
	h.tester.Frame()
	if err := h.tester.Click("Search commits"); err != nil {
		t.Fatalf("apply the filter: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the filtered history", func() bool {
		return !h.app.historyFilter.loading && len(h.app.historyFilter.matches) == 1
	})
	h.tester.Frame()
	if !h.tester.HasText("Clear all filters") {
		t.Fatalf("the header has no clear button while filtered: %q", h.texts())
	}
	if err := h.tester.Click("Clear all filters"); err != nil {
		t.Fatalf("clear the filter from the header: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.historyFilter.narrows() {
		t.Fatalf("the header clear left the filter applied: %#v", h.app.historyFilter.applied)
	}
	if !slices.Contains(h.texts(), "Second commit") || !slices.Contains(h.texts(), "Initial commit") {
		t.Fatalf("the header clear did not restore every commit: %q", h.texts())
	}
}
