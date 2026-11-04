package app

import (
	"errors"
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// dragOn moves the pointer by dy from (x, y) with the button held, one frame
// per move, then releases it.
func dragTo(t *testing.T, tester *ui.Tester, x, y, dx, dy float32) {
	t.Helper()
	tester.Press(x, y)
	tester.Move(x+dx, y+dy)
	tester.Frame()
	tester.Release(x+dx, y+dy)
	tester.Frame()
}

func TestSidebarDividerResizesTheWorkspace(t *testing.T) {
	h := menuTestApp(t)

	before := h.app.navPaneWidth
	// The divider is the split's line, one DIP wide, islandGap/2 in from the
	// window's edge where the islands float.
	dragTo(t, h.tester, islandGap/2+before+0.5, 300, 40, 0)

	if h.app.navPaneWidth < before+30 {
		t.Fatalf("the sidebar did not follow the divider: %v -> %v", before, h.app.navPaneWidth)
	}
	// The section pane gives way rather than running off the window.
	rect, ok := h.tester.Find("one.txt")
	if !ok {
		t.Fatal("the file list disappeared")
	}
	if rect.X < h.app.navPaneWidth {
		t.Errorf("the section starts at x=%v, inside the sidebar %v", rect.X, h.app.navPaneWidth)
	}
}

func TestPaneTogglesStayAtTheTopRightAndRestoreAtTheZeroEdge(t *testing.T) {
	h := menuTestApp(t)
	collapse, ok := h.tester.Find("Collapse workspace sidebar")
	if !ok {
		t.Fatal("the workspace sidebar has no collapse control")
	}
	if collapse.W != paneToggleSize || collapse.H != paneToggleHeight || collapse.W <= collapse.H {
		t.Fatalf("the collapse control is %vx%v, want a consistent rectangle", collapse.W, collapse.H)
	}
	seam := islandGap/2 + h.app.navPaneWidth
	if got := collapse.X + collapse.W; got >= seam {
		t.Fatalf("the collapse control ends at %v, outside the island card before %v", got, seam)
	}
	workspace, ok := h.tester.Find("Workspace")
	if !ok || collapse.Y > workspace.Y+workspace.H+paneToggleTopInset {
		t.Fatalf("the collapse control is not at the pane's top-right: control=%+v heading=%+v", collapse, workspace)
	}

	if err := h.tester.Click("Collapse workspace sidebar"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	restore, ok := h.tester.Find("Expand workspace sidebar")
	if !ok {
		t.Fatal("the collapsed workspace sidebar has no restore control")
	}
	zeroEdgeCenter := islandGap + paneToggleSize/2 // host padding plus its lane inset
	if got := restore.X + restore.W/2; got != zeroEdgeCenter {
		t.Fatalf("the restore control centers at %v, want zero-edge lane %v", got, zeroEdgeCenter)
	}
	heading, ok := h.tester.Find("Changes")
	if !ok || restore.X+restore.W > heading.X {
		t.Fatalf("the restore control covers the remaining pane: control=%+v heading=%+v", restore, heading)
	}
}

func TestWorkspacePanelsCollapseAndRestore(t *testing.T) {
	h := menuTestApp(t)

	collapse, ok := h.tester.Find("Collapse workspace sidebar")
	if !ok {
		t.Fatalf("the workspace sidebar has no collapse control: %q", h.texts())
	}
	if err := h.tester.Click("Collapse workspace sidebar"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.navPaneCollapsed || !h.tester.HasText("Expand workspace sidebar") {
		t.Fatalf("the workspace sidebar did not collapse: %q", h.texts())
	}
	restore, ok := h.tester.Find("Expand workspace sidebar")
	if !ok {
		t.Fatalf("the collapsed workspace sidebar has no restore control: %q", h.texts())
	}
	if restore.X >= collapse.X-paneToggleSize {
		t.Fatalf("the restore control stayed at x=%v after the pane at x=%v collapsed", restore.X, collapse.X)
	}

	if err := h.tester.Click("Collapse file list"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.filePaneCollapsed || !h.tester.HasText("Expand file list") {
		t.Fatalf("the file list did not collapse: %q", h.texts())
	}
	if h.tester.HasText("Search files") {
		t.Fatalf("the collapsed file list still drew its controls: %q", h.texts())
	}

	if err := h.tester.Click("Expand file list"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.filePaneCollapsed || !h.tester.HasText("Collapse file list") {
		t.Fatalf("the file list did not return: %q", h.texts())
	}
	if err := h.tester.Click("Expand workspace sidebar"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.navPaneCollapsed || !h.tester.HasText("Collapse workspace sidebar") {
		t.Fatalf("the workspace sidebar did not return: %q", h.texts())
	}
	// An extra pass catches controls that accidentally kept an expired build
	// element while disappearing and returning.
	h.tester.Frame()
}

func TestEveryDetailPageCollapsesAndRestoresItsListPane(t *testing.T) {
	pages := []struct {
		name    string
		section int
		title   string
	}{
		{"history", sectionHistory, "Commits"},
		{"graph", sectionGraph, "Commit graph"},
		{"branches", sectionBranches, "Local branches"},
		{"remotes", sectionRemotes, "Remotes"},
		{"stash", sectionStash, "Saved work"},
		{"tags", sectionTags, "Repository tags"},
		{"worktrees", sectionWorktrees, "Linked worktrees"},
		{"submodules", sectionSubmodules, "Configured submodules"},
		{"reflog", sectionReflog, "Recent reference movements"},
	}

	for _, page := range pages {
		t.Run(page.name, func(t *testing.T) {
			h := menuTestApp(t)
			h.app.workspaceTab = page.section
			h.tester.Frame()
			if !h.tester.HasText(page.title) {
				t.Fatalf("the %s list did not render: %q", page.name, h.texts())
			}

			if err := h.tester.Click("Collapse list pane"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if !h.app.workspacePaneCollapsed[page.section] || !h.tester.HasText("Expand list pane") {
				t.Fatalf("the %s list did not collapse: %q", page.name, h.texts())
			}
			if h.tester.HasText(page.title) {
				t.Fatalf("the collapsed %s list still rendered", page.name)
			}

			if err := h.tester.Click("Expand list pane"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if h.app.workspacePaneCollapsed[page.section] || !h.tester.HasText(page.title) {
				t.Fatalf("the %s list did not return: %q", page.name, h.texts())
			}
			// Catch stale elements after the list disappears and returns.
			h.tester.Frame()
		})
	}
}

func TestBranchesCommitListCollapsesAndRestores(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()

	if err := h.tester.Click("Collapse commit list"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.branchCommitsCollapsed || !h.tester.HasText("Expand commit list") {
		t.Fatalf("the branch commits did not collapse: %q", h.texts())
	}
	if err := h.tester.Click("Expand commit list"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.branchCommitsCollapsed || !h.tester.HasText("Collapse commit list") {
		t.Fatalf("the branch commits did not return: %q", h.texts())
	}
	h.tester.Frame()
}

// Nested collapsed panes all own one slim rail. The parent's padding already
// places each host after the previous rail, so adding the earlier lanes again
// makes inner rails wider and pushes their content and icons out of alignment.
func TestNestedCollapsedPanesUseIdenticalRails(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionBranches
	h.app.navPaneCollapsed = true
	h.app.workspacePaneCollapsed[sectionBranches] = true
	h.app.branchCommitsCollapsed = true
	h.tester.Frame()

	labels := []string{"Expand workspace sidebar", "Expand list pane", "Expand commit list"}
	controls := make([]ui.Rect, len(labels))
	for i, label := range labels {
		control, ok := h.tester.Find(label)
		if !ok {
			t.Fatalf("the collapsed panes have no %q control: %q", label, h.texts())
		}
		if control.W != paneToggleSize || control.H != paneToggleHeight {
			t.Errorf("%q is %vx%v, want the standard %vx%v control",
				label, control.W, control.H, paneToggleSize, paneToggleHeight)
		}
		controls[i] = control
		if i == 0 {
			continue
		}
		// The commit list lives inside the branch card, whose own margin,
		// border, and padding remain around its rail. They are not extra width.
		parentInset := float32(0)
		if label == "Expand commit list" {
			parentInset = islandGap/2 + 1 + 16
		}
		want := paneToggleSize + islandGap + parentInset
		if got := control.X - controls[i-1].X; got != want {
			t.Errorf("%q starts %v after the previous icon, want %v including its parent inset",
				label, got, want)
		}
	}

	// Sample below the icons, where only the rail's own backdrop is drawn. In
	// particular, the nested rail must not inherit the surrounding card face.
	image := h.tester.Image()
	railColor := func(control ui.Rect) color.RGBA {
		x := int(control.X + control.W/2)
		y := int(control.Y + control.H + 32)
		return color.RGBAModel.Convert(image.At(x, y)).(color.RGBA)
	}
	want := railColor(controls[0])
	for i, control := range controls[1:] {
		if got := railColor(control); got != want {
			t.Errorf("%q has rail color %v, want the shared %v", labels[i+1], got, want)
		}
	}
}

func TestDetailPagesRememberCollapseStateIndependently(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionHistory
	h.tester.Frame()
	if err := h.tester.Click("Collapse list pane"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	h.app.workspaceTab = sectionGraph
	h.tester.Frame()
	if !h.tester.HasText("Commit graph") || !h.tester.HasText("Collapse list pane") {
		t.Fatalf("collapsing History also hid Graph's list: %q", h.texts())
	}

	h.app.workspaceTab = sectionHistory
	h.tester.Frame()
	if !h.tester.HasText("Expand list pane") || h.tester.HasText("Commits") {
		t.Fatalf("History forgot that its list was collapsed: %q", h.texts())
	}
}

func TestTerminalDividerResizesThePane(t *testing.T) {
	original := newTerminal
	newTerminal = func(terminal.Options) (*terminal.Terminal, error) {
		return nil, errors.New("no shell here")
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the terminal to report its error", func() bool {
		return h.app.term.err != ""
	})
	// The panel slides up over 200ms; let the animation finish.
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()

	pane, ok := h.tester.Find("Terminal pane")
	if !ok {
		t.Fatalf("the terminal pane did not show: %q", h.texts())
	}
	beforePercent := h.app.term.percent
	beforeHeight := pane.H

	// The divider is the island seam, islandGap/2 above the pane's top
	// edge: dragging it up makes the pane taller, and the app keeps that
	// height.
	dragTo(t, h.tester, pane.X+100, pane.Y-islandGap/2, 0, -60)

	if h.app.term.percent <= beforePercent {
		t.Fatalf("the pane did not follow the divider: %v%% -> %v%%", beforePercent, h.app.term.percent)
	}
	resized, ok := h.tester.Find("Terminal pane")
	if !ok {
		t.Fatal("the terminal pane disappeared")
	}
	if resized.H < beforeHeight+40 {
		t.Fatalf("the pane is %v tall, was %v", resized.H, beforeHeight)
	}
	// The divider keeps the height the drag left it at, without sliding back.
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	settled, ok := h.tester.Find("Terminal pane")
	if !ok || settled.H < beforeHeight+40 {
		t.Fatalf("the pane slid back to %v tall", settled.H)
	}
}
