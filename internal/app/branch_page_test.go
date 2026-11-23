package app

import (
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func branchPageHarness(t *testing.T) *harness {
	t.Helper()
	h := menuTestApp(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()
	h.settle(t)
	return h
}

func TestBranchHeaderOmitsMaximizeButKeepsRename(t *testing.T) {
	h := branchPageHarness(t)
	for _, removed := range []string{"Expand branch page", "Restore branch page"} {
		if h.tester.HasText(removed) {
			t.Fatalf("branch header still offers %q", removed)
		}
	}
	if err := h.tester.Click("Rename…"); err != nil {
		t.Fatalf("the remaining rename control is unavailable: %v", err)
	}
	h.tester.Frame()
	if !h.app.showRenameForm || !h.tester.HasText("New branch name") {
		t.Fatal("removing maximize also disturbed the rename form")
	}
}

func TestBranchHistoryLoadingIsCentered(t *testing.T) {
	a := newApp(fakeOpener{})
	a.branchCommitsLoading = true
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			a.branchCommitsView(c, repository.Branch{Name: "main", Head: true})
		})
	}, 400, 420)
	tester.Frame()
	spinner, spinnerOK := tester.Find("Loading commits")
	message, messageOK := tester.Find("Reading the branch's history…")
	if !spinnerOK || !messageOK {
		t.Fatalf("missing centered loading presentation: %q", tester.Texts())
	}
	centerX, centerY := float32(200), float32(210)
	groupMidY := (spinner.Y + message.Y + message.H) / 2
	if spinner.X+spinner.W/2 < centerX-45 || spinner.X+spinner.W/2 > centerX+45 ||
		message.X+message.W/2 < centerX-45 || message.X+message.W/2 > centerX+45 ||
		groupMidY < centerY-55 || groupMidY > centerY+55 {
		t.Fatalf("loading is not centered in its pane: spinner=%+v message=%+v", spinner, message)
	}
	if spinner.Y >= message.Y {
		t.Fatalf("history loading icon and text should read top-to-bottom: spinner=%+v message=%+v", spinner, message)
	}
}

func TestBranchPageExpandsAndRestores(t *testing.T) {
	for _, escape := range []bool{false, true} {
		h := branchPageHarness(t)
		before, ok := h.tester.Find("Branch summary")
		if !ok {
			t.Fatal("the branch has no summary")
		}
		width, collapsed := h.app.branchListWidth, h.app.workspacePaneCollapsed[sectionBranches]
		h.app.branchFullscreen = true
		h.tester.Frame()
		h.tester.Frame()
		h.settle(t)
		after, ok := h.tester.Find("Branch summary")
		if !h.app.branchFullscreen || !ok || after.W <= before.W || after.X >= before.X {
			t.Fatalf("the branch did not expand: before=%+v after=%+v", before, after)
		}
		for _, pane := range h.app.visibleKeyboardPanes() {
			if !strings.HasPrefix(pane.id, "branch-") {
				t.Fatalf("expanded branch includes background keyboard pane %q", pane.id)
			}
		}
		h.press(t, ui.KeyF6)
		if !h.tester.Focused("Commits") && !h.tester.Focused("Commit files") && !h.tester.Focused("Diff") {
			t.Fatalf("panel traversal left the expanded branch: %q", h.app.keyboardLastPane)
		}
		if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
			saveScreenshot(t, dir, "branch-page-expanded", h.tester)
		}
		if escape {
			h.app.settings.DisableShortcuts = true
			h.press(t, ui.KeyEscape)
		} else {
			h.app.closeBranchFullscreen()
		}
		h.tester.Frame()
		h.tester.Frame()
		if h.app.branchFullscreen {
			t.Fatalf("the branch page did not restore (Escape=%v): %q", escape, h.texts())
		}
		if h.app.branchListWidth != width || h.app.workspacePaneCollapsed[sectionBranches] != collapsed {
			t.Fatal("expanding the page changed the normal branch layout")
		}
	}
}

func TestExpandedBranchEscapeCancelsFormsBeforeRestoring(t *testing.T) {
	h := branchPageHarness(t)
	h.app.branchFullscreen = true
	h.tester.Frame()
	h.settle(t)
	if err := h.tester.Click("Rename…"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.press(t, ui.KeyEscape)
	if h.app.showRenameForm || !h.app.branchFullscreen {
		t.Fatal("Escape should cancel rename before restoring the branch page")
	}
	if err := h.tester.Click("Soft reset to this commit"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("the commit action did not request confirmation")
	}
	h.press(t, ui.KeyEscape)
	if h.app.confirm != nil || !h.app.branchFullscreen {
		t.Fatal("Escape should cancel confirmation before restoring the branch page")
	}
}

func TestBranchSummaryDividerResizesAndSurvivesExpansion(t *testing.T) {
	h := branchPageHarness(t)
	for _, expanded := range []bool{false, true} {
		if expanded {
			h.app.branchFullscreen = true
			h.tester.Frame()
		}
		before := h.app.branchHeaderHeight
		summary, ok := h.tester.Find("Branch summary")
		if !ok {
			t.Fatal("the summary is missing")
		}
		dragTo(t, h.tester, summary.X+summary.W/2, summary.Y+summary.H, 0, -35)
		if h.app.branchHeaderHeight > before-25 {
			t.Fatalf("the summary divider did not shrink its height: %v -> %v", before, h.app.branchHeaderHeight)
		}
		h.tester.Frame()
		if !h.tester.HasText("Commits") {
			t.Fatal("resizing hid the branch history")
		}
	}
	shrunk := h.app.branchHeaderHeight
	h.press(t, ui.KeyEscape)
	if h.app.branchHeaderHeight != shrunk {
		t.Fatal("restoring the branch reset its adjusted summary height")
	}
}
