package app

import (
	"context"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestCompareRefsUsesWorkspacePage(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.KeyW)

	if h.app.workspaceTab != sectionCompare {
		t.Fatalf("W opened section %d, want Compare refs", h.app.workspaceTab)
	}
	if !h.tester.HasText("From ref") || !h.tester.HasText("To ref") {
		t.Fatalf("the comparison page is missing its ref fields: %q", h.texts())
	}
	if _, found := h.tester.AccessByRole(ui.RoleDialog, "Compare refs"); found {
		t.Fatal("Compare refs is still presented as a dialog")
	}
	results, found := h.tester.AccessByRole(ui.RoleRegion, "Comparison results")
	if !found {
		t.Fatal("the comparison results card is missing")
	}
	if results.Bounds.H < 500 {
		t.Fatalf("the empty results card does not fill the remaining page: %+v", results.Bounds)
	}
	theme := themeForSettings(h.app.settings, true)
	x, y := int(results.Bounds.X+18), int(results.Bounds.Y+results.Bounds.H-20)
	gotColor := color.RGBAModel.Convert(h.tester.Image().At(x, y))
	wantColor := color.RGBA{R: theme.Background.R, G: theme.Background.G, B: theme.Background.B, A: theme.Background.A}
	if gotColor != wantColor {
		t.Fatalf("empty results background = %v, want the card background %v", gotColor, wantColor)
	}

	h.app.setWorkspaceTab(sectionChanges)
	h.tester.Frame()
	if h.tester.HasText("From ref") {
		t.Fatal("Compare refs remained above the newly selected workspace page")
	}
	h.press(t, ui.KeyEscape)
	if h.app.workspaceTab != sectionChanges {
		t.Fatal("Escape reopened the comparison page")
	}

	h.app.setWorkspaceTab(sectionCompare)
	h.tester.Frame()
	if h.app.compare.from != "HEAD~1" || h.app.compare.to != "HEAD" {
		t.Fatalf("switching pages reset the refs: %q to %q", h.app.compare.from, h.app.compare.to)
	}
}

