package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// changesTestApp opens a repository holding files in the Changes section, with
// a recorder the test reads to see what an icon asked the session to do.
func changesTestApp(t *testing.T, files []repository.File) (*harness, *callRecorder) {
	t.Helper()
	return changesTestAppIn(t, files, 1280, 800)
}

// changesTestAppIn is changesTestApp in a window of the given size, for the
// layouts both the default and the minimum window must hold.
func changesTestAppIn(t *testing.T, files []repository.File, width, height int) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files:  files,
		Commits: []repository.Commit{
			{Hash: "c1c1c1c1", ShortHash: "c1c1c1c", Subject: "Initial commit", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		},
	}}, calls: calls})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, width, height)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls
}

// sectionedFiles are the fixture of the Changes tests: a file that is both
// staged and changed, a staged file, a changed file, and an untracked file.
func sectionedFiles() []repository.File {
	return []repository.File{
		{Path: "both.txt", Status: "MM", Staged: true, Unstaged: true, Tracked: true},
		{Path: "staged.txt", Status: "M ", Staged: true, Tracked: true},
		{Path: "work.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "untracked.txt", Status: "??", Unstaged: true, Added: true},
	}
}

// changeRowPaths draws the file rows of rows as "section:path", leaving the
// headers out, for readable assertions.
func changeRowPaths(rows []changeRow) []string {
	var out []string
	for _, row := range rows {
		if row.isHeader() {
			continue
		}
		out = append(out, row.sideKey()+":"+row.file.Path)
	}
	return out
}

func TestChangesListSectionsAndBadges(t *testing.T) {
	h, _ := changesTestApp(t, sectionedFiles())
	rows := h.app.changeRows
	if len(rows) != 7 {
		t.Fatalf("got %d rows, want 7: %+v", len(rows), rows)
	}
	if rows[0].header != "Staged Changes" || rows[0].count != 2 {
		t.Errorf("first header = %q with %d files, want \"Staged Changes\" with 2", rows[0].header, rows[0].count)
	}
	if rows[3].header != "Changes" || rows[3].count != 3 {
		t.Errorf("second header = %q with %d files, want \"Changes\" with 3", rows[3].header, rows[3].count)
	}
	want := []string{"staged:both.txt", "staged:staged.txt", "unstaged:both.txt", "unstaged:untracked.txt", "unstaged:work.txt"}
	got := changeRowPaths(rows)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	// A file that is both staged and changed shows on both sides, and each
	// side reads its own diff.
	if staged, unstaged := rows[1], rows[4]; !staged.staged || unstaged.staged {
		t.Errorf("both.txt sides are staged=%v and staged=%v", staged.staged, unstaged.staged)
	}
}

// A conflicted file is resolved in a section of its own, not staged or
// unstaged like an ordinary change, so it is listed once and its section is
// the first thing the panel shows.
func TestMergeChangesSectionListsConflictsOnly(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "DU", Staged: true, Unstaged: true, HasMergeConflicts: true},
		{Path: "staged.txt", Status: "M ", Staged: true, Tracked: true},
		{Path: "work.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	rows := h.app.changeRows
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6: %+v", len(rows), rows)
	}
	if rows[0].header != "Merge Changes" || rows[0].count != 1 || !rows[0].merge {
		t.Errorf("first header = %+v, want \"Merge Changes\" with 1 file", rows[0])
	}
	if rows[1].file.Path != "conflict.txt" || !rows[1].merge {
		t.Errorf("the conflict row = %+v, want conflict.txt in the merge section", rows[1])
	}
	if rows[2].header != "Staged Changes" || rows[2].count != 1 {
		t.Errorf("second header = %+v, want \"Staged Changes\" with 1 file", rows[2])
	}
	if rows[4].header != "Changes" || rows[4].count != 1 {
		t.Errorf("third header = %+v, want \"Changes\" with 1 file", rows[4])
	}
	want := []string{"merge:conflict.txt", "staged:staged.txt", "unstaged:work.txt"}
	if got := changeRowPaths(rows); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	// The conflict is chosen first, and the merge list's own selection
	// follows it.
	change, ok := h.app.selectedChange()
	if !ok || change.file.Path != "conflict.txt" || !change.merge {
		t.Fatalf("the first frame chose %+v, want the conflict", change)
	}
	if h.app.mergeSelected != 0 {
		t.Errorf("the merge list selected %d, want 0", h.app.mergeSelected)
	}
}

func TestTreeModeSectionBadgesCountFilesNotDirectories(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "incidentAnalysis/first.ts", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "incidentAnalysis/nested/second.ts", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "other/third.ts", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.app.fileTreeMode = true
	h.tester.Frame()

	if got := changeSectionFileCount(h.app.mergeRows); got != 3 {
		t.Fatalf("merge section has %d files, want 3", got)
	}
	if len(h.app.mergeRows) <= 3 {
		t.Fatalf("fixture has no directory rows to distinguish from files: %+v", h.app.mergeRows)
	}
	texts := h.tester.Texts()
	header := slices.Index(texts, "Merge Changes")
	if header < 0 {
		t.Fatalf("merge header is missing: %q", texts)
	}
	headerArea := texts[header:min(header+5, len(texts))]
	if !slices.Contains(headerArea, "3") {
		t.Fatalf("merge header does not show its three-file badge: %q", headerArea)
	}
}

// The merge section folds on its own, leaving the other sections as they were.
func TestMergeChangesHeaderFoldsItsSection(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	if err := h.tester.Click("Merge Changes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.mergeExpanded {
		t.Fatal("clicking the merge header did not fold it")
	}
	for _, row := range h.app.changeRows {
		if row.merge && !row.isHeader() {
			t.Fatalf("a folded section still lists %q", row.file.Path)
		}
	}
	if !h.app.stagedExpanded || !h.app.changesExpanded {
		t.Fatal("folding the merge section folded another one too")
	}
}

// The status letter says what happened on the row's own side of Git's
// two-character code; a file Git does not track reads as U, as in VS Code, and
// a conflict as !.
func TestChangeStatusLetters(t *testing.T) {
	cases := []struct {
		name   string
		change changeRow
		want   string
	}{
		{"staged change", changeRow{file: repository.File{Status: "M "}, staged: true}, "M"},
		{"worktree change", changeRow{file: repository.File{Status: " M"}}, "M"},
		{"both sides", changeRow{file: repository.File{Status: "MM"}}, "M"},
		{"staged add", changeRow{file: repository.File{Status: "A ", Added: true}, staged: true}, "A"},
		{"worktree delete", changeRow{file: repository.File{Status: " D", Deleted: true}}, "D"},
		{"rename", changeRow{file: repository.File{Status: "R "}, staged: true}, "R"},
		{"untracked", changeRow{file: repository.File{Status: "??", Added: true}}, "U"},
		{"conflict", changeRow{file: repository.File{Status: "UU", HasMergeConflicts: true}}, "!"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := changeStatusLetter(test.change); got != test.want {
				t.Errorf("status letter = %q, want %q", got, test.want)
			}
		})
	}
}

