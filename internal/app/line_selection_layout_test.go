package app

import (
	"fmt"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestLineSelectionToolbarFitsNarrowPanes(t *testing.T) {
	for _, width := range []float32{260, 320, 520, 960} {
		for _, staged := range []bool{false, true} {
			t.Run(fmt.Sprintf("width_%g_staged_%v", width, staged), func(t *testing.T) {
				a := &app{
					diffState:        &editor.DiffState{LineSelection: true},
					diffPatch:        "patch",
					lineSelectMode:   true,
					lineSelectRanges: []editor.DiffLineRange{{Side: editor.DiffNew, Start: 1, End: 3}},
				}
				a.diffKind = "Unstaged"
				stageLabel := "Stage selected lines"
				if staged {
					a.diffKind, stageLabel = "Staged", "Unstage selected lines"
				}
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Padding(12).Background(c.Theme().Background).Children(func() {
						a.lineSelectionControls(c, true)
					})
				}, int(width), 240)
				tt.SetDark(true)
				tt.Frame()
				find := func(label string) ui.Rect {
					t.Helper()
					r, ok := tt.Find(label)
					if !ok {
						t.Fatalf("missing %q", label)
					}
					if r.X < 12 || r.X+r.W > width-12+1 {
						t.Fatalf("%q overflows a %g-wide pane: %+v", label, width, r)
					}
					return r
				}
				title, count := find("Selecting multiple lines"), find("3 lines selected")
				if title.H > 22 || count.H > 22 {
					t.Fatalf("selection status wraps into columns: title=%+v count=%+v", title, count)
				}
				stage := find(stageLabel)
				if stage.H > 22 || stage.W > 120 {
					t.Fatalf("the stage action is too large: %+v", stage)
				}
				if width >= 320 && stage.Y >= count.Y+count.H {
					t.Fatal("the selection toolbar should fit on one row")
				}
				cancel := find("Cancel")
				if cancel.H > 22 || cancel.W > 22 || cancel.X < count.X+count.W {
					t.Fatal("Cancel should be small and right aligned")
				}
				if !staged {
					stash := find("Stash selected lines…")
					if stash.H > 22 || stash.W > 120 {
						t.Fatalf("the stash action is too large: %+v", stash)
					}
					if stash.Y < stage.Y+stage.H && stash.X < stage.X+stage.W {
						t.Fatal("the action buttons overlap")
					}
				}
				if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
					saveScreenshot(t, dir, fmt.Sprintf("selection-%g-staged-%v", width, staged), tt)
				}
			})
		}
	}
}