func TestCompareRefsPageLoadsFilesAndAppliesOptions(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.waitFor(t, "the underlying working-tree diff", func() bool { return h.app.diffState != nil })
	underlyingDiff := h.app.diffState
	underlyingFile := h.app.selectedFile

	h.press(t, ui.KeyW)
	if h.app.workspaceTab != sectionCompare {
		t.Fatal("W did not open the Compare refs page")
	}
	if h.app.compare.from != "HEAD~1" || h.app.compare.to != "HEAD" {
		t.Fatalf("default refs = %q to %q", h.app.compare.from, h.app.compare.to)
	}
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatalf("start comparison: %v", err)
	}
	h.waitFor(t, "the changed-file list and first diff", func() bool {
		return len(h.app.compare.files) == 2 && h.app.compare.diffState != nil
	})
	wantListCall := "compare-files:HEAD~1:HEAD:false:3"
	if !calls.has(wantListCall) {
		t.Fatalf("comparison call %q missing from %q", wantListCall, calls.all())
	}
	if h.app.compare.selected != 0 || h.app.compare.diffState.ContextLines != 3 {
		t.Fatalf("initial selection/context = %d/%d", h.app.compare.selected, h.app.compare.diffState.ContextLines)
	}

	if err := h.tester.Click("compare-two.txt"); err != nil {
		t.Fatalf("choose the second compared file: %v", err)
	}
	h.waitFor(t, "the second compared file's diff", func() bool {
		return calls.has("compare-file-diff:HEAD~1:HEAD:compare-two.txt:false:3") &&
			h.app.compare.selected == 1 && h.app.compare.diffState != nil
	})

	ordinaryOptions := h.app.diffOptions
	if err := h.tester.Click("Toggle whitespace-only diff changes"); err != nil {
		t.Fatalf("toggle whitespace: %v", err)
	}
	h.tester.Frame()
	h.waitFor(t, "the whitespace-ignoring comparison", func() bool {
		return calls.has("compare-files:HEAD~1:HEAD:true:3") &&
			calls.has("compare-file-diff:HEAD~1:HEAD:compare-one.txt:true:3") &&
			h.app.compare.diffState != nil
	})

	// Compare's visible patch options stay independent from ordinary diffs.
	for _, contextLines := range []int{5, 10, 0} {
		if err := h.tester.Click("Change diff context lines"); err != nil {
			t.Fatalf("change comparison context: %v", err)
		}
		h.tester.Frame()
		h.waitFor(t, "the context change to settle", func() bool {
			return h.app.compare.appliedOptions.ContextLines == contextLines &&
				!h.app.compare.filesLoading && h.app.compare.diffState != nil
		})
	}
	if h.app.diffOptions != ordinaryOptions {
		t.Fatalf("comparison controls changed ordinary diff options: %#v", h.app.diffOptions)
	}
	h.waitFor(t, "the zero-context comparison", func() bool {
		return calls.has("compare-files:HEAD~1:HEAD:true:0") &&
			calls.has("compare-file-diff:HEAD~1:HEAD:compare-one.txt:true:0") &&
			h.app.compare.diffState != nil && !h.app.compare.filesLoading
	})
	if h.app.compare.diffState.ContextLines != 0 {
		t.Errorf("viewer context = %d, want zero", h.app.compare.diffState.ContextLines)
	}

	if err := h.tester.Click("Swap refs"); err != nil {
		t.Fatalf("swap refs: %v", err)
	}
	h.waitFor(t, "the reversed comparison", func() bool {
		return calls.has("compare-files:HEAD:HEAD~1:true:0") &&
			h.app.compare.files != nil && !h.app.compare.filesLoading
	})
	if h.app.compare.from != "HEAD" || h.app.compare.to != "HEAD~1" {
		t.Errorf("swapped refs = %q to %q", h.app.compare.from, h.app.compare.to)
	}

	// A page switch keeps the completed comparison and the working-tree
	// selection. Escape does not turn the page back into a closable dialog.
	h.app.setWorkspaceTab(sectionCompare)
	h.tester.Frame()
	h.press(t, ui.KeyEscape)
	if h.app.workspaceTab != sectionCompare || len(h.app.compare.files) == 0 {
		t.Error("Escape closed or reset the Compare refs page")
	}
	h.app.setWorkspaceTab(sectionChanges)
	h.tester.Frame()
	if h.app.diffState != underlyingDiff || h.app.selectedFile != underlyingFile {
		t.Error("switching comparison pages changed the underlying file or diff")
	}

	// The repository header does not duplicate Compare navigation. Its W
	// shortcut still restores the workspace without resetting its state.
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Compare refs"); found {
		t.Fatal("the repository header still shows the removed Compare refs button")
	}
	h.press(t, ui.KeyW)
	h.tester.Frame()
	if h.app.workspaceTab != sectionCompare || h.app.compare.from != "HEAD" || h.app.compare.to != "HEAD~1" {
		t.Error("W did not restore the comparison page and its state")
	}
}

func TestCompareCancelRestoresDefaultsAndClearsResults(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.press(t, ui.KeyW)
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "a completed comparison", func() bool {
		return len(h.app.compare.files) == 2 && h.app.compare.diffState != nil
	})

	from, fromFound := h.tester.Access("From ref")
	compare, compareFound := h.tester.AccessByRole(ui.RoleButton, "Compare")
	refresh, refreshFound := h.tester.AccessByRole(ui.RoleButton, "Refresh comparison")
	swap, swapFound := h.tester.AccessByRole(ui.RoleButton, "Swap refs")
	reset, resetFound := h.tester.AccessByRole(ui.RoleButton, "Cancel comparison")
	if !fromFound || !compareFound || !refreshFound || !swapFound || !resetFound {
		t.Fatal("the compact comparison controls are incomplete")
	}
	controls := []ui.Rect{compare.Bounds, refresh.Bounds, swap.Bounds, reset.Bounds}
	for index, bounds := range controls {
		if index > 0 && controls[index-1].X+controls[index-1].W > bounds.X {
			t.Fatalf("comparison actions are out of order: %+v", controls)
		}
		if bounds.Y != compare.Bounds.Y || bounds.H != from.Bounds.H {
			t.Fatalf("comparison control is not aligned with the input: input=%+v controls=%+v", from.Bounds, controls)
		}
	}

	h.app.compare.from = "feature"
	h.app.compare.to = "main"
	h.app.compare.context = "5"
	h.app.compare.options.IgnoreWhitespace = true
	if err := h.tester.Click("Cancel comparison"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.compare.from != "HEAD~1" || h.app.compare.to != "HEAD" || h.app.compare.context != "3" {
		t.Fatalf("reset controls = %q to %q, context %q", h.app.compare.from, h.app.compare.to, h.app.compare.context)
	}
	if h.app.compare.options.IgnoreWhitespace || h.app.compare.options.ContextLines != 3 {
		t.Fatalf("reset options = %#v", h.app.compare.options)
	}
	if h.app.compare.appliedFrom != "" || h.app.compare.appliedTo != "" || h.app.compare.files != nil ||
		h.app.compare.diffState != nil || h.app.compare.selected != -1 || h.app.compare.filesLoading {
		t.Fatalf("reset retained comparison results: %#v", h.app.compare)
	}
	if !h.tester.HasText("Choose two refs, then Compare.") {
		t.Fatal("reset did not return to the empty comparison prompt")
	}
	for _, call := range calls.all() {
		if call == "compare-files:feature:main:true:5" {
			t.Fatal("reset accidentally started the edited comparison")
		}
	}
}