// The status is the last thing on the row, and its letters share one right
// edge however wide the glyphs are.
func TestChangeStatusSitsAtTheRowEnd(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "main.go", Status: " M", Unstaged: true, Tracked: true},
		{Path: "notes.md", Status: "??", Unstaged: true, Added: true},
	})
	h.tester.Frame()
	path, ok := h.tester.Find("main.go")
	if !ok {
		t.Fatalf("the row drew no path: %v", h.tester.Texts())
	}
	modified, ok := h.tester.Find("M")
	if !ok {
		t.Fatalf("the modified row drew no status: %v", h.tester.Texts())
	}
	untracked, ok := h.tester.Find("U")
	if !ok {
		t.Fatalf("the untracked row drew no status: %v", h.tester.Texts())
	}
	if modified.X < path.X {
		t.Errorf("the status is at %v, left of the path at %v", modified.X, path.X)
	}
	if right, want := modified.X+modified.W, untracked.X+untracked.W; right-want > 0.5 || want-right > 0.5 {
		t.Errorf("the statuses end at %v and %v, want one edge", right, want)
	}
}

func TestChangesHeaderFoldsItsSection(t *testing.T) {
	h, _ := changesTestApp(t, sectionedFiles())
	if err := h.tester.Click("Staged Changes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.stagedExpanded {
		t.Fatal("clicking the staged header did not fold it")
	}
	for _, row := range h.app.changeRows {
		if row.staged && !row.isHeader() {
			t.Fatalf("a folded section still lists %q", row.file.Path)
		}
	}
	if !h.app.changesExpanded {
		t.Fatal("folding the staged section folded the changes section too")
	}
}

// Choosing a file never stops on a section header, whether the keyboard or the
// pointer moves the choice.
func TestChangesSelectionSkipsSectionHeaders(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	if _, ok := h.app.selectedChange(); !ok {
		t.Fatalf("the first frame chose row %d, which is not a file", h.app.selectedFile)
	}

	if err := h.tester.Click("untracked.txt"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.settle(t)
	// A click on a row opens its diff in the detail pane.
	h.waitFor(t, "the clicked row's diff", func() bool {
		return calls.has("diff:untracked.txt:unstaged")
	})
	start := h.app.selectedFile
	h.press(t, ui.KeyDown)
	if h.app.selectedFile == start {
		t.Fatalf("Down did not move the choice from row %d", start)
	}
	for range 6 {
		h.press(t, ui.KeyDown)
		if row, ok := h.app.selectedChange(); !ok {
			t.Fatalf("Down chose row %d (%+v), which is not a file", h.app.selectedFile, h.app.changeRows)
		} else if row.file.Path == "" {
			t.Fatalf("Down chose an empty file row")
		}
	}
	for range 6 {
		h.press(t, ui.KeyUp)
		if _, ok := h.app.selectedChange(); !ok {
			t.Fatalf("Up chose row %d (%+v), which is not a file", h.app.selectedFile, h.app.changeRows)
		}
	}
}

func TestChangesExactSearchAndUntrackedToggle(t *testing.T) {
	h, _ := changesTestApp(t, sectionedFiles())
	// A substring of the path matches; a fuzzy letter sequence does not.
	h.app.fileSearch = "wtx"
	h.tester.Frame()
	if got := changeRowPaths(h.app.changeRows); len(got) != 0 {
		t.Fatalf("a fuzzy query matched %v, want nothing", got)
	}
	h.app.fileSearch = "work"
	h.tester.Frame()
	got := changeRowPaths(h.app.changeRows)
	want := []string{"unstaged:work.txt"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("filtered rows = %v, want %v", got, want)
	}

	h.app.fileSearch = ""
	h.tester.Frame()
	if err := h.tester.Click("File filters"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.changesFiltersOpen || !h.tester.HasText("Ignore untracked files") {
		t.Fatal("the filter icon did not open its options popover")
	}
	if err := h.tester.Click("Ignore untracked files"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.ignoreUntracked || !h.app.changesFiltersOpen {
		t.Fatal("the popover option did not immediately hide untracked files")
	}
	got = changeRowPaths(h.app.changeRows)
	want = []string{"staged:both.txt", "staged:staged.txt", "unstaged:both.txt", "unstaged:work.txt"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows with untracked hidden = %v, want %v", got, want)
	}
	// The badges count what the filter leaves, not the warehouse of files.
	if h.app.changeRows[3].count != 2 {
		t.Errorf("the changes badge says %d, want 2", h.app.changeRows[3].count)
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.changesFiltersOpen {
		t.Fatal("Escape did not dismiss the filter popover")
	}
	if info, ok := h.tester.Access("File filters"); !ok || !info.Checked {
		t.Fatalf("active filter is not exposed on the popover trigger: %+v, %v", info, ok)
	}
	if err := h.tester.Click("File filters"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Clear filters"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.ignoreUntracked {
		t.Fatal("Clear filters did not restore untracked files")
	}
}

// An exact query also finds a renamed file by its old path.
func TestChangesSearchMatchesPreviousPath(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "new.go", PreviousPath: "old_place.go", Status: "R ", Staged: true, Tracked: true},
	})
	h.app.fileSearch = "old_place"
	h.tester.Frame()
	if got := changeRowPaths(h.app.changeRows); fmt.Sprint(got) != "[staged:new.go]" {
		t.Fatalf("rows = %v, want the renamed file", got)
	}
}

func TestChangeRowIconsActOnTheirFile(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	chooseChangeRow(t, h, "work.txt", false)
	if err := h.tester.Click("Stage work.txt"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the file to be staged", func() bool { return calls.has("stage:work.txt") })
	h.settle(t)

	chooseChangeRow(t, h, "both.txt", true)
	if err := h.tester.Click("Unstage both.txt"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the file to be unstaged", func() bool { return calls.has("unstage:both.txt") })
	h.settle(t)

	chooseChangeRow(t, h, "work.txt", false)
	if err := h.tester.Click("Discard work.txt"); err != nil {
		t.Fatal(err)
	}
	if h.app.confirm == nil {
		t.Fatal("the discard icon opened no confirmation")
	}
}

// chooseChangeRow chooses a file's row, as a click or the arrow keys do, so
// its inline actions show.
func chooseChangeRow(t *testing.T, h *harness, path string, staged bool) {
	t.Helper()
	index := changeRowIndex(h.app.changeRows, path, staged)
	if index < 0 {
		t.Fatalf("%s is not among the change rows: %+v", path, h.app.changeRows)
	}
	h.app.selectedFile = index
	h.tester.Frame()
}

// Folding a section under the choice keeps the choice on its file rather than
// sliding it onto whatever row now shares its index.
func TestFoldingASectionKeepsTheChosenFile(t *testing.T) {
	h, _ := changesTestApp(t, sectionedFiles())
	chooseChangeRow(t, h, "work.txt", false)
	if err := h.tester.Click("Staged Changes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	change, ok := h.app.selectedChange()
	if !ok || change.file.Path != "work.txt" || change.staged {
		t.Fatalf("folding the staged section chose %+v, want work.txt in the changes section", change)
	}
	if h.app.stagedExpanded {
		t.Fatal("the staged section did not fold")
	}
}

// Each section is a list of its own, so a click in either one chooses the
// file on that side of the working tree.
func TestClickingASectionRowChoosesItsSide(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	for _, want := range []struct {
		path   string
		staged bool
		call   string
	}{
		{"staged.txt", true, "diff:staged.txt:staged"},
		{"work.txt", false, "diff:work.txt:unstaged"},
	} {
		if err := h.tester.Click(want.path); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		h.waitFor(t, "the row's diff", func() bool { return calls.has(want.call) })
		change, ok := h.app.selectedChange()
		if !ok || change.file.Path != want.path || change.staged != want.staged {
			t.Fatalf("the click chose %+v, want %s (staged %v)", change, want.path, want.staged)
		}
	}
}

func TestChangesHeaderStageAllIcons(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	if err := h.tester.Click("Stage all changes"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "every file to be staged", func() bool { return calls.has("stage-all") })
	h.settle(t)

	if err := h.tester.Click("Unstage all changes"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "every file to be unstaged", func() bool { return calls.has("unstage-all") })
}

func TestCtrlEnterCommitsTheInlineMessage(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	h.app.commitSummary = "ship the changes"
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeyEnter)
	h.waitFor(t, "the commit", func() bool { return calls.has("commit:ship the changes") })
	h.settle(t)
	if h.app.commitSummary != "" {
		t.Fatalf("the message survived the commit: %q", h.app.commitSummary)
	}
}

// The commit card says which branch the message lands on, and its Commit
// button commits the inline message as Ctrl+Enter does.
func TestCommitCardButtonCommitsTheInlineMessage(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	h.app.commitSummary = "ship the changes"
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Commit on main") {
		t.Fatalf("the card does not name the branch: %q", h.texts())
	}
	if err := h.tester.Click("Commit"); err != nil {
		t.Fatalf("click Commit: %v", err)
	}
	h.waitFor(t, "the commit", func() bool { return calls.has("commit:ship the changes") })
	h.settle(t)
	if h.app.commitSummary != "" {
		t.Fatalf("the message survived the commit: %q", h.app.commitSummary)
	}
}

// Closing blame clears the viewer while the frame that drew it is still
// building; the pane must stop drawing the state it just dropped instead of
// handing a nil state to the editor.
func TestCloseBlame(t *testing.T) {
	h, _ := changesTestApp(t, sectionedFiles())
	h.app.selectedFile = 2 // staged.txt
	h.app.beginBlame()
	h.waitFor(t, "the blame", func() bool { return h.app.blameState != nil })
	if !h.app.blameState.NoIndentGuides {
		t.Fatal("blame metadata indentation must not draw a grid beside the source")
	}

	if err := h.tester.Click("Close blame"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.settle(t)
	if h.app.blameState != nil {
		t.Fatal("Close blame left the blame viewer open")
	}
	if h.app.blameLoading || h.app.blameError != "" {
		t.Fatalf("Close blame left blame in a loading or failed state: loading=%v error=%q",
			h.app.blameLoading, h.app.blameError)
	}
	h.waitFor(t, "the file diff again", func() bool { return h.app.diffState != nil })
	if h.app.diffError != "" {
		t.Fatalf("the pane did not fall back to the diff: %s", h.app.diffError)
	}
}

// The detail pane follows the chosen side of a partially staged file: the
// staged section reads the index, the changes section the working tree.
func TestDiffFollowsTheChosenSide(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "both.txt", Status: "MM", Staged: true, Unstaged: true, Tracked: true},
	})
	h.waitFor(t, "the staged diff", func() bool { return calls.has("diff:both.txt:staged") })
	h.waitFor(t, "the loaded staged diff", func() bool { return h.app.diffState != nil || h.app.diffError != "" })

	unstaged := changeRowIndex(h.app.changeRows, "both.txt", false)
	if unstaged < 0 {
		t.Fatal("the changes section does not list both.txt")
	}
	h.app.selectedFile = unstaged
	h.tester.Frame()
	h.waitFor(t, "the working-tree diff", func() bool { return calls.has("diff:both.txt:unstaged") })
}

