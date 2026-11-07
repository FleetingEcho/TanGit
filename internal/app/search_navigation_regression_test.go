package app

import (
	"testing"

	"lazygitapp/internal/repository"
)

func TestWrappedSearchRowEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		current   int
		count     int
		direction int
		wantRow   int
		wantOK    bool
	}{
		{name: "no results", current: -1, count: 0, direction: 1, wantRow: -1},
		{name: "no direction", current: 1, count: 3, direction: 0, wantRow: -1},
		{name: "invalid selection forward starts first", current: -1, count: 3, direction: 1, wantRow: 0, wantOK: true},
		{name: "invalid selection backward starts last", current: 3, count: 3, direction: -1, wantRow: 2, wantOK: true},
		{name: "forward wraps", current: 2, count: 3, direction: 1, wantRow: 0, wantOK: true},
		{name: "backward wraps", current: 0, count: 3, direction: -1, wantRow: 2, wantOK: true},
		{name: "forward moves one result for larger direction", current: 0, count: 3, direction: 20, wantRow: 1, wantOK: true},
		{name: "backward moves one result for larger direction", current: 1, count: 3, direction: -20, wantRow: 0, wantOK: true},
		{name: "single result wraps to itself", current: 0, count: 1, direction: 1, wantRow: 0, wantOK: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row, ok := wrappedSearchRow(test.current, test.count, test.direction)
			if row != test.wantRow || ok != test.wantOK {
				t.Fatalf("wrappedSearchRow(%d, %d, %d) = (%d, %v), want (%d, %v)",
					test.current, test.count, test.direction, row, ok, test.wantRow, test.wantOK)
			}
		})
	}
}

func TestBranchSearchNavigationWrapsToFilteredSourceRows(t *testing.T) {
	a := newApp(fakeOpener{})
	a.snapshot.Branches = []repository.Branch{
		{Name: "topic-one", DisplayName: "topic-one"},
		{Name: "main", DisplayName: "main"},
		{Name: "topic-two", DisplayName: "topic-two"},
	}
	a.branchSearch = "topic"
	if got := a.visibleBranches(); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("filtered source rows = %v, want [0 2]", got)
	}

	a.selectBranchRow(1)
	if a.selectedBranch != 2 {
		t.Fatalf("initial source selection = %d, want 2", a.selectedBranch)
	}
	a.navigateBranchSearch(1)
	if a.selectedBranchRow != 0 || a.selectedBranch != 0 {
		t.Fatalf("forward wrap selected visible/source %d/%d, want 0/0",
			a.selectedBranchRow, a.selectedBranch)
	}
	a.navigateBranchSearch(-1)
	if a.selectedBranchRow != 1 || a.selectedBranch != 2 {
		t.Fatalf("backward wrap selected visible/source %d/%d, want 1/2",
			a.selectedBranchRow, a.selectedBranch)
	}
}

func TestQuickSearchNavigationNoMatchAndEmptyQueryAreNoOps(t *testing.T) {
	a := newApp(fakeOpener{})
	a.snapshot.Branches = []repository.Branch{
		{Name: "main", DisplayName: "main"},
		{Name: "feature", DisplayName: "feature"},
	}
	a.selectBranchRow(1)
	wantRow, wantSource := a.selectedBranchRow, a.selectedBranch

	a.branchSearch = "does-not-exist"
	a.navigateBranchSearch(1)
	if a.selectedBranchRow != wantRow || a.selectedBranch != wantSource {
		t.Fatalf("no-match navigation moved selection from %d/%d to %d/%d",
			wantRow, wantSource, a.selectedBranchRow, a.selectedBranch)
	}

	a.branchSearch = " \t "
	a.navigateBranchSearch(-1)
	if a.selectedBranchRow != wantRow || a.selectedBranch != wantSource {
		t.Fatalf("empty-query navigation moved selection from %d/%d to %d/%d",
			wantRow, wantSource, a.selectedBranchRow, a.selectedBranch)
	}
}

func TestChangesSearchButtonsWrapAndExposeAccessibleLabels(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "match-one.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "hidden.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "match-two.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	h.app.fileSearch = "match"
	h.tester.Frame()

	for _, label := range []string{"Previous file search result", "Next file search result"} {
		if _, ok := h.tester.Find(label); !ok {
			t.Fatalf("search navigation has no accessible %q button; texts %q", label, h.texts())
		}
	}
	first := changeRowIndex(h.app.changeRows, "match-one.txt", false)
	last := changeRowIndex(h.app.changeRows, "match-two.txt", false)
	if first < 0 || last < 0 || first == last {
		t.Fatalf("filtered changes rows do not contain both matches: %#v", h.app.changeRows)
	}
	h.app.selectedFile = last

	if err := h.tester.Click("Next file search result"); err != nil {
		t.Fatalf("activate next-result icon: %v", err)
	}
	if h.app.selectedFile != first {
		t.Fatalf("next at the last match selected row %d, want first row %d", h.app.selectedFile, first)
	}
	if err := h.tester.Click("Previous file search result"); err != nil {
		t.Fatalf("activate previous-result icon: %v", err)
	}
	if h.app.selectedFile != last {
		t.Fatalf("previous at the first match selected row %d, want last row %d", h.app.selectedFile, last)
	}
}