func TestCompareRefInputsSuggestBranchesAndKeepArbitraryRevisions(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.KeyW)

	choices := h.app.compareRefChoices("", true)
	if len(choices) < 2 || choices[0] != "HEAD" || choices[1] != "HEAD~1" {
		t.Fatalf("the revision shortcuts are missing or out of order: %q", choices)
	}
	if err := h.tester.Click("From ref"); err != nil {
		t.Fatalf("open the source ref choices: %v", err)
	}
	h.tester.Frame()
	for _, ref := range []string{"main", "feature", "origin/feature/menus", "v1.0.0"} {
		if !h.tester.HasText(ref) {
			t.Fatalf("the source ref choices have no %q: %q", ref, h.texts())
		}
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("menus")
	h.tester.Frame()
	if !h.tester.HasText("origin/feature/menus") || h.tester.HasText("origin/main") {
		t.Fatalf("typing did not filter the ref choices: %q", h.texts())
	}
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if got := h.app.compare.from; got != "origin/feature/menus" {
		t.Fatalf("chosen source ref = %q", got)
	}

	if err := h.tester.Click("To ref"); err != nil {
		t.Fatalf("focus the target ref: %v", err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("HEAD~2")
	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if got := h.app.compare.to; got != "HEAD~2" {
		t.Fatalf("an arbitrary revision was not preserved: %q", got)
	}
}

type controlledCompareListRequest struct {
	from, to string
	options  repository.DiffOptions
	response chan controlledCompareListResponse
}

type controlledCompareListResponse struct {
	files []repository.CommitFile
	err   error
}

type controlledCompareSession struct {
	fakeSession
	requests chan *controlledCompareListRequest
}

func (s *controlledCompareSession) CompareFiles(_ context.Context, from, to string, options repository.DiffOptions) ([]repository.CommitFile, error) {
	request := &controlledCompareListRequest{
		from: from, to: to, options: options,
		response: make(chan controlledCompareListResponse, 1),
	}
	s.requests <- request
	response := <-request.response
	return response.files, response.err
}

func TestCompareDropsAnOutdatedFileList(t *testing.T) {
	snapshot := repository.Snapshot{
		Root: "/work/project", Branch: "main",
		Commits: []repository.Commit{
			{Hash: "c2", ShortHash: "c2", Parents: []string{"c1"}},
			{Hash: "c1", ShortHash: "c1"},
		},
	}
	session := &controlledCompareSession{
		fakeSession: fakeSession{snapshot: snapshot},
		requests:    make(chan *controlledCompareListRequest, 2),
	}
	a := newApp(fixedSessionOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = snapshot.Root
	openFixture(t, h.tester)
	h.settle(t)

	h.press(t, ui.KeyW)
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatal(err)
	}
	first := nextCompareListRequest(t, session.requests)
	if first.from != "HEAD~1" || first.to != "HEAD" {
		t.Fatalf("first request = %q to %q", first.from, first.to)
	}

	// Swap while the first read is blocked. The replacement has its own refs
	// and options, and the first response is no longer useful.
	if err := h.tester.Click("Swap refs"); err != nil {
		t.Fatal(err)
	}
	second := nextCompareListRequest(t, session.requests)
	if second.from != "HEAD" || second.to != "HEAD~1" {
		t.Fatalf("replacement request = %q to %q", second.from, second.to)
	}
	second.response <- controlledCompareListResponse{files: []repository.CommitFile{
		{Path: "new-result.txt", Status: "M"},
	}}
	h.waitFor(t, "the replacement comparison", func() bool {
		return len(h.app.compare.files) == 1 && h.app.compare.files[0].Path == "new-result.txt"
	})

	first.response <- controlledCompareListResponse{files: []repository.CommitFile{
		{Path: "outdated-result.txt", Status: "M"},
	}}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if len(h.app.compare.files) != 1 || h.app.compare.files[0].Path != "new-result.txt" {
		t.Fatalf("outdated response replaced the current files: %#v", h.app.compare.files)
	}
}

func nextCompareListRequest(t *testing.T, requests <-chan *controlledCompareListRequest) *controlledCompareListRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a comparison request")
		return nil
	}
}