type selectiveFileHistorySession struct{ fakeSession }

func (s selectiveFileHistorySession) FileCommits(ctx context.Context, path string, limit, skip int) ([]repository.Commit, error) {
	if path == "two.txt" {
		s.calls.record("file-commits:" + path)
		return nil, nil
	}
	return s.fakeSession.FileCommits(ctx, path, limit, skip)
}

func TestFindFileHistoriesFromWorkingTreeMenu(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.session = selectiveFileHistorySession{fakeSession: h.app.session.(fakeSession)}

	h.rightClick(t, "one.txt")
	h.chooseMenuItem(t, "Find file histories")
	h.waitFor(t, "the tracked file's history", func() bool {
		return h.app.fileHistoryOpen && len(h.app.fileHistoryCommits) > 0
	})
	h.press(t, ui.KeyEscape)

	h.rightClick(t, "two.txt")
	h.chooseMenuItem(t, "Find file histories")
	h.waitFor(t, "the new file's notification", func() bool {
		return h.tester.HasText("No commits changed this file.")
	})
	if h.app.fileHistoryOpen {
		t.Fatal("a file without history opened the drawer")
	}
	if !calls.has("file-commits:two.txt") {
		t.Fatalf("the new file's history was not checked: %v", calls.all())
	}
}

func TestFileHistoryDrawer(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, sectionKeys[sectionHistory])
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })

	h.rightClick(t, "one.txt")
	h.requireMenuLabels(t, "View file changes…")
	h.chooseMenuItem(t, "View file changes…")
	if !h.app.fileHistoryOpen {
		t.Fatal("the file history drawer did not open")
	}
	h.waitFor(t, "the file's commits", func() bool { return len(h.app.fileHistoryCommits) > 0 })
	if !slices.Contains(h.texts(), "File history") || !slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("the drawer lists no commits: %q", h.texts())
	}

	h.press(t, ui.KeyEscape)
	if h.app.fileHistoryOpen {
		t.Fatal("Escape did not close the file history drawer")
	}
}

