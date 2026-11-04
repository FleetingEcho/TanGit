package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// listSearchFixture gives every list two candidates, with the wanted item
// second. Proving the selected source becomes 1 catches accidental use of the
// filtered row as a snapshot index.
func listSearchFixture(t *testing.T) *harness {
	t.Helper()
	h := menuTestApp(t)
	h.app.snapshot.Remotes = append(h.app.snapshot.Remotes, repository.Remote{
		Name: "upstream", URLs: []string{"https://example.com/upstream.git"},
	})
	h.app.snapshot.Stashes = []repository.Stash{
		{Name: "WIP on main", Hash: "stash-1", Index: 3},
		{Name: "release preparation", Hash: "stash-2", Index: 7},
	}
	h.app.snapshot.Worktrees = []repository.Worktree{
		{Name: "project", Path: "/work/project", Branch: "main", Current: true, Main: true},
		{Name: "bugfix", Path: "/work/bugfix", Branch: "feature/fix"},
	}
	h.app.snapshot.Submodules = []repository.Submodule{
		{Name: "library", Path: "vendor/library", URL: "https://example.com/library.git"},
		{Name: "editor", Path: "tools/editor", URL: "https://example.com/editor.git"},
	}
	h.app.snapshot.Reflog = append(h.app.snapshot.Reflog, repository.Commit{
		Hash: "d3d3d3d3", ShortHash: "d3d3d3d", Subject: "commit: release notes",
		AuthorName: "Ada", AuthorEmail: "ada@example.com", UnixTimestamp: 2,
	})
	h.app.selectedStashRow, h.app.selectedStash = 0, 0
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	h.app.selectedSubmoduleRow, h.app.selectedSubmodule = 0, 0
	h.app.selectedReflogRow, h.app.selectedReflog = 0, 0
	return h
}

func TestSlashSearchSelectsTheMatchingSourceItem(t *testing.T) {
	tests := []struct {
		name     string
		section  int
		query    string
		selected func(*app) int
		row      func(*app) int
	}{
		{
			name: "branches", section: sectionBranches, query: "feature",
			selected: func(a *app) int { return a.selectedBranch },
			row:      func(a *app) int { return a.selectedBranchRow },
		},
		{
			name: "remotes by URL", section: sectionRemotes, query: "upstream.git",
			selected: func(a *app) int { return a.selectedRemote },
			row:      func(a *app) int { return a.selectedRemoteRow },
		},
		{
			name: "stashes", section: sectionStash, query: "release prep",
			selected: func(a *app) int { return a.selectedStash },
			row:      func(a *app) int { return a.selectedStashRow },
		},
		{
			name: "tags by annotation", section: sectionTags, query: "interim build",
			selected: func(a *app) int { return a.selectedTag },
			row:      func(a *app) int { return a.selectedTagRow },
		},
		{
			name: "worktrees by path", section: sectionWorktrees, query: "/work/bugfix",
			selected: func(a *app) int { return a.selectedWorktree },
			row:      func(a *app) int { return a.selectedWorktreeRow },
		},
		{
			name: "submodules by path", section: sectionSubmodules, query: "tools/editor",
			selected: func(a *app) int { return a.selectedSubmodule },
			row:      func(a *app) int { return a.selectedSubmoduleRow },
		},
		{
			name: "reflog by operation", section: sectionReflog, query: "release notes",
			selected: func(a *app) int { return a.selectedReflog },
			row:      func(a *app) int { return a.selectedReflogRow },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := listSearchFixture(t)
			h.app.workspaceTab = test.section
			h.tester.Frame()
			h.tester.Key(0, ui.KeySlash)
			h.tester.Frame()
			h.tester.Type(test.query)
			h.tester.Frame()
			if got := test.selected(h.app); got != 1 {
				t.Fatalf("filtered source selection = %d, want the second item; texts %q", got, h.texts())
			}
			if got := test.row(h.app); got != 0 {
				t.Fatalf("the sole match occupies visible row %d, want 0", got)
			}
		})
	}
}

func TestFilteredStashMenuUsesTheSourceStash(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.snapshot.Stashes = []repository.Stash{
		{Name: "WIP on main", Hash: "stash-1", Index: 3},
		{Name: "release preparation", Hash: "stash-2", Index: 7},
	}
	h.app.selectedStashRow, h.app.selectedStash = 0, 0
	h.app.workspaceTab = sectionStash
	h.tester.Frame()

	h.tester.Key(0, ui.KeySlash)
	h.tester.Frame()
	h.tester.Type("release")
	h.tester.Frame()
	h.rightClick(t, "release preparation")
	if err := h.tester.Click("Apply to working tree"); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the filtered stash apply", func() bool { return calls.has("stash-apply:7:false") })
	if calls.has("stash-apply:3:false") {
		t.Fatal("the filtered row applied the stash that was hidden above it")
	}
}

func TestListSearchCanBeEmptiedAndCleared(t *testing.T) {
	h := listSearchFixture(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()
	h.tester.Key(0, ui.KeySlash)
	h.tester.Frame()
	h.tester.Type("does-not-exist")
	h.tester.Frame()
	if h.app.selectedBranch != -1 || h.app.selectedBranchRow != -1 {
		t.Fatalf("an empty search retained hidden selection source=%d row=%d",
			h.app.selectedBranch, h.app.selectedBranchRow)
	}
	if !h.tester.HasText("No local branches match this search") || h.app.crash != "" {
		t.Fatalf("empty search was not contained: crash=%q texts=%q", h.app.crash, h.texts())
	}
	if err := h.tester.Click("Clear branch search"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.branchSearch != "" || h.app.selectedBranch != 0 || h.app.selectedBranchRow != 0 {
		t.Fatalf("clear restored query=%q source=%d row=%d", h.app.branchSearch,
			h.app.selectedBranch, h.app.selectedBranchRow)
	}
}

func TestSlashSearchesChangesAndOpensStructuredCommitFilters(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(0, ui.KeySlash)
	h.tester.Frame()
	h.tester.Type("two.txt")
	if h.app.fileSearch != "two.txt" {
		t.Fatalf("Changes search = %q, want typed path", h.app.fileSearch)
	}

	for _, section := range []int{sectionHistory, sectionGraph} {
		h.app.workspaceTab = section
		h.tester.Frame()
		h.tester.Key(0, ui.KeySlash)
		h.tester.Frame()
		if !h.app.filterOpen || h.app.filterTarget != section {
			t.Fatalf("section %d search did not open its structured filter", section)
		}
		h.app.filterOpen = false
		h.tester.Frame()
	}
}

func TestFilteredRowHelpersNeverExposeHiddenSources(t *testing.T) {
	items := []string{"main", "feature", "release"}
	rows := filteredSourceRows(len(items), "FEATURE", func(source int) string {
		return strings.Join([]string{items[source], "desktop work"}, "\x00")
	})
	if len(rows) != 1 || rows[0] != 1 {
		t.Fatalf("matching source rows = %v, want [1]", rows)
	}
	source, row := -1, -1
	selectFilteredRow(rows, 0, &row, &source)
	if source != 1 || row != 0 {
		t.Fatalf("selected source=%d row=%d", source, row)
	}
	selectFilteredRow(nil, 0, &row, &source)
	if source != -1 || row != -1 {
		t.Fatalf("empty result retained source=%d row=%d", source, row)
	}
}
