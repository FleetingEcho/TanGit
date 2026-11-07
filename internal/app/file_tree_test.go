package app

import (
	"reflect"
	"strings"
	"testing"

	"lazygitapp/internal/repository"
)

func TestBuildFileTreeRowsKeepsSourceIdentityAndCollapsesParents(t *testing.T) {
	paths := []string{"z.txt", "src/ui/app.go", "README.md", "src/ui/tree/tree.go", "src/main.go"}
	rows := buildFileTreeRows(paths, true, nil, "commit")
	var got []string
	for _, row := range rows {
		got = append(got, row.path)
	}
	want := []string{"README.md", "src", "src/main.go", "src/ui", "src/ui/app.go", "src/ui/tree", "src/ui/tree/tree.go", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tree paths = %v, want %v", got, want)
	}
	for _, row := range rows {
		if !row.isDirectory() && paths[row.source] != row.path {
			t.Fatalf("row %q points at source %d (%q)", row.path, row.source, paths[row.source])
		}
	}

	collapsed := map[string]bool{fileTreeCollapseKey("commit", "src"): true}
	rows = buildFileTreeRows(paths, true, collapsed, "commit")
	got = got[:0]
	for _, row := range rows {
		got = append(got, row.path)
	}
	want = []string{"README.md", "src", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collapsed tree paths = %v, want %v", got, want)
	}

	// Collapsing a nested directory leaves its siblings visible.
	collapsed = map[string]bool{fileTreeCollapseKey("commit", "src/ui"): true}
	rows = buildFileTreeRows(paths, true, collapsed, "commit")
	got = got[:0]
	for _, row := range rows {
		got = append(got, row.path)
	}
	want = []string{"README.md", "src", "src/main.go", "src/ui", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nested collapsed paths = %v, want %v", got, want)
	}
}

func nestedTreeCommitFiles() []repository.CommitFile {
	return []repository.CommitFile{
		{Path: "README.md", Status: "M"},
		{Path: "src/main.go", Status: "M"},
		{Path: "src/ui/app.go", Status: "A"},
		{Path: "z.txt", Status: "D"},
	}
}

func visibleCommitTreePaths(rows []fileTreeRow) []string {
	paths := make([]string, len(rows))
	for i, row := range rows {
		paths[i] = row.path
	}
	return paths
}

