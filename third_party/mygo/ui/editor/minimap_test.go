package editor

import (
	"reflect"
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
