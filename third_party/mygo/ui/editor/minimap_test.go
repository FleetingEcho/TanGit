package editor

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMinimapRunsPreserveIndentAndSemanticKinds(t *testing.T) {
	minimapTestCase := "  func main()"
	tokens := []Token{{End: 2, Kind: TokenText}, {End: 6, Kind: TokenKeyword}, {End: 7, Kind: TokenText}, {End: 11, Kind: TokenFunction}}
	semantic := []SemanticToken{{Start: 7, End: 11, Kind: TokenProperty}}
	got := minimapRuns(minimapTestCase, tokens, semantic, 0)
	want := []minimapRun{
		{text: "  ", kind: TokenText},
		{text: "func", kind: TokenKeyword},
		{text: " ", kind: TokenText},
		{text: "main", kind: TokenProperty},
		{text: "()", kind: TokenText},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Minimap runs = %#v, want %#v", got, want)
	}
}

func TestMinimapLeftAlignsRunsAndNavigates(t *testing.T) {
	state := &State{Buffer: NewBuffer("x\nlong")}
	tester := ui.NewTester(func(c *ui.Context) {
		Minimap(c, state).Width(86)
	}, 320, 240)
	tester.Frame()
	short, shortOK := tester.Find("x")
	long, longOK := tester.Find("long")
	if !shortOK || !longOK {
		t.Fatalf("Minimap text is missing: %q", tester.Texts())
	}
	if short.X != long.X {
		t.Fatalf("Minimap lines are not left-aligned: short=%+v long=%+v", short, long)
	}
	if err := tester.Click("Go to line 2"); err != nil {
		t.Fatal(err)
	}
	if state.ScrollY() <= 0 {
		t.Fatal("clicking the Minimap did not reveal its document line")
	}
}

func TestMinimapDragScrollsEditorContinuously(t *testing.T) {
	state := &State{Buffer: NewBuffer(strings.Repeat("value\n", 200))}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			View(c, state).Grow(1).FillWidth().FillHeight()
			Minimap(c, state).Width(86).FillHeight().Label("minimap overview")
		})
	}, 400, 500)
	tester.Frame()
	row, found := tester.Find("Go to line 1")
	if !found {
		t.Fatalf("Minimap row not found: %q", tester.Texts())
	}
	x, y := row.X+row.W/2, row.Y+row.H/2
	tester.Press(x, y)
	tester.Move(x, y+48)
	tester.Frame()
	if state.ScrollY() == 0 {
		t.Fatal("dragging inside the Minimap did not scroll the editor before release")
	}
	during := state.ScrollY()
	tester.Release(x, y+48)
	tester.Frame()
	if math.Abs(state.ScrollY()-during) > 0.01 {
		t.Fatalf("releasing a Minimap drag changed the scroll unexpectedly: during=%v after=%v", during, state.ScrollY())
	}
	beforeWheel := state.ScrollY()
	tester.Scroll(x, y+48, 0, 36)
	tester.Frame()
	if state.ScrollY() <= beforeWheel {
		t.Fatal("scrolling over the Minimap did not reach the editor")
	}
	if err := tester.Click("Go to line 1"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if state.ScrollY() != 0 {
		t.Fatalf("a click after a drag retained stale gesture state: scroll=%v", state.ScrollY())
	}
}

func TestMinimapCancelEndsDrag(t *testing.T) {
	state := &State{Buffer: NewBuffer(strings.Repeat("value\n", 200))}
	tester := ui.NewTester(func(c *ui.Context) {
		Minimap(c, state).Width(86).FillHeight()
	}, 160, 400)
	tester.Frame()
	row, _ := tester.Find("Go to line 1")
	x, y := row.X+row.W/2, row.Y+row.H/2
	tester.Press(x, y)
	tester.Move(x, y+36)
	tester.Frame()
	tester.CancelPress()
	tester.Frame()
	if state.minimapDrag.active {
		t.Fatal("canceled Minimap drag remained active")
	}
	if err := tester.Click("Go to line 1"); err != nil {
		t.Fatal(err)
	}
	if state.ScrollY() != 0 {
		t.Fatalf("click after cancellation retained stale drag state: %v", state.ScrollY())
	}
}