// The file history drawer has three columns: the file's commits, the files of
// the chosen commit, and the chosen file's diff. Choosing another commit reads
// that commit's files and diff, which was the dead click the drawer had.
func TestFileHistoryDrawerHasThreeLiveColumns(t *testing.T) {
	h := menuTestApp(t)
	h.app.openFileHistory("one.txt")
	h.tester.Frame()
	h.waitFor(t, "the file's commits", func() bool { return len(h.app.fileHistoryCommits) > 0 })
	h.waitFor(t, "the chosen commit's files", func() bool { return len(h.app.fileHistoryFiles) > 0 })
	h.waitFor(t, "the chosen file's diff", func() bool { return h.app.fileHistoryDiffState != nil })

	for _, want := range []string{"Commits", "Changed files", "File diff"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the drawer does not show the %q column: %q", want, h.texts())
		}
	}

	second := h.app.fileHistoryCommits[1]
	r, found := h.tester.Find(second.Subject)
	if !found {
		t.Fatalf("the second commit %q is not shown", second.Subject)
	}
	h.tester.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	h.waitFor(t, "the second commit's files", func() bool {
		return h.app.fileHistoryFilesHash == second.Hash
	})
	h.waitFor(t, "the second commit's diff", func() bool {
		return h.app.fileHistoryDiffHash == second.Hash
	})
	selected, ok := h.tester.AccessByRole(ui.RoleListItem, h.app.fileHistoryFiles[h.app.fileHistorySelectedFile].Path)
	if !ok || !selected.Selected {
		t.Fatalf("the chosen commit's file has no selected row: index=%d files=%+v access=%+v",
			h.app.fileHistorySelectedFile, h.app.fileHistoryFiles, selected)
	}
}

func openLoadedFileHistory(t *testing.T, h *harness) {
	t.Helper()
	h.app.openFileHistory("one.txt")
	h.tester.Frame()
	h.waitFor(t, "the file's commits", func() bool { return len(h.app.fileHistoryCommits) > 0 })
	h.waitFor(t, "the chosen commit's files", func() bool { return len(h.app.fileHistoryFiles) > 0 })
	h.waitFor(t, "the chosen file's diff", func() bool { return h.app.fileHistoryDiffState != nil })
}

type disjointFileHistorySession struct{ fakeSession }

func (s disjointFileHistorySession) CommitFiles(ctx context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	if commit.Hash == "c1c1c1c1" {
		s.calls.record("commit-files:" + commit.Hash)
		return []repository.CommitFile{{Path: "other.txt", Status: "A"}}, nil
	}
	return s.fakeSession.CommitFiles(ctx, commit)
}

func TestFileHistorySelectsDefaultFileWhenCommitsHaveDisjointFiles(t *testing.T) {
	h := menuTestApp(t)
	h.app.session = disjointFileHistorySession{fakeSession: h.app.session.(fakeSession)}
	openLoadedFileHistory(t, h)

	second := h.app.fileHistoryCommits[1]
	row, ok := h.tester.Find(second.Subject)
	if !ok {
		t.Fatalf("the second commit %q is not shown", second.Subject)
	}
	h.tester.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	h.waitFor(t, "files disjoint from the previous commit", func() bool {
		return h.app.fileHistoryFilesHash == second.Hash && !h.app.fileHistoryFilesLoading &&
			len(h.app.fileHistoryFiles) == 1 && h.app.fileHistoryFiles[0].Path == "other.txt"
	})

	selected, ok := h.tester.AccessByRole(ui.RoleListItem, "other.txt")
	if !ok || h.app.fileHistorySelectedFile != 0 || !selected.Selected {
		t.Fatalf("the new commit's default file is not selected: index=%d access=%+v",
			h.app.fileHistorySelectedFile, selected)
	}
}

func TestFileHistoryColumnsCollapseAndRestoreIndependently(t *testing.T) {
	h := menuTestApp(t)
	openLoadedFileHistory(t, h)

	if err := h.tester.Click("Collapse file history commits"); err != nil {
		t.Fatalf("collapse the commit column: %v", err)
	}
	h.tester.Frame()
	if !h.app.fileHistoryCommitsCollapsed || !h.tester.HasText("Expand file history commits") {
		t.Fatalf("the commit column did not collapse: %q", h.texts())
	}
	if !h.tester.HasText("Changed files") || !h.tester.HasText("File diff") ||
		h.app.fileHistoryDiffState == nil {
		t.Fatal("collapsing commits disturbed the loaded files or diff")
	}

	if err := h.tester.Click("Collapse changed files"); err != nil {
		t.Fatalf("collapse the changed-files column: %v", err)
	}
	h.tester.Frame()
	if !h.app.fileHistoryFilesCollapsed || !h.tester.HasText("Expand changed files") ||
		!h.tester.HasText("Expand file history commits") || !h.tester.HasText("File diff") {
		t.Fatalf("the two collapsed columns do not have independent restore rails: %q", h.texts())
	}

	if err := h.tester.Click("Expand file history commits"); err != nil {
		t.Fatalf("restore the commit column: %v", err)
	}
	h.tester.Frame()
	if h.app.fileHistoryCommitsCollapsed || !h.tester.HasText("Commits") ||
		!h.app.fileHistoryFilesCollapsed || h.tester.HasText("Changed files") {
		t.Fatalf("restoring commits also changed the files column: %q", h.texts())
	}
	if err := h.tester.Click("Expand changed files"); err != nil {
		t.Fatalf("restore the changed-files column: %v", err)
	}
	h.tester.Frame()
	if h.app.fileHistoryFilesCollapsed || !h.tester.HasText("Changed files") ||
		h.app.fileHistoryDiffState == nil {
		t.Fatal("restoring changed files discarded the loaded diff")
	}
	second := h.app.fileHistoryCommits[1]
	row, ok := h.tester.Find(second.Subject)
	if !ok {
		t.Fatal("the restored commit list is not populated")
	}
	h.tester.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	h.waitFor(t, "a commit selected after restoring the columns", func() bool {
		return h.app.fileHistoryFilesHash == second.Hash && h.app.fileHistoryDiffHash == second.Hash
	})
}

func TestFileHistoryInnerDividersResize(t *testing.T) {
	for _, test := range []struct {
		name    string
		percent func(*app) *float32
		second  bool
	}{
		{name: "commits and files", percent: func(a *app) *float32 { return &a.fileHistoryCommitPercent }},
		{name: "files and diff", percent: func(a *app) *float32 { return &a.fileHistoryFilePercent }, second: true},
	} {
		for _, width := range []int{1280, 720} {
			t.Run(fmt.Sprintf("%s at width %d", test.name, width), func(t *testing.T) {
				h := menuTestApp(t)
				openLoadedFileHistory(t, h)
				h.tester.SetSize(width, 800)
				h.tester.Frame()
				drawer, ok := h.tester.Find("File history")
				if !ok {
					t.Fatal("the file history drawer is missing")
				}
				changedFiles, ok := h.tester.Find("Changed files")
				if !ok || changedFiles.H > paneToggleHeight {
					t.Fatalf("the narrow changed-files header wraps: %+v", changedFiles)
				}
				total := drawer.W - islandGap
				first := (total - 1) * h.app.fileHistoryCommitPercent / 100
				x := drawer.X + islandGap/2 + first + 0.5
				if test.second {
					remaining := total - first - 1
					x += 1 + (remaining-1)*h.app.fileHistoryFilePercent/100
				}
				percent := test.percent(h.app)
				before := *percent
				// Drag the visible centered grip, not just the native split line.
				dragTo(t, h.tester, x, drawer.Y+drawer.H/2, 40, 0)
				if *percent <= before {
					t.Fatalf("the divider did not follow the drag: %v%% -> %v%%", before, *percent)
				}
			})
		}
	}
}

