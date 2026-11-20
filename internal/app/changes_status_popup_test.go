package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func visibleChangePaths(a *app) []string {
	paths := []string{}
	for _, row := range a.changeRows {
		if row.isFile() {
			paths = append(paths, row.file.Path)
		}
	}
	return paths
}

func openChangesFilterPopover(t *testing.T, h *harness) {
	t.Helper()
	if !h.app.changesFiltersOpen {
		if err := h.tester.Click("File filters"); err != nil {
			t.Fatalf("open file filters: %v", err)
		}
	}
	h.tester.Frame()
	if !h.app.changesFiltersOpen || !h.tester.HasText("Show only staged files") {
		t.Fatal("file-filter popup did not open its status choices")
	}
}

func TestChangesStatusPopoverMovesUnreviewedAndFiltersFiles(t *testing.T) {
	h := menuTestApp(t)
	if _, found := h.tester.AccessByRole(ui.RoleCheckBox, "Unreviewed"); found {
		t.Fatal("Unreviewed should not remain as a standalone row in the file list")
	}
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Next unreviewed"); !found {
		t.Fatal("moving Unreviewed removed its navigation action")
	}

	openChangesFilterPopover(t, h)
	for _, label := range []string{"Show only staged files", "Show only unstaged files", "Show only tracked files", "Show only untracked files", "No filter"} {
		if _, found := h.tester.AccessByRole(ui.RoleRadio, label); !found {
			t.Errorf("status popup is missing %q", label)
		}
	}
	if _, found := h.tester.AccessByRole(ui.RoleCheckBox, "Unreviewed"); !found {
		t.Fatal("Unreviewed checkbox is not in the file-filter popup")
	}
	if err := h.tester.Click("Unreviewed"); err != nil {
		t.Fatalf("enable Unreviewed: %v", err)
	}
	h.tester.Frame()
	if !h.app.review.filter || !h.app.changesFiltersOpen {
		t.Fatal("review checkbox should stay enabled while the popup remains open")
	}
	if err := h.tester.Click("Show only staged files"); err != nil {
		t.Fatalf("choose staged files: %v", err)
	}
	h.tester.Frame()
	if got, want := visibleChangePaths(h.app), []string{"one.txt"}; !slices.Equal(got, want) {
		t.Fatalf("staged-only paths = %v, want %v", got, want)
	}
	if h.app.changesFiltersOpen {
		t.Fatal("choosing a single status should close the popup")
	}
	openChangesFilterPopover(t, h)
	if err := h.tester.Click("Show only untracked files"); err != nil {
		t.Fatalf("choose untracked files: %v", err)
	}
	h.tester.Frame()
	if got, want := visibleChangePaths(h.app), []string{"two.txt"}; !slices.Equal(got, want) {
		t.Fatalf("untracked-only paths = %v, want %v", got, want)
	}
	openChangesFilterPopover(t, h)
	if err := h.tester.Click("Show only tracked files"); err != nil {
		t.Fatalf("choose tracked files: %v", err)
	}
	h.tester.Frame()
	if got, want := visibleChangePaths(h.app), []string{"one.txt"}; !slices.Equal(got, want) {
		t.Fatalf("tracked-only paths = %v, want %v", got, want)
	}
}

func TestChangesStatusPopoverUnreviewedEmptyStateAndClear(t *testing.T) {
	h := menuTestApp(t)
	h.app.review.files = map[string]bool{"one.txt": true}
	openChangesFilterPopover(t, h)
	if err := h.tester.Click("Unreviewed"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.review.filter {
		t.Fatal("Unreviewed checkbox did not bind to the review filter")
	}
	if err := h.tester.Click("Show only staged files"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.tester.HasText("No files match the current filters") {
		t.Fatal("a filtered-out file set did not explain the empty state")
	}
	if h.app.selectedFile != -1 || h.app.diffState != nil {
		t.Fatal("the hidden reviewed file was still selected or showing a diff")
	}
	if err := h.tester.Click("Clear filters"); err != nil {
		t.Fatalf("clear filters from the empty state: %v", err)
	}
	h.tester.Frame()
	if len(visibleChangePaths(h.app)) != 2 || h.app.review.filter || h.app.fileStatusFilter != changesStatusAll {
		t.Fatal("clearing filters did not restore the visible files")
	}
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Next unreviewed"); !found {
		t.Fatal("clearing filters removed the review traversal action")
	}
}
