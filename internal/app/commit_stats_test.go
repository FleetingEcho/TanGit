package app

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestCommitBlocksStayVisibleAcrossRowStates(t *testing.T) {
	for _, section := range []int{sectionHistory, sectionGraph} {
		t.Run(workspaceSectionIDs[section], func(t *testing.T) {
			h, _ := historyHarness(t)
			h.app.workspaceTab = section
			h.tester.Frame()
			h.settle(t)
			commits := h.app.visibleCommits()
			commits[0].LinesAdded = 3870
			commits[0].LinesDeleted = 1674
			commits[1].LinesAdded = 100
			commits[1].LinesDeleted = 10
			h.app.selectedCommit = 0
			h.tester.Move(0, 0)
			h.tester.Frame()
			check := func() {
				t.Helper()
				var previous ui.Rect
				for i, commit := range commits[:2] {
					label := trf("Diff statistics for %s: %d additions, %d deletions", commit.ShortHash, commit.LinesAdded, commit.LinesDeleted)
					bounds, found := h.tester.Find(label)
					if !found {
						t.Fatalf("commit blocks missing: %v", h.texts())
					}
					if i > 0 && (bounds.X != previous.X || bounds.W != previous.W) {
						t.Fatalf("commit blocks misaligned: %+v, %+v", previous, bounds)
					}
					previous = bounds
				}
				if h.tester.HasText("+100") || h.tester.HasText("−10") {
					t.Fatal("numeric commit stats remain visible")
				}
			}
			check()
			subject := commits[1].Subject
			row, ok := h.tester.AccessByRole(ui.RoleListItem, subject)
			if !ok {
				t.Fatal("commit row missing")
			}
			checkHeight := func() {
				t.Helper()
				current, found := h.tester.AccessByRole(ui.RoleListItem, subject)
				if !found || current.Bounds.H != row.Bounds.H {
					t.Fatalf("row height changed: before=%+v after=%+v", row.Bounds, current.Bounds)
				}
			}
			h.tester.Move(row.Bounds.X+4, row.Bounds.Y+4)
			h.tester.Frame()
			check()
			checkHeight()
			if err := h.tester.Click(subject); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			check()
			checkHeight()
			h.tester.Move(0, 0)
			h.tester.Frame()
			check()
			checkHeight()
			label := trf("Diff statistics for %s: %d additions, %d deletions", commits[1].ShortHash, 100, 10)
			blocks, found := h.tester.Find(label)
			if !found {
				t.Fatal("commit blocks missing")
			}
			for i := 0; i < 6; i++ {
				// Six equally sized squares separated by five 2 DIP gaps.
				size := (blocks.W - 10) / 6
				h.tester.Move(blocks.X+float32(i)*(size+2)+size/2, blocks.Y+blocks.H/2)
				h.tester.Frame()
				h.tester.Advance(700 * time.Millisecond)
				h.tester.Frame()
				_, tooltipShown := h.tester.AccessByRole(ui.RoleTooltip, "")
				if !tooltipShown || !h.tester.HasText("+100") || !h.tester.HasText("−10") {
					t.Fatalf("block %d did not show exact totals: %v", i, h.tester.Texts())
				}
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, "commit-totals-"+workspaceSectionIDs[section], h.tester)
			}
		})
	}
}