func TestFileHistoryInnerDividersKeepSmallSizes(t *testing.T) {
	for _, test := range []struct {
		name    string
		percent func(*app) *float32
		second  bool
	}{
		{name: "commits", percent: func(a *app) *float32 { return &a.fileHistoryCommitPercent }},
		{name: "changed files", percent: func(a *app) *float32 { return &a.fileHistoryFilePercent }, second: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := menuTestApp(t)
			openLoadedFileHistory(t, h)
			h.tester.SetSize(720, 800)
			h.tester.Frame()
			drawer, ok := h.tester.Find("File history")
			if !ok {
				t.Fatal("the file history drawer is missing")
			}
			total := drawer.W - islandGap
			first := (total - 1) * h.app.fileHistoryCommitPercent / 100
			x := drawer.X + islandGap/2 + first + 0.5
			shown := first
			if test.second {
				remaining := total - first - 1
				shown = (remaining - 1) * h.app.fileHistoryFilePercent / 100
				x += 1 + shown
			}
			percent := test.percent(h.app)
			// Move the pane to the splitter's 40-DIP minimum. This is below the
			// old 12% reset threshold at this narrow width.
			dragTo(t, h.tester, x, drawer.Y+drawer.H/2, 40-shown, 0)
			for range 3 {
				h.tester.Frame()
			}
			if *percent >= 12 {
				t.Fatalf("the small pane snapped back to %v%%, want less than 12%%", *percent)
			}
			settled := *percent
			h.tester.Frame()
			if *percent != settled {
				t.Fatalf("the pane kept moving after the drag: %v%% -> %v%%", settled, *percent)
			}
		})
	}
}

// fixedSessionOpener opens a session the test built, rather than a snapshot.
type fixedSessionOpener struct{ session repository.Session }

func (o fixedSessionOpener) Open(context.Context, string) (repository.Session, error) {
	return o.session, nil
}

func (fixedSessionOpener) Init(context.Context, string) error { return nil }

// pagedFileSession answers FileCommits in pages, recording the skip of every
// read so a test can see the drawer walk down a long file history.
type pagedFileSession struct {
	fakeSession
	total int
	mu    sync.Mutex
	skips []int
}

func (s *pagedFileSession) FileCommits(_ context.Context, path string, limit, skip int) ([]repository.Commit, error) {
	s.mu.Lock()
	s.skips = append(s.skips, skip)
	s.mu.Unlock()
	if skip >= s.total {
		return nil, nil
	}
	end := min(skip+limit, s.total)
	commits := make([]repository.Commit, 0, end-skip)
	for i := skip; i < end; i++ {
		commits = append(commits, repository.Commit{
			Hash:      fmt.Sprintf("%040d", i),
			ShortHash: fmt.Sprintf("%07d", i),
			Subject:   fmt.Sprintf("Commit %d", i),
		})
	}
	return commits, nil
}

func (s *pagedFileSession) skipsRead() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.skips...)
}

// Scrolling past the commits the drawer holds reads the next page of older
// ones: a file changed hundreds of times is not cut off at the first read.
func TestFileHistoryPagesOlderCommits(t *testing.T) {
	session := &pagedFileSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root:   "/work/project",
			Branch: "main",
			Files: []repository.File{
				{Path: "big.go", Status: " M", Unstaged: true, Tracked: true},
			},
			Commits: []repository.Commit{{Hash: "head", Subject: "Head"}},
		}},
		total: 2*fileHistoryPage + 7,
	}
	a := newApp(fixedSessionOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)

	a.openFileHistory("big.go")
	h.waitFor(t, "the first page of the file history", func() bool {
		return !a.fileHistoryLoading && len(a.fileHistoryCommits) == fileHistoryPage
	})
	if !a.fileHistoryMore {
		t.Fatal("a full first page did not report more commits")
	}
	a.loadMoreFileHistory()
	h.waitFor(t, "the second page of the file history", func() bool {
		return len(a.fileHistoryCommits) == 2*fileHistoryPage
	})
	a.loadMoreFileHistory()
	h.waitFor(t, "the last page of the file history", func() bool {
		return len(a.fileHistoryCommits) == 2*fileHistoryPage+7
	})
	if a.fileHistoryMore {
		t.Fatal("a short page still reported more commits")
	}
	want := fmt.Sprint([]int{0, fileHistoryPage, 2 * fileHistoryPage})
	if got := fmt.Sprint(session.skipsRead()); got != want {
		t.Fatalf("the file history skipped %s, want %s", got, want)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Beginning of file history") {
		t.Fatalf("the drawer does not show the end of the history: %q", h.texts())
	}
}

// The section bars carry the actions on their own files: unstage all sits on
// the Staged Changes bar, and stage all and discard all on the Changes bar,
// rather than in the panel's title row.
func TestSectionBarsCarryTheirOwnActions(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	staged, ok := h.tester.Find("Staged Changes")
	if !ok {
		t.Fatal("the Staged Changes bar is missing")
	}
	unstage, ok := h.tester.Find("Unstage all changes")
	if !ok {
		t.Fatal("the Staged Changes bar has no unstage action")
	}
	if unstage.Y+unstage.H/2 < staged.Y || unstage.Y+unstage.H/2 > staged.Y+staged.H {
		t.Fatalf("unstage sits at y=%v, away from the Staged bar at y=%v", unstage.Y, staged.Y)
	}

	stage, ok := h.tester.Find("Stage all changes")
	if !ok {
		t.Fatal("the Changes bar has no stage action")
	}
	discard, ok := h.tester.Find("Discard all changes")
	if !ok {
		t.Fatal("the Changes bar has no discard action")
	}
	if stage.Y <= staged.Y {
		t.Fatalf("stage sits above the Staged bar: %v vs %v", stage.Y, staged.Y)
	}
	if stage.Y+stage.H/2 < discard.Y || stage.Y+stage.H/2 > discard.Y+discard.H {
		t.Fatalf("stage and discard do not share the Changes bar: %v vs %v", stage.Y, discard.Y)
	}

	if err := h.tester.Click("Stage all changes"); err != nil {
		t.Fatalf("stage all from its bar: %v", err)
	}
	h.waitFor(t, "every file to be staged", func() bool { return calls.has("stage-all") })
	h.settle(t)
	if err := h.tester.Click("Unstage all changes"); err != nil {
		t.Fatalf("unstage all from its bar: %v", err)
	}
	h.waitFor(t, "every file to be unstaged", func() bool { return calls.has("unstage-all") })
}

