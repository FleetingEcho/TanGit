package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func changesStatusFixture() []repository.File {
	return []repository.File{
		{Path: "both.txt", Status: "MM", Staged: true, Unstaged: true, Tracked: true},
		{Path: "unstaged.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "untracked.txt", Status: "??", Unstaged: true, Added: true},
		{Path: "deleted.txt", Status: "D ", Staged: true, Tracked: true, Deleted: true},
	}
}

func TestChangesStatusFiltersMatchRepositoryFlags(t *testing.T) {
	files := changesStatusFixture()
	for _, test := range []struct {
		filter changesStatusFilter
		want   []string
	}{
		{filter: changesStatusAll, want: []string{"both.txt", "unstaged.txt", "untracked.txt", "deleted.txt"}},
		{filter: changesStatusStaged, want: []string{"both.txt", "deleted.txt"}},
		{filter: changesStatusUnstaged, want: []string{"both.txt", "unstaged.txt", "untracked.txt"}},
		{filter: changesStatusTracked, want: []string{"both.txt", "unstaged.txt", "deleted.txt"}},
		{filter: changesStatusUntracked, want: []string{"untracked.txt"}},
	} {
		visible := changesFilesWithStatus(files, test.filter)
		got := make([]string, len(visible))
		for i, file := range visible {
			got[i] = file.Path
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("%s returned %v, want %v", test.filter.label(), got, test.want)
		}
	}
}

func TestChangesStatusFiltersPreserveCompatibleSelectionAndClearHiddenDiff(t *testing.T) {
	h := menuTestApp(t)
	h.app.snapshot.Files = changesStatusFixture()
	h.app.fileTreeMode = false
	h.app.changeRows = h.app.changeRowsFrom(h.app.snapshot.Files)
	h.app.selectedFile = changeRowIndex(h.app.changeRows, "both.txt", false)
	h.app.diffState = &editor.DiffState{}
	h.app.diffGeneration = 1

	h.app.selectChangesStatusFilter(changesStatusStaged)
	selected, ok := h.app.selectedChange()
	if !ok || selected.file.Path != "both.txt" || selected.sideKey() != "unstaged" {
		t.Fatalf("compatible partial-stage selection = %+v, want both.txt on its original side", selected)
	}
	if h.app.diffState == nil || h.app.diffGeneration != 1 {
		t.Fatal("preserving a compatible file and side should not cancel its diff")
	}
	if h.app.changesFiltersOpen {
		t.Fatal("a status choice did not close the filter menu")
	}

	h.app.fileStatusFilter = changesStatusAll
	h.app.changeRows = h.app.changeRowsFrom(h.app.snapshot.Files)
	h.app.selectedFile = changeRowIndex(h.app.changeRows, "unstaged.txt", false)
	h.app.diffState = &editor.DiffState{}
	h.app.selectChangesStatusFilter(changesStatusTracked)
	selected, ok = h.app.selectedChange()
	if !ok || selected.file.Path != "unstaged.txt" || h.app.diffState == nil {
		t.Fatal("a compatible selection should retain its file and existing diff")
	}

	h.app.selectChangesStatusFilter(changesStatusUntracked)
	if _, ok := h.app.selectedChange(); ok || h.app.selectedFile != -1 || h.app.diffState != nil {
		t.Fatal("a hidden selected file retained selection or diff state")
	}
	if h.app.reviewedFiles == nil {
		h.app.reviewedFiles = make(map[string]bool)
	}
	h.app.reviewedFiles["untracked.txt"] = true
	h.app.reviewFilter = true
	if h.app.hasUnreviewedFiles() {
		t.Fatal("Next unreviewed should only consider files allowed by the status filter")
	}
	h.app.ignoreUntracked = true
	h.app.clearChangesFileFilters()
	if h.app.fileStatusFilter != changesStatusAll || h.app.reviewFilter || h.app.ignoreUntracked {
		t.Fatal("clear did not reset status, ignore-untracked and review filters")
	}
	if rows := h.app.visibleReviewFiles(h.app.snapshot.Files); len(rows) != len(h.app.snapshot.Files) {
		t.Fatalf("clear restored %d files, want %d", len(rows), len(h.app.snapshot.Files))
	}
}

func TestChangesStatusFiltersResetForANewRepository(t *testing.T) {
	h := menuTestApp(t)
	h.app.fileStatusFilter = changesStatusUntracked
	h.app.reviewFilter = true
	h.app.resetRepositoryState(h.app.snapshot)
	if h.app.fileStatusFilter != changesStatusAll || h.app.reviewFilter {
		t.Fatal("a new repository inherited the old status or review filter")
	}
}
