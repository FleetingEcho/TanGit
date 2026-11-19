package ui_test

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The clear control used in every single-line search editor must explain
// itself on hover, not just show a name that may be clipped in a compact pane.
func TestSearchFieldClearTooltip(t *testing.T) {
	query := "needle"
	tt := ui.NewTester(func(c *ui.Context) {
		ui.SearchField(c, &query).Label("search code")
	}, 280, 100)
	button, found := tt.Find("Clear")
	if !found {
		t.Fatal("non-empty search does not offer Clear")
	}
	tt.Move(button.X+button.W/2, button.Y+button.H/2)
	tt.Advance(700 * time.Millisecond)
	if !tt.HasText("Clear") {
		t.Fatalf("hover never showed the clear tooltip: %q", tt.Texts())
	}
}