// Discarding all changes asks first and then throws away every changed file,
// staged ones included.
func TestDiscardAllChangesConfirmsAndDiscardsEveryFile(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	if err := h.tester.Click("Discard all changes"); err != nil {
		t.Fatalf("discard all changes: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("discarding every change asked no confirmation")
	}
	if !slices.Contains(h.texts(), "Discard All Changes") {
		t.Fatalf("the confirmation is missing its title: %q", h.texts())
	}
	if err := h.tester.Click("Discard all"); err != nil {
		t.Fatalf("confirm the discard: %v", err)
	}
	h.waitFor(t, "every file to be discarded", func() bool {
		for _, path := range []string{"both.txt", "staged.txt", "work.txt", "untracked.txt"} {
			if !calls.has("discard:" + path) {
				return false
			}
		}
		return true
	})
	h.settle(t)
	if h.app.confirm != nil {
		t.Fatal("the confirmation stayed open after discarding")
	}
}

// A working tree can hold more files than the panel shows; the changes list
// scrolls its rows under the section bar rather than growing to every row and
// running off the bottom of the panel, where they could not be reached.
func TestChangesListScrollsWhenItOverflows(t *testing.T) {
	files := make([]repository.File, 0, 80)
	for i := range 80 {
		files = append(files, repository.File{
			Path:     fmt.Sprintf("file-%02d.txt", i),
			Status:   " M",
			Unstaged: true,
			Tracked:  true,
		})
	}
	h, _ := changesTestApp(t, files)
	first, ok := h.tester.Find("file-00.txt")
	if !ok {
		t.Fatalf("the changes list shows no rows: %q", h.texts())
	}
	// Rows past the panel are not built: the list knows its viewport, and
	// the last file is out of it.
	if last, built := h.tester.Find("file-79.txt"); built && last.Y > first.Y+first.H {
		if last.Y > 800 {
			t.Fatalf("the list laid out past the panel: the last row sits at y=%v", last.Y)
		}
	}

	x, y := first.X+first.W/2, first.Y+first.H/2
	h.tester.Move(x, y)
	h.tester.Frame()
	h.tester.Scroll(x, y, 0, 300)
	h.tester.Frame()
	scrolled, ok := h.tester.Find("file-00.txt")
	if !ok {
		t.Fatal("the changes list lost its first row while scrolling")
	}
	if scrolled.Y >= first.Y {
		t.Fatalf("the changes list did not scroll: its first row stayed at y=%v", scrolled.Y)
	}
}

// The Staged Changes bar can put the index away on its own: the staged
// files leave the tree as a stash, while the unstaged work stays in place.
// It saves the index alone, not the whole working tree.
func TestStagedBarStashesOnlyTheIndex(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())
	if err := h.tester.Click("Stash staged changes"); err != nil {
		t.Fatalf("stash the staged files: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the index to be stashed", func() bool {
		for _, call := range calls.all() {
			if strings.HasPrefix(call, "stash-staged:WIP on main at ") {
				return true
			}
		}
		return false
	})
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "stash:") {
			t.Fatalf("the bar stashed the whole tree: %q", call)
		}
	}
}

// The actions of a row appear on hover, and they must not resize the row:
// a glyph taller than the line once did, so the list jumped under the
// pointer. Small fonts showed it most, because the text line shrinks but a
// fixed-size button does not.
func TestHoveringAFileRowKeepsItsHeight(t *testing.T) {
	for _, fontSize := range []float64{9, 14} {
		t.Run(fmt.Sprintf("font %g", fontSize), func(t *testing.T) {
			h, _ := changesTestApp(t, sectionedFiles())
			h.app.settings.FontSize = fontSize
			h.tester.Frame()

			row, ok := h.tester.Find("work.txt")
			if !ok {
				t.Fatalf("the changes list shows no work.txt: %q", h.texts())
			}
			next, _ := h.tester.Find("untracked.txt")
			h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
			h.tester.Frame()

			hovered, _ := h.tester.Find("work.txt")
			if hovered.H != row.H {
				t.Fatalf("hovering grew the row from %v to %v", row.H, hovered.H)
			}
			if moved, _ := h.tester.Find("untracked.txt"); moved.Y != next.Y {
				t.Fatalf("hovering moved the next row from y=%v to y=%v", next.Y, moved.Y)
			}
			if _, found := h.tester.Find("Stage work.txt"); !found {
				t.Fatalf("the hovered row hides its actions: %q", h.texts())
			}
		})
	}
}

// A staged row offers unstage alone: discarding it from there would throw
// away the index and the working tree at once, so that action stays in the
// row's context menu. The unstaged rows keep their discard icon.
func TestStagedRowsOfferOnlyUnstage(t *testing.T) {
	h, _ := changesTestApp(t, sectionedFiles())
	staged, ok := h.tester.Find("both.txt")
	if !ok {
		t.Fatalf("the staged row is missing: %q", h.texts())
	}
	h.tester.Move(staged.X+staged.W/2, staged.Y+staged.H/2)
	h.tester.Frame()
	if _, found := h.tester.Find("Unstage both.txt"); !found {
		t.Fatalf("the staged row lost its unstage action: %q", h.texts())
	}
	if _, found := h.tester.Find("Discard both.txt"); found {
		t.Fatalf("the staged row still offers discard: %q", h.texts())
	}

	work, ok := h.tester.Find("work.txt")
	if !ok {
		t.Fatalf("the unstaged row is missing: %q", h.texts())
	}
	h.tester.Move(work.X+work.W/2, work.Y+work.H/2)
	h.tester.Frame()
	if _, found := h.tester.Find("Discard work.txt"); !found {
		t.Fatalf("the unstaged row lost its discard action: %q", h.texts())
	}
	if _, found := h.tester.Find("Stage work.txt"); !found {
		t.Fatalf("the unstaged row lost its stage action: %q", h.texts())
	}
}