type refreshingCompareSession struct {
	fakeSession
	files []repository.CommitFile
}

func (s *refreshingCompareSession) CompareFiles(ctx context.Context, from, to string, options repository.DiffOptions) ([]repository.CommitFile, error) {
	if _, err := s.fakeSession.CompareFiles(ctx, from, to, options); err != nil {
		return nil, err
	}
	return append([]repository.CommitFile(nil), s.files...), nil
}

func TestCompareRefreshRereadsMovingRefs(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	session := &refreshingCompareSession{
		fakeSession: fakeSession{snapshot: h.app.snapshot, calls: calls},
		files:       []repository.CommitFile{{Path: "compare-one.txt", Status: "M"}},
	}
	h.app.session = session
	h.press(t, ui.KeyW)
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the initial comparison", func() bool {
		return len(h.app.compare.files) == 1 && h.app.compare.diffState != nil && !h.app.compare.filesLoading
	})

	// A name such as HEAD can resolve to new repository contents while the
	// form text stays unchanged. Refresh must issue a new read, not restore a
	// cached file list.
	session.files = []repository.CommitFile{
		{Path: "compare-one.txt", Status: "M"},
		{Path: "compare-two.txt", Status: "A"},
	}
	if err := h.tester.Click("Refresh comparison"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the refreshed moving-ref comparison", func() bool {
		return len(h.app.compare.files) == 2 && h.app.compare.files[1].Path == "compare-two.txt" &&
			h.app.compare.diffState != nil && !h.app.compare.filesLoading
	})
}

func compareTestApp(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	h.press(t, ui.KeyW)
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatalf("start comparison: %v", err)
	}
	h.waitFor(t, "the comparison and first diff", func() bool {
		return len(h.app.compare.files) > 0 && h.app.compare.diffState != nil && !h.app.compare.filesLoading
	})
	return h, calls
}

func TestCompareFileTreeSearchKeepsSourceIndicesAndFoldsDirectories(t *testing.T) {
	h, calls := compareTestApp(t)
	h.app.compare.files = nestedTreeCommitFiles()
	h.app.compare.selected = 0
	h.app.compare.selectedRow = -1
	h.app.compare.fileRows = nil
	h.app.compare.fileSearch = "app.go"
	h.app.fileTreeMode = true
	h.app.clearCompareDiff()
	h.tester.Frame()

	if !h.tester.HasText("Search files") || !h.tester.HasText("src") || !h.tester.HasText("ui") {
		t.Fatalf("the searchable comparison tree is incomplete: %q", h.texts())
	}
	if !h.tester.HasText("Show files as a flat list") || h.tester.HasText("Expand all directories") ||
		h.tester.HasText("Collapse all directories") {
		t.Fatalf("Compare has the wrong directory controls: %q", h.texts())
	}
	if h.app.compare.selected != 2 || h.app.compare.files[h.app.compare.selected].Path != "src/ui/app.go" {
		t.Fatalf("the filtered tree targeted source %d, want 2", h.app.compare.selected)
	}
	h.waitFor(t, "the filtered source file's diff", func() bool {
		return calls.has("compare-file-diff:HEAD~1:HEAD:src/ui/app.go:false:3") &&
			h.app.compare.diffState != nil
	})

	if err := h.tester.Click("Collapse directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if got := visibleCommitTreePaths(h.app.compare.fileRows); len(got) != 1 || got[0] != "src" {
		t.Fatalf("collapsed comparison rows = %v", got)
	}
	if h.app.compare.selected != -1 || h.app.compare.diffState != nil || h.app.compare.diffPatch != "" {
		t.Fatal("a directory selection retained the previous file diff")
	}
	if err := h.tester.Click("Expand directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("app.go"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.compare.selected != 2 {
		t.Fatalf("expanding and choosing the nested row targeted source %d", h.app.compare.selected)
	}

	h.app.compare.fileSearch = "missing"
	h.tester.Frame()
	if len(h.app.compare.fileRows) != 0 || h.app.compare.selected != -1 || !h.tester.HasText("No files match this filter") {
		t.Fatalf("an empty comparison search retained rows or selection: rows=%v selected=%d texts=%q",
			h.app.compare.fileRows, h.app.compare.selected, h.texts())
	}
}