func TestChangesFileTreeFoldsDirectoriesAndRestoresFlatPaths(t *testing.T) {
	h := menuTestApp(t)
	h.app.snapshot.Files = []repository.File{
		{Path: "README.md", Status: " M", Unstaged: true, Tracked: true},
		{Path: "src/main.go", Status: " M", Unstaged: true, Tracked: true},
		{Path: "src/ui/app.go", Status: "??", Unstaged: true, Added: true},
	}
	h.tester.Frame()
	if !h.app.fileTreeMode || !h.tester.HasText("src") || !h.tester.HasText("ui") {
		t.Fatalf("directory grouping was not enabled by default: %q", h.texts())
	}
	toggle, ok := h.tester.Access("Show files as a flat list")
	if !ok || !toggle.Checked {
		t.Fatalf("the active directory-grouping toggle is not exposed as pressed: %+v, found=%v", toggle, ok)
	}
	if err := h.tester.Click("Collapse directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	for _, row := range h.app.changeRows {
		if row.isFile() && strings.HasPrefix(row.file.Path, "src/") {
			t.Fatalf("collapsed directory still shows %q", row.file.Path)
		}
	}
	if !h.tester.HasText("README.md") {
		t.Fatal("collapsing src hid its sibling")
	}
	if err := h.tester.Click("Show files as a flat list"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.fileTreeMode || changeRowIndex(h.app.changeRows, "src/ui/app.go", false) < 0 {
		t.Fatalf("flat mode did not restore full paths: %+v", h.app.changeRows)
	}
}

func TestCommitFileTreeKeepsTheChosenDiffOnItsSourceFile(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionHistory
	h.tester.Frame()
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.app.commitFiles = nestedTreeCommitFiles()
	h.app.selectedCommitFile = 0
	h.app.selectedCommitFileRow = -1
	h.app.commitFilesHeight = 400
	h.tester.Frame()
	if got := h.tester.HasText("src") && h.tester.HasText("ui") && h.tester.HasText("app.go"); !got {
		t.Fatalf("commit tree is incomplete: %q", h.texts())
	}
	if err := h.tester.Click("Collapse directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if got := visibleCommitTreePaths(h.app.commitFileRows); !reflect.DeepEqual(got, []string{"README.md", "src", "z.txt"}) {
		t.Fatalf("collapsed commit rows = %v", got)
	}
	if h.app.selectedCommitFile != -1 || selectedFileTreeDirectory(h.app.commitFileRows, h.app.selectedCommitFileRow) != "src" {
		t.Fatalf("collapsed directory was not the safe selection: row=%d source=%d",
			h.app.selectedCommitFileRow, h.app.selectedCommitFile)
	}
	if err := h.tester.Click("Expand directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.selectedCommitFile != -1 || h.app.commitDiffPath != "" || h.app.commitDiffState != nil {
		t.Fatalf("directory retained a file diff: source=%d path=%q state=%v",
			h.app.selectedCommitFile, h.app.commitDiffPath, h.app.commitDiffState != nil)
	}
	if err := h.tester.Click("app.go"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.selectedCommitFile != 2 || h.app.commitFiles[h.app.selectedCommitFile].Path != "src/ui/app.go" {
		t.Fatalf("tree row targeted source file %d", h.app.selectedCommitFile)
	}
	h.waitFor(t, "the nested file's diff", func() bool {
		return h.app.commitDiffPath == "src/ui/app.go" && h.app.commitDiffState != nil
	})
}

func TestBranchAndStashFileTreesFoldDirectories(t *testing.T) {
	t.Run("branch commit", func(t *testing.T) {
		h := menuTestApp(t)
		h.app.workspaceTab = sectionBranches
		h.tester.Frame()
		h.waitFor(t, "the branch commit files", func() bool { return len(h.app.branchCommitFiles) > 0 })
		h.app.branchCommitFiles = nestedTreeCommitFiles()
		h.app.selectedBranchCommitFile = 0
		h.app.selectedBranchCommitFileRow = -1
		h.tester.Frame()
		if err := h.tester.Click("Collapse directory src"); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		if got := visibleCommitTreePaths(h.app.branchCommitFileRows); !reflect.DeepEqual(got, []string{"README.md", "src", "z.txt"}) {
			t.Fatalf("collapsed branch commit rows = %v", got)
		}
	})

	t.Run("stash", func(t *testing.T) {
		h, _ := stashTestApp(t)
		if err := h.tester.Click("Stash"); err != nil {
			t.Fatal(err)
		}
		h.waitFor(t, "the stash files", func() bool { return len(h.app.stashFiles) > 0 })
		h.app.stashFiles = nestedTreeCommitFiles()
		h.app.selectedStashFile = 0
		h.app.selectedStashFileRow = -1
		h.tester.Frame()
		if err := h.tester.Click("Collapse directory src"); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		if got := visibleCommitTreePaths(h.app.stashFileRows); !reflect.DeepEqual(got, []string{"README.md", "src", "z.txt"}) {
			t.Fatalf("collapsed stash rows = %v", got)
		}
		if h.app.selectedStashFile != -1 || h.app.stashDiffPath != "" || h.app.stashDiffState != nil {
			t.Fatalf("stash directory retained a file target: source=%d path=%q",
				h.app.selectedStashFile, h.app.stashDiffPath)
		}
	})
}

func TestFileTreeSelectionDistinguishesDirectoriesFromFiles(t *testing.T) {
	rows := buildFileTreeRows([]string{"dir/a.txt", "dir/b.txt"}, true, nil, "changes")
	selectedRow, selectedSource := -1, -1
	selectFileTreeRow(rows, 0, &selectedRow, &selectedSource)
	if selectedRow != 0 || selectedSource != -1 {
		t.Fatalf("directory selection row=%d source=%d", selectedRow, selectedSource)
	}
	syncFileTreeRow(rows, &selectedRow, &selectedSource)
	if selectedRow != 0 || selectedSource != -1 {
		t.Fatalf("directory selection was replaced row=%d source=%d", selectedRow, selectedSource)
	}
	selectFileTreeRow(rows, 1, &selectedRow, &selectedSource)
	if selectedSource != 0 || rows[selectedRow].path != "dir/a.txt" {
		t.Fatalf("file selection row=%d source=%d", selectedRow, selectedSource)
	}

	// The same directory names in another pane have independent fold state.
	a := newApp(fakeOpener{})
	a.toggleFileTreeDirectory("changes", "dir")
	if a.fileTreeCollapsed[fileTreeCollapseKey("commit", "dir")] {
		t.Fatal("folding a Changes directory also folded commit files")
	}
	if got := buildFileTreeRows([]string{"dir/a.txt"}, false, a.fileTreeCollapsed, "changes"); len(got) != 1 || got[0].path != "dir/a.txt" {
		t.Fatalf("flat rows changed with fold state: %#v", got)
	}
}