// Discarding never runs straight from a click: the row icon, the context
// menu, and the details pane all ask first, naming the file, and only the
// dialog's button throws the changes away.
func TestEveryDiscardPathAsksFirst(t *testing.T) {
	h, calls := changesTestApp(t, sectionedFiles())

	// A staged row's menu still offers discard, though the row no longer
	// wears that icon, and it asks before running.
	h.app.selectedFile = 0
	h.rightClick(t, "both.txt")
	h.chooseMenuItem(t, "Discard changes…")
	if h.app.confirm == nil {
		t.Fatal("the staged row's menu discarded without asking")
	}
	if !slices.Contains(h.texts(), "Discard all changes to both.txt?") {
		t.Fatalf("the confirmation does not name the file: %q", h.texts())
	}
	h.press(t, ui.KeyEscape)
	if calls.has("discard:both.txt") {
		t.Fatal("cancelling the confirmation still discarded the file")
	}

	// The row icon on an unstaged file asks too.
	work, ok := h.tester.Find("work.txt")
	if !ok {
		t.Fatalf("the unstaged row is missing: %q", h.texts())
	}
	h.tester.Move(work.X+work.W/2, work.Y+work.H/2)
	h.tester.Frame()
	if err := h.tester.Click("Discard work.txt"); err != nil {
		t.Fatalf("discard from the row icon: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("the row icon discarded without asking")
	}
	if calls.has("discard:work.txt") {
		t.Fatal("the row icon discarded before the dialog was answered")
	}
	if err := h.tester.Click("Discard"); err != nil {
		t.Fatalf("confirm the row discard: %v", err)
	}
	h.waitFor(t, "the row discard", func() bool { return calls.has("discard:work.txt") })
	h.settle(t)

	// The details pane's own menu asks as well.
	h.app.selectedFile = 2
	h.settle(t)
	if err := h.tester.Click("File actions"); err != nil {
		t.Fatalf("open the details pane's menu: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Discard changes…"); err != nil {
		t.Fatalf("discard from the details pane: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("the details pane discarded without asking")
	}
}

// The merge and staged sections share the panel's top with the changes, and
// the line between them drags: giving one side more room takes it from the
// other.
func TestTheStagedAndChangesSectionsShareTheirHeight(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "staged.txt", Status: "M ", Staged: true, Tracked: true},
	})
	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	emptyBefore, ok := h.tester.Find("No files here")
	if !ok {
		t.Fatalf("the empty changes section did not show: %q", h.texts())
	}
	// The line starts under the staged file, not at a share of the pane
	// that leaves room nothing fills.
	if h.app.changesTopHeight > body.H/2 {
		t.Fatalf("the line starts %v DIPs into a %v tall pane, not under its file",
			h.app.changesTopHeight, body.H)
	}

	before := h.app.changesTopHeight
	dragTo(t, h.tester, body.X+body.W/2, body.Y+before, 0, -40)
	if h.app.changesTopHeight >= before {
		t.Fatalf("dragging the line up gave the staged side %v DIPs, was %v",
			h.app.changesTopHeight, before)
	}
	if h.app.changesTopAuto {
		t.Fatal("a drag did not take the line off its automatic height")
	}
	emptyAfter, ok := h.tester.Find("No files here")
	if !ok {
		t.Fatal("the changes section disappeared")
	}
	if emptyAfter.Y >= emptyBefore.Y {
		t.Fatalf("the changes did not move up: %v -> %v", emptyBefore.Y, emptyAfter.Y)
	}

	// Dragging the line back down gives the staged side its room again.
	body, ok = h.tester.Find("Changes sections")
	if !ok {
		t.Fatal("the split disappeared")
	}
	shrunk := h.app.changesTopHeight
	dragTo(t, h.tester, body.X+body.W/2, body.Y+shrunk, 0, 40)
	if h.app.changesTopHeight <= shrunk {
		t.Fatalf("dragging the line down left the staged side at %v DIPs, was %v",
			h.app.changesTopHeight, shrunk)
	}
}

// The merge changes have a line of their own above the staged ones: giving
// the merge list more or less room leaves the line under the staged section
// where the user put it.
func TestTheMergeAndStagedSectionsShareTheirHeight(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict-a.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "conflict-b.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "staged.txt", Status: "M ", Staged: true, Tracked: true},
	})
	// The line's automatic height needs the panel measured once, so settle
	// the layout before reading where it is.
	for range 3 {
		h.tester.Frame()
	}
	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	topBefore := h.app.changesTopHeight
	mergeBefore := h.app.mergeHeight
	if mergeBefore <= 0 || mergeBefore >= topBefore {
		t.Fatalf("the merge line is at %v in a %v tall top", mergeBefore, topBefore)
	}

	// Dragging the merge line up takes room from its list alone.
	dragTo(t, h.tester, body.X+body.W/2, body.Y+mergeBefore, 0, -30)
	if h.app.mergeHeight >= mergeBefore {
		t.Fatalf("dragging the merge line up left it at %v DIPs, was %v",
			h.app.mergeHeight, mergeBefore)
	}
	if h.app.mergeAuto {
		t.Fatal("a drag did not take the merge line off its automatic height")
	}
	if h.app.changesTopHeight != topBefore {
		t.Fatalf("the merge drag moved the line under the staged section: %v -> %v",
			topBefore, h.app.changesTopHeight)
	}

	// And down again, without touching the line under the staged section.
	body, ok = h.tester.Find("Changes sections")
	if !ok {
		t.Fatal("the split disappeared")
	}
	shrunk := h.app.mergeHeight
	dragTo(t, h.tester, body.X+body.W/2, body.Y+shrunk, 0, 30)
	if h.app.mergeHeight <= shrunk {
		t.Fatalf("dragging the merge line down left it at %v DIPs, was %v",
			h.app.mergeHeight, shrunk)
	}
	if h.app.changesTopHeight != topBefore {
		t.Fatalf("the merge drag moved the line under the staged section: %v -> %v",
			topBefore, h.app.changesTopHeight)
	}
}

// A merge and a staged file both show their rows in the default window: the
// line between them is placed so the staged row is not clipped, even when the
// changes section under it has no file of its own.
func TestAMergeAndAStagedFileBothShowInTheDefaultWindow(t *testing.T) {
	h, _ := changesTestAppIn(t, []repository.File{
		{Path: "conflict-a.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "conflict-b.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "staged.txt", Status: "A ", Staged: true, Tracked: true},
	}, 1040, 700)
	for range 3 {
		h.tester.Frame()
	}

	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	staged, ok := h.tester.Find("staged.txt")
	if !ok {
		t.Fatalf("the staged row is not drawn: %q", h.texts())
	}
	header, ok := h.tester.Find("Staged Changes")
	if !ok {
		t.Fatalf("the staged section has no header: %q", h.texts())
	}
	top := body.Y + h.app.changesTopHeight
	if staged.Y < header.Y+header.H {
		t.Fatalf("the staged row at %v is above its header at %v", staged.Y, header.Y)
	}
	if staged.Y+staged.H > top {
		t.Fatalf("the staged row ends at %v, past the line at %v: it is clipped",
			staged.Y+staged.H, top)
	}
}