func TestCompareListPaneCollapsesAndRestoresWithoutLosingTheDiff(t *testing.T) {
	h, _ := compareTestApp(t)
	width := h.app.compare.listWidth
	diff := h.app.compare.diffState
	selected := h.app.compare.selected

	if err := h.tester.Click("Collapse list pane"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.workspacePaneCollapsed[sectionCompare] || !h.tester.HasText("Expand list pane") ||
		h.tester.HasText("Search files") {
		t.Fatalf("the comparison list did not collapse cleanly: %q", h.texts())
	}
	if h.app.compare.diffState != diff || h.app.compare.selected != selected {
		t.Fatal("collapsing the comparison list changed its selection or diff")
	}
	if err := h.tester.Click("Expand list pane"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.workspacePaneCollapsed[sectionCompare] || !h.tester.HasText("Search files") ||
		h.app.compare.listWidth != width || h.app.compare.diffState != diff {
		t.Fatal("restoring the comparison list did not preserve its width and diff")
	}
}

func TestComparePanesKeepSpaceAroundTheDivider(t *testing.T) {
	h, _ := compareTestApp(t)
	download, found := h.tester.AccessByRole(ui.RoleButton, "Save comparison diff")
	if !found {
		t.Fatal("the whole comparison download is missing")
	}
	results, found := h.tester.AccessByRole(ui.RoleRegion, "Comparison results")
	if !found || h.app.compare.listWidth <= 0 {
		t.Fatal("the comparison panes have no measurable layout")
	}
	paneRight := results.Bounds.X + 12 + h.app.compare.listWidth
	margin := paneRight - (download.Bounds.X + download.Bounds.W)
	if margin < comparePaneDividerInset || margin > comparePaneDividerInset+1 {
		t.Fatalf("whole comparison download has %v DIP to the pane border, want %v",
			margin, comparePaneDividerInset)
	}
	diffHeader, found := h.tester.AccessByRole(ui.RoleRegion, "File diff")
	if !found {
		t.Fatal("the selected comparison file heading is missing its region")
	}
	if margin := diffHeader.Bounds.X - (paneRight + 1); margin < comparePaneDividerInset || margin > comparePaneDividerInset+1 {
		t.Fatalf("selected comparison file has %v DIP from the pane border, want %v",
			margin, comparePaneDividerInset)
	}
}

func TestCompareExportsSelectedFileAndWholeComparisonPatches(t *testing.T) {
	t.Run("selected file", func(t *testing.T) {
		h, _ := compareTestApp(t)
		saved := filepath.Join(t.TempDir(), "selected.patch")
		dialog := &saveDialog{path: saved, done: make(chan struct{})}
		stubSaveDialog(t, dialog)
		if err := h.tester.Click("Save diff"); err != nil {
			t.Fatal(err)
		}
		<-dialog.done
		h.waitFor(t, "the selected comparison file to be saved", func() bool {
			content, err := os.ReadFile(saved)
			return err == nil && string(content) == h.app.compare.diffPatch
		})
		if want := filepath.Join("/work/project", "compare-one.txt.patch"); dialog.opts.DefaultPath != want {
			t.Fatalf("selected comparison save path = %q, want %q", dialog.opts.DefaultPath, want)
		}
	})

	t.Run("whole comparison", func(t *testing.T) {
		h, calls := compareTestApp(t)
		saved := filepath.Join(t.TempDir(), "all.patch")
		dialog := &saveDialog{path: saved, done: make(chan struct{})}
		stubSaveDialog(t, dialog)
		if err := h.tester.Click("Save comparison diff"); err != nil {
			t.Fatal(err)
		}
		<-dialog.done
		h.waitFor(t, "the whole comparison patch to be saved", func() bool {
			content, err := os.ReadFile(saved)
			return err == nil && string(content) == fakeWorkingTreePatch
		})
		if !calls.has("compare-diff:HEAD~1:HEAD:false:3") {
			t.Fatalf("the whole comparison did not use its applied options: %q", calls.all())
		}
		if want := filepath.Join("/work/project", "comparison.patch"); dialog.opts.DefaultPath != want {
			t.Fatalf("whole comparison save path = %q, want %q", dialog.opts.DefaultPath, want)
		}
	})
}

func TestCompareDiffActionsAreSmallAndRightAligned(t *testing.T) {
	h, _ := compareTestApp(t)
	labels := []string{
		"Toggle whitespace-only diff changes", "Change diff context lines", "Toggle diff layout",
		"Toggle diff line wrapping", "Save diff", "Expand diff",
	}
	var previous ui.Rect
	for index, label := range labels {
		access, found := h.tester.AccessByRole(ui.RoleButton, label)
		if !found {
			t.Fatalf("Compare is missing %q: %q", label, h.texts())
		}
		bounds := access.Bounds
		if bounds.W != compareDiffActionSize || bounds.H != compareDiffActionSize {
			t.Errorf("%q is %vx%v, want %vx%v", label, bounds.W, bounds.H,
				compareDiffActionSize, compareDiffActionSize)
		}
		if bounds.X < 640 {
			t.Errorf("%q starts at %.0f, want the actions on the right side of the file header", label, bounds.X)
		}
		if index > 0 {
			if bounds.Y != previous.Y {
				t.Errorf("%q is not on the same row as the previous action: %+v, %+v", label, previous, bounds)
			}
			if gap := bounds.X - (previous.X + previous.W); gap != 4 {
				t.Errorf("gap before %q = %v, want 4", label, gap)
			}
		}
		previous = bounds
	}
}

func TestCompareWholeWindowDiffSwitchesFilesAndKeepsAllControls(t *testing.T) {
	h, calls := compareTestApp(t)
	ordinaryOptions := h.app.diffOptions
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.diffFullscreen || len(h.app.diffFullscreenRows) != len(h.app.compare.files) {
		t.Fatal("the comparison did not open with its files in the whole-window diff")
	}
	for _, label := range []string{
		"Toggle whitespace-only diff changes", "Change diff context lines", "Toggle diff layout",
		"Toggle diff line wrapping", "Save diff", "Restore diff view",
	} {
		if !h.tester.HasText(label) {
			t.Fatalf("the whole comparison view is missing %q: %q", label, h.texts())
		}
	}

	// The page behind the overlay has the same Changed files heading, so drive
	// the overlay row's own choice rather than resolving an ambiguous label.
	h.app.diffFullscreenRows[1].choose()
	h.tester.Frame()
	h.waitFor(t, "the second comparison file in the whole-window view", func() bool {
		return h.app.compare.selected == 1 && h.app.compare.diffKey == "compare-two.txt" &&
			h.app.compare.diffState != nil && !h.app.compare.diffLoading
	})
	if !calls.has("compare-file-diff:HEAD~1:HEAD:compare-two.txt:false:3") {
		t.Fatalf("the whole-window file choice did not read the selected comparison: %q", calls.all())
	}
	if h.app.diffOptions != ordinaryOptions {
		t.Fatal("the comparison fullscreen view changed ordinary diff options")
	}
	h.press(t, ui.KeyEscape)
	if h.app.diffFullscreen || h.app.compare.selected != 1 {
		t.Fatal("closing the whole-window comparison lost its selected file")
	}
}

func TestCompareSlashSearchAndF3NavigateVisibleFiles(t *testing.T) {
	h, _ := compareTestApp(t)
	h.tester.Key(0, ui.KeySlash)
	h.tester.Frame()
	h.tester.Type("compare")
	h.tester.Frame()
	if h.app.compare.fileSearch != "compare" || h.app.compare.selected != 0 {
		t.Fatalf("slash did not focus the comparison search: query=%q selected=%d",
			h.app.compare.fileSearch, h.app.compare.selected)
	}
	h.tester.Key(0, ui.KeyF3)
	h.tester.Frame()
	if h.app.compare.selected != 1 {
		t.Fatalf("F3 selected comparison source %d, want the second file", h.app.compare.selected)
	}
	h.tester.Key(0, ui.KeyF3)
	h.tester.Frame()
	if h.app.compare.selected != 0 {
		t.Fatalf("wrapping F3 selected comparison source %d, want the first file", h.app.compare.selected)
	}
	h.tester.Key(ui.Shift, ui.KeyF3)
	h.tester.Frame()
	if h.app.compare.selected != 1 {
		t.Fatalf("Shift+F3 selected comparison source %d, want the second file", h.app.compare.selected)
	}
}