// A merge line left where the user dragged it in a taller window still leaves
// the staged section its first row when the window shrinks under it.
func TestADraggedMergeLineKeepsTheStagedRowVisible(t *testing.T) {
	h, _ := changesTestAppIn(t, []repository.File{
		{Path: "conflict-a.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "conflict-b.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "staged.txt", Status: "M ", Staged: true, Tracked: true},
	}, 1040, 700)
	for range 3 {
		h.tester.Frame()
	}
	// The line was dragged to the bottom of a window that no longer exists.
	h.app.mergeAuto = false
	h.app.mergeHeight = 1000
	for range 2 {
		h.tester.Frame()
	}

	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	staged, ok := h.tester.Find("staged.txt")
	if !ok {
		t.Fatalf("the staged row is not drawn: %q", h.texts())
	}
	// The row keeps its own height rather than the sliver the pane has left.
	if staged.H < changeRowHeight/2 {
		t.Fatalf("the staged row is %v DIPs tall: the drag squeezed it out", staged.H)
	}
	if top := body.Y + h.app.changesTopHeight; staged.Y+staged.H > top+0.5 {
		t.Fatalf("the staged row ends at %v, past the line at %v: the drag squeezed it out",
			staged.Y+staged.H, top)
	}
}

// The list's scroll bar floats over its rows while the pointer is on them, so
// a row keeps a gutter at its end: the status letter must stay clear of the
// bar instead of moving under it on hover.
func TestTheFileRowKeepsClearOfTheScrollBar(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "gone.txt", Status: "D ", Staged: true, Deleted: true, Tracked: true},
	})
	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	status, ok := h.tester.Find("D")
	if !ok {
		t.Fatalf("the row shows no status letter: %q", h.texts())
	}
	barLeft := body.X + body.W - scrollbarGutter(&ui.Theme{})
	if status.X+status.W > barLeft {
		t.Fatalf("the status ends at %v, under the scroll bar that starts at %v",
			status.X+status.W, barLeft)
	}
}

// The three sections each scroll on their own: a wheel over one moves its
// list, not the ones above or below it, and none of them is squeezed out of
// the panel by a long list in another.
func TestEachChangeSectionScrollsOnItsOwn(t *testing.T) {
	var files []repository.File
	for i := 0; i < 6; i++ {
		files = append(files, repository.File{
			Path: fmt.Sprintf("conflict-%d.txt", i), Status: "UU", Unstaged: true, HasMergeConflicts: true,
		})
	}
	for i := 0; i < 8; i++ {
		files = append(files, repository.File{
			Path: fmt.Sprintf("staged-%d.txt", i), Status: "M ", Staged: true, Tracked: true,
		})
	}
	for i := 0; i < 10; i++ {
		files = append(files, repository.File{
			Path: fmt.Sprintf("work-%d.txt", i), Status: " M", Unstaged: true, Tracked: true,
		})
	}
	h, _ := changesTestApp(t, files)
	// The line's automatic height needs the panel measured once, so settle
	// the layout before reading where the sections and their lists are.
	for range 3 {
		h.tester.Frame()
	}
	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	// Every section keeps room for its list, even with the merge and staged
	// sections at their tallest.
	if h.app.changesTopHeight <= 0 || h.app.changesTopHeight >= body.H {
		t.Fatalf("the line is at %v in a %v tall pane", h.app.changesTopHeight, body.H)
	}
	mergeFirst, mergeLast := h.app.mergeList.Visible()
	stagedFirst, stagedLast := h.app.stagedList.Visible()
	changesFirst, changesLast := h.app.fileList.Visible()
	if mergeLast-mergeFirst >= len(h.app.mergeRows) {
		t.Fatalf("the merge list fits all %d files, so it never scrolls", len(h.app.mergeRows))
	}
	if stagedLast-stagedFirst >= len(h.app.stagedRows) {
		t.Fatalf("the staged list fits all %d files, so it never scrolls", len(h.app.stagedRows))
	}
	if changesLast-changesFirst >= len(h.app.changesRows) {
		t.Fatalf("the changes list fits all %d files, so it never scrolls", len(h.app.changesRows))
	}

	scroll := func(y float32) {
		t.Helper()
		h.tester.Move(body.X+body.W/2, y)
		h.tester.Scroll(body.X+body.W/2, y, 0, 160)
		h.tester.Frame()
	}
	top := h.app.changesTopHeight
	mergeY := body.Y + changeHeaderHeight + 20
	stagedY := body.Y + top - 40
	changesY := body.Y + top + 1 + changeHeaderHeight + 20

	scroll(mergeY)
	if first, _ := h.app.mergeList.Visible(); first == mergeFirst {
		t.Fatalf("the merge list did not scroll")
	}
	if first, _ := h.app.stagedList.Visible(); first != stagedFirst {
		t.Fatalf("scrolling the merge list moved the staged list")
	}
	if first, _ := h.app.fileList.Visible(); first != changesFirst {
		t.Fatalf("scrolling the merge list moved the changes list")
	}

	mergeFirst, _ = h.app.mergeList.Visible()
	scroll(stagedY)
	if first, _ := h.app.stagedList.Visible(); first == stagedFirst {
		t.Fatalf("the staged list did not scroll")
	}
	if first, _ := h.app.mergeList.Visible(); first != mergeFirst {
		t.Fatalf("scrolling the staged list moved the merge list")
	}
	if first, _ := h.app.fileList.Visible(); first != changesFirst {
		t.Fatalf("scrolling the staged list moved the changes list")
	}

	stagedFirst, _ = h.app.stagedList.Visible()
	scroll(changesY)
	if first, _ := h.app.fileList.Visible(); first == changesFirst {
		t.Fatalf("the changes list did not scroll")
	}
	if first, _ := h.app.stagedList.Visible(); first != stagedFirst {
		t.Fatalf("scrolling the changes list moved the staged list")
	}
}

// A merge with files and a staged section with none still fits: the line is
// placed past the placeholder "No files here" line, so neither the conflicted
// row above it nor that line is cut in half by the Changes header below.
func TestAMergeAndAnEmptyStagedSectionFitAboveTheChanges(t *testing.T) {
	files := []repository.File{
		{Path: "conflict-a.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		{Path: "conflict-b.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	}
	h, _ := changesTestApp(t, files)
	for range 3 {
		h.tester.Frame()
	}
	body, ok := h.tester.Find("Changes sections")
	if !ok {
		t.Fatalf("the sections have no split: %q", h.texts())
	}
	empty, ok := h.tester.Find("No files here")
	if !ok {
		t.Fatalf("the staged section shows no placeholder: %q", h.texts())
	}
	if top := body.Y + h.app.changesTopHeight; empty.Y+empty.H > top+0.5 {
		t.Fatalf("the placeholder runs to %v, past the line at %v: it is clipped", empty.Y+empty.H, top)
	}
	// Both conflicted rows are drawn under the merge header and before the
	// staged header, with room for the list's rows and the gaps between.
	stagedHeader, ok := h.tester.Find("Staged Changes")
	if !ok {
		t.Fatalf("the staged section has no header: %q", h.texts())
	}
	if mergeLast := stagedHeader.Y - changeDividerHeight; mergeLast < body.Y+changeHeaderHeight+2*changeRowHeight {
		t.Fatalf("the merge rows end at %v, before %v: they are squeezed", mergeLast, body.Y+changeHeaderHeight+2*changeRowHeight)
	}
}
